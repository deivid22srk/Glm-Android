# Plan 016 — Fix exported-activity phishing vector: validate EXTRA_CAPTCHA_URL scheme

**Status:** TODO
**Written against commit:** `f7aaab7`
**Source finding:** SEC-03
**Estimated effort:** Small (30 minutes)
**Risk of fix:** Low — narrows accepted URLs to a strict allowlist.

## Why this matters

`MainActivity` is declared `android:exported="true"` (required for the
launcher icon) at `AndroidManifest.xml:34-45`. The activity processes
the `ACTION_SHOW_CAPTCHA` intent from **any caller** — including
third-party apps on the same device.

When such an intent arrives, `MainActivity.maybeShowCaptchaDialogFromIntent`
(`MainActivity.kt:146-153`) reads two extras without validation:

```kotlin
val log = intent.getStringExtra(ProxyService.EXTRA_CAPTCHA_LOG) ?: ""
val url = intent.getStringExtra(ProxyService.EXTRA_CAPTCHA_URL)
    ?: "http://127.0.0.1:${ProxyBinary.port}/zcode/captcha/browser?client=standalone-browser"
showCaptchaDialog(log, url)
```

The `url` is then forwarded verbatim to the system browser when the
user taps "Abrir no navegador" (`MainActivity.kt:178-186`):

```kotlin
dialogView.findViewById<...>(R.id.btn_captcha_open)
    .setOnClickListener {
        startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)))
    }
```

**Attack scenario**: a malicious app constructs an explicit intent to
`com.glmproxy.app/.MainActivity` with `action=SHOW_CAPTCHA` and an
attacker-chosen `captcha_url` (any scheme: `http(s)://`, `intent://`,
`content://`, `file://`, `javascript:`). The dialog looks identical to
the trusted Z.ai captcha prompt (same title, same button labels, same
icon), so the user is primed to tap "Abrir" and land on an
attacker-controlled URL. The `captcha_log` extra is also rendered as
the dialog body, allowing arbitrary text injection.

This is a classic phishing-from-exported-activity vector.

## Current state

`AndroidManifest.xml:34-45`:
```xml
<activity
    android:name=".MainActivity"
    android:exported="true"
    android:launchMode="singleTop"
    ...>
```

`MainActivity.kt:146-153` reads extras without validation.
`MainActivity.kt:178-186` opens the URL without validation.

## Steps

### 1. Add a URL validation helper in MainActivity

Add a private method that strictly validates the captcha URL:

```kotlin
/**
 * Returns the captcha URL only if it points to the local proxy's
 * captcha endpoint. Returns null otherwise — callers must fall back
 * to the default URL.
 *
 * This guards against the exported-activity phishing vector where a
 * third-party app could send ACTION_SHOW_CAPTCHA with an arbitrary
 * URL extra, tricking the user into opening an attacker-controlled
 * page disguised as the trusted captcha dialog.
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
```

### 2. Use the validator in maybeShowCaptchaDialogFromIntent

```kotlin
private fun maybeShowCaptchaDialogFromIntent(intent: Intent?) {
    if (intent?.action == ProxyService.ACTION_SHOW_CAPTCHA) {
        val log = intent.getStringExtra(ProxyService.EXTRA_CAPTCHA_LOG) ?: ""
        val url = sanitizeCaptchaUrl(intent.getStringExtra(ProxyService.EXTRA_CAPTCHA_URL))
        showCaptchaDialog(log, url)
    }
}
```

### 3. Sanitize the log extra too

The `captcha_log` extra is rendered as the dialog body
(`MainActivity.kt:168-169`). An attacker could inject misleading text
("Your account will be deleted in 5 minutes. Tap here to confirm.").
Cap its length and strip control characters:

```kotlin
private fun sanitizeCaptchaLog(input: String?): String {
    if (input.isNullOrBlank()) return "(sem log detalhado)"
    // Cap at 500 chars to prevent dialog overflow
    val capped = if (input.length > 500) input.take(500) + "…" else input
    // Strip control characters (newlines/tabs allowed)
    return capped.replace(Regex("[\\x00-\\x08\\x0B\\x0C\\x0E-\\x1F]"), "")
}
```

Use it in `maybeShowCaptchaDialogFromIntent`:

```kotlin
val log = sanitizeCaptchaLog(intent.getStringExtra(ProxyService.EXTRA_CAPTCHA_LOG))
```

### 4. (Optional, defense in depth) Restrict the activity's exported-ness

If we never need third-party apps to launch `MainActivity` directly
(the launcher icon works through `ACTION_MAIN` which is always allowed
for exported activities), we could restrict the intent-filter. But
since the launcher intent-filter requires `exported=true`, the fix
above (input validation) is the correct mitigation. No manifest change.

## Files in scope

- `android-app/app/src/main/java/com/glmproxy/app/MainActivity.kt`

## Files explicitly out of scope

- `AndroidManifest.xml` — no change; `exported=true` is required for
  the launcher icon.
- `ProxyService.kt` — the service constructs the intent correctly; the
  vulnerability is only in the consumer (MainActivity) accepting
  arbitrary extras.

## Verification

1. `./gradlew assembleDebug` — build passes.
2. Install the app.
3. Verify the legitimate path still works: trigger a real captcha via
   the Go proxy, tap the notification, confirm the dialog opens with
   the correct URL and log.
4. Verify the attack path is blocked. From `adb shell`:
   ```
   am start -n com.glmproxy.app.debug/com.glmproxy.app.MainActivity \
     -a com.glmproxy.app.action.SHOW_CAPTCHA \
     --es captcha_url "https://evil.example.com/phish" \
     --es captcha_log "Your account will be deleted"
   ```
   The dialog should open with the **default** URL
   (`http://127.0.0.1:3005/zcode/captcha/browser?client=standalone-browser`)
   instead of `evil.example.com`, and the log should be the sanitized
   version of the injected text.
5. Verify a loopback URL with the wrong path is also rejected:
   ```
   am start -n com.glmproxy.app.debug/com.glmproxy.app.MainActivity \
     -a com.glmproxy.app.action.SHOW_CAPTCHA \
     --es captcha_url "http://127.0.0.1:3005/admin/delete-everything"
   ```
   Should fall back to the default captcha URL.

## Test plan

Manual verification above. Once plan 019 (test scaffold) lands, add a
JVM unit test for `sanitizeCaptchaUrl` and `sanitizeCaptchaLog`
covering:
- null/blank input → default
- valid loopback captcha URL → passthrough
- valid loopback non-captcha path → default
- non-loopback host → default
- non-http scheme (intent://, content://, file://) → default
- log > 500 chars → truncated
- log with control chars → stripped

## Maintenance notes

- The port comparison uses `ProxyBinary.port` (currently hardcoded to
  3005). If a future settings screen allows custom ports, update the
  validator to read the configured port.
- The path allowlist is `/zcode/captcha/`. If the Go side adds a new
  captcha endpoint path, extend the validator.

## Escape hatches

- If `Uri.parse` returns unexpected results for some schemes (e.g.
  `javascript:` parsing as a valid URL with empty host), the validator
  already handles it via the `isLoopback` check. But if a new edge
  case appears, the safest fallback is to always use the default URL
  when in doubt — never use the extra. The `else` branch of the
  validator already does this.
