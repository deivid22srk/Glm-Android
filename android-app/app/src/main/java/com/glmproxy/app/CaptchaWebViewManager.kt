package com.glmproxy.app

import android.annotation.SuppressLint
import android.content.Context
import android.content.Intent
import android.graphics.Color
import android.net.Uri
import android.os.Handler
import android.os.Looper
import android.util.Log
import android.view.ViewGroup
import android.view.WindowManager
import android.webkit.ConsoleMessage
import android.webkit.JavascriptInterface
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebSettings
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.FrameLayout
import org.json.JSONObject

/**
 * Manages an invisible background WebView that continuously loads the
 * captcha broker page (`/zcode/captcha/browser?client=android-webview`)
 * and runs its JavaScript — replicating the desktop "headless Chrome"
 * behavior on Android.
 *
 * ## Why a WebView instead of an external browser
 *
 * On desktop, the Go proxy launches a headless Chrome/Edge process that
 * stays alive in the background, polls `/zcode/captcha/poll`, and solves
 * captchas automatically via the Aliyun SDK. Android doesn't let apps
 * spawn Chrome as a subprocess, so we can't do that directly.
 *
 * A WebView running inside our foreground service is the closest
 * equivalent: it loads the same captcha page, runs the same JavaScript
 * (including the Aliyun SDK), and polls the same endpoint — but inside
 * our process, where we control its lifecycle.
 *
 * Unlike Chrome Custom Tabs (which throttle JS in background and can't
 * be hidden), a WebView attached to a `WindowManager` view keeps
 * running JavaScript as long as the foreground service is alive.
 *
 * ## Interactive captcha fallback
 *
 * The Aliyun SDK can solve "traceless" captchas automatically (no user
 * interaction). When an interactive challenge is required (slider,
 * image pick, etc.), the SDK calls `instance.show()` which would display
 * a popup in the WebView — but the WebView is invisible, so the user
 * can't see it. The page's JavaScript detects this case and calls our
 * `AndroidBridge.onInteractiveRequired()` JS interface, which opens the
 * system browser at the captcha page as a fallback so the user can
 * solve it manually.
 *
 * ## Lifecycle
 *
 * Created by [ProxyService.onCreate], destroyed by [ProxyService.onDestroy].
 * The WebView must be created and destroyed on the main thread (UI thread),
 * so we use a [Handler] to dispatch those operations.
 */
class CaptchaWebViewManager(private val context: Context) {

    private val mainHandler = Handler(Looper.getMainLooper())
    private var webView: WebView? = null
    private var attachedView: FrameLayout? = null

    /**
     * JS interface exposed to the captcha page. The page calls
     * `AndroidBridge.onInteractiveRequired(requestId)` when the Aliyun SDK
     * requires a challenge the traceless verification can't solve
     * automatically.
     */
    inner class AndroidBridge {
        @JavascriptInterface
        fun onInteractiveRequired(requestId: String) {
            Log.i(TAG, "Interactive captcha required for request $requestId — opening system browser")
            // Open the system browser at the captcha page so the user
            // can solve the interactive challenge manually. The same
            // request ID will be requeued by the bridge's requeue
            // endpoint so the browser tab picks it up.
            openSystemBrowserForInteractive()
        }

        @JavascriptInterface
        fun onLog(message: String) {
            Log.i(TAG, "[webview-js] $message")
        }
    }

    /**
     * Creates the WebView on the main thread and attaches it to the
     * window manager as an invisible view (size 1x1, alpha 0). Then
     * loads the captcha broker page.
     *
     * MUST be called from the main thread — caller is responsible for
     * dispatching, or use [start] which handles it.
     */
    fun start() {
        mainHandler.post { createAndLoadWebView() }
    }

    /**
     * Destroys the WebView and removes it from the window manager.
     * MUST be called from the main thread — caller is responsible for
     * dispatching, or use [stop] which handles it.
     */
    fun stop() {
        mainHandler.post { destroyWebView() }
    }

