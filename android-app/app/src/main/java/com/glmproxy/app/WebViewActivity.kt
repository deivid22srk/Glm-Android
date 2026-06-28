package com.glmproxy.app

import android.annotation.SuppressLint
import android.content.Context
import android.content.Intent
import android.graphics.Bitmap
import android.net.Uri
import android.os.Bundle
import android.util.Log
import android.view.KeyEvent
import android.view.View
import android.webkit.ConsoleMessage
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebSettings
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.Toast
import androidx.appcompat.app.AppCompatActivity
import androidx.browser.customtabs.CustomTabColorSchemeParams
import androidx.browser.customtabs.CustomTabsIntent
import androidx.core.view.ViewCompat
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.updatePadding
import com.google.android.material.color.DynamicColors
import com.glmproxy.app.databinding.ActivityWebviewBinding

/**
 * A visible WebView activity used for two purposes:
 *
 * 1. **Login Google** — loads the ZCode/Z.ai OAuth authorization URL.
 *    When the OAuth flow redirects to `http://127.0.0.1:3005/...`,
 *    the Go proxy handles the callback and returns a success page.
 *
 * 2. **View captcha WebView** — loads the captcha broker page so the
 *    user can see (and interact with) the same page that the invisible
 *    background WebView is running.
 *
 * ## Google login anti-detection
 *
 * Google's login page blocks WebViews with the "this browser or app
 * may not be secure" error. Google detects WebView through multiple
 * signals:
 *
 * - `navigator.webdriver` is `true` in WebView
 * - `window.chrome` object is missing or incomplete
 * - `navigator.permissions.query` returns different results
 * - `navigator.plugins` is empty
 * - WebGL renderer string differs from desktop Chrome
 *
 * We inject a comprehensive anti-detection script via `evaluateJavascript`
 * on every `onPageStarted` (before any page JavaScript runs) that
 * patches these signals to make the WebView look like a real desktop
 * Chrome browser.
 *
 * ## Automatic fallback to Chrome Custom Tab
 *
 * If Google's detection still catches us (they update their bot
 * detection frequently), we detect the "this browser or app may not
 * be secure" error page in `onPageFinished` by checking the page's
 * text content. When detected, we automatically:
 *
 * 1. Close the WebView activity
 * 2. Open the SAME Google login URL in a Chrome Custom Tab (real
 *    Chrome, which Google doesn't block)
 * 3. The OAuth flow continues in the CCT — the user signs in, Google
 *    redirects to chat.z.ai, chat.z.ai redirects to the proxy callback
 * 4. The CCT closes when it hits 127.0.0.1:3005 (handled by Chrome)
 *
 * This hybrid approach gives the user the best of both worlds: try
 * the in-app WebView first (faster, no context switch), and fall back
 * to real Chrome automatically if Google blocks it.
 */
class WebViewActivity : AppCompatActivity() {

    private lateinit var binding: ActivityWebviewBinding
    private var googleBlockDetected = false
    private var originalLoginUrl: String? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        WindowCompat.setDecorFitsSystemWindows(window, false)
        DynamicColors.applyToActivityIfAvailable(this)

        binding = ActivityWebviewBinding.inflate(layoutInflater)
        setContentView(binding.root)
        setSupportActionBar(binding.toolbar)

        ViewCompat.setOnApplyWindowInsetsListener(binding.toolbar) { v, insets ->
            val bars = insets.getInsets(WindowInsetsCompat.Type.systemBars())
            v.updatePadding(top = bars.top)
            insets
        }
        ViewCompat.setOnApplyWindowInsetsListener(binding.webview) { v, insets ->
            val bars = insets.getInsets(WindowInsetsCompat.Type.systemBars())
            v.updatePadding(bottom = bars.bottom)
            insets
        }
        WindowCompat.getInsetsController(window, window.decorView)
            .isAppearanceLightStatusBars = false

        binding.toolbar.setNavigationOnClickListener { finish() }

        val url = intent.getStringExtra(EXTRA_URL) ?: run {
            Log.e(TAG, "No URL extra provided — finishing")
            finish()
            return
        }
        val title = intent.getStringExtra(EXTRA_TITLE) ?: "WebView"
        binding.toolbar.title = title
        originalLoginUrl = url

        // Add JS interface for captcha interactive detection
        binding.webview.addJavascriptInterface(CaptchaBridge(), "CaptchaBridge")

