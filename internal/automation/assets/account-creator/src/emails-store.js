import fs from 'node:fs';
import { config } from './config.js';
import log from './logger.js';

function _read() {
  if (!fs.existsSync(config.EMAILS_FILE)) {
    return { emails: [], contas_criadas: 0 };
  }
  try {
    const raw = fs.readFileSync(config.EMAILS_FILE, 'utf8').replace(/^\uFEFF/, '');
    return JSON.parse(raw);
  } catch (e) {
    log.error('Falha ao ler emails.json, recriando', { err: e.message });
    return { emails: [], contas_criadas: 0 };
  }
}

function _write(data) {
  fs.mkdirSync(config.DATA_DIR, { recursive: true });
  fs.writeFileSync(config.EMAILS_FILE, JSON.stringify(data, null, 2));
}

export const store = {
  exists(email) {
    return _read().emails.some(e => e.email === email);
  },

  nextAccountNumber() {
    return _read().contas_criadas + 1;
  },

  addInUse(email, username) {
    const data = _read();
    if (data.emails.some(e => e.email === email)) {
      log.warn('Email ja existe no store, nao duplicando', { email });
      return;
    }
    data.emails.push({
      email,
      username,
      status: 'em_uso',
      criado_em: new Date().toISOString(),
    });
    _write(data);
    log.email('Email marcado como em_uso', { email, username });
  },

  markCompleted(email) {
    const data = _read();
    const entry = data.emails.find(e => e.email === email);
    if (entry) {
      entry.status = 'concluido';
      entry.concluido_em = new Date().toISOString();
    }
    data.contas_criadas = data.contas_criadas + 1;
    _write(data);
    log.email('Conta concluida', { email, total: data.contas_criadas });
  },

  markFailed(email, error) {
    if (!email) {
      return;
    }
    const data = _read();
    const entry = data.emails.find(e => e.email === email);
    if (!entry || entry.status === 'concluido') {
      return;
    }
    entry.status = 'falhou';
    entry.erro = error;
    entry.falhou_em = new Date().toISOString();
    _write(data);
    log.email('Conta marcada como falhou', { email, error });
  },

  all() {
    return _read();
  },
};

export default store;
