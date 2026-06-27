# Plan 015 — Exclude `.glm5.2proxy/` from backup (credentials would sync to Google Drive)

**Status:** TODO
**Written against commit:** `f7aaab7`
**Source finding:** SEC-02
**Estimated effort:** Small (15 minutes)
**Risk of fix:** Low — narrows backup scope; no impact on app functionality.

## Why this matters

The app stores credentials and admin state at
`/data/data/com.glmproxy.app/files/.glm5.2proxy/` (set via
`ZCODE_PROXY_DATA_DIR` env var in `ProxyBinary.kt:152`). This directory
contains:

- `zcode-accounts.enc.json` — encrypted ZCode account credentials
  (OAuth tokens, refresh tokens)
- `admin.json` — admin state including local API keys

The Go side encrypts the credentials file at rest (AES-256-GCM per
`README.md:246`), but the encryption key derivation is not visible from
the Android wrapper's scope. If the key is **not** device-bound (e.g.
derived from a hardcoded salt or a value stored alongside the cipher
blob), the backed-up blob is decryptable off-device.

The current backup rules include the **entire `filesDir`** in both
cloud backup AND device-transfer:

`android-app/app/src/main/res/xml/backup_rules.xml`:
```xml
<full-backup-content>
    <include domain="file" path="."/>
</full-backup-content>
```

`android-app/app/src/main/res/xml/data_extraction_rules.xml`:
```xml
<data-extraction-rules>
    <cloud-backup>
        <include domain="file" path="."/>
    </cloud-backup>
    <device-transfer>
        <include domain="file" path="."/>
    </device-transfer>
</data-extraction-rules>
```

Result: when the user has backup enabled (default on most Android
devices), the encrypted credentials blob is uploaded to their Google
Drive account. On a new device, it's restored automatically. An
attacker who compromises the Google account gets the blob; whether they
can decrypt it depends entirely on the Go side's key derivation, which
we cannot verify from this audit.

The safe default for any credential-bearing app is to **exclude the
credentials directory from backup**.

## Steps

### 1. Update backup_rules.xml to exclude `.glm5.2proxy/`

`android-app/app/src/main/res/xml/backup_rules.xml`:

```xml
<?xml version="1.0" encoding="utf-8"?>
<full-backup-content>
    <include domain="file" path="."/>
    <exclude domain="file" path=".glm5.2proxy/"/>
</full-backup-content>
```

### 2. Update data_extraction_rules.xml similarly

`android-app/app/src/main/res/xml/data_extraction_rules.xml`:

```xml
<?xml version="1.0" encoding="utf-8"?>
<data-extraction-rules>
    <cloud-backup>
        <include domain="file" path="."/>
        <exclude domain="file" path=".glm5.2proxy/"/>
    </cloud-backup>
    <device-transfer>
        <include domain="file" path="."/>
        <exclude domain="file" path=".glm5.2proxy/"/>
    </device-transfer>
</data-extraction-rules>
```

### 3. Document the exclusion in ProxyBinary.kt

Add a comment near the `ZCODE_PROXY_DATA_DIR` assignment
(`ProxyBinary.kt:152`) so future maintainers know the backup rules
depend on this path:

```kotlin
// Data dir for the Go proxy. Contains encrypted credentials and admin
// state. Excluded from cloud backup / device transfer — see
// res/xml/backup_rules.xml and res/xml/data_extraction_rules.xml.
// If you change this path, update those rules to match.
env["ZCODE_PROXY_DATA_DIR"] = dataDir.absolutePath
```

## Files in scope

- `android-app/app/src/main/res/xml/backup_rules.xml`
- `android-app/app/src/main/res/xml/data_extraction_rules.xml`
- `android-app/app/src/main/java/com/glmproxy/app/ProxyBinary.kt` (comment only)

## Files explicitly out of scope

- `AndroidManifest.xml` — `allowBackup="true"` stays (other app state
  like settings should be backed up).
- Go source (`internal/zcodeenv/`, `internal/accounts/`) — the
  encryption key derivation is out of scope for this audit.

## Verification

1. `./gradlew assembleDebug` — build passes.
2. Install the app, start the proxy, add a ZCode account so the
   `.glm5.2proxy/` directory is populated.
3. Run `adb shell bmgr backupnow com.glmproxy.app.debug` (debug variant).
4. Check `adb logcat | grep Backup` for the backup result — the
   `.glm5.2proxy/` directory should NOT appear in the backed-up file
   list.
5. (Optional, requires fresh device or factory reset) Restore on a
   new device — the credentials should NOT be present after restore;
   the user must re-add accounts.

## Test plan

Manual verification above. No automated test — backup behavior is
hard to assert without a full backup/restore cycle.

## Maintenance notes

- If the data dir path ever changes (e.g. user-configurable via a
  settings screen — deferred plan 002), the exclude rule must be
  updated to match. The comment added in step 3 guards against this.
- If the Go side ever adds a device-bound key derivation (e.g. using
  Android Keystore), this exclusion can be relaxed — but until that's
  verified, keep the exclude in place.

## Escape hatches

- If `bmgr backupnow` doesn't show the exclude taking effect on a
  given Android version, fall back to
  `android:allowBackup="false"` on the `<application>` element. This
  disables backup entirely (lossy for settings) but is the strongest
  guarantee. Stop and report back before doing this — it's a UX
  regression for legitimate device-migration use cases.
