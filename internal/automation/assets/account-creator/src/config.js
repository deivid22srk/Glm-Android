import path from 'node:path';
import os from 'node:os';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const ROOT = path.resolve(__dirname, '..');
const DATA_DIR = process.env.ZCODE_ACCOUNT_CREATOR_DATA_DIR || path.join(ROOT, 'data');
const LOG_DIR = process.env.ZCODE_ACCOUNT_CREATOR_LOG_DIR || path.join(ROOT, 'logs');
const DOCUMENTS_DIR = path.join(os.homedir(), 'Documents');
const fallbackWorkdirs = (
  process.env.ZCODE_ACCOUNT_CREATOR_CAPTCHA_API_FALLBACK_WORKDIRS
    ? process.env.ZCODE_ACCOUNT_CREATOR_CAPTCHA_API_FALLBACK_WORKDIRS.split(path.delimiter)
    : [
        path.join(DOCUMENTS_DIR, 'dev', 'teste frotend kimi', 'aliyun-captcha-solver'),
        path.join(DOCUMENTS_DIR, 'aliyun-captcha-solver'),
      ]
).map((value) => value.trim()).filter(Boolean);

export const config = {
  ROOT,
  DATA_DIR,
  LOG_DIR,
  EMAILS_FILE: process.env.ZCODE_ACCOUNT_CREATOR_EMAILS_FILE || path.join(DATA_DIR, 'emails.json'),
  LOG_FILE: process.env.ZCODE_ACCOUNT_CREATOR_LOG_FILE || path.join(LOG_DIR, 'run.log'),

  TEMP_MAIL_URL: 'https://tuamaeaquelaursa.com/',
  ZAI_AUTH_URL: 'https://chat.z.ai/auth',
  ZCODE_PROXY_BASE_URL: process.env.ZCODE_PROXY_BASE_URL || 'http://127.0.0.1:3005',

  CDP_HOST: process.env.ZCODE_ACCOUNT_CREATOR_CDP_HOST || '127.0.0.1',
  CDP_PORT: Number(process.env.ZCODE_ACCOUNT_CREATOR_CDP_PORT || '9222'),

  CAPTCHA_API: process.env.ZCODE_ACCOUNT_CREATOR_CAPTCHA_API || 'http://127.0.0.1:8787',
  CAPTCHA_API_WORKDIR: process.env.CAPTCHA_API_WORKDIR || '',
  CAPTCHA_API_FALLBACK_WORKDIRS: fallbackWorkdirs,
  CAPTCHA_API_START_TIMEOUT_MS: Number(process.env.ZCODE_ACCOUNT_CREATOR_CAPTCHA_API_START_TIMEOUT_MS || '30000'),
  CAPTCHA_RETRIES: Number(process.env.ZCODE_ACCOUNT_CREATOR_CAPTCHA_RETRIES || '3'),
  CAPTCHA_GESTURE: process.env.ZCODE_ACCOUNT_CREATOR_CAPTCHA_GESTURE || 'human_replay',

  PASSWORD: 'Kricktcaro#1234',
  USERNAME_PREFIX: 'zaiconta',

  POLL_EMAIL_INTERVAL_MS: Number(process.env.ZCODE_ACCOUNT_CREATOR_POLL_EMAIL_INTERVAL_MS || '5000'),
  POLL_EMAIL_TIMEOUT_MS: Number(process.env.ZCODE_ACCOUNT_CREATOR_POLL_EMAIL_TIMEOUT_MS || '180000'),
  POLL_JOB_INTERVAL_MS: Number(process.env.ZCODE_ACCOUNT_CREATOR_POLL_JOB_INTERVAL_MS || '1000'),
  POLL_JOB_TIMEOUT_MS: Number(process.env.ZCODE_ACCOUNT_CREATOR_POLL_JOB_TIMEOUT_MS || '120000'),

  NAV_TIMEOUT_MS: Number(process.env.ZCODE_ACCOUNT_CREATOR_NAV_TIMEOUT_MS || '45000'),
  PROXY_LINK_TIMEOUT_MS: Number(process.env.ZCODE_ACCOUNT_CREATOR_PROXY_LINK_TIMEOUT_MS || '180000'),
  PROXY_QUOTA_TIMEOUT_MS: Number(process.env.ZCODE_ACCOUNT_CREATOR_PROXY_QUOTA_TIMEOUT_MS || '120000'),
  SLOW_MO_MS: Number(process.env.ZCODE_ACCOUNT_CREATOR_SLOW_MO_MS || '50'),
  HEADLESS: process.env.ZCODE_ACCOUNT_CREATOR_HEADLESS === '1' || process.env.ZCODE_ACCOUNT_CREATOR_HEADLESS === 'true',
};
