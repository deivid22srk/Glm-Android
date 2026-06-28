import { config } from './config.js';
import log from './logger.js';
import captchaClient from './captcha-client.js';

function btnByText(page, text) {
  return page.locator('button', { hasText: text });
}

export const zaiRegister = {
  async openAuth(page) {
    log.zai('Abrindo pagina de auth', { url: config.ZAI_AUTH_URL });
    await page.goto(config.ZAI_AUTH_URL, { waitUntil: 'domcontentloaded' });
    await page.waitForSelector('button', { timeout: config.NAV_TIMEOUT_MS });
  },

  async clickContinueWithEmail(page) {
    log.zai('Clicando em "Continue with Email"');
    await btnByText(page, 'Continue with Email').click();
    await page.waitForSelector('input[name="email"]', { timeout: 15000 });
    await page.waitForSelector('input[name="current-password"]', { timeout: 15000 });
    log.ok('Tela de login visivel');
  },

  async clickSignUp(page) {
    log.zai('Clicando em "Sign up"');
    await btnByText(page, 'Sign up').click();
    await page.waitForSelector('input[autocomplete="name"]', { timeout: 15000 });
    log.ok('Tela de cadastro visivel');
  },

  async fillForm(page, { username, email, password }) {
    log.zai('Preenchendo formulario', { username, email });
    await page.locator('input[autocomplete="name"]').fill(username);
    await page.locator('input[name="email"]').fill(email);
    await page.locator('input[name="new-password"]').fill(password);
    log.ok('Formulario preenchido');
  },

  async startVerification(page) {
    log.zai('Clicando em "Click to start verification"');
    const trigger = page.locator('#aliyunCaptcha-captcha-text');
    await trigger.waitFor({ state: 'visible', timeout: 15000 });
    await trigger.click();
    log.zai('Aguardando widget do captcha abrir');
    await page.waitForSelector('#aliyunCaptcha-puzzle, #aliyunCaptcha-sliding-slider', { timeout: 20000 });
    log.ok('Widget do captcha aberto');
  },

  async solveCaptcha() {
    log.step('Chamando solver de captcha');
    const job = await captchaClient.solve();
    return job;
  },

  async clickCreateAccount(page) {
    log.zai('Clicando em "Create Account"');
    const btn = page.locator('button.ButtonCreateAccount');
    await btn.waitFor({ state: 'visible', timeout: 15000 });
    await btn.click();
    log.ok('Create Account clicado');
  },

  async waitForVerifyScreen(page) {
    log.zai('Aguardando tela "Verify Your Email"');
    await page.waitForURL(/\/auth\/verify/, { timeout: config.NAV_TIMEOUT_MS });
    await page.waitForSelector('text=Verify Your Email', { timeout: 20000 });
    log.ok('Tela de verificacao de email visivel');
  },

  async run(page, { username, email, password = config.PASSWORD }) {
    await this.openAuth(page);
    await this.clickContinueWithEmail(page);
    await this.clickSignUp(page);
    await this.fillForm(page, { username, email, password });
    await this.startVerification(page);
    await this.solveCaptcha();
    await this.clickCreateAccount(page);
    await this.waitForVerifyScreen(page);
    log.ok('Etapa de cadastro concluida');
  },
};

export default zaiRegister;
