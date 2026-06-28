package com.glmproxy.app

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.os.Build
import android.os.IBinder
import android.util.Log
import androidx.core.app.NotificationCompat
import java.util.concurrent.atomic.AtomicBoolean

/**
 * Foreground service that keeps the embedded Go proxy alive even when the
 * user navigates away from the activity. Without this, Android may kill the
 * child process within seconds of the activity being backgrounded, which
 * would break any external OpenAI-compatible client currently streaming a
 * chat completion through 127.0.0.1:3005.
 *
 * The service owns the lifecycle of the [ProxyBinary] process: it starts the
 * binary in onCreate and stops it in onDestroy.
 *
 * In addition, the service subscribes to [ProxyBinary] log events and posts
 * a high-priority notification whenever the Go proxy reports that Z.ai is
 * requesting a captcha (event `captcha.browser_missing`). Tapping that
 * notification opens MainActivity which shows a dialog with the captcha URL
 * and a button to open it in the system browser.
 */
class ProxyService : Service() {
    private val starting = AtomicBoolean(false)

    /**
     * Listener registered with [ProxyBinary] while the service is running.
     * When a captcha request is detected in the Go proxy's log, opens
     * WebViewActivity automatically with the captcha page URL — no
     * notification needed, the activity coming to foreground IS the alert.
     */
    private val logListener = { line: String ->
        if (ProxyBinary.isCaptchaRequest(line)) {
            // Debounce: don't open a new WebViewActivity if one is
            // already showing (user hasn't closed it yet).
            if (captchaPendingStatic.compareAndSet(false, true)) {
                openCaptchaWebView(line)
            }
        }
    }

    override fun onCreate() {
        super.onCreate()
        createNotificationChannel()
        startForeground(NOTIF_ID, buildNotification())
        if (starting.compareAndSet(false, true)) {
            try {
                ProxyBinary.start(this)
                ProxyBinary.addLogListener(logListener)
            } catch (e: Exception) {
                Log.e(TAG, "Failed to start Go proxy", e)
                stopSelf()
            }
        }
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        // Handle the captcha-dismiss signal: the user swiped the captcha
        // notification away (or the system auto-dismissed it). Clear the
        // pending flag so future captcha log lines can post a fresh
        // notification — without this, one swipe would permanently
        // suppress all subsequent captcha alerts.
        if (intent?.action == ACTION_CAPTCHA_DISMISSED) {
            captchaPendingStatic.set(false)
            Log.i(TAG, "Captcha notification dismissed by user — re-arming alert")
        }
        return START_STICKY
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onTaskRemoved(rootIntent: Intent?) {
        // When the user swipes the app away from recents, Android may kill
        // the process. To keep the proxy alive for external clients that
        // depend on 127.0.0.1:3005, we restart the service as a foreground
        // service if it was running.
        // Note: the proxy is NOT stopped here — only when the user explicitly
        // taps "Parar servidor" in the activity (which calls stopService).
        super.onTaskRemoved(rootIntent)
    }

    override fun onDestroy() {
        ProxyBinary.removeLogListener(logListener)
        ProxyBinary.stop()
        captchaPendingStatic.set(false)
        super.onDestroy()
    }

    /**
     * Opens WebViewActivity with the captcha broker page. Called when
     * the Go proxy logs a captcha request. The WebView polls
     * /zcode/captcha/poll?client=standalone-browser and receives the
     * next captcha request from the Go bridge (the Go proxy retries
     * the chat completion, which triggers a new captcha request that
     * the WebView picks up).
     */
    private fun openCaptchaWebView(logLine: String) {
        Log.i(TAG, "Captcha needed — opening WebViewActivity: $logLine")
        val url = "http://127.0.0.1:${ProxyBinary.port}/zcode/captcha/browser?client=standalone-browser"
        val intent = Intent(this, WebViewActivity::class.java).apply {
            putExtra(WebViewActivity.EXTRA_URL, url)
            putExtra(WebViewActivity.EXTRA_TITLE, "Captcha")
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        }
        try {
            startActivity(intent)
        } catch (e: Exception) {
            Log.w(TAG, "Could not open WebViewActivity for captcha", e)
        }
    }

    private fun createNotificationChannel() {
        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            // Foreground service channel (low importance, no sound).
            val fgChannel = NotificationChannel(
                CHANNEL_ID_FOREGROUND,
                getString(R.string.notification_channel_name),
                NotificationManager.IMPORTANCE_LOW
            ).apply {
                description = getString(R.string.notification_channel_description)
                setShowBadge(false)
            }
            // Captcha channel (high importance, sound + popup) so the user
            // actually notices that intervention is required.
            val captchaChannel = NotificationChannel(
                CHANNEL_ID_CAPTCHA,
                getString(R.string.notification_channel_captcha_name),
                NotificationManager.IMPORTANCE_HIGH
            ).apply {
                description = getString(R.string.notification_channel_captcha_description)
                setShowBadge(true)
                enableVibration(true)
            }
            nm.createNotificationChannels(listOf(fgChannel, captchaChannel))
        }
    }

