package com.glmproxy.app

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.util.Log
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import org.json.JSONObject
import java.io.BufferedReader
import java.io.IOException
import java.io.InputStreamReader
import java.io.OutputStreamWriter
import java.net.HttpURLConnection
import java.net.URL

/**
 * Background captcha broker that keeps the Go proxy's captcha bridge
 * "armed" by continuously long-polling `GET /zcode/captcha/poll?client=android-broker`.
 *
 * ## Why this exists
 *
 * The Go proxy's captcha bridge (`internal/captcha/bridge.go`) works like this:
 * 1. When the upstream Z.ai API demands a captcha during a chat completion,
 *    the proxy creates a `Request` and pushes it into a Go channel
 *    belonging to the *client* that's currently long-polling for work.
 * 2. The client (normally a browser tab loaded with the captcha page at
 *    `/zcode/captcha/browser`) receives the request, solves the captcha
 *    via the Aliyun SDK in JavaScript, and POSTs the token back to
 *    `/zcode/captcha/submit`.
 * 3. The proxy's chat-completion stream then resumes with the captcha
 *    token attached.
 *
 * The bridge's `chooseClient()` only returns clients that have polled
 * in the last 45 seconds. If no client is polling, every chat completion
 * that needs a captcha fails immediately with `ErrBrowserUnavailable`.
 *
 * ## Why the broker uses a DIFFERENT client name from the browser
 *
 * The broker polls with `client=android-broker`. The browser polls with
 * `client=standalone-browser` (the default). The bridge's config sets
 * `ZCODE_CAPTCHA_CLIENT_PREFERENCE=standalone-browser`, so:
 *
 * - When the browser IS open: `chooseClient()` returns `standalone-browser`
 *   (preferred). The browser gets the request directly. The broker never
 *   sees it. This is correct — the browser can solve captchas, the broker
 *   can't.
 * - When the browser is NOT open: `chooseClient()` falls back to the most
 *   recent poller, which is the broker (`android-broker`). The broker
 *   receives the request, opens the browser, then **requeues** the request
 *   into the `standalone-browser` channel so the browser (now polling)
 *   can receive it and solve the captcha.
 *
 * ## Requeue flow (the critical part)
 *
 * When the broker receives a Request:
 * 1. Opens the system browser at `/zcode/captcha/browser?client=standalone-browser`
 * 2. Waits 2 seconds for the browser page to load and start its own poll loop
 * 3. POSTs the original Request to `/zcode/captcha/requeue?client=standalone-browser`
 * 4. The Go bridge pushes the Request into the `standalone-browser` client's channel
 * 5. The browser's next poll receives the Request
 * 6. The browser solves the captcha via Aliyun SDK and POSTs the token to
 *    `/zcode/captcha/submit` with the original Request ID
 * 7. The original waiter (blocked in `FreshChallenge`) receives the token
 * 8. The chat-completion stream resumes
 *
 * Without the requeue, the browser would sit idle showing
 * "Broker pronto. Aguardando request..." forever because the broker
 * already consumed the Request from the channel.
 */
