package com.glmproxy.app

import android.content.Context
import android.util.Log
import java.io.File
import java.io.IOException
import java.util.concurrent.atomic.AtomicReference

/**
 * Manages the lifecycle of the embedded Go proxy binary on Android.
 *
 * ## Why `nativeLibraryDir` (not `assets/` or `filesDir`)
 *
 * Since Android 10 (API 29), the SELinux policy forbids `execve()` on any
 * file inside `filesDir`, `cacheDir`, or extracted `assets/`. The rule is:
 *   `neverallow ... app_data_file:file execute;`
 *
 * The only writable-ish location with execute permission is the directory
 * where the system installs the app's native libraries — typically
 * `/data/app/<pkg>/lib/arm64/`. Files there have the SELinux label
 * `apk_data_file` (not `app_data_file`) and are world-executable.
 *
 * To get the Go binary there, we **disguise it as a `.so` library**:
 *   1. At build time, the Go binary is placed at
 *      `app/src/main/jniLibs/arm64-v8a/libglmproxy.so` (prefix `lib` +
 *      suffix `.so` are mandatory — the package manager ignores anything
 *      else).
 *   2. The manifest declares `android:extractNativeLibs="true"` and
 *      `build.gradle.kts` sets `useLegacyPackaging = true` so the binary
 *      is actually extracted to the filesystem (not memory-mapped from
 *      the APK, which would not be `execve`-able either).
 *   3. At runtime we look up `context.applicationInfo.nativeLibraryDir`
 *      and `ProcessBuilder(libglmproxy.so)` from there.
 *
 * The Go binary itself doesn't need to be a real shared library — it just
 * has to be a PIE ELF executable. The package manager doesn't validate
 * the header.
 *
 * This is the same trick Termux, ReTerminal, and UserLAnd use to ship
 * native binaries on modern Android without root.
 */
object ProxyBinary {
    private const val TAG = "ProxyBinary"
    private const val LIB_NAME = "libglmproxy.so"
    const val DEFAULT_PORT = 3005

    private val processRef = AtomicReference<Process?>(null)

    val port: Int
        get() = DEFAULT_PORT

    /**
     * Returns the absolute path of the embedded Go binary, as installed by
     * the package manager under `nativeLibraryDir`. Does NOT extract or
     * install anything — the system already did that at APK install time.
     *
     * Throws [IOException] if the binary isn't there (which would indicate
     * a broken build or unsupported ABI).
     */
    @Throws(IOException::class)
    fun binaryPath(context: Context): String {
        val nativeDir = context.applicationInfo.nativeLibraryDir
        val binary = File(nativeDir, LIB_NAME)
        if (!binary.exists()) {
            throw IOException(
                "Go binary not found at ${binary.absolutePath}. " +
                "nativeLibraryDir=$nativeDir, ABI=${
                    android.os.Build.SUPPORTED_ABIS.joinToString(",")
                }"
            )
        }
        return binary.absolutePath
    }

    /**
     * Starts the proxy as a child process with the environment variables
     * required for Android sandbox compatibility. Idempotent — if a process
     * is already running, returns without doing anything.
     */
    @Throws(IOException::class)
    fun start(context: Context): Process {
        processRef.get()?.let { existing ->
            if (existing.isAlive) return existing
            processRef.set(null)
        }

        val binary = binaryPath(context)
        val dataDir = File(context.filesDir, ".glm5.2proxy").apply { mkdirs() }

        val pb = ProcessBuilder(binary)
        pb.directory(context.filesDir)
        pb.redirectErrorStream(true)

        // Environment overrides for Android sandbox:
        // - Data dir: app-private storage (always writable, never wiped by OS
        //   while the app is installed).
        // - Host: loopback only — never expose the proxy to the network.
        // - Port: matches the port the WebView will hit.
        // - Captcha headless browser: disabled on Android (no Chrome/Edge
        //   binary available; users solve captchas interactively via WebView).
        // - Account creator: disabled (depends on PowerShell on Windows).
        val env = pb.environment()
        env["ZCODE_PROXY_DATA_DIR"] = dataDir.absolutePath
        env["ZCODE_PROXY_HOST"] = "127.0.0.1"
        env["ZCODE_PROXY_PORT"] = port.toString()
        env["ZCODE_CAPTCHA_ENABLED"] = "0"
        env["ZCODE_HEADLESS_ENABLED"] = "0"
        env["ZCODE_ACCOUNT_CREATOR_ENABLED"] = "0"
        // Force TMPDIR into app-private storage (default /tmp doesn't exist
        // on Android; the Go runtime uses it for some tempfile operations).
        val tmpDir = File(context.cacheDir, "tmp").apply { mkdirs() }
        env["TMPDIR"] = tmpDir.absolutePath

        val process = pb.start()

        // Drain stdout/stderr on a background thread so the process doesn't
        // deadlock on buffer full. Log to logcat for debuggability.
        Thread({
            process.inputStream.bufferedReader().useLines { lines ->
                lines.forEach { line -> Log.i(TAG, "[go] $line") }
            }
        }, "proxy-stdout").start()

        processRef.set(process)
        Log.i(TAG, "Started Go proxy on 127.0.0.1:$port (process=$process)")
        return process
    }

    /**
     * Gracefully stops the proxy. Sends SIGTERM first, then SIGKILL after a
     * short grace period if the process hasn't exited.
     */
    fun stop() {
        processRef.getAndSet(null)?.let { process ->
            try {
                process.destroy()
                if (!process.waitFor(2, java.util.concurrent.TimeUnit.SECONDS)) {
                    process.destroyForcibly()
                    process.waitFor(1, java.util.concurrent.TimeUnit.SECONDS)
                }
                Log.i(TAG, "Stopped Go proxy")
            } catch (e: Exception) {
                Log.w(TAG, "Error stopping Go proxy", e)
            }
        }
    }

    /**
     * Returns true if the proxy process is currently running.
     */
    fun isRunning(): Boolean = processRef.get()?.isAlive == true
}
