import { config } from './config.js';
import log from './logger.js';
import tempMail from './temp-mail.js';

function btnByText(page, text) {
  return page.locator('button', { hasText: text });
}

export const zaiVerify = {
  async pollAndOpenVerifyEmail(mailPage) {
    log.step('Etapa 3: aguardando email do Z.ai');
    await tempMail.waitForZaiEmail(mailPage);
    await tempMail.clickFirstZaiMessage(mailPage);
    const verifyHref = await tempMail.getVerifyEmailHref(mailPage);
    if (!verifyHref) throw new Error('Link de verificacao nao encontrado no email');
    log.email('Link de verificacao capturado', { href: verifyHref });
    return verifyHref;
  },

  async openVerifyLink(browser, verifyHref) {
    log.zai('Abrindo link de verificacao em nova aba', { href: verifyHref });
    const verifyPage = await browser.newPage();
    await verifyPage.goto(verifyHref, { waitUntil: 'domcontentloaded' });
    return verifyPage;
  },

  async completeRegistration(verifyPage, password = config.PASSWORD) {
    log.step('Etapa 4: concluir cadastro');
    log.zai('Aguardando campos de senha da tela de conclusao');
    const pw1 = verifyPage.getByPlaceholder('Enter your password');
    const pw2 = verifyPage.getByPlaceholder('Confirm your password');
    await pw1.waitFor({ state: 'visible', timeout: 30000 });
    await pw1.fill(password);
    await pw2.fill(password);
    log.ok('Senhas preenchidas');

    const submit = verifyPage.getByRole('button', { name: /complete registration|conclua o cadastro/i });
    await submit.click();
    log.ok('Botao de conclusao clicado');
    await verifyPage.waitForTimeout(5000);
    log.ok('Cadastro concluido');
  },
};

export default zaiVerify;
