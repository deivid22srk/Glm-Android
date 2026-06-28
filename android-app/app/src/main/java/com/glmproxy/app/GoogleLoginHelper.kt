package com.glmproxy.app

import android.content.Context
import android.util.Log
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL

/**
 * Initiates the ZCode/Z.ai OAuth login flow.
 *
 * ## How it works
 *
 * 1. Calls `POST /api/admin/auth/login/start` on the proxy to get the
 *    authorization URL.
 * 2. Launches [WebViewActivity] with that URL — the user signs in
 *    with Google directly inside the app's WebView.
 * 3. The OAuth flow redirects through chat.z.ai → accounts.google.com →
 *    back to `http://127.0.0.1:3005/zcode/auth/login/callback`.
 * 4. The Go proxy handles the callback, exchanges the code for tokens,
 *    and stores the account in the encrypted credentials file.
 * 5. The WebView shows the proxy's success page.
 * 6. The user presses back to return to MainActivity.
 *
 * ## Why WebView (not Chrome Custom Tab)
 *
 * The user explicitly requested WebView support so the login happens
 * inside the app — no context switch to an external browser. The
 * WebViewActivity uses a desktop User-Agent so Google's login page
 * doesn't block it with the "this browser or app may not be secure"
 * error.
 *
 * If Google tightens detection and starts blocking the WebView, the
 * user can fall back to the "Abrir no navegador" button on the main
 * screen which opens the system browser (Chrome Custom Tab) where
 * Google's bot detection doesn't trigger.
 */
object GoogleLoginHelper {

    private const val TAG = "GoogleLoginHelper"

    /**
     * Initiates the OAuth login flow by:
     * 1. Calling POST /api/admin/auth/login/start on the proxy to get
     *    the authorization URL
     * 2. Launching WebViewActivity with that URL
     *
     * Returns true if the WebView was launched, false if the proxy
     * wasn't reachable or returned an empty URL.
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
        Log.i(TAG, "Launching WebViewActivity with OAuth URL: $startUrl")
        WebViewActivity.start(context, startUrl, title = "Login Google")
        return true
    }

    /**
     * Calls POST /api/admin/auth/login/start on the proxy and returns
     * the authorization URL the user should visit.
     */
    private fun fetchLoginStartUrl(port: Int): String {
        val url = URL("http://127.0.0.1:$port/api/admin/auth/login/start")
        val conn = (url.openConnection() as HttpURLConnection).apply {
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
            val json = JSONObject(body)
            // The proxy returns either { authorizeUrl: "..." } or
            // { url: "..." } — try both.
            return json.optString("authorizeUrl")
                .ifBlank { json.optString("url") }
                .ifBlank { json.optString("authorizationUrl") }
        } finally {
            conn.disconnect()
        }
    }
}
