# Plan 010 — Fix captcha notification never fires on Android 13+ + add swipe-dismiss handling

**Status:** TODO
**Written against commit:** `f7aaab7`
**Source findings:** C-01 (POST_NOTIFICATIONS never requested), C-02 (no deleteIntent)
**Estimated effort:** Small (30–60 minutes)
**Risk of fix:** Low — additive permission flow + one PendingIntent.

## Why this matters

The entire captcha-notification feature (built in commit `f7aaab7`) is
**non-functional on Android 13+** (API 33+). The permission
`android.permission.POST_NOTIFICATIONS` is declared in the manifest
(`AndroidManifest.xml:15`) but never requested at runtime. On Android 13+
apps must request this permission at runtime; without it,
`NotificationManager.notify()` silently drops the notification.

Worse: even on Android ≤12 where the notification does fire, the
debounce flag `captchaPendingStatic` (`ProxyService.kt:192`) is only
cleared when the user **taps** the notification (which opens the dialog,
which calls `clearCaptchaPending`). If the user **swipes** the
notification away instead, `setAutoCancel(true)` cancels the notification
UI but the flag stays `true` forever — every subsequent captcha request
is silently suppressed by the `compareAndSet(false, true)` check at
`ProxyService.kt:45`. The proxy stays blocked with no further alerts
until the service is destroyed.

These two bugs together mean: **the captcha notification feature has
never actually worked for a user on a modern device.**

## Current state

`MainActivity.kt` (post-commit `0d0c3dd`) — the runtime permission
request code that was in the previous WebView-based version was removed
when the activity was rewritten. Grep confirms:

```
$ grep -n "requestPermission\|POST_NOTIFICATIONS" android-app/app/src/main/java/com/glmproxy/app/MainActivity.kt
(no matches)
```

`ProxyService.kt:144-152` — the captcha notification's PendingIntent:

```kotlin
val pendingIntent = PendingIntent.getActivity(
    this,
    REQUEST_CODE_CAPTCHA,
    intent,
    PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
)
```

No `setDeleteIntent` is set on the notification builder
(`ProxyService.kt:154-164`), so swipe-dismiss is invisible to the service.

## Steps

### 1. Request POST_NOTIFICATIONS at runtime in MainActivity.onCreate

In `MainActivity.kt`, after `setContentView(binding.root)`, add:

```kotlin
import android.Manifest
import android.os.Build
import androidx.activity.result.contract.ActivityResultContracts

// Class field
private val requestNotificationPermission =
    registerForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        if (!granted) {
            Toast.makeText(this,
                "Sem permissão de notificações — alertas de captcha não serão exibidos",
                Toast.LENGTH_LONG).show()
        }
    }

// In onCreate, after binding setup
if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
    requestNotificationPermission.launch(Manifest.permission.POST_NOTIFICATIONS)
}
```

### 2. Add a deleteIntent to the captcha notification

The deleteIntent fires when the user swipes the notification away
(or the system auto-dismisses it). Wire it to a broadcast that clears
the pending flag — simplest is to point it at the service itself via
a static action.

In `ProxyService.kt`, add a constant:

```kotlin
const val ACTION_CAPTCHA_DISMISSED = "com.glmproxy.app.action.CAPTCHA_DISMISSED"
```

In `postCaptchaNotification`, build a deleteIntent and attach it:

```kotlin
val deleteIntent = Intent(this, ProxyService::class.java).apply {
    action = ACTION_CAPTCHA_DISMISSED
}
val deletePendingIntent = PendingIntent.getService(
    this,
    REQUEST_CODE_CAPTCHA_DELETE,
    deleteIntent,
    PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
)

val notification = NotificationCompat.Builder(this, CHANNEL_ID_CAPTCHA)
    // ... existing setup ...
    .setDeleteIntent(deletePendingIntent)
    .build()
```

Add the constant:

```kotlin
private const val REQUEST_CODE_CAPTCHA_DELETE = 1002
```

### 3. Handle the dismiss action in onStartCommand

In `ProxyService.onStartCommand`:

```kotlin
override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
    when (intent?.action) {
        ACTION_CAPTCHA_DISMISSED -> {
            // User swiped the notification away — clear the pending flag
            // so future captcha requests can post a fresh notification.
            captchaPendingStatic.set(false)
            // Don't cancel the notification (already gone) — just clear state.
        }
    }
    return START_STICKY
}
```

Replace the existing dead `ACTION_SHOW_CAPTCHA` branch (which is never
triggered, per finding C-11) with this one.

## Files in scope

- `android-app/app/src/main/java/com/glmproxy/app/MainActivity.kt`
- `android-app/app/src/main/java/com/glmproxy/app/ProxyService.kt`

## Files explicitly out of scope

- `AndroidManifest.xml` — permission already declared; no manifest change.
- `ProxyBinary.kt` — no change to log listener or captcha detection.
- Layout XML — no UI change.

## Verification

1. `./gradlew assembleDebug` — build passes.
2. Install on Android 13+ device (or emulator with Google APIs).
3. On first launch, observe the permission dialog asking for notifications.
4. Tap "Allow".
5. Start the proxy, then trigger a captcha (e.g. point any client at
   `http://127.0.0.1:3005/v1/chat/completions` and wait for the upstream
   to require captcha).
6. Verify the high-priority captcha notification appears.
7. **Swipe** the notification away (do not tap). Wait 30s, trigger
   another captcha. Verify a fresh notification appears (the debounce
   flag was cleared by the deleteIntent).
8. Tap the notification this time. Verify the captcha dialog opens
   (the existing `setContentIntent` path still works).
9. On Android ≤12 (or after denying permission on 13+), verify no
   crash; the Toast warning appears on permission denial.

## Test plan

Manual checklist above. Once plan 019 (test scaffold) lands, add an
automated Robolectric test that:
- Registers a ShadowNotificationManager
- Posts a fake captcha log line
- Asserts exactly one notification fired
- Simulates swipe-dismiss (invoke the deleteIntent)
- Posts another captcha line
- Asserts a second notification fired (debounce cleared)

## Maintenance notes

- The deleteIntent uses `PendingIntent.getService` which starts the
  service with the dismiss action. This is fine because the service is
  already running (foreground) — `onStartCommand` is invoked with the
  new intent without spawning a second instance.
- If the user denies notification permission, the captcha feature is
  unusable. The Toast warning is the best we can do; a future settings
  screen (deferred plan 002) could surface this state persistently.
- When DIR-02 (replace log-scraping with `/zcode/captcha/poll`) lands,
  the deleteIntent mechanism stays the same — only the trigger changes.

## Escape hatches

- If `registerForActivityResult` is not available (older
  appcompat-androidx combo), fall back to
  `ActivityCompat.requestPermission` with a `onRequestPermissionsResult`
  callback. Stop and report back; do not silently skip the request.
- If the deleteIntent doesn't fire on some OEM ROMs (Xiaomi/Huawei are
  known to deviate from AOSP notification behavior), add a fallback
  timeout: schedule a one-shot AlarmManager tick 60s after posting the
  captcha notification that clears `captchaPendingStatic` regardless.
  This is heavier — only add if users report missed re-alerts.
