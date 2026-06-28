# GLM Proxy Android

Native Android wrapper for [glm5.2proxy](./) — a local Go proxy that exposes
the Z.ai/ZCode Start Plan as an OpenAI-compatible API and ships a React panel
for account management.

This repository contains:

- **`/`** (root) — original Go source of `glm5.2proxy`, with `cmd/server`
  modified to embed and serve the React panel at `/` (so it works headless
  on Android without the Wails desktop shell).
- **`cmd/server/`** — Go entrypoint that embeds `cmd/desktop/frontend/dist`
  via `//go:embed` and serves it through the same HTTP server that already
  powers the OpenAI-compatible API.
- **`android-app/`** — minimal Kotlin Android app (no Compose, no WebView) that:
  - Locates the embedded Go binary from `nativeLibraryDir` (where Android
    extracts `libglmproxy.so` from `jniLibs/arm64-v8a/` at install time).
  - Starts it as a child process via `ProcessBuilder` (no terminal, no shell).
  - Polls `http://127.0.0.1:3005/health` until the proxy is ready.
  - Renders a native Material 3 control surface (status card, URL card,
    logs card, captcha dialog) — the React panel is opened in the system
    browser via "Abrir no navegador" when the user needs account management.
  - Runs as a foreground service so the proxy stays alive when the user
    navigates away from the app.
  - Posts a high-priority captcha notification when the Go proxy reports
    `captcha.browser_missing`; tapping it opens a dialog with the URL.
- **`.github/workflows/build.yml`** — CI that builds the frontend, compiles
  the Go binary for `linux/arm64` (Android-compatible), and assembles a
  signed APK.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                       Android APK                            │
