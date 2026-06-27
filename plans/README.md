# Improvement plans for GLM Android wrapper

This document is the output of running the `shadcn/improve` skill against
the current state of the repository (commit `3dc085b`, 2026-06-27).

The audit focused on the **Android wrapper** we just built (not the upstream
Go proxy, which is out of scope for this round). Categories covered:
correctness, security, performance, test coverage, DX, and direction.

Plans are ordered by leverage (impact ÷ effort). Execute in this order
unless dependencies force otherwise.

| # | Plan | Category | Impact | Effort | Status |
|---|------|----------|--------|--------|--------|
| 001 | Replace WebView with debuggable Chrome Custom Tab fallback | correctness | HIGH | M | TODO |
| 002 | Add a settings screen for port, host, and feature flags | DX | MED | M | TODO |
| 003 | Capture and surface Go binary crash logs in the UI | DX | HIGH | S | TODO |
| 004 | Add E2E smoke test that boots the proxy on an emulator | tests | HIGH | L | TODO |
| 005 | Stop proxy cleanly on app removal (onTaskRemoved) | correctness | MED | S | TODO |
| 006 | Sign release builds with a default CI keystore when secrets absent | DX | MED | S | TODO |
| 007 | Add `proguard-rules.pro` keep rules for the JS bridge | security | LOW | S | TODO |
| 008 | Split `libglmproxy.so` per-ABI to support x86_64 emulators | DX | MED | M | TODO |

## Dependencies
- 003 (crash logs) should land before 004 (smoke test) — the test needs to
  assert on the crash log surface.
- 006 (default CI keystore) is a prerequisite for tagging a real release.

## Considered and rejected
- **Migrating to Compose Multiplatform for the UI**: the React panel
  already exists and is shared with desktop. Rewrite cost > benefit.
- **Using gomobile bind instead of subprocess**: would simplify lifecycle
  but breaks the "same binary as desktop" invariant and adds JNI complexity.
- **Bundle size optimization (R8 / minify)**: APK is 9.2MB, dominated by
  the Go binary (8MB+). R8 won't help here; the binary is already stripped.
