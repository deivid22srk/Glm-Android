package com.glmproxy.app

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.util.Log
import android.view.View
import android.widget.Toast
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat
import androidx.core.view.ViewCompat
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.updatePadding
import com.google.android.material.color.DynamicColors
import com.glmproxy.app.databinding.ActivityMainBinding
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL

/**
 * Hosts the native Material 3 control surface for the GLM proxy.
 *
 * The activity is intentionally lightweight — it observes the state of
 * [ProxyBinary] (a singleton) and renders cards based on a small state
 * machine:
 *
 *   STOPPED → STARTING → RUNNING → STOPPING → STOPPED
 *                  ↓                       ↓
 *                FAILED ← (any failure) ← ─┘
 *
 * The actual Go process lives in [ProxyService], so the activity can be
 * destroyed and recreated (rotation, theme change) without affecting the
 * proxy. When the user taps "Abrir no navegador", the system browser
 * loads the React panel served by the Go binary at 127.0.0.1:PORT.
 *
 * Material 3 specifics:
 *  - Edge-to-edge layout (`WindowCompat.setDecorFitsSystemWindows(false)`)
 *    with manual inset application on the toolbar (top) and the scroll
 *    view (bottom) so content draws behind the status / navigation bars
 *    without being obscured by them.
 *  - Dynamic Color (Material You) is applied on Android 12+, deriving
 *    the color palette from the user's wallpaper.
 */
enum class ProxyState {
    STOPPED,
    STARTING,
    RUNNING,
    STOPPING,
    FAILED
}

class MainActivity : AppCompatActivity() {
    private lateinit var binding: ActivityMainBinding
    private val handler = Handler(Looper.getMainLooper())
    private var state: ProxyState = ProxyState.STOPPED
    private var lastError: String? = null

