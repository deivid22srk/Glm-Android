package com.glmproxy.app

import android.Manifest
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.util.Log
import android.view.View
import android.widget.Toast
import androidx.activity.result.contract.ActivityResultContracts
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
     * Requests POST_NOTIFICATIONS at runtime on Android 13+ (API 33+).
     * Without this permission, the captcha notification silently does
     * nothing on modern devices — the entire captcha-alert feature
     * depends on it. See plan 010 for details.
     *
     * On permission denial we show a Toast (best-effort, no persistent
     * UI surface for the denial state — would belong in a future
     * settings screen).
     */
    private val requestNotificationPermission =
        registerForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
            if (!granted) {
                Toast.makeText(
                    this,
                    "Sem permissão de notificações — alertas de captcha não serão exibidos",
                    Toast.LENGTH_LONG
                ).show()
            }
        }

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

        // Request POST_NOTIFICATIONS at runtime on Android 13+.
        // Without this the captcha notification silently does nothing —
        // the permission is declared in the manifest but Android requires
        // an explicit runtime request starting from API 33.
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            requestNotificationPermission.launch(Manifest.permission.POST_NOTIFICATIONS)
        }

        binding.btnToggle.setOnClickListener { onToggleClicked() }
        binding.btnCopyUrl.setOnClickListener { copyUrlToClipboard() }
        binding.btnOpenBrowser.setOnClickListener { openInBrowser() }
        binding.btnCopyLogs.setOnClickListener { copyLogsToClipboard() }
        binding.btnLoginGoogle.setOnClickListener { onLoginGoogleClicked() }
        binding.btnViewCaptchaWebview.setOnClickListener { onViewCaptchaWebViewClicked() }
        binding.btnRefreshAccounts.setOnClickListener { fetchAccounts() }

        // Assess current state on launch — the proxy may already be running
        // (e.g. service started from a previous launch that the user
        // navigated away from without stopping).
        if (ProxyBinary.isRunning()) {
            setState(ProxyState.STARTING)
        } else {
            setState(ProxyState.STOPPED)
        }

        // If launched from the captcha notification, show the captcha dialog.
        maybeShowCaptchaDialogFromIntent(intent)
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        // Capture new intents sent while the activity is already running
        // (e.g. user taps the captcha notification while the app is open).
        setIntent(intent)
        maybeShowCaptchaDialogFromIntent(intent)
    }

    /**
     * Inspects the launch intent for the [ProxyService.ACTION_SHOW_CAPTCHA]
     * action and opens the captcha dialog if present.
     *
     * Both extras ([ProxyService.EXTRA_CAPTCHA_URL] and
     * [ProxyService.EXTRA_CAPTCHA_LOG]) are sanitized before use because
     * MainActivity is exported=true and any third-party app can construct
     * an explicit intent with arbitrary extras. Without sanitization, a
     * malicious app could phish the user by injecting an attacker-chosen
     * URL into the trusted captcha dialog (finding SEC-03, plan 016).
     */
    private fun maybeShowCaptchaDialogFromIntent(intent: Intent?) {
        if (intent?.action == ProxyService.ACTION_SHOW_CAPTCHA) {
            val log = sanitizeCaptchaLog(intent.getStringExtra(ProxyService.EXTRA_CAPTCHA_LOG))
            val url = sanitizeCaptchaUrl(intent.getStringExtra(ProxyService.EXTRA_CAPTCHA_URL))
            showCaptchaDialog(log, url)
        }
    }

    /**
     * Returns the captcha URL only if it points to the local proxy's
     * captcha endpoint. Returns the default URL otherwise — never trusts
     * an out-of-scheme URL from an untrusted caller.
     *
     * Guards against the exported-activity phishing vector where a
     * third-party app could send ACTION_SHOW_CAPTCHA with an arbitrary
     * URL extra (any scheme: http(s), intent:, content:, file:,
     * javascript:), tricking the user into tapping "Abrir no navegador"
     * and landing on an attacker-controlled page. See plan 016.
     */
    private fun sanitizeCaptchaUrl(input: String?): String {
        val default = "http://127.0.0.1:${ProxyBinary.port}/zcode/captcha/browser?client=standalone-browser"
        if (input.isNullOrBlank()) return default
        return try {
            val parsed = Uri.parse(input)
            // Strict allowlist: only http(s) to 127.0.0.1 or localhost on
            // the proxy port, path must start with /zcode/captcha/.
            val isLoopback = parsed.host == "127.0.0.1" || parsed.host == "localhost"
            val isHttp = parsed.scheme == "http" || parsed.scheme == "https"
            val isCaptchaPath = parsed.path?.startsWith("/zcode/captcha/") == true
            val portMatches = parsed.port == -1 || parsed.port == ProxyBinary.port
            if (isLoopback && isHttp && isCaptchaPath && portMatches) {
                input
            } else {
                Log.w(TAG, "Rejected out-of-scope captcha URL: scheme=${parsed.scheme} host=${parsed.host} path=${parsed.path}")
                default
            }
        } catch (e: Exception) {
            Log.w(TAG, "Failed to parse captcha URL extra", e)
            default
        }
    }

    /**
     * Caps the log line length and strips control characters so a
     * malicious caller can't inject misleading text into the captcha
     * dialog body (e.g. "Your account will be deleted in 5 minutes").
     * Newlines and tabs are preserved. See plan 016.
     */
    private fun sanitizeCaptchaLog(input: String?): String {
        if (input.isNullOrBlank()) return "(sem log detalhado)"
        // Cap at 500 chars to prevent dialog overflow
        val capped = if (input.length > 500) input.take(500) + "…" else input
        // Strip control characters (newlines/tabs preserved)
        return capped.replace(Regex("[\\x00-\\x08\\x0B\\x0C\\x0E-\\x1F]"), "")
    }

    /**
     * Shows a Material 3 AlertDialog with the captcha details and a button
     * to open the captcha URL in the system browser. Also clears the
     * captcha-pending flag in [ProxyService] so future captcha requests
     * can post a fresh notification.
     */
    private fun showCaptchaDialog(logLine: String, url: String) {
        // Clear the pending flag + cancel the active notification now that
        // the user has seen the dialog.
        ProxyService.clearCaptchaPending(this)

        val dialogView = layoutInflater.inflate(R.layout.dialog_captcha, null)
        dialogView.findViewById<android.widget.TextView>(R.id.captcha_url_text).text = url
        dialogView.findViewById<android.widget.TextView>(R.id.captcha_log_text).text =
            if (logLine.isBlank()) "(sem log detalhado)" else logLine

        val dialog = com.google.android.material.dialog.MaterialAlertDialogBuilder(this)
            .setTitle(R.string.captcha_dialog_title)
            .setView(dialogView)
            .setCancelable(true)
            .setNegativeButton(R.string.captcha_dialog_dismiss_button, null)
            .create()

        dialogView.findViewById<com.google.android.material.button.MaterialButton>(R.id.btn_captcha_open)
            .setOnClickListener {
                try {
                    startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)))
                } catch (e: Exception) {
                    Log.w(TAG, "No browser available for captcha URL", e)
                    Toast.makeText(this, "Nenhum navegador disponível", Toast.LENGTH_SHORT).show()
                }
            }

        dialogView.findViewById<com.google.android.material.button.MaterialButton>(R.id.btn_captcha_copy)
            .setOnClickListener {
                val clipboard = getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
                clipboard.setPrimaryClip(ClipData.newPlainText("captcha_url", url))
                Toast.makeText(this, R.string.url_copied, Toast.LENGTH_SHORT).show()
            }

        dialog.setOnDismissListener {
            // Make sure pending flag is clear when user closes the dialog
            // (defensive — already cleared at show time).
            ProxyService.clearCaptchaPending(this)
        }

        dialog.show()
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
        binding.accountsCard.visibility = View.GONE
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
        // Auto-fetch accounts when entering RUNNING state
        fetchAccounts()
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
        // Show a dialog with two options: open in system browser, or
        // open in the in-app WebView. The user requested both options
        // so they can choose whether to context-switch to an external
        // browser or stay inside the app.
        com.google.android.material.dialog.MaterialAlertDialogBuilder(this)
            .setTitle("Abrir painel")
            .setMessage("Como você quer abrir o painel do proxy?")
            .setPositiveButton("Navegador do sistema") { _, _ ->
                val url = "http://127.0.0.1:${ProxyBinary.port}/"
                try {
                    startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)))
                } catch (e: Exception) {
                    Log.w(TAG, "No browser available", e)
                    Toast.makeText(this, "Nenhum navegador disponível", Toast.LENGTH_SHORT).show()
                }
            }
            .setNegativeButton("WebView no app") { _, _ ->
                val url = "http://127.0.0.1:${ProxyBinary.port}/"
                WebViewActivity.start(this, url, title = "Painel do proxy")
            }
            .setNeutralButton("Cancelar", null)
            .show()
    }

    /**
     * Opens the captcha broker page in a visible WebView so the user
     * can see what the invisible background WebView is doing — useful
     * for debugging captcha issues and for solving interactive
     * challenges manually without leaving the app.
     *
     * This loads the SAME URL that the background CaptchaWebViewManager
     * loads, but in a visible activity. The user can see the "Broker
     * pronto. Aguardando request..." status, watch the Aliyun SDK
     * solve traceless captchas, or solve interactive challenges
     * (sliders, image picks) directly.
     *
     * Note: this is a SEPARATE WebView from the background one. Both
     * poll the Go bridge, but with different client names
     * (android-webview for background, standalone-browser for this
     * visible one). The Go bridge's preferred client is android-webview
     * (set via ZCODE_CAPTCHA_CLIENT_PREFERENCE), so the background
     * WebView gets priority for solving. This visible WebView is
     * primarily for inspection and manual interactive fallback.
     */
    private fun onViewCaptchaWebViewClicked() {
        if (!ProxyBinary.isRunning()) {
            Toast.makeText(
                this,
                "Inicie o servidor antes de visualizar o captcha",
                Toast.LENGTH_SHORT
            ).show()
            return
        }
        val url = "http://127.0.0.1:${ProxyBinary.port}/zcode/captcha/browser?client=standalone-browser"
        WebViewActivity.start(this, url, title = "Captcha WebView")
    }

    /**
     * Called when the user taps "Login Google". Initiates the ZCode/Z.ai
     * OAuth flow by calling the proxy's /api/admin/auth/login/start
     * endpoint and opening the returned authorization URL in a Chrome
     * Custom Tab. The user signs in with Google on chat.z.ai, the
     * callback redirects to the proxy which stores the account, and the
     * user returns to the app.
     *
     * Requires the proxy to be running (state == RUNNING). If the proxy
     * is stopped, shows a Toast asking the user to start it first.
     */
    private fun onLoginGoogleClicked() {
        if (!ProxyBinary.isRunning()) {
            Toast.makeText(
                this,
                "Inicie o servidor antes de fazer login",
                Toast.LENGTH_SHORT
            ).show()
            return
        }
        Toast.makeText(this, "Abrindo login Google...", Toast.LENGTH_SHORT).show()
        // Network call must be off the main thread.
        Thread({
            val ok = GoogleLoginHelper.startLogin(this)
            if (!ok) {
                runOnUiThread {
                    Toast.makeText(
                        this,
                        "Falha ao iniciar login. Verifique se o proxy está rodando.",
                        Toast.LENGTH_LONG
                    ).show()
                }
            }
        }, "google-login").start()
    }

    /**
     * Fetches account and queue info from the Go proxy's admin API and
     * renders it in the accounts card. Called when the user taps
     * "Atualizar" or automatically when the proxy enters RUNNING state.
     *
     * The fetch happens on a background thread; the UI is updated on
     * the main thread.
     */
    private fun fetchAccounts() {
        if (!ProxyBinary.isRunning()) {
            binding.accountsCard.visibility = View.GONE
            return
        }
        Thread({
            val state = AccountFetcher.fetchState(ProxyBinary.port)
            runOnUiThread { renderAccounts(state) }
        }, "account-fetch").start()
    }

    /**
     * Renders the account list and queue info in the accounts card.
     * Called on the main thread after [fetchAccounts] completes.
     */
    private fun renderAccounts(state: AccountFetcher.ProxyState?) {
        if (state == null) {
            binding.accountsCard.visibility = View.GONE
            return
        }
        binding.accountsCard.visibility = View.VISIBLE
        binding.accountsList.removeAllViews()

        if (state.accounts.isEmpty()) {
            binding.accountsEmpty.visibility = View.VISIBLE
            return
        }
        binding.accountsEmpty.visibility = View.GONE

        for (account in state.accounts) {
            val row = layoutInflater.inflate(R.layout.item_account, binding.accountsList, false)
            row.findViewById<android.widget.TextView>(R.id.account_label).text = account.label
            row.findViewById<android.widget.TextView>(R.id.account_model).text = account.model
            val quotaText = if (account.quotaTotal > 0) {
                getString(R.string.quota_format, account.quotaUsed, account.quotaTotal, account.quotaPercent)
            } else {
                "—"
            }
            row.findViewById<android.widget.TextView>(R.id.account_quota).text = quotaText
            val statusChip = row.findViewById<com.google.android.material.chip.Chip>(R.id.account_status)
            statusChip.text = if (account.active) getString(R.string.active_label) else account.status
            // Highlight the active account
            val isActive = account.active || account.id == state.activeAccountId
            if (isActive) {
                row.alpha = 1f
                statusChip.isChecked = true
            } else {
                row.alpha = 0.6f
            }
            binding.accountsList.addView(row)
        }

        // Render queue if non-empty
        if (state.queue.isNotEmpty()) {
            val queueHeader = android.widget.TextView(this).apply {
                text = "Fila: ${state.queue.size} entrada(s)"
                setTextColor(getColor(com.google.android.material.R.color.material_on_surface_emphasis_medium))
                textSize = 12f
                setPadding(0, 16, 0, 0)
            }
            binding.accountsList.addView(queueHeader)
        }
    }

    companion object {
        private const val TAG = "MainActivity"
        private const val POLL_INTERVAL_MS = 1000L
    }
}
