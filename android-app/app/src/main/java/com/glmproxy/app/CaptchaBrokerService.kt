package com.glmproxy.app

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.util.Log
import androidx.core.app.NotificationCompat
import androidx.lifecycle.LifecycleService
import androidx.lifecycle.lifecycleScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
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
 * Before this service existed, the user had to keep a browser tab open
 * at `http://127.0.0.1:3005/zcode/captcha/browser?client=standalone-browser`
 * all the time just to keep the bridge armed — even when no captcha was
 * actually being requested.
 *
 * ## What this service does
 *
 * This service runs as part of the main [ProxyService] process (started
 * and stopped alongside it) and:
 *
 * 1. Long-polls `/zcode/captcha/poll?client=standalone-browser` in a
 *    coroutine, keeping the client "online" in the bridge's client map.
 *    The Go side returns 204 No Content every 25s when there's no work,
 *    at which point we immediately poll again.
 * 2. When the bridge returns 200 with a `Request` JSON, the service
 *    launches the system browser at the captcha page URL (which loads
 *    the Aliyun SDK and solves the captcha interactively) AND posts a
 *    high-priority notification telling the user to solve it.
 *
 * The browser tab can be closed once the captcha is solved — the next
 * time a captcha is needed, this service will open a fresh tab
 * automatically.
 *
 * ## Threading model
 *
 * All HTTP calls happen on `Dispatchers.IO` via `lifecycleScope`. The
 * service is a `LifecycleService` so the coroutine scope is tied to the
 * service's lifecycle — when [onDestroy] cancels it, all in-flight polls
 * are cancelled cleanly.
 */
class CaptchaBrokerService : LifecycleService() {

    private var pollJob: Job? = null

    override fun onCreate() {
        super.onCreate()
        Log.i(TAG, "Captcha broker started — long-polling /zcode/captcha/poll")
        startPolling()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        super.onStartCommand(intent, flags, startId)
        return START_STICKY
    }

    override fun onDestroy() {
        pollJob?.cancel()
        Log.i(TAG, "Captcha broker stopped")
        super.onDestroy()
    }

    private fun startPolling() {
        pollJob = lifecycleScope.launch(Dispatchers.IO) {
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
     * Performs one long-poll cycle. Returns the [CaptchaRequest] if the
     * bridge has work for us, or null if it returned 204 (no work).
     *
     * The Go bridge blocks for up to 25 seconds waiting for work, so this
     * call appears to "hang" — that's by design. We set our own read
     * timeout to 35s (25s bridge window + slack) so we don't time out
     * before the Go side does.
     */
    private suspend fun pollOnce(): CaptchaRequest? = withContext(Dispatchers.IO) {
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
                return@withContext null  // No work — bridge will keep us registered
            }
            if (code != 200) {
                throw IOException("HTTP $code from /zcode/captcha/poll")
            }
            val body = BufferedReader(InputStreamReader(conn.inputStream)).use { it.readText() }
            val json = JSONObject(body)
            CaptchaRequest(
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
        // Reuse the captcha notification infrastructure in ProxyService
        // so the user gets a consistent UX: a high-priority notification
        // that, when tapped, opens MainActivity → captcha dialog →
        // "Abrir no navegador" button.
        val logLine = "[broker] captcha request ${request.id} received — opening browser automatically"
        ProxyService.notifyCaptchaFromBroker(this, logLine)

        // Also open the browser automatically — the user doesn't need to
        // tap anything. The captcha page is smart enough to solve
        // traceless captchas without user interaction in many cases;
        // when an interactive challenge is required, the user will see
        // the popup in the browser tab.
        val browserIntent = Intent(Intent.ACTION_VIEW, Uri.parse(captchaUrl())).apply {
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        }
        try {
            startActivity(browserIntent)
            Log.i(TAG, "Opened browser for captcha request ${request.id}")
        } catch (e: Exception) {
            Log.w(TAG, "No browser available to open captcha page — user will need to open manually", e)
        }
    }

    private fun captchaUrl(): String =
        "http://127.0.0.1:${ProxyBinary.port}/zcode/captcha/browser?client=standalone-browser"

    /**
     * Posts a minimal foreground notification so the service isn't killed
     * by the system. We piggyback on the ProxyService's foreground
     * notification instead of creating our own — the broker runs in the
     * same process and the user doesn't need to know there are two
     * logical services.
     *
     * Note: this method is currently a no-op because CaptchaBrokerService
     * is started by ProxyService via startForegroundService(), which
     * means the broker is bound to the same process lifetime. If we
     * later split it into a separate process, this would need to post
     * its own foreground notification.
     */
    @Suppress("unused")
    private fun postForegroundNotification() {
        val notification = NotificationCompat.Builder(this, "glm_proxy_foreground")
            .setContentTitle("GLM Proxy captcha broker")
            .setContentText("Monitorando pedidos de captcha")
            .setSmallIcon(android.R.drawable.stat_sys_download)
            .setOngoing(true)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .build()
        // Not used currently — kept for future process-split.
    }

    companion object {
        private const val TAG = "CaptchaBroker"

        /**
         * Starts the broker service. Called by [ProxyService.onCreate].
         */
        fun start(context: Context) {
            val intent = Intent(context, CaptchaBrokerService::class.java)
            try {
                androidx.core.content.ContextCompat.startForegroundService(context, intent)
            } catch (e: Exception) {
                Log.w(TAG, "Could not start CaptchaBrokerService", e)
            }
        }

        /**
         * Stops the broker service. Called by [ProxyService.onDestroy].
         */
        fun stop(context: Context) {
            context.stopService(Intent(context, CaptchaBrokerService::class.java))
        }
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
