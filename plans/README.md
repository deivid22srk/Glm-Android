# Improvement plans for GLM Android wrapper

This document is the output of running the `shadcn/improve` skill (v1.0.0)
against the repository at commit `f7aaab7` (2026-06-27).

## Audit scope

The audit focused on the **Android wrapper** (`android-app/`), not the
upstream Go proxy. Five categories were audited in parallel by separate
subagents:

- **Correctness/bugs** (13 findings, C-01..C-13)
- **Security** (10 findings, SEC-01..SEC-10)
- **DX & tooling** (13 findings, DX-01..DX-13)
- **Test coverage** (8 findings, TC-01..TC-08)
- **Direction** (4 findings, DIR-01..DIR-04)

After Phase 3 (vet), 6 findings were promoted to actionable plans below.
The rest are recorded in the "Considered and rejected" section with reasons.

## Reconciliation with previous run (commit `3dc085b`)

The previous audit produced plans 001–008. Status update:

| # | Previous plan | Status | Disposition |
|---|---|---|---|
| 001 | Replace WebView with debuggable Chrome Custom Tab fallback | **OBSOLETE** | The WebView was removed entirely; the new direction (DIR-01) is the opposite — re-embed the panel only if needed. Superseded by current plans. |
| 002 | Add a settings screen for port, host, and feature flags | DEFERRED | Still wanted but lower priority than fixing the captcha notification actually firing (C-01) and the doc drift (DIR-01). |
| 003 | Capture and surface Go binary crash logs in the UI | **DONE** | Implemented in commit `2bda104` (Material 3 rewrite). Logs card visible when proxy is running. |
| 004 | Add E2E smoke test that boots the proxy on an emulator | DEFERRED | Subsumed by TC-01 (test infrastructure scaffold) — without a test layer there is nothing for an E2E test to plug into. |
| 005 | Stop proxy cleanly on app removal (onTaskRemoved) | **CHANGED** | The previous plan was to increase SIGTERM grace from 2s to 12s. The code currently uses 2s+1s = 3s total (the previous plan's claim of 12s was wrong). New plan 005 below reflects the correct numbers. |
| 006 | Sign release builds with a default CI keystore when secrets absent | **KEPT** | Still TODO. The CI still falls back to `assembleDebug`. |
| 007 | Add proguard-rules.pro keep rules for the JS bridge | **OBSOLETE** | The WebView was removed; no JS bridge exists. The keep rule at `proguard-rules.pro:8-10` is dead code and should be deleted (folded into plan 010). |
| 008 | Split libglmproxy.so per-ABI to support x86_64 emulators | DEFERRED | Still wanted but lower priority. |

## Prioritized plans (current run)

Plans are ordered by leverage (impact ÷ effort, weighted by confidence).
Execute in this order unless dependencies force otherwise.

| # | Plan | Category | Impact | Effort | Status | Source |
|---|------|----------|--------|--------|--------|--------|
| 010 | Fix captcha notification never fires on Android 13+ (request POST_NOTIFICATIONS) + add deleteIntent for swipe-dismiss | correctness | HIGH | S | **DONE** | C-01, C-02 |
| 011 | Fix CI heredoc terminator (indented EOF swallows echo line) | correctness | MED | S | **DONE** | C-08 |
| 012 | Add contentIntent to foreground notification so tapping opens the app | DX | MED | S | **DONE** | C-12 |
| 013 | Update README_ANDROID.md to remove WebView references (doc drift) | docs | MED | S | **DONE** | DIR-01 |
| 014 | Scope `usesCleartextTraffic` to 127.0.0.1 via network_security_config.xml | security | MED | S | **DONE** | SEC-01 |
| 015 | Exclude `.glm5.2proxy/` from backup (credentials would sync to Google Drive) | security | HIGH | S | **DONE** | SEC-02 |
| 016 | Fix exported-activity phishing vector: validate EXTRA_CAPTCHA_URL scheme | security | HIGH | S | **DONE** | SEC-03 |
| 017 | Add Gradle dependency caching + wrapper validation + Dependabot config | DX | MED | S | **DONE** | DX-02, DX-03, DX-11 |
| 018 | Always run assembleRelease in CI with fallback keystore (hard-fail on tag if secrets missing) | DX | MED | S | **DONE** | DX-13, SEC-06 |
| 019 | Scaffold JVM test infrastructure + first regression test for isCaptchaRequest() | tests | HIGH | M | **DONE** | TC-01, TC-04 |
| 020 | Add start-on-boot (opt-in) — closes the always-alive loop | direction | HIGH | S | **DONE** | DIR-03 |

## Dependency ordering

- 019 (test scaffold) should land before any refactor of the captcha
  detection logic — it makes future changes to `CAPTCHA_MARKERS` safe.
- 015 (backup exclude) is independent — can land in parallel with any
  other plan.
- 016 (URL validation) and 010 (notification permission) both touch the
  captcha flow; land 010 first so the feature actually works on Android 13+,
  then 016 to harden it.

## Considered and rejected (this run)

- **C-03** (FGS dataSync 6-hour timeout on Android 15) — real finding but
  the fix requires choosing a different foregroundServiceType (`specialUse`
  needs Play Console justification, which we don't have yet) or implementing
  `onTimeout` + restart logic that touches lifecycle design. Deferred until
  the user hits the 6h cap in practice. Recorded for future revisit.
- **C-04** (Start→Stop race) — real but extremely unlikely to be hit by a
  single-user app with manual toggle. Cost of fix (synchronization barrier)
  outweighs benefit.
- **C-05** (waitFor return value ignored) — defensive; on Android
  `Process.destroy()` is SIGKILL so the concern is theoretical.
- **C-06** (handler.post after destroy) — minor leak, no user-visible
  symptom. Will be naturally fixed when the activity moves to
  LifecycleScope in a future Compose migration.
- **C-07** (captcha markers include PT-BR strings) — real, but addressed
  indirectly by plan 019 (regression test) and by DIR-02 (replace with
  structured /zcode/captcha/poll). Not separately planned.
- **C-09** (onTaskRemoved comment drift) — folded into the disposition of
  the previous plan 005. The comment will be cleaned up when 005 lands.
- **C-10, C-11, C-13** (dead code, stale comments) — folded into a
  general "cleanup" pass that should accompany any of the planned changes
  to the affected files.
- **SEC-04, SEC-05** (Go stdout may contain secrets) — depends on the Go
  side's logging discipline, which is out of scope for this audit. Will be
  revisited when the Go side is audited.
- **SEC-06** (debug-signed release published to GitHub Releases) — folded
  into plan 018 (always run assembleRelease + hard-fail on tag).
- **SEC-07** (no integrity check on libglmproxy.so) — defense-in-depth;
  low priority for a personal-use sideloaded app.
- **SEC-08** (no admin auth token from wrapper) — depends on Go side's
  auth model. Deferred.
- **SEC-09** (clipboard plaintext) — accepted risk; clipboard is
  inherently transient.
- **SEC-10** (signing files in /tmp world-readable) — only matters on
  self-hosted runners; we use GitHub-hosted.
- **DX-01** (no ktlint/detekt) — nice-to-have but not blocking. Will be
  added when test infrastructure lands.
- **DX-04** (inconsistent go test invocation) — needs Go-side
  investigation; out of scope.
- **DX-05, DX-06, DX-09, DX-12** (doc improvements) — folded into plan 013
  (README update) where applicable; the rest are micro-improvements that
  can accompany any future doc pass.
- **DX-07** (ubuntu-22.04 deprecation) — premature; 22.04 is still
  supported. Will revisit when GitHub announces retirement.
- **DX-08** (R8/ProGuard disabled) — enabling R8 risks runtime crashes
  without device testing, which doesn't exist yet (TC-01). Deferred until
  test infrastructure lands.
- **DX-10** (desktop.yml stale) — out of scope; desktop CI is a separate
  concern from the Android wrapper.
- **TC-02, TC-03, TC-05, TC-06, TC-07, TC-08** — all subsumed by TC-01
  (scaffold test infrastructure). Once tests exist, these become
  individual test cases, not separate plans.
- **DIR-01** (re-embed WebView) — the user explicitly asked to remove the
  WebView in favor of native Material 3 UI. Reversing that decision is a
  product call, not an audit finding. The doc drift (plan 013) is the
  actionable artifact.
- **DIR-02** (replace log-scraping with /zcode/captcha/poll) — good
  direction but depends on Go-side API contract verification. Will be
  proposed as a follow-up after the test scaffold (plan 019) lands.
- **DIR-04** (quota card) — depends on DIR-01's product decision. Deferred.

## Commit this audit was written against

```
f7aaab719c32b5bcd915585364d7ad604d76cdca
```

Each plan file stamps this commit; the executor uses it for drift detection.