    @SuppressLint("SetJavaScriptEnabled")
    private fun createAndLoadWebView() {
        if (webView != null) {
            Log.w(TAG, "WebView already exists — skipping create")
            return
        }

        // Create a tiny invisible FrameLayout to host the WebView.
        // We attach it to the WindowManager so the WebView has a valid
        // window to render into (required for JS execution in background).
        val container = FrameLayout(context).apply {
            setBackgroundColor(Color.TRANSPARENT)
            alpha = 0f
        }
        val params = WindowManager.LayoutParams(
            1,  // 1px wide
            1,  // 1px tall
            WindowManager.LayoutParams.TYPE_APPLICATION_OVERLAY,
            WindowManager.LayoutParams.FLAG_NOT_TOUCHABLE
                or WindowManager.LayoutParams.FLAG_NOT_FOCUSABLE
                or WindowManager.LayoutParams.FLAG_NOT_TOUCH_MODAL
                or WindowManager.LayoutParams.FLAG_LAYOUT_NO_LIMITS,
            android.graphics.PixelFormat.TRANSLUCENT
        )
        // TYPE_APPLICATION_OVERLAY requires SYSTEM_ALERT_WINDOW permission.
        // If not granted, fall back to a private-to-app window type that
        // doesn't need a permission but still keeps the WebView alive.
        val windowManager = context.getSystemService(Context.WINDOW_SERVICE) as WindowManager
        val finalParams = try {
            windowManager.addView(container, params)
            params
        } catch (e: Exception) {
            Log.w(TAG, "TYPE_APPLICATION_OVERLAY failed (need SYSTEM_ALERT_WINDOW), falling back to TYPE_PRIVATE: ${e.message}")
            // Fall back to a private-to-app presentation — doesn't need
            // SYSTEM_ALERT_WINDOW but still keeps the WebView alive.
            val fallbackParams = WindowManager.LayoutParams(
                1, 1,
                WindowManager.LayoutParams.TYPE_PRIVATE,
                WindowManager.LayoutParams.FLAG_NOT_TOUCHABLE
                    or WindowManager.LayoutParams.FLAG_NOT_FOCUSABLE,
                android.graphics.PixelFormat.TRANSLUCENT
            )
            try {
                windowManager.addView(container, fallbackParams)
                fallbackParams
            } catch (e2: Exception) {
                Log.e(TAG, "Could not attach WebView to window manager — JS will not run in background", e2)
                return
            }
        }
        attachedView = container

        val wv = WebView(context)
        container.addView(wv, FrameLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT,
            ViewGroup.LayoutParams.MATCH_PARENT
        ))

        // Configure WebSettings for running the Aliyun SDK in background.
        val settings: WebSettings = wv.settings
        settings.javaScriptEnabled = true
        settings.domStorageEnabled = true
        settings.databaseEnabled = true
        settings.allowFileAccess = false
        settings.allowContentAccess = false
        // Desktop-class User-Agent so the Aliyun SDK doesn't try to
        // use a mobile-specific flow that requires user gestures.
        // Realistic Chrome 120 on Windows UA.
        settings.userAgentString = DESKTOP_USER_AGENT
        settings.cacheMode = WebSettings.LOAD_NO_CACHE
        settings.mediaPlaybackRequiresUserGesture = false
        // Required by some captchas that use mixed content
        settings.mixedContentMode = WebSettings.MIXED_CONTENT_COMPATIBILITY_MODE

        wv.webViewClient = object : WebViewClient() {
            override fun shouldOverrideUrlLoading(
                view: WebView,
                request: WebResourceRequest
            ): Boolean {
                // Don't allow the captcha page to navigate away from
                // 127.0.0.1 — any external link would be a bug.
                val url = request.url
                val host = url.host ?: return true
                if (host == "127.0.0.1" || host == "localhost") {
                    return false  // allow internal navigation
                }
                Log.w(TAG, "Blocking external navigation from captcha page: $url")
                return true
            }
        }

        wv.webChromeClient = object : WebChromeClient() {
            override fun onConsoleMessage(consoleMessage: ConsoleMessage): Boolean {
                Log.i(TAG, "[webview-console] ${consoleMessage.message()} (source: ${consoleMessage.sourceId()}:${consoleMessage.lineNumber()})")
                return true
            }
        }

        // Add the JS interface so the page can signal interactive-required.
        wv.addJavascriptInterface(AndroidBridge(), "AndroidBridge")

        // Inject a tiny shim BEFORE the page's own scripts run, so we
        // can intercept calls to instance.show() and notify the host.
        // We do this by wrapping the page's poll/verify loop — but
        // simpler: just inject a script via evaluateJavascript after
        // page load that hooks into the page's window functions.
        wv.webViewClient = object : WebViewClient() {
            override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
                val url = request.url
                val host = url.host ?: return true
                if (host == "127.0.0.1" || host == "localhost") return false
                Log.w(TAG, "Blocking external navigation: $url")
                return true
            }

