import { config } from './config.js';
import log from './logger.js';
import store from './emails-store.js';

const SELECTORS = {
  input: 'input.email-section-input-email',
  suffix: '.email-section-input-suffix',
  accessBtn: 'button.email-section-submit-button',
};

export const tempMail = {
  async getCurrentEmail(page) {
    const input = page.locator(SELECTORS.input);
    const suffix = page.locator(SELECTORS.suffix);
    const name = (await input.inputValue()).trim();
    const dom = (await suffix.textContent()).trim();
    return name + dom;
  },

  async getFreshEmail(page, maxAttempts = 20) {
    for (let i = 1; i <= maxAttempts; i++) {
      const email = await this.getCurrentEmail(page);
      if (!store.exists(email)) {
        log.email('Email novo disponivel', { email, tentativa: i });
        return email;
      }
      log.warn('Email ja usado, recarregando para outro', { email, tentativa: i });
      await page.reload({ waitUntil: 'domcontentloaded' });
      await page.waitForSelector(SELECTORS.input);
      await page.waitForTimeout(500);
    }
    throw new Error(`Nao conseguiu email novo apos ${maxAttempts} tentativas`);
  },

  async openInbox(page, email) {
    const name = email.split('@')[0];
    const inboxUrl = `https://tuamaeaquelaursa.com/${name}`;
    log.email('Abrindo inbox', { inboxUrl });
    await page.goto(inboxUrl, { waitUntil: 'domcontentloaded' });
    await page.waitForSelector('.all-messages, body', { timeout: 15000 });
  },

  async reloadInbox(page) {
    const url = page.url().split('/')[0] === 'https:'
      ? page.url()
      : `https://tuamaeaquelaursa.com${page.url()}`;
    await page.goto(url, { waitUntil: 'domcontentloaded' });
    await page.waitForSelector('.all-messages', { timeout: 15000 });
    await page.waitForTimeout(1500);
  },

  async listMessages(page) {
    const rows = page.locator('.the-message');
    const count = await rows.count();
    const items = [];
    for (let i = 0; i < count; i++) {
      const from = await rows.nth(i).locator('.the-message-from').textContent().catch(() => '');
      const subject = await rows.nth(i).locator('.the-message-subject').textContent().catch(() => '');
      items.push({ from: (from||'').trim(), subject: (subject||'').trim() });
    }
    return items;
  },

  async waitForZaiEmail(page, { timeout = config.POLL_EMAIL_TIMEOUT_MS, interval = config.POLL_EMAIL_INTERVAL_MS } = {}) {
    const start = Date.now();
    for (;;) {
      const msgs = await this.listMessages(page);
      const zaiMsgs = msgs.filter(m => /z\.ai|verify your email/i.test(m.from + ' ' + m.subject));
      log.email('Polling inbox', { total: msgs.length, zai: zaiMsgs.length, decorrido_ms: Date.now() - start, amostra: msgs.slice(0,3) });
      if (zaiMsgs.length > 0) {
        log.ok('Email do Z.ai encontrado', { subject: zaiMsgs[0].subject });
        return true;
      }
      if (Date.now() - start > timeout) {
        throw new Error('Timeout aguardando email do Z.ai');
      }
      await this.reloadInbox(page);
      await page.waitForTimeout(interval);
    }
  },

  async clickFirstZaiMessage(page) {
    const msgs = await this.listMessages(page);
    const idx = msgs.findIndex(m => /z\.ai|verify your email/i.test(m.from + ' ' + m.subject));
    if (idx < 0) throw new Error('Nenhum email do Z.ai para clicar');
    const target = page.locator('.the-message').nth(idx);
    await target.click();
    log.email('Clicou no email do Z.ai', { index: idx, subject: msgs[idx].subject });
    await page.waitForSelector('.message-details-body', { timeout: 20000 });
  },

  async clickVerifyEmailLink(page) {
    const link = page.locator('.message-details-body a', { hasText: 'Verify Email' }).first();
    const href = await link.getAttribute('href');
    log.email('Link de verificacao encontrado', { href });
    return href;
  },

  async getVerifyEmailHref(page) {
    return page.locator('.message-details-body a').filter({ hasText: 'Verify Email' }).first().getAttribute('href');
  },
};

export default tempMail;
