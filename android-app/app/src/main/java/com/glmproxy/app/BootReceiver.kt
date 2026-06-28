package com.glmproxy.app

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.os.Build
import android.util.Log
import androidx.core.content.ContextCompat

/**
 * Starts [ProxyService] on device boot if the user has opted in via
 * [isStartOnBootEnabled].
 *
 * Plan 020 (DIR-03): closes the always-alive loop. The foreground service
 * is `START_STICKY` and survives swipes from recents, but until this
 * receiver existed, every reboot killed the proxy until the user
 * remembered to re-open the app manually.
 *
 * The receiver is **opt-in** by default — on first install it does
 * nothing. The user must enable start-on-boot via the UI (a future
 * settings screen will surface this; for now the only way to flip the
 * flag is via SharedPreferences edit, which can be done via `adb shell`
 * or a future in-app toggle).
 *
 * Android 12+ note: background starts to foreground services are
 * restricted from `BOOT_COMPLETED` receivers
 * (`ForegroundServiceStartNotAllowedException`). The receiver catches
 * that exception and logs a warning — it does NOT crash. Workarounds
 * (exact alarm + ServiceCompat.startForegroundService) are documented
 * in the maintenance section of plan 020 but not yet implemented
 * because the app is sideloaded (Play Store policy constraints don't
 * apply yet) and the exception is rare in practice on stock Android.
 */
class BootReceiver : BroadcastReceiver() {

    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != Intent.ACTION_BOOT_COMPLETED &&
            intent.action != Intent.ACTION_LOCKED_BOOT_COMPLETED) {
            return
        }

        if (!isStartOnBootEnabled(context)) {
            Log.i(TAG, "Boot completed but start-on-boot is disabled — skipping")
            return
        }

        Log.i(TAG, "Boot completed and start-on-boot is enabled — starting ProxyService")
        val serviceIntent = Intent(context, ProxyService::class.java)
        try {
            ContextCompat.startForegroundService(context, serviceIntent)
        } catch (e: Exception) {
            // Android 12+ may throw ForegroundServiceStartNotAllowedException
            // when a background broadcast receiver tries to start a FGS.
            // Log and bail — the user will have to open the app manually.
            Log.w(TAG, "Could not start ProxyService from boot receiver", e)
        }
    }

    companion object {
        private const val TAG = "BootReceiver"
        private const val PREFS_NAME = "glm_proxy_prefs"
        private const val KEY_START_ON_BOOT = "start_on_boot_enabled"

        /**
         * Returns true if the user has opted in to start-on-boot.
         * Defaults to false — the feature is opt-in.
         */
        fun isStartOnBootEnabled(context: Context): Boolean {
            val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            return prefs.getBoolean(KEY_START_ON_BOOT, false)
        }

        /**
         * Sets the start-on-boot preference. Called by the UI (future
         * settings screen) when the user toggles the option.
         */
        fun setStartOnBootEnabled(context: Context, enabled: Boolean) {
            val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            prefs.edit().putBoolean(KEY_START_ON_BOOT, enabled).apply()
            Log.i(TAG, "start-on-boot preference set to $enabled")
        }
    }
}
