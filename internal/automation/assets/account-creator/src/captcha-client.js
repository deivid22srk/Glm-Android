import fs from 'node:fs';
import { spawn } from 'node:child_process';
import path from 'node:path';
import { config } from './config.js';
import log from './logger.js';

let apiStartPromise = null;

async function fetchJSON(url, options = {}) {
  const r = await fetch(url, options);
  const text = await r.text();
  const data = text ? JSON.parse(text) : {};
  return { ok: r.ok, status: r.status, data };
}

async function waitForHealth(timeoutMs = config.CAPTCHA_API_START_TIMEOUT_MS) {
  const started = Date.now();
  let lastErr;
  while (Date.now() - started < timeoutMs) {
    try {
      const result = await fetchJSON(`${config.CAPTCHA_API}/health`);
      if (result.ok && result.data?.ok) {
        return result.data;
      }
      lastErr = new Error(`health HTTP ${result.status}`);
    } catch (e) {
      lastErr = e;
    }
    await new Promise(resolve => setTimeout(resolve, 1000));
  }
  throw new Error(`Captcha API nao ficou pronta em ${timeoutMs}ms: ${lastErr?.message || 'sem resposta'}`);
}

function candidateWorkdirs() {
  const seen = new Set();
  const values = [config.CAPTCHA_API_WORKDIR, ...(config.CAPTCHA_API_FALLBACK_WORKDIRS || [])];
  const candidates = [];
  for (const raw of values) {
    const value = String(raw || '').trim();
    if (!value) continue;
    const key = process.platform === 'win32' ? value.toLowerCase() : value;
    if (seen.has(key)) continue;
    seen.add(key);
    candidates.push(value);
  }
  return candidates;
}

function launchSpecFor(workDir) {
  if (fs.existsSync(path.join(workDir, 'server.js'))) {
    return {
      command: process.execPath,
      args: ['server.js'],
      label: 'node server.js',
    };
  }
  if (fs.existsSync(path.join(workDir, 'src', 'api', 'server.ts'))) {
    return {
      command: process.platform === 'win32' ? 'npm.cmd' : 'npm',
      args: ['run', 'api'],
      label: 'npm run api',
    };
  }
  return null;
}

async function startLocalAPI() {
  const candidates = candidateWorkdirs();
  if (candidates.length === 0) {
    throw new Error('Nenhum diretorio local da Captcha API foi configurado');
  }

  const errors = [];
  for (const workDir of candidates) {
    if (!fs.existsSync(workDir)) {
      errors.push(`${workDir}: diretorio nao existe`);
      continue;
    }
    const spec = launchSpecFor(workDir);
    if (!spec) {
      errors.push(`${workDir}: nao achei server.js nem src/api/server.ts`);
      continue;
    }
    try {
      log.captcha('Captcha API offline, iniciando processo local', { cwd: workDir, mode: spec.label, command: spec.command, args: spec.args });
      const child = spawn(spec.command, spec.args, {
        cwd: workDir,
        detached: true,
        stdio: 'ignore',
        windowsHide: true,
      });
      child.unref();
      log.captcha('Processo da Captcha API iniciado', { pid: child.pid, cwd: workDir, mode: spec.label });
      const health = await waitForHealth();
      log.captcha('Captcha API respondeu no health', { cwd: workDir, mode: spec.label });
      return health;
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      errors.push(`${workDir}: ${message}`);
      log.warn('Falha ao subir Captcha API local; tentando proximo fallback', { cwd: workDir, mode: spec.label, err: message });
    }
  }

  throw new Error(`Nao foi possivel subir a Captcha API local. Tentativas: ${errors.join(' | ')}`);
}

async function ensureAPI() {
  try {
    return await waitForHealth(1500);
  } catch {
    // Continua abaixo e tenta subir o processo local.
  }

  if (!apiStartPromise) {
    apiStartPromise = (async () => {
      try {
        return await startLocalAPI();
      } catch (error) {
        apiStartPromise = null;
        throw error;
      }
    })();
  }

  return apiStartPromise;
}

export const captchaClient = {
  async health() {
    return ensureAPI();
  },

  async createJob({ targetUrl = config.ZAI_AUTH_URL } = {}) {
    await ensureAPI();
    const body = {
      browser: { host: config.CDP_HOST, port: config.CDP_PORT },
      targetUrl,
      captchaOpenMode: 'captcha_only',
      retries: config.CAPTCHA_RETRIES,
      gestureProfile: config.CAPTCHA_GESTURE,
      debugScreenshots: true,
      debugDir: 'artifacts/contas',
    };
    log.captcha('POST /solve', body);
    const { ok, status, data } = await fetchJSON(`${config.CAPTCHA_API}/solve`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (!ok) {
      throw new Error(`POST /solve falhou: ${data?.error?.message || status}`);
    }
    log.captcha('Job criado', { jobId: data.jobId, status: data.status });
    return data;
  },

  async getJob(jobId) {
    let lastErr;
    for (let attempt = 1; attempt <= 4; attempt++) {
      try {
        const r = await fetch(`${config.CAPTCHA_API}/jobs/${jobId}`);
        return await r.json();
      } catch (e) {
        lastErr = e;
        log.warn('Falha de rede ao consultar job, tentando novamente', { attempt, err: e.message });
        await new Promise(res => setTimeout(res, 1000 * attempt));
      }
    }
    throw lastErr;
  },

  async waitJob(jobId, { timeout = config.POLL_JOB_TIMEOUT_MS, interval = config.POLL_JOB_INTERVAL_MS } = {}) {
    const start = Date.now();
    for (;;) {
      const job = await this.getJob(jobId);
      log.captcha('Job status', { status: job.status, decorrido_ms: Date.now() - start });
      if (job.status === 'succeeded') {
        log.captcha('Captcha resolvido', { attempts: job.result?.attempts, confidence: job.result?.confidence });
        return job;
      }
      if (job.status === 'failed') {
        throw new Error(`Captcha falhou: ${job.result?.error || 'motivo desconhecido'}`);
      }
      if (job.status === 'error') {
        throw new Error(`Erro de infra: ${job.error?.message || 'desconhecido'}`);
      }
      if (Date.now() - start > timeout) {
        throw new Error(`Timeout aguardando job ${jobId}`);
      }
      await new Promise(res => setTimeout(res, interval));
    }
  },

  async solve() {
    const created = await this.createJob();
    return this.waitJob(created.jobId);
  },
};

export default captchaClient;
