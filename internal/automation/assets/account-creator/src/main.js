import { pathToFileURL } from 'node:url';
import browser from './browser.js';
import tempMail from './temp-mail.js';
import zaiRegister from './zai-register.js';
import zaiVerify from './zai-verify.js';
import proxyLink from './proxy-link.js';
import store from './emails-store.js';
import { config } from './config.js';
import log from './logger.js';

function shouldLinkToProxy(options = {}) {
  if (typeof options.linkToProxy === 'boolean') {
    return options.linkToProxy;
  }
  return process.env.ZCODE_PROXY_AUTO_LINK === '1' || process.env.ZCODE_PROXY_AUTO_LINK === 'true';
}

export async function runOne(options = {}) {
  const b = await browser.launchBrowser();
  const mailPage = b.pages()[0] || await b.newPage();
  const zaiPage = await b.newPage();
  let email = '';
  let username = '';

  try {
    log.step('=== Iniciando criacao de conta ===');

    // --- Etapa 1: email temporario ---
    log.step('Etapa 1: obter email temporario');
    await mailPage.bringToFront();
    await mailPage.goto(config.TEMP_MAIL_URL, { waitUntil: 'domcontentloaded' });
    await mailPage.waitForSelector('input.email-section-input-email');
    email = await tempMail.getFreshEmail(mailPage);
    const accountNumber = store.nextAccountNumber();
    username = `${config.USERNAME_PREFIX}${accountNumber}`;
    store.addInUse(email, username);
    await tempMail.openInbox(mailPage, email);

    // --- Etapa 2: cadastro no Z.ai ate tela de verificacao ---
    log.step('Etapa 2: cadastro no Z.ai', { username });
    await zaiPage.bringToFront();
    await zaiRegister.run(zaiPage, { username, email, password: config.PASSWORD });

    // --- Etapa 3: aguardar email e abrir link de verificacao ---
    log.step('Etapa 3: verificacao por email');
    await mailPage.bringToFront();
    const verifyHref = await zaiVerify.pollAndOpenVerifyEmail(mailPage);
    const verifyPage = await zaiVerify.openVerifyLink(b, verifyHref);
    await verifyPage.bringToFront();

    // --- Etapa 4: concluir cadastro ---
    log.step('Etapa 4: concluir cadastro');
    await zaiVerify.completeRegistration(verifyPage, config.PASSWORD);

    if (shouldLinkToProxy(options)) {
      await proxyLink.linkCreatedAccount(verifyPage, { email, proxyBaseUrl: options.proxyBaseUrl });
    }

    store.markCompleted(email);
    log.ok('=== Conta criada com sucesso ===', { username, email });
    return { ok: true, username, email };
  } catch (e) {
    store.markFailed(email, e.message);
    log.error('Falha durante a criacao da conta', { err: e.message, stack: e.stack });
    return { ok: false, error: e.message };
  } finally {
    await browser.closeBrowser(b);
  }
}

async function main() {
  const howMany = parseInt(process.argv[2] || '1', 10);
  log.info('Iniciando automacao', { quantidade: howMany });
  const results = [];
  for (let i = 1; i <= howMany; i++) {
    log.info(`--- Conta ${i}/${howMany} ---`);
    const res = await runOne();
    results.push(res);
    if (!res.ok) {
      log.error('Interrompendo lote por falha', { indice: i });
      process.exitCode = 1;
      break;
    }
    if (i < howMany) {
      log.info('Pausa de 8s antes da proxima conta');
      await new Promise(r => setTimeout(r, 8000));
    }
  }
  log.info('Resultado final', { results });
}

if (import.meta.url === pathToFileURL(process.argv[1]).href) {
  main();
}
