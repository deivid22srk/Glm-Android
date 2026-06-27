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
     * Posts a captcha notification when a captcha-request log line arrives.
     */
    private val logListener = { line: String ->
        if (ProxyBinary.isCaptchaRequest(line)) {
            // Debounce: don't post a new notification if one is already
            // pending (user hasn't dismissed it yet). The flag is cleared
            // when the user taps the notification (which opens MainActivity
            // and shows the dialog) or when the service is stopped.
            if (captchaPendingStatic.compareAndSet(false, true)) {
                postCaptchaNotification(line)
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
        return NotificationCompat.Builder(this, CHANNEL_ID_FOREGROUND)
            .setContentTitle(getString(R.string.notification_title))
            .setContentText(text)
            .setSmallIcon(android.R.drawable.stat_sys_download)
            .setOngoing(true)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .build()
    }

    /**
     * Posts a high-priority notification informing the user that a captcha
     * needs to be solved. Tapping the notification opens MainActivity with
     * the [ACTION_SHOW_CAPTCHA] action, which triggers the captcha dialog.
     *
     * If the user swipes the notification away instead of tapping it, the
     * [deleteIntent] fires [ACTION_CAPTCHA_DISMISSED] back to this service,
     * which clears the [captchaPendingStatic] debounce flag so future
     * captcha requests can post a fresh notification. Without this, one
     * swipe-dismiss would permanently suppress all subsequent captcha
     * alerts (the bug fixed in plan 010).
     */
    private fun postCaptchaNotification(logLine: String) {
        val contentIntent = Intent(this, MainActivity::class.java).apply {
            action = ACTION_SHOW_CAPTCHA
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)
            putExtra(EXTRA_CAPTCHA_LOG, logLine)
            putExtra(EXTRA_CAPTCHA_URL, captchaUrl())
        }
        val contentPendingIntent = PendingIntent.getActivity(
            this,
            REQUEST_CODE_CAPTCHA,
            contentIntent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )

        // Delete intent: fires when the user swipes the notification away
        // (or the system auto-dismisses it). Distinct from contentIntent
        // which fires on tap. Routes back to this service with the
        // ACTION_CAPTCHA_DISMISSED action so onStartCommand can clear the
        // debounce flag.
        val deleteIntent = Intent(this, ProxyService::class.java).apply {
            action = ACTION_CAPTCHA_DISMISSED
        }
        val deletePendingIntent = PendingIntent.getService(
            this,
            REQUEST_CODE_CAPTCHA_DELETE,
            deleteIntent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )

        val notification = NotificationCompat.Builder(this, CHANNEL_ID_CAPTCHA)
            .setContentTitle(getString(R.string.captcha_notification_title))
            .setContentText(getString(R.string.captcha_notification_text))
            .setStyle(NotificationCompat.BigTextStyle()
                .bigText(getString(R.string.captcha_notification_big, logLine)))
            .setSmallIcon(android.R.drawable.stat_notify_error)
            .setPriority(NotificationCompat.PRIORITY_HIGH)
            .setCategory(NotificationCompat.CATEGORY_ERROR)
            .setContentIntent(contentPendingIntent)
            .setDeleteIntent(deletePendingIntent)
            .setAutoCancel(true)
            .build()

        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        nm.notify(NOTIF_ID_CAPTCHA, notification)
        Log.i(TAG, "Posted captcha notification")
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
        private const val REQUEST_CODE_CAPTCHA = 1001
        private const val REQUEST_CODE_CAPTCHA_DELETE = 1002

        /**
         * Notification tap action — opens MainActivity with extras
         * [EXTRA_CAPTCHA_LOG] and [EXTRA_CAPTCHA_URL]. MainActivity shows
         * the captcha dialog in response.
         */
        const val ACTION_SHOW_CAPTCHA = "com.glmproxy.app.action.SHOW_CAPTCHA"

        /**
         * Notification swipe-dismiss action — fires the deleteIntent back
         * to this service so we can clear the [captchaPendingStatic]
         * debounce flag and let future captcha requests post a fresh
         * notification. Without this, one swipe would permanently suppress
         * all subsequent captcha alerts.
         */
        const val ACTION_CAPTCHA_DISMISSED = "com.glmproxy.app.action.CAPTCHA_DISMISSED"

        const val EXTRA_CAPTCHA_LOG = "captcha_log"
        const val EXTRA_CAPTCHA_URL = "captcha_url"

        private val captchaPendingStatic = java.util.concurrent.atomic.AtomicBoolean(false)

        /**
         * Called by [MainActivity] when the user dismisses the captcha
         * dialog (either by solving it or by closing it). Clears the
         * pending flag so future captcha requests can post a fresh
         * notification, and cancels any active captcha notification.
         */
        fun clearCaptchaPending(context: Context) {
            captchaPendingStatic.set(false)
            val nm = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
            nm.cancel(NOTIF_ID_CAPTCHA)
        }
    }
}
