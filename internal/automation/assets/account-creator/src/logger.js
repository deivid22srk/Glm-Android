import fs from 'node:fs';
import { config } from './config.js';

if (!fs.existsSync(config.LOG_DIR)) fs.mkdirSync(config.LOG_DIR, { recursive: true });

const ts = () => new Date().toISOString();

export const log = {
  _write(tag, msg, extra) {
    const line = `[${ts()}] [${tag}] ${msg}${extra ? ' ' + JSON.stringify(extra) : ''}`;
    console.log(line);
    fs.appendFileSync(config.LOG_FILE, line + '\n');
  },
  info(msg, extra) { this._write('INFO', msg, extra); },
  ok(msg, extra) { this._write('OK  ', msg, extra); },
  warn(msg, extra) { this._write('WARN', msg, extra); },
  error(msg, extra) { this._write('ERR ', msg, extra); },
  step(msg, extra) { this._write('STEP', msg, extra); },
  email(msg, extra) { this._write('MAIL', msg, extra); },
  zai(msg, extra) { this._write('ZAI ', msg, extra); },
  captcha(msg, extra) { this._write('CAPT', msg, extra); },
};

export default log;
