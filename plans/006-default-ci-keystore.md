# Plan 006 — Sign release builds with a default CI keystore when secrets absent

**Status:** TODO
**Written against commit:** `3dc085b`
**Estimated effort:** Small (30 minutes)
**Risk of fix:** Low — only affects CI signing path.

## Why this matters

The current `build.yml` falls back to `assembleDebug` when signing secrets
are absent. That produces an APK signed with the debug keystore, which:

- Cannot be installed alongside the Play Store version (different signing
  identity).
- Triggers "Unknown sources" warnings on stock Android.
- Has a `applicationIdSuffix = ".debug"`, breaking deep links and OAuth
  redirect URIs that target the release application ID.

For a side-loaded release channel, we want the CI to **always produce a
release-variant APK**, signed with a stable keystore that lives in the
repo's secrets. If the user hasn't set up secrets yet, the build should
generate a one-time debug-equivalent keystore (so the APK is still
installable) and print a clear warning in the workflow log.

## Current state

`.github/workflows/build.yml:110-128` — "Setup signing (release)" step
only runs `if: startsWith(github.ref, 'refs/tags/') || github.event_name
== 'workflow_dispatch'`, and silently no-ops when secrets are absent.

## Steps

### 1. Always generate a fallback keystore in CI

In `.github/workflows/build.yml`, replace the "Setup signing (release)"
step with:

```yaml
      - name: Setup signing (release)
        env:
          KEYSTORE_BASE64: ${{ secrets.KEYSTORE_BASE64 }}
          KEYSTORE_PASSWORD: ${{ secrets.KEYSTORE_PASSWORD }}
          KEY_ALIAS: ${{ secrets.KEY_ALIAS }}
          KEY_PASSWORD: ${{ secrets.KEY_PASSWORD }}
        run: |
          mkdir -p /tmp
          if [ -n "$KEYSTORE_BASE64" ]; then
            echo "Using release keystore from repository secrets"
            echo "$KEYSTORE_BASE64" | base64 -d > /tmp/xed.keystore
            cat > /tmp/signing.properties <<EOF
          storeFile=/tmp/xed.keystore
          storePassword=$KEYSTORE_PASSWORD
          keyAlias=$KEY_ALIAS
          keyPassword=$KEY_PASSWORD
          EOF
          else
            echo "::warning::No signing secrets — generating a one-time debug keystore"
            echo "::warning::APKs from this run CANNOT be upgraded in place by future builds."
            echo "::warning::Set KEYSTORE_BASE64 / KEYSTORE_PASSWORD / KEY_ALIAS / KEY_PASSWORD"
            echo "::warning::as repository secrets to enable stable release signing."
            keytool -genkeypair -v \
              -keystore /tmp/xed.keystore \
              -storepass android \
              -alias androiddebugkey \
              -keypass android \
              -keyalg RSA -keysize 2048 -validity 10000 \
              -dname "CN=Android Debug,O=Android,C=US"
            cat > /tmp/signing.properties <<EOF
          storeFile=/tmp/xed.keystore
          storePassword=android
          keyAlias=androiddebugkey
          keyPassword=android
          EOF
          fi
```

### 2. Always run `assembleRelease`, never `assembleDebug`

In the "Build APK" step, replace the if/else with:

```yaml
      - name: Build APK
        working-directory: android-app
        run: |
          echo "sdk.dir=$ANDROID_HOME" > local.properties
          chmod +x gradlew
          ./gradlew assembleRelease --no-daemon --stacktrace
          cp app/build/outputs/apk/release/app-release.apk \
             ../glm-proxy-android-arm64.apk
```

### 3. Update `app/build.gradle.kts` to always apply the release signing config

The current `signingConfigs.release` block already loads from
`/tmp/signing.properties` when `GITHUB_ACTIONS == "true"`. No change
needed — the CI now always writes that file.

But we should make the build **fail loudly** if `GITHUB_ACTIONS == "true"`
and the file is missing:

```kotlin
signingConfigs {
    create("release") {
        val isGitHubAction = System.getenv("GITHUB_ACTIONS") == "true"
        val propertiesFilePath = if (isGitHubAction) {
            "/tmp/signing.properties"
        } else {
            System.getProperty("user.home") + "/.glm-android/signing.properties"
        }
        val propertiesFile = File(propertiesFilePath)
        if (propertiesFile.exists()) {
            val properties = Properties()
            properties.load(propertiesFile.inputStream())
            keyAlias = properties["keyAlias"] as String?
            keyPassword = properties["keyPassword"] as String?
            storeFile = (properties["storeFile"] as String?)?.let { File(it) }
            storePassword = properties["storePassword"] as String?
        } else if (isGitHubAction) {
            throw GradleException(
                "GITHUB_ACTIONS=true but /tmp/signing.properties is missing. " +
                "The CI workflow must create it before invoking Gradle."
            )
        }
    }
}
```

## Files in scope

- `.github/workflows/build.yml`
- `android-app/app/build.gradle.kts`

## Files explicitly out of scope

- `android-app/app/src/main/AndroidManifest.xml` — no manifest change.
- `android-app/.github/workflows/desktop.yml` — desktop signing is a
  separate concern.

## Verification

1. Push the change on a branch (not main).
2. Trigger `workflow_dispatch` on the branch.
3. Without secrets configured: workflow log should print the `::warning::`
   lines, then `assembleRelease` should succeed, and the artifact should
   be `glm-proxy-android-arm64.apk` (not `app-debug.apk`).
4. `apksigner verify --verbose glm-proxy-android-arm64.apk` should report
   `Verifies` (with the debug-equivalent cert subject `CN=Android Debug`).
5. With secrets configured: no `::warning::` lines, artifact verifies
   against the user's cert.

## Test plan

Manual verification above. No automated test — CI-only change.

## Maintenance notes

- When the user sets up real secrets, the `::warning::` lines disappear
  automatically. No further change needed.
- If the `keytool` invocation fails on a future runner image, switch to
  pre-generating the keystore with `openssl` + `apksigner` — but stop
  and report back; the `keytool` path is simpler and stable.

## Escape hatches

- If `assembleRelease` fails because `signing.properties` references a
  non-existent file, the `GradleException` in step 3 will fail the build
  with a clear message. Do not silently fall back to debug — that
  reproduces the original bug.
- If the one-time keystore causes install conflicts with prior user-
  installed builds, document the `adb uninstall com.glmproxy.app` escape
  hatch in the release notes. Do not change the application ID.
