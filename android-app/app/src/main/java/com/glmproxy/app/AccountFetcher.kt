package com.glmproxy.app

import android.util.Log
import org.json.JSONObject
import java.io.BufferedReader
import java.io.InputStreamReader
import java.net.HttpURLConnection
import java.net.URL

/**
 * Fetches account and queue information from the Go proxy's admin API.
 *
 * The Go proxy exposes:
 * - GET /api/admin/accounts → list of all configured accounts with quota/status
 * - GET /api/admin/queue → current request queue snapshot
 * - GET /api/admin/overview → summary (active account, model, total quota, etc.)
 *
 * This helper calls these endpoints and returns parsed data for the UI
 * to display in the accounts card.
 */
object AccountFetcher {

    private const val TAG = "AccountFetcher"

    data class AccountInfo(
        val id: String,
        val label: String,
        val active: Boolean,
        val model: String,
        val quotaUsed: Long,
        val quotaTotal: Long,
        val quotaPercent: Int,
        val status: String,
        val raw: JSONObject
    )

    data class QueueEntry(
        val accountLabel: String,
        val model: String,
        val depth: Int,
        val raw: JSONObject
    )

    data class ProxyState(
        val accounts: List<AccountInfo>,
        val queue: List<QueueEntry>,
        val activeAccountId: String?,
        val activeModel: String?,
        val rawOverview: JSONObject?
    )

    /**
     * Fetches the full state (overview + accounts + queue) in one call.
     * Returns null if the proxy isn't reachable.
     */
    fun fetchState(port: Int): ProxyState? {
        val overview = fetchJson(port, "/api/admin/overview") ?: return null
        val accountsJson = fetchJson(port, "/api/admin/accounts")
        val queueJson = fetchJson(port, "/api/admin/queue")

        val accounts = mutableListOf<AccountInfo>()
        if (accountsJson != null) {
            val arr = accountsJson.optJSONArray("data") ?: accountsJson.optJSONArray("accounts")
            if (arr != null) {
                for (i in 0 until arr.length()) {
                    val obj = arr.getJSONObject(i)
                    accounts.add(parseAccount(obj))
                }
            }
        }

        val queue = mutableListOf<QueueEntry>()
        if (queueJson != null) {
            val arr = queueJson.optJSONArray("data") ?: queueJson.optJSONArray("entries") ?: queueJson.optJSONArray("queue")
            if (arr != null) {
                for (i in 0 until arr.length()) {
                    val obj = arr.getJSONObject(i)
                    queue.add(QueueEntry(
                        accountLabel = obj.optString("accountLabel", obj.optString("label", "?")),
                        model = obj.optString("model", "?"),
                        depth = obj.optInt("depth", obj.optInt("pending", 0)),
                        raw = obj
                    ))
                }
            }
        }

        val activeAccount = overview.optJSONObject("activeAccount")
        return ProxyState(
            accounts = accounts,
            queue = queue,
            activeAccountId = activeAccount?.optString("id"),
            activeModel = overview.optString("model", overview.optString("activeModel")),
            rawOverview = overview
        )
    }

    private fun parseAccount(obj: JSONObject): AccountInfo {
        val id = obj.optString("id", "?")
        val label = obj.optString("label", obj.optString("email", obj.optString("name", id)))
        val active = obj.optBoolean("active", obj.optBoolean("isActive", false))
        val model = obj.optString("model", obj.optString("activeModel", "glm-5.2"))
        val quota = obj.optJSONObject("quota") ?: obj.optJSONObject("usage")
        val quotaUsed = quota?.optLong("used", quota.optLong("tokensUsed", 0)) ?: 0
        val quotaTotal = quota?.optLong("total", quota.optLong("limit", quota.optLong("tokensTotal", 0))) ?: 0
        val quotaPercent = if (quotaTotal > 0) ((quotaUsed * 100) / quotaTotal).toInt() else 0
        val status = obj.optString("status", obj.optString("state", if (active) "active" else "idle"))
        return AccountInfo(id, label, active, model, quotaUsed, quotaTotal, quotaPercent, status, obj)
    }

    private fun fetchJson(port: Int, path: String): JSONObject? {
        val url = URL("http://127.0.0.1:$port$path")
        val conn = (url.openConnection() as HttpURLConnection).apply {
            connectTimeout = 3000
            readTimeout = 5000
            requestMethod = "GET"
            setRequestProperty("Accept", "application/json")
        }
        try {
            conn.connect()
            val code = conn.responseCode
            if (code != 200) {
                Log.w(TAG, "$path returned HTTP $code")
                return null
            }
            val body = BufferedReader(InputStreamReader(conn.inputStream)).use { it.readText() }
            return JSONObject(body)
        } catch (e: Exception) {
            Log.w(TAG, "Failed to fetch $path: ${e.message}")
            return null
        } finally {
            conn.disconnect()
        }
    }
}