class CaptchaBroker(
    private val context: Context,
    private val scope: CoroutineScope
) {
    private var pollJob: Job? = null

    fun start() {
        if (pollJob?.isActive == true) return
        Log.i(TAG, "Captcha broker started — long-polling /zcode/captcha/poll as client=$BROKER_CLIENT_ID")
        pollJob = scope.launch(Dispatchers.IO) {
            delay(1000)
            while (isActive) {
                val request = try {
                    pollOnce()
                } catch (e: Exception) {
                    Log.w(TAG, "Poll failed (will retry in 5s): ${e.message}")
                    delay(5000)
                    continue
                }
                if (request != null) {
                    Log.i(TAG, "Captcha request received: id=${request.id}")
                    onCaptchaRequest(request)
                }
            }
        }
    }

    fun stop() {
        pollJob?.cancel()
        pollJob = null
        Log.i(TAG, "Captcha broker stopped")
    }

    private suspend fun pollOnce(): CaptchaRequest? {
        val url = URL("http://127.0.0.1:${ProxyBinary.port}/zcode/captcha/poll?client=$BROKER_CLIENT_ID")
        val conn = (url.openConnection() as HttpURLConnection).apply {
            connectTimeout = 5000
            readTimeout = 35000
            requestMethod = "GET"
            useCaches = false
            setRequestProperty("Cache-Control", "no-store")
        }
        try {
            conn.connect()
            val code = conn.responseCode
            if (code == 204) return null
            if (code != 200) throw IOException("HTTP $code from /zcode/captcha/poll")
            val body = BufferedReader(InputStreamReader(conn.inputStream)).use { it.readText() }
            val json = JSONObject(body)
            return CaptchaRequest(
                id = json.getString("id"),
                source = json.optString("source", "openai_proxy"),
                timeoutMs = json.optLong("timeoutMs", 120000),
                createdAt = json.optLong("createdAt", 0)
            )
        } finally {
            conn.disconnect()
        }
    }

    /**
     * Called when the bridge pushed a captcha request to us (the broker).
     * Opens the browser, waits for it to start polling, then requeues the
     * request so the browser can receive it and solve the captcha.
     */
    private fun onCaptchaRequest(request: CaptchaRequest) {
        val logLine = "[broker] captcha request ${request.id} received — opening browser and requeuing"
        ProxyService.notifyCaptchaFromBroker(context, logLine)

        // Open the browser at the captcha page.
        val browserIntent = Intent(Intent.ACTION_VIEW, Uri.parse(captchaUrl())).apply {
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        }
        try {
            context.startActivity(browserIntent)
            Log.i(TAG, "Opened browser for captcha request ${request.id}")
        } catch (e: Exception) {
            Log.w(TAG, "No browser available — user will need to open manually", e)
        }

        // Requeue the request into the browser's channel. We wait a bit
        // for the browser page to load and start its own poll loop before
        // requeuing — otherwise the requeued request would land in the
        // channel before the browser is ready to read it, and the browser's
        // first poll would immediately get it (which is actually fine, but
        // the 2s delay also gives the browser time to load the Aliyun SDK).
        scope.launch(Dispatchers.IO) {
            delay(2000)
            requeueRequest(request)
        }
    }

    /**
     * POSTs the Request to /zcode/captcha/requeue?client=standalone-browser
     * so the browser (which is now polling with that client name) can
     * receive it on its next poll cycle.
     */
    private fun requeueRequest(request: CaptchaRequest) {
        val url = URL("http://127.0.0.1:${ProxyBinary.port}/zcode/captcha/requeue?client=$BROWSER_CLIENT_ID")
        val conn = (url.openConnection() as HttpURLConnection).apply {
            connectTimeout = 5000
            readTimeout = 10000
            requestMethod = "POST"
            setRequestProperty("Content-Type", "application/json")
            doOutput = true
        }
        try {
            val body = JSONObject().apply {
                put("id", request.id)
                put("source", request.source)
                put("timeoutMs", request.timeoutMs)
                put("createdAt", request.createdAt)
            }
            OutputStreamWriter(conn.outputStream).use { it.write(body.toString()) }
            conn.connect()
            val code = conn.responseCode
            val response = BufferedReader(InputStreamReader(
                if (code in 200..299) conn.inputStream else conn.errorStream
            )).use { it.readText() }
            if (code == 200) {
                Log.i(TAG, "Requeued captcha request ${request.id} to $BROWSER_CLIENT_ID: $response")
            } else {
                Log.w(TAG, "Requeue failed for ${request.id}: HTTP $code — $response")
            }
        } catch (e: Exception) {
            Log.w(TAG, "Requeue failed for ${request.id}: ${e.message}", e)
        } finally {
            conn.disconnect()
        }
    }

    private fun captchaUrl(): String =
        "http://127.0.0.1:${ProxyBinary.port}/zcode/captcha/browser?client=$BROWSER_CLIENT_ID"

    companion object {
        private const val TAG = "CaptchaBroker"

        /** Client name the broker uses when polling. MUST be different
         *  from [BROWSER_CLIENT_ID] so they don't compete for the same
         *  Go channel. */
        private const val BROKER_CLIENT_ID = "android-broker"

        /** Client name the browser page uses (matches the Go bridge's
         *  preferred client configured via ZCODE_CAPTCHA_CLIENT_PREFERENCE). */
        private const val BROWSER_CLIENT_ID = "standalone-browser"
    }
}

data class CaptchaRequest(
    val id: String,
    val source: String,
    val timeoutMs: Long,
    val createdAt: Long
)