│                                                              │
│  ┌────────────────────────────┐    ┌─────────────────────┐  │
│  │  MainActivity (Kotlin)     │    │  ProxyService       │  │
│  │  Material 3 cards:         │    │  (foreground)       │  │
│  │   - Status dot + text      │    │                     │  │
│  │   - URL + copy/open btns   │    │  ProcessBuilder     │  │
│  │   - Error                  │    │     .start()        │  │
│  │   - Logs (last 500 lines)  │    │        │            │  │
│  │   - Toggle button          │    │        ▼            │  │
│  │                            │    │  ┌──────────────┐   │  │
│  │  + Captcha dialog          │    │  │ Go binary    │   │  │
│  │    (from notif tap)        │    │  │ (cmd/server) │   │  │
│  └─────────────┬──────────────┘    │  │              │   │  │
│                │ "Abrir navegador" │  │ HTTP :3005   │   │  │
│                ▼                    │  │ React panel  │   │  │
│  ┌────────────────────────────┐    │  │ /v1/* API    │   │  │
│  │  System browser            │    │  └──────┬───────┘   │  │
│  │  loads http://127.0.0.1:3k │    └─────────┼───────────┘  │
│  │  → React panel             │              │              │
│  └────────────────────────────┘              │              │
└──────────────────────────────────────────────┼──────────────┘
                                               │
                                               ▼
                          https://zcode.z.ai  (Z.ai upstream)
```

The Go binary is the **exact same code** that powers the desktop Wails app.
The only difference is the shell around it: Wails on desktop, native
Material 3 Kotlin UI on Android. No rewrite, no terminal, no `proot` —
just a normal child process that happens to be a Go HTTP server.

## How the Go binary is shipped (important)

**Android 10+ blocks execution of arbitrary binaries from `assets/` or
`filesDir/`.** The SELinux rule `neverallow app_data_file:file execute`
makes `execve()` fail with `EACCES` on any file in app-private storage.

The workaround (same one Termux and ReTerminal use) is to **disguise the Go
binary as a native library**:

1. The binary is placed at `app/src/main/jniLibs/arm64-v8a/libglmproxy.so`
   (the `lib` prefix + `.so` suffix are mandatory — the package manager
   ignores anything else).
2. `AndroidManifest.xml` declares `android:extractNativeLibs="true"` and
   `build.gradle.kts` sets `packaging { jniLibs { useLegacyPackaging = true } }`
   so the binary is actually extracted to the filesystem at install time.
3. At install time, Android extracts `libglmproxy.so` to
   `/data/app/<pkg>/lib/arm64/` with execute permission and an SELinux
   label that allows `execve()`.
4. At runtime, the app locates the binary via
   `context.applicationInfo.nativeLibraryDir` and starts it with
   `ProcessBuilder`.

The Go binary doesn't need to be a real shared library — just a valid
PIE ELF executable. Android doesn't validate the ELF header beyond that.

## Local build

### Prerequisites
- Go 1.24+
- Node 22+
- Android SDK with platform 35 + build-tools 35 + NDK (any recent r25+)
- JDK 17

### Steps
```bash
# 1. Build the React frontend
cd cmd/desktop/frontend
npm ci
npm run build
cd ../../..

# 2. Copy frontend dist into the server's embed dir
rm -rf cmd/server/frontend_dist
cp -r cmd/desktop/frontend/dist cmd/server/frontend_dist

# 3. Cross-compile the Go binary for Android arm64
#    IMPORTANT: GOOS=android (not linux) + CGO_ENABLED=1 + -buildmode=pie
#    because Android requires PIE and uses its own dynamic linker.
export ANDROID_NDK_HOME=/path/to/ndk
export CC="$ANDROID_NDK_HOME/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android24-clang"
GOOS=android GOARCH=arm64 CGO_ENABLED=1 \
  go build -buildmode=pie -trimpath -ldflags='-s -w' \
  -o android-app/app/src/main/jniLibs/arm64-v8a/libglmproxy.so \
  ./cmd/server

# 4. Build the APK
cd android-app
./gradlew assembleRelease
# → app/build/outputs/apk/release/app-release.apk
```

## CI build

The `.github/workflows/build.yml` workflow does all of the above
automatically on every push to `main` and on every tag `v*`. The resulting
APK is published as a GitHub Release artifact.

To sign release builds in CI, set these repository secrets:
- `KEYSTORE_BASE64` — base64-encoded `.keystore` file
- `KEYSTORE_PASSWORD`
- `KEY_ALIAS`
- `KEY_PASSWORD`

If secrets are absent, the workflow falls back to a debug-signed APK so the
build always produces an installable artifact.

## Usage

1. Install the APK on an Android 8.0+ device (arm64).
2. Open the app — it shows the control surface (proxy is NOT auto-started).
3. Tap "Iniciar servidor". The status dot turns yellow (Starting), then
   green (Running) once `/health` returns 200.
4. The URL card appears showing `http://127.0.0.1:3005`. Tap "Copiar URL"
   to copy it, or "Abrir no navegador" to open the React panel in the
   system browser.
5. In the browser panel, add a ZCode account and generate a local API key.
6. Configure any OpenAI-compatible client on the device to use:
   - Base URL: `http://127.0.0.1:3005/v1`
   - Model: `glm-5.2` or `glm-5-turbo`
   - API key: the key you generated in the panel.
7. If the Z.ai upstream requests a captcha, a high-priority notification
   appears. Tap it to open a dialog with the captcha URL and a button to
   open it in the browser. Solve the captcha; the proxy continues
   automatically.
8. Tap "Parar servidor" when done. The foreground service stops and the
   Go process is gracefully shut down (SIGTERM, 3s grace).

## Android-specific notes

- **Captcha**: the desktop app uses a headless Chrome/Edge binary to solve
  captchas. Android has no such binary available to apps, so the proxy is
  launched with `ZCODE_CAPTCHA_ENABLED=0` and `ZCODE_HEADLESS_ENABLED=0`.
  Captchas that require user interaction surface as a high-priority
  system notification. Tapping the notification opens a dialog with the
  captcha URL and a button to open it in the system browser (no in-app
  WebView — the panel itself runs in the system browser).
- **Account creator**: disabled on Android (`ZCODE_ACCOUNT_CREATOR_ENABLED=0`)
  because it depends on PowerShell on Windows.
- **Data directory**: credentials and admin state are stored in
  `/data/data/com.glmproxy.app/files/.glm5.2proxy/` (app-private storage,
  wiped when the app is uninstalled).
- **Network**: the proxy listens on `127.0.0.1` only — it is never exposed
  to the network. The `INTERNET` permission is required only for the proxy
  to call the upstream Z.ai APIs.

### Material 3 (Material You)

The native UI uses Material 3 with:

- `Theme.Material3.Dark.NoActionBar` as the base
- A Blue tonal palette (source color `#3B82F6`) mapped to all M3 color roles
  (primary, secondary, tertiary, error, surface container tones, outline)
- Dynamic Color (Material You) applied on Android 12+ — palette derives
  from the user's wallpaper
- Edge-to-edge layout with manual inset application on the toolbar (top)
  and `NestedScrollView` (bottom) via `WindowInsetsCompat` listeners
- `AppBarLayout` with `liftOnScroll` for the toolbar elevation effect
- `?attr/materialCardViewOutlinedStyle` for all cards (16dp corner radius)
- `Widget.Material3.Button` (filled) for the primary toggle,
  `?attr/materialButtonTonalStyle` for secondary actions

See <https://m3.material.io/> for the spec.

## License

Same as the upstream `glm5.2proxy` project.