    /**
     * Periodic state refresher. Polls /health once per second while the
     * activity is in the foreground and the proxy is supposed to be
     * running (STARTING or RUNNING). Updates the UI in place.
     */
    private val statePoller = object : Runnable {
        override fun run() {
            if (state == ProxyState.STARTING || state == ProxyState.RUNNING) {
                checkHealthAsync()
            }
            handler.postDelayed(this, POLL_INTERVAL_MS)
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        // Enable edge-to-edge so the app draws behind the status / nav bars.
        // The toolbar and bottom of the scroll view get manual padding via
        // WindowInsetsCompat listeners below so content is not obscured.
        WindowCompat.setDecorFitsSystemWindows(window, false)

        // Apply Material You dynamic color on Android 12+ (no-op on older
        // versions — falls back to the static theme palette).
        DynamicColors.applyToActivityIfAvailable(this)

        binding = ActivityMainBinding.inflate(layoutInflater)
        setContentView(binding.root)
        setSupportActionBar(binding.toolbar)

        // Toolbar: apply top inset = status bar height so the title and
        // subtitle are not obscured by the status bar.
        ViewCompat.setOnApplyWindowInsetsListener(binding.toolbar) { v, insets ->
            val bars = insets.getInsets(WindowInsetsCompat.Type.systemBars())
            v.updatePadding(top = bars.top)
            insets
        }

        // Scroll view: apply bottom inset = navigation bar height so the
        // last card is not obscured by the gesture nav bar.
        ViewCompat.setOnApplyWindowInsetsListener(binding.scrollView) { v, insets ->
            val bars = insets.getInsets(WindowInsetsCompat.Type.systemBars())
            v.updatePadding(bottom = bars.bottom)
            insets
        }

        // Make sure the toolbar icons are visible against the dark background.
        // (Already set in theme via android:windowLightStatusBar=false, but
        // repeat here in case dynamic color overrides the theme.)
        WindowCompat.getInsetsController(window, window.decorView)
            .isAppearanceLightStatusBars = false

        binding.btnToggle.setOnClickListener { onToggleClicked() }
        binding.btnCopyUrl.setOnClickListener { copyUrlToClipboard() }
        binding.btnOpenBrowser.setOnClickListener { openInBrowser() }
        binding.btnCopyLogs.setOnClickListener { copyLogsToClipboard() }

        // Assess current state on launch — the proxy may already be running
        // (e.g. service started from a previous launch that the user
        // navigated away from without stopping).
        if (ProxyBinary.isRunning()) {
            setState(ProxyState.STARTING)
        } else {
            setState(ProxyState.STOPPED)
        }
    }

    override fun onResume() {
        super.onResume()
        handler.post(statePoller)
    }

    override fun onPause() {
        super.onPause()
        handler.removeCallbacks(statePoller)
    }

    override fun onCreateOptionsMenu(menu: android.view.Menu): Boolean {
        // No menu items — all controls are inline cards. Returning true so
        // the toolbar shows; we can add items later if needed.
        return true
    }

    // --- State transitions -------------------------------------------------

    private fun onToggleClicked() {
        when (state) {
            ProxyState.STOPPED, ProxyState.FAILED -> startServer()
            ProxyState.STARTING, ProxyState.RUNNING, ProxyState.STOPPING -> stopServer()
        }
    }

    private fun startServer() {
        lastError = null
        setState(ProxyState.STARTING)
        // ProxyService.onCreate() will call ProxyBinary.start() and post
        // the foreground notification.
        val intent = Intent(this, ProxyService::class.java)
        ContextCompat.startForegroundService(this, intent)
        // Kick off an immediate health check rather than waiting for the
        // next poller tick.
        checkHealthAsync()
    }

    private fun stopServer() {
        setState(ProxyState.STOPPING)
        // ProxyBinary.stop() blocks for up to 12s waiting for graceful
        // shutdown — must not run on the UI thread.
        Thread({
            try {
                stopService(Intent(this, ProxyService::class.java))
                ProxyBinary.stop()
            } catch (e: Exception) {
                Log.w(TAG, "Error stopping proxy", e)
            }
            handler.post { setState(ProxyState.STOPPED) }
        }, "proxy-stop").start()
    }

    private fun checkHealthAsync() {
        Thread({
            val code = try {
                val url = URL("http://127.0.0.1:${ProxyBinary.port}/health")
                val conn = (url.openConnection() as HttpURLConnection).apply {
                    connectTimeout = 800
                    readTimeout = 800
                    requestMethod = "GET"
                }
                conn.connect()
                val c = conn.responseCode
                conn.disconnect()
                c
            } catch (_: IOException) {
                -1
            }
            handler.post { onHealthResult(code) }
        }, "proxy-health").start()
    }

    private fun onHealthResult(code: Int) {
        if (state == ProxyState.STOPPING || state == ProxyState.STOPPED) {
            // User already initiated shutdown — don't transition based on
            // stale health results.
            return
        }
        if (code == 200) {
            setState(ProxyState.RUNNING)
        } else if (ProxyBinary.isRunning()) {
            // Process is alive but health check failed — still starting.
            setState(ProxyState.STARTING)
        } else {
            // Process died unexpectedly.
            val logs = ProxyBinary.recentLogs().takeLast(10).joinToString("\n")
            setState(ProxyState.FAILED, "Processo morreu.\nÚltimos logs:\n$logs")
        }
    }

    private fun setState(newState: ProxyState, error: String? = null) {
        state = newState
        if (error != null) lastError = error
        renderState()
    }

    // --- Rendering ---------------------------------------------------------

    private fun renderState() {
        when (state) {
            ProxyState.STOPPED -> renderStopped()
            ProxyState.STARTING -> renderStarting()
            ProxyState.RUNNING -> renderRunning()
            ProxyState.STOPPING -> renderStopping()
            ProxyState.FAILED -> renderFailed()
        }
        renderLogs()
    }

    private fun renderStopped() {
        binding.statusText.text = getString(R.string.proxy_stopped)
        binding.statusDot.setBackgroundResource(R.drawable.status_dot_red)
        binding.loadingProgress.visibility = View.GONE
        binding.urlCard.visibility = View.GONE
        binding.errorCard.visibility = View.GONE
        binding.logsCard.visibility = if (ProxyBinary.recentLogs().isEmpty()) View.GONE else View.VISIBLE
        binding.btnToggle.text = getString(R.string.start_server)
        binding.btnToggle.icon = ContextCompat.getDrawable(this, android.R.drawable.ic_media_play)
        binding.btnToggle.isEnabled = true
        binding.toolbar.subtitle = getString(R.string.proxy_stopped)
    }

    private fun renderStarting() {
        binding.statusText.text = getString(R.string.proxy_starting)
        binding.statusDot.setBackgroundResource(R.drawable.status_dot_yellow)
        binding.loadingProgress.visibility = View.VISIBLE
        binding.urlCard.visibility = View.GONE
        binding.errorCard.visibility = View.GONE
        binding.logsCard.visibility = View.VISIBLE
        binding.btnToggle.text = getString(R.string.stop_server)
        binding.btnToggle.icon = ContextCompat.getDrawable(this, android.R.drawable.ic_media_pause)
        binding.btnToggle.isEnabled = true
        binding.toolbar.subtitle = getString(R.string.proxy_starting)
    }

    private fun renderRunning() {
        val url = "http://127.0.0.1:${ProxyBinary.port}"
        binding.statusText.text = getString(R.string.proxy_running, ProxyBinary.port)
        binding.statusDot.setBackgroundResource(R.drawable.status_dot_green)
        binding.loadingProgress.visibility = View.GONE
        binding.urlCard.visibility = View.VISIBLE
        binding.urlText.text = url
        binding.errorCard.visibility = View.GONE
        binding.logsCard.visibility = View.VISIBLE
        binding.btnToggle.text = getString(R.string.stop_server)
        binding.btnToggle.icon = ContextCompat.getDrawable(this, android.R.drawable.ic_media_pause)
        binding.btnToggle.isEnabled = true
        binding.toolbar.subtitle = url
    }

    private fun renderStopping() {
        binding.statusText.text = getString(R.string.proxy_stopping)
        binding.statusDot.setBackgroundResource(R.drawable.status_dot_yellow)
        binding.loadingProgress.visibility = View.VISIBLE
        binding.urlCard.visibility = View.GONE
        binding.errorCard.visibility = View.GONE
        binding.logsCard.visibility = View.VISIBLE
        binding.btnToggle.text = getString(R.string.proxy_stopping)
        binding.btnToggle.icon = ContextCompat.getDrawable(this, android.R.drawable.ic_media_pause)
        binding.btnToggle.isEnabled = false
        binding.toolbar.subtitle = getString(R.string.proxy_stopping)
    }

    private fun renderFailed() {
        binding.statusText.text = getString(R.string.proxy_failed_short)
        binding.statusDot.setBackgroundResource(R.drawable.status_dot_red)
        binding.loadingProgress.visibility = View.GONE
        binding.urlCard.visibility = View.GONE
        binding.errorCard.visibility = if (lastError != null) View.VISIBLE else View.GONE
        binding.errorText.text = lastError
        binding.logsCard.visibility = View.VISIBLE
        binding.btnToggle.text = getString(R.string.start_server)
        binding.btnToggle.icon = ContextCompat.getDrawable(this, android.R.drawable.ic_media_play)
        binding.btnToggle.isEnabled = true
        binding.toolbar.subtitle = getString(R.string.proxy_failed_short)
    }

    private fun renderLogs() {
        val logs = ProxyBinary.recentLogs()
        binding.logsText.text = if (logs.isEmpty()) {
            getString(R.string.no_logs)
        } else {
            logs.takeLast(500).joinToString("\n")
        }
    }

    // --- Actions -----------------------------------------------------------

    private fun copyUrlToClipboard() {
        val url = "http://127.0.0.1:${ProxyBinary.port}"
        val clipboard = getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
        clipboard.setPrimaryClip(ClipData.newPlainText("url", url))
        Toast.makeText(this, R.string.url_copied, Toast.LENGTH_SHORT).show()
    }

    private fun copyLogsToClipboard() {
        val logs = ProxyBinary.recentLogs().joinToString("\n")
        val clipboard = getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
        clipboard.setPrimaryClip(ClipData.newPlainText("logs", logs))
        Toast.makeText(this, R.string.logs_copied, Toast.LENGTH_SHORT).show()
    }

    private fun openInBrowser() {
        val url = "http://127.0.0.1:${ProxyBinary.port}/"
        try {
            startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)))
        } catch (e: Exception) {
            Log.w(TAG, "No browser available", e)
            Toast.makeText(this, "Nenhum navegador disponível", Toast.LENGTH_SHORT).show()
        }
    }

    companion object {
        private const val TAG = "MainActivity"
        private const val POLL_INTERVAL_MS = 1000L
    }
}
