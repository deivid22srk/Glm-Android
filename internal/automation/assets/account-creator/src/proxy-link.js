import { config } from './config.js';
import log from './logger.js';

function proxyBaseURL(baseURL) {
  return (baseURL || process.env.ZCODE_PROXY_BASE_URL || config.ZCODE_PROXY_BASE_URL).replace(/\/+$/, '');
}

async function fetchJSON(path, options = {}, baseURL) {
  const response = await fetch(`${proxyBaseURL(baseURL)}${path}`, {
    ...options,
    headers: {
      'content-type': 'application/json',
      ...(options.headers || {}),
    },
  });
  const text = await response.text();
  let payload = {};
  if (text.trim()) {
    payload = JSON.parse(text);
  }
  if (!response.ok) {
    const message = payload?.error?.message || text || response.statusText;
    throw new Error(`proxy ${path} retornou HTTP ${response.status}: ${message}`);
  }
  return payload;
}

async function checkTerms(page) {
  await page.waitForSelector('input[type="checkbox"], [role="checkbox"]', {
    state: 'visible',
    timeout: 30000,
  });
  const checkbox = page.locator('input[type="checkbox"]').first();
  if (await checkbox.count()) {
    await checkbox.check({ force: true });
    return;
  }
  const roleCheckbox = page.locator('[role="checkbox"]').first();
  await roleCheckbox.waitFor({ state: 'visible', timeout: 15000 });
  await roleCheckbox.click({ force: true });
}

async function clickContinue(page) {
  const button = page.getByRole('button', { name: /continuar|continue/i });
  await button.waitFor({ state: 'visible', timeout: 15000 });
  await page.waitForFunction(() => {
    const buttons = [...document.querySelectorAll('button')];
    const target = buttons.find(button => /continuar|continue/i.test((button.innerText || button.textContent || '').trim()));
    if (!target) return false;
    return !target.disabled && target.getAttribute('aria-disabled') !== 'true';
  }, { timeout: 15000 });
  await button.click();
}

export async function acceptZCodeConsent(page) {
  await page.waitForSelector('body', { timeout: config.NAV_TIMEOUT_MS });
  await checkTerms(page);
  await clickContinue(page);
}

export async function linkCreatedAccount(page, { email, proxyBaseUrl } = {}) {
  log.step('Etapa 5: vincular conta criada ao proxy GLM5.2');
  const flow = await fetchJSON('/api/admin/auth/login/start', { method: 'POST', body: '{}' }, proxyBaseUrl);
  if (!flow.authorizeUrl || !flow.flowId) {
    throw new Error(`proxy retornou fluxo OAuth invalido: ${JSON.stringify(flow)}`);
  }

  log.zai('Abrindo tela de consentimento ZCode no Chrome da automacao', { flowId: flow.flowId });
  await page.bringToFront();
  await page.goto(flow.authorizeUrl, { waitUntil: 'domcontentloaded' });
  await acceptZCodeConsent(page);
  log.ok('Consentimento enviado para o ZCode');

  const account = await pollLinkedAccount(flow.flowId, flow.pollIntervalSec || 2, email, proxyBaseUrl);
  log.ok('Conta vinculada ao proxy GLM5.2', { email, accountId: account?.id, label: account?.label });
  try {
    await refreshLinkedAccountCodingPlan(account, proxyBaseUrl);
    await waitForProvisionedQuota(account, proxyBaseUrl);
  } catch (error) {
    log.warn('Conta foi criada e vinculada, mas a validacao final de Coding Plan/cota nao concluiu', {
      email,
      accountId: account?.id,
      label: account?.label,
      err: error instanceof Error ? error.message : String(error),
    });
  }
  return account;
}

async function pollLinkedAccount(flowId, pollIntervalSec, email, proxyBaseUrl) {
  const started = Date.now();
  const timeoutMs = config.PROXY_LINK_TIMEOUT_MS;
  while (Date.now() - started < timeoutMs) {
    const result = await fetchJSON(`/api/admin/auth/login/poll?flow_id=${encodeURIComponent(flowId)}`, {}, proxyBaseUrl);
    log.info('Poll do vinculo no proxy', { flowId, status: result.status });
    if (result.status === 'ready') {
      await waitForAccountInProxy(email, proxyBaseUrl);
      return result.account;
    }
    if (result.status === 'failed') {
      throw new Error(`vinculo OAuth falhou: ${JSON.stringify(result)}`);
    }
    await new Promise(resolve => setTimeout(resolve, Math.max(1000, pollIntervalSec * 1000)));
  }
  throw new Error(`timeout aguardando vinculo OAuth no proxy para ${email}`);
}

async function waitForAccountInProxy(email, proxyBaseUrl) {
  const started = Date.now();
  while (Date.now() - started < 30000) {
    const listed = await fetchJSON('/api/admin/accounts?quota=0', {}, proxyBaseUrl);
    const found = (listed.data || []).find(account => account?.user?.email === email);
    if (found) {
      return found;
    }
    await new Promise(resolve => setTimeout(resolve, 1000));
  }
  throw new Error(`proxy concluiu OAuth, mas a conta ${email} nao apareceu em /api/admin/accounts`);
}

async function refreshLinkedAccountCodingPlan(account, proxyBaseUrl) {
  if (!account?.id) {
    throw new Error(`proxy retornou conta vinculada sem id: ${JSON.stringify(account)}`);
  }
  log.step('Etapa 6: refrescar Coding Plan direto pelo proxy');
  const result = await fetchJSON(
    `/api/admin/accounts/${encodeURIComponent(account.id)}/coding-plan/refresh`,
    { method: 'POST', body: '{}' },
    proxyBaseUrl,
  );
  log.ok('Coding Plan atualizado direto pelo proxy', {
    accountId: account.id,
    label: account.label,
    organizationId: result?.data?.organizationId,
    projectId: result?.data?.projectId,
    apiKeyCreated: result?.data?.apiKeyCreated,
    secretResolved: result?.data?.secretResolved,
  });
}

async function waitForProvisionedQuota(account, proxyBaseUrl) {
  const accountId = account?.id;
  const started = Date.now();
  while (Date.now() - started < config.PROXY_QUOTA_TIMEOUT_MS) {
    const detail = await fetchJSON(`/api/admin/accounts/${encodeURIComponent(accountId)}`, {}, proxyBaseUrl);
    const balance = findGLM52Balance(detail?.quota?.balances || []);
    if (balance?.available > 0) {
      log.ok('Cota GLM-5.2 confirmada para a conta criada', {
        accountId,
        label: detail.label,
        email: detail?.user?.email,
        total: balance.total,
        used: balance.used,
        remaining: balance.remaining,
        available: balance.available,
      });
      return balance;
    }
    log.info('Aguardando billing liberar cota GLM-5.2 apos refresh do Coding Plan', {
      accountId,
      quotaError: detail?.quotaError?.message,
      balances: (detail?.quota?.balances || []).map(item => item?.model).filter(Boolean),
    });
    await new Promise(resolve => setTimeout(resolve, 3000));
  }
  throw new Error(`timeout aguardando cota GLM-5.2 para a conta ${accountId}`);
}

function findGLM52Balance(balances) {
  return balances.find(item => String(item?.model || '').toLowerCase() === 'glm-5.2') || null;
}

export default { linkCreatedAccount, acceptZCodeConsent };
