package com.glmproxy.app

import android.content.Context
import android.net.Uri
import android.util.Log
import androidx.browser.customtabs.CustomTabColorSchemeParams
import androidx.browser.customtabs.CustomTabsIntent

/**
 * Opens the ZCode/Z.ai OAuth login flow in a Chrome Custom Tab.
 *
 * ## Why Chrome Custom Tabs for login (but not for captcha)
 *
 * Chrome Custom Tabs (CCT) is ideal for the OAuth login flow because:
 *
 * 1. **The user needs to see and interact with the page** — they have to
 *    enter credentials, pick a Google account, approve scopes, etc.
 *    Unlike the captcha broker (which runs invisibly in background),
 *    login is inherently interactive.
 *
 * 2. **Chrome has Google account cookies** — when the OAuth flow shows
 *    "Pick an account", Chrome already has the user's Google accounts
 *    loaded from previous logins. A fresh WebView would force the user
 *    to re-enter credentials every time.
 *
 * 3. **OAuth redirects work correctly** — the OAuth callback URL
 *    (`http://127.0.0.1:3005/zcode/auth/login/callback?code=...`) needs
 *    to be handled by the Go proxy running on the device. Chrome Custom
 *    Tabs handle custom schemes and loopback URLs correctly; a WebView
 *    would need a custom WebViewClient to intercept the redirect.
 *
 * 4. **No anti-automation detection** — Google's login flow detects and
 *    blocks WebViews (returns "this browser or app may not be secure").
 *    Chrome Custom Tabs use the real Chrome, so Google's bot detection
 *    doesn't trigger.
 *
 * The captcha WebView is different — it runs the Aliyun SDK which
 * doesn't have WebView-specific blocking, and it needs to run in
 * background (which CCT can't do). So we use WebView for captcha and
 * CCT for login.
 *
 * ## Flow
 *
 * 1. User taps "Login Google" in the activity
 * 2. We POST to /api/admin/auth/login/start on the proxy to initiate
 *    the OAuth flow and get the authorization URL
 * 3. We open that URL in a Chrome Custom Tab
 * 4. User signs in with Google on chat.z.ai
 * 5. chat.z.ai redirects to http://127.0.0.1:3005/zcode/auth/login/callback
 * 6. The Go proxy handles the callback, exchanges the code for tokens,
 *    and stores the account in the encrypted credentials file
 * 7. The Chrome Custom Tab shows the proxy's success page
 * 8. User closes the tab and returns to the app
 */
object GoogleLoginHelper {

    private const val TAG = "GoogleLoginHelper"

    /**
     * Initiates the OAuth login flow by:
     * 1. Calling POST /api/admin/auth/login/start on the proxy to get
     *    the authorization URL
     * 2. Opening that URL in a Chrome Custom Tab
     *
     * Returns true if the CCT was launched, false if the proxy wasn't
     * reachable or no browser was available.
     */
    fun startLogin(context: Context): Boolean {
        val port = ProxyBinary.port
        val startUrl = try {
            fetchLoginStartUrl(port)
        } catch (e: Exception) {
            Log.e(TAG, "Failed to fetch login start URL from proxy", e)
            return false
        }
        if (startUrl.isBlank()) {
            Log.w(TAG, "Proxy returned empty login URL")
            return false
        }
        Log.i(TAG, "Opening OAuth URL in Chrome Custom Tab: $startUrl")
        return openInCustomTab(context, startUrl)
    }

    /**
     * Calls POST /api/admin/auth/login/start on the proxy and returns
     * the authorization URL the user should visit.
     */
    private fun fetchLoginStartUrl(port: Int): String {
        val url = java.net.URL("http://127.0.0.1:$port/api/admin/auth/login/start")
        val conn = (url.openConnection() as java.net.HttpURLConnection).apply {
            connectTimeout = 10000
            readTimeout = 30000
            requestMethod = "POST"
            setRequestProperty("Content-Type", "application/json")
            setRequestProperty("Accept", "application/json")
            doOutput = true
        }
        try {
            conn.outputStream.use { it.write("{}\n".toByteArray()) }
            conn.connect()
            val code = conn.responseCode
            val body = if (code in 200..299) {
                conn.inputStream.bufferedReader().use { it.readText() }
            } else {
                conn.errorStream?.bufferedReader()?.use { it.readText() } ?: ""
            }
            if (code != 200) {
                Log.w(TAG, "Login start failed: HTTP $code — $body")
                return ""
            }
            val json = org.json.JSONObject(body)
            // The proxy returns either { authorizeUrl: "..." } or
            // { url: "..." } — try both. Fall back to an empty string
            // if neither is present (caller will show an error).
            return json.optString("authorizeUrl")
                .ifBlank { json.optString("url") }
                .ifBlank { json.optString("authorizationUrl") }
        } finally {
            conn.disconnect()
        }
    }

    /**
     * Opens the given URL in a Chrome Custom Tab with the app's dark
     * theme. Returns true if a browser was available, false otherwise.
     */
    private fun openInCustomTab(context: Context, url: String): Boolean {
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
        // Try to launch — if no browser supporting CCT is installed,
        // this will throw ActivityNotFoundException.
        return try {
            intent.launchUrl(context, uri)
            true
        } catch (e: Exception) {
            Log.w(TAG, "No Chrome Custom Tab provider available, falling back to ACTION_VIEW", e)
            // Fallback: open in any browser via ACTION_VIEW
            try {
                val fallbackIntent = android.content.Intent(
                    android.content.Intent.ACTION_VIEW,
                    uri
                ).addFlags(android.content.Intent.FLAG_ACTIVITY_NEW_TASK)
                context.startActivity(fallbackIntent)
                true
            } catch (e2: Exception) {
                Log.e(TAG, "No browser available at all", e2)
                false
            }
        }
    }
}
