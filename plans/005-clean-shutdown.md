# Plan 005 — Stop proxy cleanly on app removal (onTaskRemoved)

**Status:** TODO
**Written against commit:** `3dc085b`
**Estimated effort:** Small (15 minutes)
**Risk of fix:** Low — single-method change.

## Why this matters

`ProxyService.onTaskRemoved` already calls `stopSelf()`, which triggers
`onDestroy()` and `ProxyBinary.stop()`. **But** `ProxyBinary.stop()` uses
`process.destroy()` which on Linux/Android sends `SIGTERM` — and the Go
binary's `signal.NotifyContext(SIGTERM)` handler in `cmd/server/main.go`
shuts down gracefully. Good.

The bug is more subtle: `waitFor(2, SECONDS)` returns `false` if the Go
binary takes >2s to drain in-flight HTTP requests on shutdown, and then
`destroyForcibly()` sends `SIGKILL` — killing the proxy mid-stream,
potentially leaving the upstream ZCode API in a half-consumed state.

## Current state

`ProxyBinary.kt:133-145`:

```kotlin
fun stop() {
    processRef.getAndSet(null)?.let { process ->
        try {
            process.destroy()                                      // SIGTERM
            if (!process.waitFor(2, java.util.concurrent.TimeUnit.SECONDS)) {
                process.destroyForcibly()                          // SIGKILL
                process.waitFor(1, java.util.concurrent.TimeUnit.SECONDS)
            }
            Log.i(TAG, "Stopped Go proxy")
        } catch (e: Exception) {
            Log.w(TAG, "Error stopping Go proxy", e)
        }
    }
}
```

`cmd/server/main.go` shutdown handler uses `service.Shutdown(ctx)` with a
**10-second** context timeout (see `internal/app/app.go:77`). So we are
SIGKILLing processes that would have finished cleanly in up to 10s.

## Steps

### 1. Increase the SIGTERM grace period to 12 seconds

In `ProxyBinary.kt`, change the `waitFor` timeout from 2s to 12s:

```kotlin
fun stop() {
    processRef.getAndSet(null)?.let { process ->
        try {
            process.destroy()
            if (!process.waitFor(12, java.util.concurrent.TimeUnit.SECONDS)) {
                Log.w(TAG, "Go proxy did not exit after 12s, sending SIGKILL")
                process.destroyForcibly()
                process.waitFor(1, java.util.concurrent.TimeUnit.SECONDS)
            }
            Log.i(TAG, "Stopped Go proxy")
        } catch (e: Exception) {
            Log.w(TAG, "Error stopping Go proxy", e)
        }
    }
}
```

### 2. Call `stop()` from `onTaskRemoved` directly (not via stopSelf)

In `ProxyService.kt`, change `onTaskRemoved`:

```kotlin
override fun onTaskRemoved(rootIntent: Intent?) {
    ProxyBinary.stop()      // explicit, synchronous-ish
    stopSelf()
    super.onTaskRemoved(rootIntent)
}
```

This avoids a race where `onDestroy` runs after `stopSelf` returns and
the OS has already reaped the process.

## Files in scope

- `android-app/app/src/main/java/com/glmproxy/app/ProxyBinary.kt`
- `android-app/app/src/main/java/com/glmproxy/app/ProxyService.kt`

## Files explicitly out of scope

- `internal/api/server.go` — Go-side shutdown timeout stays at 10s.
- `cmd/server/main.go` — signal handling unchanged.

## Verification

1. Build and install: `./gradlew assembleDebug && adb install -r app-debug.apk`.
2. Launch the app, wait for panel to load, start a streaming chat
   completion from another client on the device.
3. Swipe the app away from recents.
4. Within ~12 seconds, logcat should show:
   ```
   I/ProxyBinary: [go] ... shutdown complete
   I/ProxyBinary: Stopped Go proxy
   ```
5. `adb shell ps | grep glm5.2proxy` — no process listed.

## Test plan

No automated test. Add to the smoke checklist in plan #004.

## Maintenance notes

- If the Go binary's shutdown timeout is later increased beyond 12s,
  bump this grace period to match (Go timeout + 2s).
- If users complain that swiping away the app "takes too long" before
  the proxy dies, that's expected — graceful drain is more important
  than instant kill.

## Escape hatches

- If `waitFor(12s)` causes an ANR dialog on Android 12+ when the user
  swipes away, move the `stop()` call to a background coroutine.
  Stop and report back; do not silently reduce the timeout.