            override fun onPageFinished(view: WebView, url: String?) {
                super.onPageFinished(view, url)
                Log.i(TAG, "Captcha page loaded — injecting interactive-required hook")
                // Inject a hook that wraps the Aliyun SDK's instance.show()
                // so we get notified when an interactive challenge is
                // requested. The page's verify() function calls
                // instance.show() when the SDK demands interaction.
                view.evaluateJavascript(INTERACTIVE_HOOK_JS, null)
            }
        }

        webView = wv
        val url = "http://127.0.0.1:${ProxyBinary.port}/zcode/captcha/browser?client=$CLIENT_ID"
        Log.i(TAG, "Loading captcha broker page: $url")
        wv.loadUrl(url)
    }

    private fun destroyWebView() {
        val wv = webView ?: return
        try {
            wv.stopLoading()
            wv.removeJavascriptInterface("AndroidBridge")
            (wv.parent as? ViewGroup)?.removeView(wv)
            wv.destroy()
        } catch (e: Exception) {
            Log.w(TAG, "Error destroying WebView", e)
        }
        webView = null

        val container = attachedView ?: return
        try {
            val windowManager = context.getSystemService(Context.WINDOW_SERVICE) as WindowManager
            windowManager.removeView(container)
        } catch (e: Exception) {
            Log.w(TAG, "Error removing view from window manager", e)
        }
        attachedView = null
    }

    /**
     * Opens the system browser at the captcha page so the user can
     * solve an interactive challenge manually. Called from the
     * AndroidBridge JS interface when the Aliyun SDK requires
     * interaction.
     */
    private fun openSystemBrowserForInteractive() {
        val url = "http://127.0.0.1:${ProxyBinary.port}/zcode/captcha/browser?client=$BROWSER_FALLBACK_CLIENT_ID"
        val intent = Intent(Intent.ACTION_VIEW, Uri.parse(url)).apply {
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        }
        try {
            context.startActivity(intent)
            // Post a high-priority notification too, in case the user
            // misses the browser opening.
            ProxyService.notifyCaptchaFromBroker(
                context,
                "[webview] interactive captcha required — opened browser for manual solving"
            )
        } catch (e: Exception) {
            Log.w(TAG, "No browser available for interactive fallback", e)
            ProxyService.notifyCaptchaFromBroker(
                context,
                "[webview] interactive captcha required but no browser available — open $url manually"
            )
        }
    }

    companion object {
        private const val TAG = "CaptchaWebView"

        /** Client name the WebView uses when polling. Different from
         *  the browser fallback so they don't compete for the same
         *  Go channel. */
        private const val CLIENT_ID = "android-webview"

        /** Client name the system browser uses (when interactive
         *  captcha requires manual solving). */
        private const val BROWSER_FALLBACK_CLIENT_ID = "standalone-browser"

        /**
         * Desktop Chrome User-Agent so the Aliyun SDK doesn't try to
         * use a mobile-specific flow that requires user gestures.
         * Realistic Chrome 120 on Windows 10.
         */
        private const val DESKTOP_USER_AGENT =
            "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
            "(KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

        /**
         * JavaScript injected into the captcha page after load. Hooks
         * into the page's `verify()` flow to detect when the Aliyun SDK
         * demands an interactive challenge, and notifies the Android
         * host via the `AndroidBridge` JS interface.
         *
         * The page's verify() function calls `instance.show()` when
         * the SDK requires interaction (see internal/captcha/page.go:100).
         * We monkey-patch any object's `show` method that gets called
         * by intercepting the global `initAliyunCaptcha` callback.
         */
        private val INTERACTIVE_HOOK_JS = """
            (function() {
                console.log('[AndroidBridge] interactive hook installed');
                // Poll for the AndroidBridge availability (in case the
                // JS interface isn't ready when the page first runs).
                if (typeof AndroidBridge === 'undefined') {
                    console.log('[AndroidBridge] not available yet — will retry');
                    return;
                }
                // Override the global initAliyunCaptcha to intercept
                // the instance returned via getInstance callback.
                var origInit = window.initAliyunCaptcha;
                if (typeof origInit !== 'function') {
                    console.log('[AndroidBridge] initAliyunCaptcha not yet defined — deferring');
                    // Try again in 500ms — the SDK loads asynchronously.
                    setTimeout(arguments.callee, 500);
                    return;
                }
                window.initAliyunCaptcha = function(config) {
                    var origGetInstance = config.getInstance;
                    config.getInstance = function(instance) {
                        if (instance && typeof instance.show === 'function') {
                            var origShow = instance.show;
                            instance.show = function() {
                                console.log('[AndroidBridge] instance.show() called — interactive required');
                                try {
                                    AndroidBridge.onInteractiveRequired(
                                        (window.__currentRequestId) || 'unknown'
                                    );
                                } catch (e) {
                                    console.log('[AndroidBridge] error: ' + e);
                                }
                                return origShow.apply(this, arguments);
                            };
                        }
                        if (origGetInstance) return origGetInstance.apply(this, arguments);
                    };
                    return origInit.apply(this, arguments);
                };
                console.log('[AndroidBridge] initAliyunCaptcha hook installed');
            })();
        """.trimIndent()
    }
}
