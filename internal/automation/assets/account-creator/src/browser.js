import { chromium } from 'playwright-extra';
import stealth from 'puppeteer-extra-plugin-stealth';
import { config } from './config.js';
import log from './logger.js';

chromium.use(stealth());

function _profileDir() {
  return `${config.ROOT}/.chrome-profile-contas`;
}

export async function launchBrowser() {
  log.info('Lancando Chrome com CDP', { port: config.CDP_PORT });
  const browser = await chromium.launchPersistentContext(_profileDir(), {
    headless: config.HEADLESS,
    channel: 'chrome',
    viewport: { width: 1280, height: 800 },
    args: [
      `--remote-debugging-port=${config.CDP_PORT}`,
      '--remote-debugging-address=127.0.0.1',
      '--disable-blink-features=AutomationControlled',
      '--no-first-run',
      '--no-default-browser-check',
    ],
    slowMo: config.SLOW_MO_MS,
  });
  browser.setDefaultNavigationTimeout(config.NAV_TIMEOUT_MS);

  const ver = browser.browser()?.version() || 'desconhecido';
  log.ok('Chrome lancado', { profile: _profileDir(), version: ver, port: config.CDP_PORT });
  return browser;
}

export async function closeBrowser(browser) {
  try {
    await browser.close();
    log.info('Chrome fechado');
  } catch (e) {
    log.warn('Erro ao fechar Chrome', { err: e.message });
  }
}

export default { launchBrowser, closeBrowser };