        configureWebView()
        binding.webview.loadUrl(url)
        Log.i(TAG, "WebViewActivity loading: $url")
    }

    @SuppressLint("SetJavaScriptEnabled")
    private fun configureWebView() {
        val settings: WebSettings = binding.webview.settings
        settings.javaScriptEnabled = true
        settings.domStorageEnabled = true
        settings.databaseEnabled = true
        settings.allowFileAccess = false
        settings.allowContentAccess = false
        // Desktop UA so Google login doesn't block the WebView with
        // "this browser or app may not be secure". This alone isn't
        // enough — we also inject anti-detection JS below.
        settings.userAgentString = DESKTOP_USER_AGENT
        settings.cacheMode = WebSettings.LOAD_DEFAULT
        settings.mediaPlaybackRequiresUserGesture = false
        settings.mixedContentMode = WebSettings.MIXED_CONTENT_COMPATIBILITY_MODE
        settings.setSupportMultipleWindows(false)
        settings.javaScriptCanOpenWindowsAutomatically = true

        binding.webview.webViewClient = object : WebViewClient() {
            override fun shouldOverrideUrlLoading(
                view: WebView,
                request: WebResourceRequest
            ): Boolean {
                val url = request.url.toString()
                Log.i(TAG, "Navigation: $url")

                // If we're being redirected to 127.0.0.1 (the proxy
                // callback), the OAuth flow is complete — let it load
                // so the proxy can exchange the code for tokens.
                if (url.startsWith("http://127.0.0.1:") ||
                    url.startsWith("http://localhost:")) {
                    return false
                }

                // If we already detected the Google block and are in
                // the process of falling back, block all navigations.
                if (googleBlockDetected) {
                    return true
                }

                return false  // allow all other navigations
            }

            override fun onPageStarted(view: WebView?, url: String?, favicon: Bitmap?) {
                super.onPageStarted(view, url, favicon)
                binding.toolbar.subtitle = url
                binding.progressBar.visibility = View.VISIBLE
                // Inject anti-detection script BEFORE any page JS runs.
                // This patches navigator.webdriver, window.chrome,
                // permissions, plugins, etc. to make the WebView look
                // like a real desktop Chrome browser.
                if (url != null && (url.contains("google.com") ||
                        url.contains("googleapis.com") ||
                        url.contains("chat.z.ai"))) {
                    view?.evaluateJavascript(ANTI_DETECTION_JS, null)
                    Log.i(TAG, "Injected anti-detection JS for $url")
                }
            }

            override fun onPageFinished(view: WebView?, url: String?) {
                super.onPageFinished(view, url)
                binding.progressBar.visibility = View.GONE

                // Check if this is a Google login page that shows the
                // "this browser or app may not be secure" error.
                if (url != null && url.contains("accounts.google.com")) {
                    checkForGoogleBlock(view, url)
                }

                // If this is the captcha broker page, inject a hook
                // that detects when the Aliyun SDK requires an
                // interactive challenge. When detected, automatically
                // open the system browser (Chrome real) which renders
                // the captcha popup correctly — the WebView often
                // fails to render the Aliyun popup.
                if (url != null && url.contains("/zcode/captcha/browser")) {
                    view?.evaluateJavascript(CAPTCHA_INTERACTIVE_HOOK_JS, null)
                    Log.i(TAG, "Injected captcha interactive hook for $url")
                }
            }

            override fun onReceivedError(
                view: WebView?,
                request: WebResourceRequest?,
                error: android.webkit.WebResourceError?
            ) {
                super.onReceivedError(view, request, error)
                Log.w(TAG, "WebView error: ${error?.description} (code: ${error?.errorCode}) for ${request?.url}")
            }
        }

        binding.webview.webChromeClient = object : WebChromeClient() {
            override fun onConsoleMessage(consoleMessage: ConsoleMessage): Boolean {
                Log.i(TAG, "[webview-console] ${consoleMessage.message()} (${consoleMessage.sourceId()}:${consoleMessage.lineNumber()})")
                return true
            }

            override fun onProgressChanged(view: WebView?, newProgress: Int) {
                binding.progressBar.progress = newProgress
            }
        }
    }

    /**
     * Checks if the Google login page is showing the "this browser or
     * app may not be secure" error. If so, automatically falls back to
     * opening the same URL in a Chrome Custom Tab (real Chrome, which
     * Google doesn't block).
     *
     * The check is done by evaluating JavaScript that reads the page's
     * text content and looks for the error message in multiple
     * languages (Portuguese + English).
     */
    private fun checkForGoogleBlock(view: WebView?, url: String?) {
        if (view == null || url == null) return
        if (googleBlockDetected) return  // already handled

        val checkJs = """
            (function() {
                try {
                    var body = document.body ? document.body.innerText : '';
                    var blockSignals = [
                        'navegador ou app pode não estar seguro',
                        'navegador ou app pode nao estar seguro',
                        'browser or app may not be secure',
                        'this browser or app might not be secure',
                        'não foi possível fazer o login',
                        'nao foi possivel fazer o login'
                    ];
                    for (var i = 0; i < blockSignals.length; i++) {
                        if (body.toLowerCase().indexOf(blockSignals[i]) !== -1) {
                            return blockSignals[i];
                        }
                    }
                    return '';
                } catch(e) { return 'error:' + e; }
            })();
        """.trimIndent()

        view.evaluateJavascript(checkJs) { result ->
            // result is a JSON-encoded string (with quotes) or "null"
            val cleaned = result
                ?.removeSurrounding("\"")
                ?.replace("\\n", " ")
                ?.trim()
            if (!cleaned.isNullOrBlank() && cleaned != "null" && !cleaned.startsWith("error:")) {
                Log.w(TAG, "Google block detected: '$cleaned' — falling back to Chrome Custom Tab")
                googleBlockDetected = true
                fallbackToChromeCustomTab(url)
            }
        }
    }

    /**
     * Opens the given URL in a Chrome Custom Tab (real Chrome) as a
     * fallback when the WebView is blocked by Google's anti-automation
     * detection. The CCT inherits Chrome's real browser fingerprint
     * and the user's Google account cookies, so Google's login flow
     * works normally.
     *
     * After launching the CCT, we finish this activity so the WebView
     * doesn't stay visible behind the CCT.
     */
    private fun fallbackToChromeCustomTab(url: String) {
        runOnUiThread {
            Toast.makeText(
                this,
                "Google bloqueou o WebView. Abrindo no Chrome...",
                Toast.LENGTH_LONG
            ).show()

            val uri = Uri.parse(url)
            val darkParams = CustomTabColorSchemeParams.Builder()
                .setToolbarColor(0xFF0A0D10.toInt())
                .setNavigationBarColor(0xFF0A0D10.toInt())
                .build()
            val intent = CustomTabsIntent.Builder()
                .setDefaultColorSchemeParams(darkParams)
                .setShowTitle(true)
                .setUrlBarHidingEnabled(false)
                .build()

            try {
                intent.launchUrl(this, uri)
            } catch (e: Exception) {
                Log.w(TAG, "No Chrome Custom Tab available, trying ACTION_VIEW", e)
                try {
                    val fallbackIntent = Intent(Intent.ACTION_VIEW, uri)
                        .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                    startActivity(fallbackIntent)
                } catch (e2: Exception) {
                    Log.e(TAG, "No browser available at all", e2)
                }
            }

            // Close the WebView activity so the CCT is on top.
            finish()
        }
    }

    /**
     * JS interface exposed to the captcha broker page. The page calls
     * `CaptchaBridge.onInteractiveRequired()` when the Aliyun SDK demands
     * an interactive challenge (slider, image pick). The WebView often
     * fails to render the Aliyun popup correctly, so we open the system
     * browser (Chrome real) which handles it properly.
     */
    inner class CaptchaBridge {
        @android.webkit.JavascriptInterface
        fun onInteractiveRequired() {
            Log.i(TAG, "Interactive captcha required — opening system browser")
            runOnUiThread {
                Toast.makeText(
                    this@WebViewActivity,
                    "Abrindo captcha no navegador do sistema...",
                    Toast.LENGTH_SHORT
                ).show()
                openSystemBrowserForCaptcha()
            }
        }
    }

    /**
     * Opens the captcha page in the system browser (Chrome/Firefox/etc.)
     * as a fallback when the WebView can't render the Aliyun popup.
     *
     * The system browser polls the same endpoint
     * (/zcode/captcha/poll?client=standalone-browser) and will receive
     * the next captcha request from the Go bridge. The user solves it
     * in the real browser, the token is submitted, and the chat
     * completion continues.
     *
     * After opening the browser, we finish this activity so the user
     * isn't confused by two captcha pages.
     */
    private fun openSystemBrowserForCaptcha() {
        val url = "http://127.0.0.1:${ProxyBinary.port}/zcode/captcha/browser?client=standalone-browser"
        try {
            val intent = Intent(Intent.ACTION_VIEW, Uri.parse(url))
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            startActivity(intent)
            // Close this activity — the browser will handle the captcha.
            finish()
        } catch (e: Exception) {
            Log.e(TAG, "No browser available for captcha fallback", e)
            Toast.makeText(
                this,
                "Nenhum navegador disponível. Abra manualmente: $url",
                Toast.LENGTH_LONG
            ).show()
        }
    }

    override fun onKeyDown(keyCode: Int, event: KeyEvent?): Boolean {
        if (keyCode == KeyEvent.KEYCODE_BACK && binding.webview.canGoBack()) {
            binding.webview.goBack()
            return true
        }
        return super.onKeyDown(keyCode, event)
    }

    override fun onDestroy() {
        binding.webview.apply {
            stopLoading()
            removeJavascriptInterface("CaptchaBridge")
            destroy()
        }
        super.onDestroy()
    }

    companion object {
        private const val TAG = "WebViewActivity"

        const val EXTRA_URL = "url"
        const val EXTRA_TITLE = "title"

        /**
         * Desktop Chrome User-Agent so Google's login page doesn't
         * immediately block the WebView. Chrome 120 on Windows 10.
         */
        private const val DESKTOP_USER_AGENT =
            "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
            "(KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

        /**
         * Anti-detection JavaScript injected before any page JS runs.
         * Patches the signals Google uses to detect WebView:
         *
         * 1. `navigator.webdriver` → `undefined` (WebView sets this to `true`)
         * 2. `window.chrome` → fake Chrome runtime object (WebView doesn't have it)
         * 3. `navigator.permissions.query` → returns expected results
         * 4. `navigator.plugins` → non-empty array (WebView has empty plugins)
         * 5. `navigator.languages` → `['en-US', 'en']` (desktop pattern)
         * 6. `navigator.connection` → undefined (mobile signal)
         *
         * This is the same technique used by puppeteer-extra-plugin-stealth
         * and similar anti-detection libraries. Google updates their
         * detection frequently, so this may not work forever — the
         * automatic CCT fallback (checkForGoogleBlock) catches cases
         * where the spoofing isn't enough.
         */
        private val ANTI_DETECTION_JS = """
            (function() {
                try {
                    // 1. Remove navigator.webdriver
                    Object.defineProperty(navigator, 'webdriver', {
                        get: () => undefined,
                        configurable: true
                    });

                    // 2. Add window.chrome if missing or incomplete
                    if (!window.chrome) {
                        window.chrome = {};
                    }
                    if (!window.chrome.runtime) {
                        window.chrome.runtime = {
                            OnInstalledReason: {},
                            OnRestartRequiredReason: {},
                            PlatformArch: {},
                            PlatformOs: {},
                            RequestUpdateCheckStatus: {}
                        };
                    }
                    if (!window.chrome.loadTimes) {
                        window.chrome.loadTimes = function() {
                            return {
                                requestTime: Date.now() / 1000,
                                startLoadTime: Date.now() / 1000,
                                commitLoadTime: Date.now() / 1000,
                                finishDocumentLoadTime: Date.now() / 1000,
                                finishLoadTime: Date.now() / 1000,
                                firstPaintTime: Date.now() / 1000,
                                firstPaintAfterLoadTime: 0,
                                navigationType: 'Other',
                                wasFetchedViaSpdy: true,
                                wasNpnNegotiated: true,
                                npnNegotiatedProtocol: 'h2',
                                wasAlternateProtocolAvailable: false,
                                connectionInfo: 'h2'
                            };
                        };
                    }
                    if (!window.chrome.csi) {
                        window.chrome.csi = function() {
                            return {
                                startE: Date.now(),
                                onloadT: Date.now(),
                                pageT: Date.now() - 1000,
                                tran: 15
                            };
                        };
                    }
                    if (!window.chrome.app) {
                        window.chrome.app = {
                            isInstalled: false,
                            InstallState: {DISABLED: 'disabled', INSTALLED: 'installed', NOT_INSTALLED: 'not_installed'},
                            RunningState: {CANNOT_RUN: 'cannot_run', READY_TO_RUN: 'ready_to_run', RUNNING: 'running'},
                            getDetails: function() { return null; },
                            getIsInstalled: function() { return false; }
                        };
                    }

                    // 3. Override permissions.query to look like real Chrome
                    if (window.navigator.permissions && window.navigator.permissions.query) {
                        var originalQuery = window.navigator.permissions.query.bind(window.navigator.permissions);
                        window.navigator.permissions.query = function(parameters) {
                            if (parameters && parameters.name === 'notifications') {
                                return Promise.resolve({state: Notification.permission, onchange: null});
                            }
                            return originalQuery(parameters);
                        };
                    }

                    // 4. Override plugins to look like desktop Chrome
                    Object.defineProperty(navigator, 'plugins', {
                        get: function() {
                            return [
                                {name: 'Chrome PDF Plugin'},
                                {name: 'Chrome PDF Viewer'},
                                {name: 'Native Client'}
                            ];
                        },
                        configurable: true
                    });

                    // 5. Override languages
                    Object.defineProperty(navigator, 'languages', {
                        get: function() { return ['en-US', 'en']; },
                        configurable: true
                    });

                    // 6. Remove mobile signals
                    Object.defineProperty(navigator, 'connection', {
                        get: function() { return undefined; },
                        configurable: true
                    });

                    // 7. Override hardwareConcurrency (desktop usually has 8+)
                    Object.defineProperty(navigator, 'hardwareConcurrency', {
                        get: function() { return 8; },
                        configurable: true
                    });

                    // 8. Override deviceMemory (desktop signal)
                    Object.defineProperty(navigator, 'deviceMemory', {
                        get: function() { return 8; },
                        configurable: true
                    });

                    // 9. Override platform
                    Object.defineProperty(navigator, 'platform', {
                        get: function() { return 'Win32'; },
                        configurable: true
                    });

                    // 10. Override touch support (desktop doesn't have touch)
                    Object.defineProperty(navigator, 'maxTouchPoints', {
                        get: function() { return 0; },
                        configurable: true
                    });

                    console.log('[anti-detection] patches applied');
                } catch(e) {
                    console.log('[anti-detection] error: ' + e);
                }
            })();
        """.trimIndent()

        /**
         * JavaScript injected into the captcha broker page after load.
         * Hooks into the page's `setStatus()` function to detect when
         * the Aliyun SDK shows "Desafio interativo exigido" — meaning
         * the traceless verification failed and an interactive challenge
         * (slider, image pick) is required.
         *
         * When detected, calls `CaptchaBridge.onInteractiveRequired()`
         * via the @JavascriptInterface, which opens the system browser
         * (Chrome real) to handle the interactive challenge — the
         * WebView often fails to render the Aliyun popup correctly.
         *
         * Also polls the status element text every 500ms as a fallback
         * in case the setStatus hook doesn't catch it.
         */
        private val CAPTCHA_INTERACTIVE_HOOK_JS = """
            (function() {
                console.log('[captcha-hook] installing interactive detection');

                // Method 1: Hook setStatus if it exists
                if (typeof window.setStatus === 'function') {
                    var origSetStatus = window.setStatus;
                    window.setStatus = function(message) {
                        console.log('[captcha-hook] status: ' + message);
                        if (message && message.indexOf('interativo') !== -1) {
                            console.log('[captcha-hook] interactive required detected via setStatus');
                            try { CaptchaBridge.onInteractiveRequired(); } catch(e) { console.log('[captcha-hook] error: ' + e); }
                        }
                        return origSetStatus.apply(this, arguments);
                    };
                }

                // Method 2: Poll the status element text every 500ms
                // as a fallback (in case setStatus isn't global or the
                // status is set via direct DOM manipulation).
                var pollCount = 0;
                var pollInterval = setInterval(function() {
                    pollCount++;
                    if (pollCount > 600) {  // 5 minutes max
                        clearInterval(pollInterval);
                        return;
                    }
                    var statusEl = document.getElementById('status');
                    if (statusEl) {
                        var text = statusEl.textContent || '';
                        if (text.indexOf('interativo') !== -1 || text.indexOf('interactive') !== -1) {
                            console.log('[captcha-hook] interactive required detected via poll: ' + text);
                            clearInterval(pollInterval);
                            try { CaptchaBridge.onInteractiveRequired(); } catch(e) { console.log('[captcha-hook] error: ' + e); }
                        }
                    }
                }, 500);

                console.log('[captcha-hook] interactive detection installed');
            })();
        """.trimIndent()

        fun start(context: Context, url: String, title: String = "WebView") {
            val intent = Intent(context, WebViewActivity::class.java).apply {
                putExtra(EXTRA_URL, url)
                putExtra(EXTRA_TITLE, title)
            }
            context.startActivity(intent)
        }
    }
}
