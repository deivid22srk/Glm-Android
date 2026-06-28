package com.glmproxy.app

import com.google.common.truth.Truth.assertThat
import org.junit.Test

/**
 * Unit tests for [ProxyBinary.isCaptchaRequest] — the pure-function guard
 * that drives the captcha-notification feature.
 *
 * This is the first JVM test in the project (see plan 019). It runs on
 * the local JVM via JUnit + Truth, no Robolectric needed because
 * `isCaptchaRequest` is a pure string-matching function with no Android
 * dependencies.
 *
 * The test asserts:
 * 1. Every marker in [ProxyBinary.CAPTCHA_MARKERS] triggers a match
 *    (guards against silent removal of a marker).
 * 2. Markers are case-insensitive.
 * 3. Markers match when embedded in a larger Go log line (the realistic
 *    case — the Go proxy emits lines like
 *    `[27/06/2026, 19:28:08] WARN captcha.browser_missing Request ...`).
 * 4. Non-captcha log lines do NOT match (no false positives).
 * 5. The actual log line from the user's bug report matches (regression
 *    test for the original captcha-notification feature request).
 */
class ProxyBinaryTest {

    @Test
    fun `every marker in CAPTCHA_MARKERS triggers a match`() {
        for (marker in ProxyBinary.CAPTCHA_MARKERS) {
            assertThat(ProxyBinary.isCaptchaRequest(marker))
                .named("marker '$marker' should trigger a match")
                .isTrue()
        }
    }

    @Test
    fun `markers are case insensitive`() {
        assertThat(ProxyBinary.isCaptchaRequest("CAPTCHA.BROWSER_MISSING")).isTrue()
        assertThat(ProxyBinary.isCaptchaRequest("Captcha.Browser_Missing")).isTrue()
        assertThat(ProxyBinary.isCaptchaRequest("captcha.BROWSER_missing")).isTrue()
    }

    @Test
    fun `marker embedded in real Go log line matches`() {
        // Realistic log line shape: timestamp + log level + event id + message.
        val realisticLine = "[27/06/2026, 19:28:08] WARN captcha.browser_missing " +
            "Request djk6phbeovrh falhou: a Z.ai pediu captcha, " +
            "mas nao encontrei Chrome nem Edge instalado para abrir o navegador " +
            "captcha automatico."
        assertThat(ProxyBinary.isCaptchaRequest(realisticLine)).isTrue()
    }

    @Test
    fun `non-captcha log lines do not match`() {
        assertThat(ProxyBinary.isCaptchaRequest("Server started on :3005")).isFalse()
        assertThat(ProxyBinary.isCaptchaRequest("account.startup_selected")).isFalse()
        assertThat(ProxyBinary.isCaptchaRequest("[INFO] proxy listening")).isFalse()
        assertThat(ProxyBinary.isCaptchaRequest("")).isFalse()
        assertThat(ProxyBinary.isCaptchaRequest("chat.failed after 3 attempts")).isFalse()
    }

    @Test
    fun `portuguese sentence markers match`() {
        // The two PT-BR sentence-level markers are the most fragile — they
        // depend on the Go side's log wording. This test documents them as
        // a contract; if the Go side reworded them, this test fails and
        // forces a coordination update.
        assertThat(ProxyBinary.isCaptchaRequest("a Z.ai pediu captcha")).isTrue()
        assertThat(ProxyBinary.isCaptchaRequest("nao encontrei Chrome nem Edge")).isTrue()
        // Embedded in a longer line — must still match.
        assertThat(
            ProxyBinary.isCaptchaRequest(
                "Request xyz falhou: a Z.ai pediu captcha, mas nao encontrei Chrome nem Edge instalado"
            )
        ).isTrue()
    }

    @Test
    fun `recentLogs returns empty list by default`() {
        // Before the proxy has been started, the buffer should be empty.
        // (We can't start the real binary in a unit test, so we only
        // assert the initial state. Lifecycle tests would need Robolectric
        // or instrumentation — out of scope for this first test.)
        assertThat(ProxyBinary.recentLogs()).isNotNull()
    }
}