    private fun buildNotification(): Notification {
        val text = getString(R.string.notification_text, ProxyBinary.port)
        // Tapping the foreground notification opens MainActivity — the same
        // behavior users expect from any persistent notification (compare
        // with media players, VPN apps, etc.). Without this, tapping did
        // nothing (finding C-12). Uses FLAG_IMMUTABLE per Android 12+
        // requirement; FLAG_UPDATE_CURRENT so the intent is reused across
        // notification updates (the foreground notification is reposted
        // every time the service starts).
        val contentIntent = Intent(this, MainActivity::class.java).apply {
            // FLAG_ACTIVITY_NEW_TASK: required because we're starting an
            //   Activity from outside an Activity context (the Service).
            // FLAG_ACTIVITY_CLEAR_TOP: if MainActivity is already in the
            //   back stack, bring it to the front instead of stacking a
            //   new instance on top (matches launchMode=singleTop which
            //   is set in the manifest).
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)
        }
        val contentPendingIntent = PendingIntent.getActivity(
            this,
            REQUEST_CODE_FOREGROUND,
            contentIntent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )
        return NotificationCompat.Builder(this, CHANNEL_ID_FOREGROUND)
            .setContentTitle(getString(R.string.notification_title))
            .setContentText(text)
            .setSmallIcon(android.R.drawable.stat_sys_download)
            .setContentIntent(contentPendingIntent)
            .setOngoing(true)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .build()
    }

    /**
     * Returns the captcha URL the user should open in a browser. The Go
     * proxy serves an interactive captcha page at this path.
     */
    private fun captchaUrl(): String {
        return "http://127.0.0.1:${ProxyBinary.port}/zcode/captcha/browser?client=standalone-browser"
    }

    companion object {
        private const val TAG = "ProxyService"
        private const val CHANNEL_ID_FOREGROUND = "glm_proxy_foreground"
        private const val CHANNEL_ID_CAPTCHA = "glm_proxy_captcha"
        private const val NOTIF_ID = 1
        private const val NOTIF_ID_CAPTCHA = 2
        private const val REQUEST_CODE_FOREGROUND = 1000
        private const val REQUEST_CODE_CAPTCHA = 1001
        private const val REQUEST_CODE_CAPTCHA_DELETE = 1002

        const val ACTION_SHOW_CAPTCHA = "com.glmproxy.app.action.SHOW_CAPTCHA"
        const val ACTION_CAPTCHA_DISMISSED = "com.glmproxy.app.action.CAPTCHA_DISMISSED"
        const val EXTRA_CAPTCHA_LOG = "captcha_log"
        const val EXTRA_CAPTCHA_URL = "captcha_url"

        private val captchaPendingStatic = java.util.concurrent.atomic.AtomicBoolean(false)

        /**
         * Called by [MainActivity] or [WebViewActivity] when the user
         * closes the captcha WebView. Clears the pending flag so future
         * captcha requests can open a fresh WebViewActivity.
         */
        fun clearCaptchaPending(context: Context) {
            captchaPendingStatic.set(false)
        }
    }
}
