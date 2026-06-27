# Plan 003 — Capture and surface Go binary crash logs in the UI

**Status:** TODO
**Written against commit:** `3dc085b`
**Estimated effort:** Small (1–2 hours)
**Risk of fix:** Low — additive only, no changes to existing flows.

## Why this matters

When the Go proxy crashes on Android (e.g. port conflict, upstream API
change, panic), the user sees a stuck "Iniciando proxy…" spinner for 30
seconds and then a generic error. The actual Go stacktrace is buried in
logcat, which a normal user will never open.

This plan adds a "Ver logs do proxy" button to the toolbar that opens a
bottom sheet showing the last N lines of stdout/stderr captured from the
Go process. It is the single highest-leverage debug aid for field issues.

## Current state

`ProxyBinary.kt:118-122` already drains stdout to logcat:

```kotlin
Thread({
    process.inputStream.bufferedReader().useLines { lines ->
        lines.forEach { line -> Log.i(TAG, "[go] $line") }
    }
}, "proxy-stdout").start()
```

It discards the lines after logging. We need to also retain them in a
ring buffer.

## Steps

### 1. Add a ring buffer in `ProxyBinary.kt`

In `ProxyBinary.kt`, add:

```kotlin
private const val LOG_BUFFER_LINES = 1000
private val logBuffer = java.util.concurrent.ConcurrentLinkedDeque<String>()

fun recentLogs(): List<String> = logBuffer.toList()

private fun appendLog(line: String) {
    logBuffer.addLast(line)
    while (logBuffer.size > LOG_BUFFER_LINES) {
        logBuffer.pollFirst()
    }
}
```

### 2. Wire the drain thread to the buffer

Change the drain thread in `start()`:

```kotlin
Thread({
    process.inputStream.bufferedReader().useLines { lines ->
        lines.forEach { line ->
            appendLog(line)
            Log.i(TAG, "[go] $line")
        }
    }
    // Process exited — record that too
    val exitCode = process.waitFor()
    appendLog("[proxy] process exited with code $exitCode")
}, "proxy-stdout").start()
```

### 3. Add a logs bottom sheet layout

Create `android-app/app/src/main/res/layout/bottom_sheet_logs.xml`:

```xml
<?xml version="1.0" encoding="utf-8"?>
<LinearLayout xmlns:android="http://schemas.android.com/apk/res/android"
    android:layout_width="match_parent"
    android:layout_height="wrap_content"
    android:orientation="vertical"
    android:maxHeight="480dp"
    android:padding="16dp"
    android:background="@color/bg_panel">

    <TextView
        android:layout_width="wrap_content"
        android:layout_height="wrap_content"
        android:text="Logs do proxy"
        android:textColor="@color/text_primary"
        android:textStyle="bold"
        android:textSize="16sp" />

    <ScrollView
        android:layout_width="match_parent"
        android:layout_height="0dp"
        android:layout_weight="1"
        android:layout_marginTop="12dp">

        <TextView
            android:id="@+id/logs_text"
            android:layout_width="match_parent"
            android:layout_height="wrap_content"
            android:fontFamily="monospace"
            android:textSize="11sp"
            android:textColor="@color/text_secondary"
            android:textIsSelectable="true" />
    </ScrollView>

    <com.google.android.material.button.MaterialButton
        android:id="@+id/btn_copy_logs"
        android:layout_width="wrap_content"
        android:layout_height="wrap_content"
        android:layout_marginTop="8dp"
        android:text="Copiar logs" />
</LinearLayout>
```

### 4. Add menu item and bottom sheet dialog in `MainActivity.kt`

Add to `res/menu/main_menu.xml`:

```xml
<item
    android:id="@+id/action_view_logs"
    android:title="Ver logs do proxy"
    app:showAsAction="never" />
```

In `MainActivity.onOptionsItemSelected`, add:

```kotlin
R.id.action_view_logs -> {
    val view = layoutInflater.inflate(R.layout.bottom_sheet_logs, null)
    val text = view.findViewById<TextView>(R.id.logs_text)
    text.text = ProxyBinary.recentLogs().joinToString("\n")
    val dialog = com.google.android.material.bottomsheet.BottomSheetDialog(this)
    dialog.setContentView(view)
    view.findViewById<Button>(R.id.btn_copy_logs).setOnClickListener {
        val clipboard = getSystemService(CLIPBOARD_SERVICE) as android.content.ClipboardManager
        clipboard.setPrimaryClip(android.content.ClipData.newPlainText("logs", text.text))
        android.widget.Toast.makeText(this, "Logs copiados", android.widget.Toast.LENGTH_SHORT).show()
    }
    dialog.show()
    true
}
```

### 5. Trigger error overlay with last log lines when health poll fails

In `MainActivity.pollProxyHealth`, change the failure path to include logs:

```kotlin
handler.post {
    val lastLogs = ProxyBinary.recentLogs().takeLast(30).joinToString("\n")
    showFatalError("Proxy não respondeu após $MAX_HEALTH_ATTEMPTS tentativas.\n\nÚltimos logs:\n$lastLogs")
}
```

(Note: `error_text` in `activity_main.xml` may need `android:scrollbars="vertical"` and `android:textIsSelectable="true"` to handle multi-line logs.)

## Files in scope

- `android-app/app/src/main/java/com/glmproxy/app/ProxyBinary.kt`
- `android-app/app/src/main/java/com/glmproxy/app/MainActivity.kt`
- `android-app/app/src/main/res/layout/bottom_sheet_logs.xml` (new)
- `android-app/app/src/main/res/menu/main_menu.xml`

## Files explicitly out of scope

- `internal/**/*.go` — no Go changes; the binary keeps printing to stdout.
- `ProxyService.kt` — the service doesn't touch the buffer.

## Verification

1. `./gradlew assembleDebug` — build passes.
2. Install on device, launch app, wait for panel to load.
3. Tap toolbar overflow → "Ver logs do proxy" → bottom sheet shows Go
   startup lines (e.g. `Go proxy listening on http://127.0.0.1:3005`).
4. Force a crash: `adb shell am force-stop com.glmproxy.app.debug`, then
   re-launch, then `adb shell run-as com.glmproxy.app.debug pkill -SIGSEGV
   glm5.2proxy-server` (or just wait for the proxy to fail). Re-open the
   logs sheet — the last lines should contain the Go panic stacktrace.
5. Tap "Copiar logs" — clipboard contains the same text.

## Test plan

No new automated tests in this plan — UI behavior is hard to assert
without an instrumentation test harness, which is plan #004.

Manual smoke test checklist is in the verification section above.

## Maintenance notes

- If `LOG_BUFFER_LINES` is increased beyond ~5000, switch to a
  `CircularBuffer` from a library to avoid GC pressure.
- If you add more environment overrides to `ProxyBinary.start`, log them
  (masking secrets) so the logs sheet is self-documenting.
- When plan #002 (settings screen) lands, the port and host should also
  be echoed to the log buffer on startup.

## Escape hatches

- If `ConcurrentLinkedDeque` causes ANRs on low-end devices (unlikely
  but possible at 1000 lines), downgrade to a fixed-size array with a
  write index — stop and report back before optimizing further.
- If `waitFor()` blocks the drain thread forever (because the process
  is killed without closing stdout), wrap it in a 5-second timeout and
  log "[proxy] drain thread timed out".
