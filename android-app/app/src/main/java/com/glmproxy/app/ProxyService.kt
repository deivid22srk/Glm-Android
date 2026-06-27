package com.glmproxy.app

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Context
import android.content.Intent
import android.os.Build
import android.os.IBinder
import android.util.Log
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
 */
class ProxyService : Service() {
    private val starting = AtomicBoolean(false)

    override fun onCreate() {
        super.onCreate()
        createNotificationChannel()
        startForeground(NOTIF_ID, buildNotification())
        if (starting.compareAndSet(false, true)) {
            try {
                ProxyBinary.start(this)
            } catch (e: Exception) {
                Log.e(TAG, "Failed to start Go proxy", e)
                stopSelf()
            }
        }
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
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
        ProxyBinary.stop()
        super.onDestroy()
    }

    private fun createNotificationChannel() {
        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val channel = NotificationChannel(
                CHANNEL_ID,
                getString(R.string.notification_channel_name),
                NotificationManager.IMPORTANCE_LOW
            ).apply {
                description = getString(R.string.notification_channel_description)
                setShowBadge(false)
            }
            nm.createNotificationChannel(channel)
        }
    }

    private fun buildNotification(): Notification {
        val text = getString(R.string.notification_text, ProxyBinary.port)
        return androidx.core.app.NotificationCompat.Builder(this, CHANNEL_ID)
            .setContentTitle(getString(R.string.notification_title))
            .setContentText(text)
            .setSmallIcon(android.R.drawable.stat_sys_download)
            .setOngoing(true)
            .setPriority(androidx.core.app.NotificationCompat.PRIORITY_LOW)
            .build()
    }

    companion object {
        private const val TAG = "ProxyService"
        private const val CHANNEL_ID = "glm_proxy_foreground"
        private const val NOTIF_ID = 1
    }
}
