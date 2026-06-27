# Plan 013 — Update README_ANDROID.md to remove WebView references (doc drift)

**Status:** TODO
**Written against commit:** `f7aaab7`
**Source finding:** DIR-01 (doc-vs-code contradiction)
**Estimated effort:** Small (20 minutes)
**Risk of fix:** None — documentation-only change.

## Why this matters

The README still describes the original WebView-based architecture that
was replaced in commit `2bda104` (Material 3 rewrite). A new contributor
reading `README_ANDROID.md` would believe the app embeds a WebView
showing the React panel, when in fact the app shows native Material 3
cards and pushes the user to the system browser to access the panel.

Specific contradictions (verified by grep):

```
$ grep -n "WebView" README_ANDROID.md
19:  - Loads the React panel into a full-screen `WebView`.
35:│  │  WebView ───────────────────────────┼───▶│
57:WebView on Android. No rewrite, no terminal, no `proot` — just a normal
152:  Captchas that require user interaction will surface through the WebView
```

But:
```
$ grep -n "WebView\|webview" android-app/app/src/main/java/com/glmproxy/app/MainActivity.kt
(no matches)
$ grep -n "WebView\|webview" android-app/app/src/main/res/layout/activity_main.xml
(no matches)
```

The README also claims (line 139): *"Open the app — the proxy starts
automatically and the panel loads."* Neither is true anymore — the
proxy does NOT auto-start (the user must tap "Iniciar servidor") and
the panel does NOT load (the user must tap "Abrir no navegador").

## Steps

### 1. Update the "Architecture" diagram and bullet list

In `README_ANDROID.md`, replace the architecture section (around lines
25–57) to reflect:

- The native Material 3 UI (cards: Status, URL, Error, Logs, Toggle)
- The foreground service that owns the Go binary process
- The captcha notification flow (added in commit `f7aaab7`)
- "Abrir no navegador" opens the system browser at the panel URL
  (the panel itself is NOT embedded)

New diagram should show:

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
│                │                    │  │ HTTP :3005   │   │  │
│                │ "Abrir navegador"  │  │ React panel  │   │  │
│                ▼                    │  │ /v1/* API    │   │  │
│  ┌────────────────────────────┐    │  └──────┬───────┘   │  │
│  │  System browser            │    └─────────┼───────────┘  │
│  │  loads http://127.0.0.1:3k │              │              │
│  │  → React panel             │              │              │
│  └────────────────────────────┘              │              │
└──────────────────────────────────────────────┼──────────────┘
                                               │
                                               ▼
                          https://zcode.z.ai  (Z.ai upstream)
```

### 2. Update the "Usage" section

Replace line 139 area:

```
## Usage

1. Install the APK on an Android 8.0+ device (arm64).
2. Open the app — it opens to the control surface (proxy is NOT
   auto-started).
3. Tap "Iniciar servidor". The status dot turns yellow (Starting),
   then green (Running) once /health returns 200.
4. The URL card appears showing http://127.0.0.1:3005. Tap "Copiar URL"
   to copy it, or "Abrir no navegador" to open the React panel in the
   system browser.
5. In the browser panel, add a ZCode account and generate a local API key.
6. Configure any OpenAI-compatible client on the device to use:
   - Base URL: http://127.0.0.1:3005/v1
   - Model: glm-5.2 or glm-5-turbo
   - API key: the key you generated in the panel.
7. If the Z.ai upstream requests a captcha, a high-priority notification
   appears. Tap it to open a dialog with the captcha URL and a button
   to open it in the browser. Solve the captcha; the proxy continues
   automatically.
8. Tap "Parar servidor" when done. The foreground service stops and the
   Go process is gracefully shut down (SIGTERM, 3s grace).
```

### 3. Update the "Android-specific notes" section (line 145+)

Update the captcha note (line 152) from:
> Captchas that require user interaction will surface through the WebView

To:
> Captchas that require user interaction will surface as a high-priority
> system notification. Tapping the notification opens a dialog with the
> captcha URL and a button to open it in the system browser (no
> in-app WebView).

### 4. Add a "Material 3" subsection under "Android-specific notes"

Document the design system in use so contributors know what to follow:

```
### Material 3 (Material You)

The native UI uses Material 3 with:
- Theme.Material3.Dark.NoActionBar as the base
- A Blue tonal palette (source color #3B82F6) mapped to all M3 color roles
- Dynamic Color (Material You) applied on Android 12+ — palette derives
  from the user's wallpaper
- Edge-to-edge layout with manual inset application on the toolbar and
  scroll view
- AppBarLayout with liftOnScroll for the toolbar elevation effect
- MaterialCardViewOutlinedStyle for all cards (16dp corner radius)
- MaterialButton (filled) for the primary toggle, TonalButton for
  secondary actions

See https://m3.material.io/ for the spec.
```

### 5. Remove or update the architecture section's reference to "no terminal, no proot"

Line 57:
> WebView on Android. No rewrite, no terminal, no `proot` — just a normal

This is still accurate (no terminal, no proot) but the "WebView" part
is wrong. Change to:
> Native Material 3 UI on Android. No rewrite, no terminal, no `proot` —
> just a normal child process that happens to be a Go HTTP server.

## Files in scope

- `README_ANDROID.md`

## Files explicitly out of scope

- All Kotlin source — no code change.
- `README.md` (the upstream Go README) — out of scope.

## Verification

1. Read the updated README start-to-finish and confirm:
   - No mention of "WebView" remains as a description of the current
     architecture (only as historical context if desired).
   - The "Usage" steps match what the app actually does.
   - The architecture diagram matches the code.
2. `grep -n "WebView" README_ANDROID.md` — should return zero matches
   (or only historical mentions clearly marked as such).
3. `grep -n "auto-start\|starts automatically" README_ANDROID.md` —
   should return zero matches (proxy does NOT auto-start).

## Test plan

Doc-only change. No automated test. The verification steps above are
the test.

## Maintenance notes

- If a future decision re-introduces a WebView (per DIR-01), update
  this README again to reflect it. The current doc reflects the
  native-UI decision made in commit `2bda104`.
- When new features land (captcha notification, settings screen,
  boot-start, etc.), update the "Usage" section to mention them.

## Escape hatches

- If the README has other stale sections not covered by this plan
  (e.g. signing instructions, CI secret setup), don't expand scope —
  those are covered by other plans (006, 018) or deferred. Just fix
  the WebView doc drift here.
