package com.glmproxy.app

import android.annotation.SuppressLint
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.util.Log
import android.view.Menu
import android.view.MenuItem
import android.view.View
import android.webkit.WebResourceRequest
import android.webkit.WebSettings
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.activity.OnBackPressedCallback
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat
import com.glmproxy.app.databinding.ActivityMainBinding
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL
import kotlin.concurrent.thread

/**
 * Hosts the React panel served by the embedded Go proxy.
 *
 * Lifecycle:
 *   1. onCreate starts [ProxyService] (which starts the Go binary).
 *   2. Polls http://127.0.0.1:3005/health until the proxy is ready.
 *   3. Loads the React panel into the WebView and hides the loading overlay.
 *
 * The WebView is configured to behave like a desktop browser:
 *   - JavaScript + DOM storage enabled (React + localStorage).
 *   - Viewport = device width (panel is responsive but prefers desktop layout).
 *   - Custom WebViewClient keeps navigation inside the WebView (no external
 *     browser jump) for same-origin links, and routes external http(s) links
 *     to the system browser.
 */
class MainActivity : AppCompatActivity() {
    private lateinit var binding: ActivityMainBinding
    private val handler = Handler(Looper.getMainLooper())
    private var healthAttempts = 0

    private val requestNotificationPermission =
        registerForActivityResult(ActivityResultContracts.RequestPermission()) { /* ignored */ }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        binding = ActivityMainBinding.inflate(layoutInflater)
        setContentView(binding.root)
        setSupportActionBar(binding.toolbar)

        // Request POST_NOTIFICATIONS on Android 13+ so the foreground service
        // can post its persistent notification.
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            requestNotificationPermission.launch(android.Manifest.permission.POST_NOTIFICATIONS)
        }

        configureWebView()
        startProxyService()
        pollProxyHealth()

        // Handle back press inside WebView history.
        onBackPressedDispatcher.addCallback(this, object : OnBackPressedCallback(true) {
            override fun handleOnBackPressed() {
                if (binding.webview.canGoBack()) {
                    binding.webview.goBack()
                } else {
                    isEnabled = false
                    onBackPressedDispatcher.onBackPressed()
                }
            }
        })

        binding.btnRetry.setOnClickListener {
            binding.errorOverlay.visibility = View.GONE
            binding.loadingOverlay.visibility = View.VISIBLE
            binding.loadingText.text = getString(R.string.proxy_starting)
            healthAttempts = 0
            startProxyService()
            pollProxyHealth()
        }
    }

    @SuppressLint("SetJavaScriptEnabled")
    private fun configureWebView() {
        val settings: WebSettings = binding.webview.settings
        settings.javaScriptEnabled = true
        settings.domStorageEnabled = true
        settings.databaseEnabled = true
        settings.allowFileAccess = false
        settings.allowContentAccess = false
        settings.mediaPlaybackRequiresUserGesture = false
        settings.cacheMode = WebSettings.LOAD_DEFAULT
        settings.userAgentString = "GLMProxy-Android/1.0 (WebView)"
        // The React panel was designed for desktop width; force a wide viewport
        // and let the page scale down to fit, so the UI matches the desktop app.
        settings.useWideViewPort = true
        settings.loadWithOverviewMode = true

        binding.webview.webViewClient = object : WebViewClient() {
            override fun shouldOverrideUrlLoading(
                view: WebView,
                request: WebResourceRequest
            ): Boolean {
                val url = request.url
                val host = url.host ?: return false
                // Internal loopback URLs (panel + API) stay in the WebView.
                if (host == "127.0.0.1" || host == "localhost") {
                    return false
                }
                // Anything else (e.g. OAuth callback links, external docs) opens
                // in the system browser.
                startActivity(Intent(Intent.ACTION_VIEW, url))
                return true
            }
        }
    }

    private fun startProxyService() {
        val intent = Intent(this, ProxyService::class.java)
        ContextCompat.startForegroundService(this, intent)
    }

    private fun pollProxyHealth() {
        thread(name = "proxy-health") {
            val url = URL("http://127.0.0.1:${ProxyBinary.port}/health")
            while (healthAttempts < MAX_HEALTH_ATTEMPTS) {
                try {
                    val conn = (url.openConnection() as HttpURLConnection).apply {
                        connectTimeout = 800
                        readTimeout = 800
                        requestMethod = "GET"
                    }
                    conn.connect()
                    val code = conn.responseCode
                    conn.disconnect()
                    if (code == 200) {
                        handler.post { loadPanel() }
                        return
                    }
                } catch (_: IOException) {
                    // proxy not ready yet — retry
                }
                healthAttempts++
                Thread.sleep(HEALTH_POLL_INTERVAL_MS)
            }
            handler.post { showFatalError("Proxy não respondeu após $MAX_HEALTH_ATTEMPTS tentativas.") }
        }
    }

    private fun loadPanel() {
        val url = "http://127.0.0.1:${ProxyBinary.port}/"
        binding.webview.loadUrl(url)
        binding.loadingOverlay.visibility = View.GONE
        binding.toolbar.subtitle = getString(R.string.proxy_running, ProxyBinary.port)
        // Give the WebView a moment to render before hiding overlay, to avoid
        // a brief flash of background color.
        handler.postDelayed({
            binding.loadingOverlay.visibility = View.GONE
        }, 400)
    }

    private fun showFatalError(message: String) {
        binding.loadingOverlay.visibility = View.GONE
        binding.errorText.text = message
        binding.errorOverlay.visibility = View.VISIBLE
    }

    override fun onCreateOptionsMenu(menu: Menu): Boolean {
        menuInflater.inflate(R.menu.main_menu, menu)
        return true
    }

    override fun onOptionsItemSelected(item: MenuItem): Boolean {
        return when (item.itemId) {
            R.id.action_reload -> {
                binding.webview.reload()
                true
            }
            R.id.action_open_browser -> {
                startActivity(Intent(Intent.ACTION_VIEW,
                    Uri.parse("http://127.0.0.1:${ProxyBinary.port}/")))
                true
            }
            else -> super.onOptionsItemSelected(item)
        }
    }

    override fun onDestroy() {
        super.onDestroy()
        handler.removeCallbacksAndMessages(null)
        // Note: we intentionally do NOT stop ProxyService here. The service is
        // stopped when the user explicitly swipes the app away from recents,
        // which triggers onTaskRemoved → stopSelf in the service.
    }

    companion object {
        private const val TAG = "MainActivity"
        private const val MAX_HEALTH_ATTEMPTS = 60
        private const val HEALTH_POLL_INTERVAL_MS = 500L
    }
}
