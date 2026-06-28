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
import java.net.HttpURLConnection
import java.net.URL

/**
 * Background captcha broker that keeps the Go proxy's captcha bridge
 * "armed" by continuously long-polling `GET /zcode/captcha/poll?client=standalone-browser`.
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
 * that needs a captcha fails immediately with `ErrBrowserUnavailable`
 * and the proxy enters a retry loop that the user sees as
 * "A Z.ai pediu captcha, mas nenhum navegador captcha esta disponivel".
 *
 * Before this broker existed, the user had to keep a browser tab open
 * at the captcha page all the time just to keep the bridge armed.
 *
 * ## Design
 *
 * This is a plain class, NOT a Service. It runs as a coroutine inside
 * [ProxyService]'s process — the proxy service is already a foreground
 * service with its own persistent notification, so the broker doesn't
 * need its own service lifecycle or notification. This avoids:
 *
 * 1. A second persistent notification (bad UX — the user would see
 *    "GLM Proxy ativo" AND "GLM Proxy captcha broker" simultaneously).
 * 2. `ForegroundServiceDidNotStartInTimeException` — which crashed the
 *    app when the broker was a separate foreground service that didn't
 *    call `startForeground()` within the 5-second window.
 *
 * The broker is created by [ProxyService.onCreate] and destroyed by
 * [ProxyService.onDestroy]. The coroutine scope is owned by ProxyService
 * and passed in at construction time.
 *
 * ## Threading model
 *
 * All HTTP calls happen on `Dispatchers.IO` via the scope passed in.
 * When the scope is cancelled (in ProxyService.onDestroy), all in-flight
 * polls are cancelled cleanly.
 */
class CaptchaBroker(
    private val context: Context,
    private val scope: CoroutineScope
) {
    private var pollJob: Job? = null

    /**
     * Starts the long-poll loop. Idempotent — calling twice is a no-op.
     */
    fun start() {
        if (pollJob?.isActive == true) return
        Log.i(TAG, "Captcha broker started — long-polling /zcode/captcha/poll")
        pollJob = scope.launch(Dispatchers.IO) {
            // Initial small delay so the proxy has time to bind its
            // listener before we start hitting it.
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
                // Loop immediately — the Go side blocks up to 25s on its
                // end, so this is naturally paced.
            }
        }
    }

    /**
     * Stops the long-poll loop. Cancels any in-flight HTTP request.
     */
    fun stop() {
        pollJob?.cancel()
        pollJob = null
        Log.i(TAG, "Captcha broker stopped")
    }

    /**
     * Performs one long-poll cycle. Returns the [CaptchaRequest] if the
     * bridge has work for us, or null if it returned 204 (no work).
     *
     * The Go bridge blocks for up to 25 seconds waiting for work, so this
     * call appears to "hang" — that's by design. We set our own read
     * timeout to 35s (25s bridge window + slack) so we don't time out
     * before the Go side does.
     */
    private suspend fun pollOnce(): CaptchaRequest? {
        val url = URL("http://127.0.0.1:${ProxyBinary.port}/zcode/captcha/poll?client=standalone-browser")
        val conn = (url.openConnection() as HttpURLConnection).apply {
            connectTimeout = 5000
            readTimeout = 35000   // 25s bridge window + 10s slack
            requestMethod = "GET"
            useCaches = false
            setRequestProperty("Cache-Control", "no-store")
        }
        try {
            conn.connect()
            val code = conn.responseCode
            if (code == 204) {
                return null  // No work — bridge will keep us registered
            }
            if (code != 200) {
                throw IOException("HTTP $code from /zcode/captcha/poll")
            }
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
     * Called when the bridge pushed a captcha request to us. Opens the
     * system browser at the captcha page URL (which loads the Aliyun SDK
     * and solves the captcha interactively) and posts a high-priority
     * notification.
     *
     * We do NOT submit the captcha token ourselves — the captcha page at
     * `/zcode/captcha/browser` does that via JavaScript. We just need to
     * open the page; the user solves the captcha; the page POSTs the
     * token to `/zcode/captcha/submit`; the bridge unblocks the waiting
     * chat completion.
     */
    private fun onCaptchaRequest(request: CaptchaRequest) {
        val logLine = "[broker] captcha request ${request.id} received — opening browser automatically"
        ProxyService.notifyCaptchaFromBroker(context, logLine)

        // Also open the browser automatically — the user doesn't need to
        // tap anything. The captcha page is smart enough to solve
        // traceless captchas without user interaction in many cases;
        // when an interactive challenge is required, the user will see
        // the popup in the browser tab.
        val browserIntent = Intent(Intent.ACTION_VIEW, Uri.parse(captchaUrl())).apply {
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        }
        try {
            context.startActivity(browserIntent)
            Log.i(TAG, "Opened browser for captcha request ${request.id}")
        } catch (e: Exception) {
            Log.w(TAG, "No browser available to open captcha page — user will need to open manually", e)
        }
    }

    private fun captchaUrl(): String =
        "http://127.0.0.1:${ProxyBinary.port}/zcode/captcha/browser?client=standalone-browser"

    companion object {
        private const val TAG = "CaptchaBroker"
    }
}

/**
 * Mirror of the Go-side `captcha.Request` struct. Only the fields we
 * actually use are decoded; the rest are optional.
 */
data class CaptchaRequest(
    val id: String,
    val source: String,
    val timeoutMs: Long,
    val createdAt: Long
)
