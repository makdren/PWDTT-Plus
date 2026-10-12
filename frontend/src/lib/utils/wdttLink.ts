export type WdttLinkFormat = 'wdtt-plus' | 'legacy' | 'qwdtt';

export interface WdttLink {
  name: string;
  host: string;      // peer address (host:dtlsPort)
  password: string;
  hashes: string[];
  workers?: number;
  port?: string;     // local listen port
  maxWorkers?: number; // предел мощности, выставленный владельцем (WDTT-Plus max_workers)
  format?: WdttLinkFormat;
}

export type WdttLinkResult =
  | { ok: true; link: WdttLink }
  | { ok: false; error: string };

export const WORKERS_PER_GROUP = 9;
export const MAX_WORKERS = 108;
const MAX_HASHES = 4;

// Достаёт первую ссылку wdtt:// или qwdtt:// из произвольного текста
// (сообщение из Telegram, текст с QR и т.п.) и обрезает хвостовую пунктуацию.
export function extractWdttLink(raw: string): string | null {
  const trimmed = raw.trim();
  // Ввод целиком — ссылка (одной строкой): берём как есть, пробелы внутри допустимы.
  // Ссылки WDTT-Plus кодируют пробелы как %20, поэтому для них текст после пробела — не часть ссылки.
  if (/^q?wdtt:\/\//i.test(trimmed) && !/^wdtt:\/\/connect\?/i.test(trimmed) && !/[\r\n]/.test(trimmed)) return trimmed;
  const m = /q?wdtt:\/\//i.exec(raw);
  if (!m) return null;
  const tail = raw.slice(m.index).split(/[\s]/)[0];
  const clean = tail.replace(/[.,;)\]}»"']+$/, '');
  return clean || null;
}

// Короткие защищённые ссылки WDTT-Plus (https://<домен>/c/<код>) выдаются ботом
// и требуют серверного API привязки устройства, которого нет в открытом коде.
function isWdttPlusShortLink(raw: string): boolean {
  return /^https:\/\/[^/\s]+\/c\/[A-Za-z0-9_-]{6,64}(\?open=\d{1,20})?$/i.test(raw.trim());
}

// VK-хеш: голый код или ссылка вида vk.ru/call/join/<код>.
export function normalizeVkHash(input: string): string {
  let s = input.trim().replace(/^[<"']+|[>"']+$/g, '');
  if (!s) return '';
  const idx = s.toLowerCase().indexOf('/call/join/');
  if (idx !== -1) s = s.slice(idx + '/call/join/'.length);
  else if (/^https?:\/\//i.test(s)) return '';
  s = s.split(/[?#/\s]/)[0].trim();
  return /^[A-Za-z0-9_-]{1,512}$/.test(s) ? s : '';
}

function splitHashes(value: string): string[] {
  const out: string[] = [];
  for (const part of value.split(/[,\s]+/)) {
    const h = normalizeVkHash(part);
    if (h && !out.includes(h)) out.push(h);
  }
  return out.slice(0, MAX_HASHES);
}

export function parseWdttLink(raw: string): WdttLinkResult {
  const text = (raw ?? '').trim();
  if (!text) return { ok: false, error: 'Ссылка пустая' };
  if (isWdttPlusShortLink(text)) {
    return {
      ok: false,
      error: 'Короткие ссылки бота WDTT Plus (…/c/…) работают только в Android-приложении. Попросите полную ссылку wdtt://connect?…',
    };
  }
  const clean = extractWdttLink(text);
  if (!clean) return { ok: false, error: 'Ссылка должна начинаться с wdtt:// или qwdtt://' };

  try {
    if (/^qwdtt:\/\/config\?/i.test(clean)) {
      const link = parseQwdttConfig(clean);
      return link ? { ok: true, link } : { ok: false, error: 'В ссылке qwdtt:// нет адреса или пароля' };
    }
    if (/^wdtt:\/\/connect\?/i.test(clean)) return parseWdttPlusConnect(clean);
    if (/^wdtt:\/\//i.test(clean)) {
      const link = parseLegacyWdtt(clean);
      return link ? { ok: true, link } : { ok: false, error: 'Старая ссылка wdtt:// неполная: нужны адрес, порты и пароль' };
    }
  } catch {
    // падаем в общий ответ ниже
  }
  return { ok: false, error: 'Неверный формат ссылки' };
}

export function parseWdttUrl(raw: string): WdttLink | null {
  const r = parseWdttLink(raw);
  return r.ok ? r.link : null;
}

// Парсинг qwdtt://config?name=X&peer=IP:PORT&hashes=H1,H2&workers=N&port=PORT&pass=SECRET
function parseQwdttConfig(raw: string): WdttLink | null {
  const queryStr = raw.replace(/^qwdtt:\/\/config\?/i, '');
  const params = new URLSearchParams(queryStr);

  const name = params.get('name') || 'Server';
  const peer = params.get('peer') || '';
  const pass = params.get('pass') || '';
  const hashesStr = params.get('hashes') || '';
  const workersStr = params.get('workers') || '';
  const port = params.get('port') || '';

  if (!peer || !pass) return null;

  const hashes = hashesStr.split(',').map(h => h.trim()).filter(Boolean);
  const workers = parseWorkers(workersStr);

  return { name, host: peer, password: pass, hashes, workers, port, format: 'qwdtt' };
}

// Ссылка WDTT-Plus (TransferCodec.buildConnectionLink):
// wdtt://connect?v=1&host=H&dtls=P&wg=P&local=P&password=S&hashes=H1,H2[&name=N][&max_workers=N]
// Значения закодированы URLEncoder с заменой "+" на "%20".
function parseWdttPlusConnect(raw: string): WdttLinkResult {
  const query = raw.slice(raw.indexOf('?') + 1);
  const values = new Map<string, string>();
  for (const token of query.split('&')) {
    const sep = token.indexOf('=');
    if (sep <= 0) continue;
    values.set(decode(token.slice(0, sep)), decode(token.slice(sep + 1)));
  }
  if (values.get('v') !== '1') {
    return { ok: false, error: 'Ссылка WDTT Plus неподдерживаемой версии' };
  }

  const host = (values.get('host') ?? '').trim();
  const password = (values.get('password') ?? '').trim();
  if (!isValidHost(host)) return { ok: false, error: 'В ссылке неверный адрес сервера' };
  if (!password) return { ok: false, error: 'В ссылке не указан пароль туннеля' };

  const dtls = parsePort(values.get('dtls'));
  const wg = parsePort(values.get('wg'));
  const local = parsePort(values.get('local'));
  if (dtls === null || wg === null || local === null) {
    return { ok: false, error: 'В ссылке WDTT Plus не хватает портов или они неверны' };
  }

  let maxWorkers: number | undefined;
  const mwRaw = (values.get('max_workers') ?? '').trim();
  if (mwRaw) {
    const n = Number(mwRaw);
    if (!Number.isInteger(n) || n < WORKERS_PER_GROUP || n > MAX_WORKERS || n % WORKERS_PER_GROUP !== 0) {
      return { ok: false, error: 'В ссылке неверный предел мощности (max_workers)' };
    }
    maxWorkers = n;
  }

  const name = (values.get('name') ?? '').trim().slice(0, 40) || 'Server';
  const hashes = splitHashes(values.get('hashes') ?? '');

  return {
    ok: true,
    link: {
      name,
      host: `${host}:${dtls}`,
      password,
      hashes,
      port: String(local),
      maxWorkers,
      workers: maxWorkers,
      format: 'wdtt-plus',
    },
  };
}

function decode(v: string): string {
  try { return decodeURIComponent(v.replace(/\+/g, ' ')); } catch { return v; }
}

function parsePort(v: string | undefined): number | null {
  const s = (v ?? '').trim();
  if (!/^\d{1,5}$/.test(s)) return null;
  const n = Number(s);
  return n >= 1 && n <= 65535 ? n : null;
}

function isValidHost(host: string): boolean {
  if (!host || host.includes('/') || host.includes(':')) return false;
  if (/^(\d{1,3}\.){3}\d{1,3}$/.test(host)) return host.split('.').every(p => Number(p) <= 255);
  return host.length <= 253 && host.includes('.') &&
    host.split('.').every(l => /^[A-Za-z0-9-]{1,63}$/.test(l) && !l.startsWith('-') && !l.endsWith('-'));
}

// workers валиден только как целое число в диапазоне 9–108 (4 хеша × 27).
// Любой мусор (NaN, вне диапазона, нечисловой) → undefined, без clamp.
function parseWorkers(workersStr: string): number | undefined {
  if (!workersStr) return undefined;
  const trimmed = workersStr.trim();
  if (!/^-?\d+$/.test(trimmed)) return undefined;
  const n = Number(trimmed);
  if (!Number.isFinite(n) || !Number.isInteger(n)) return undefined;
  if (n < WORKERS_PER_GROUP || n > MAX_WORKERS) return undefined;
  return n;
}

// Парсинг старого формата (общий для WDTT и WDTT-Plus):
// wdtt://IP:DTLS:WG:LOCAL:PASSWORD[:HASHES][#name]
function parseLegacyWdtt(raw: string): WdttLink | null {
  const stripped = raw.replace(/^wdtt:\/\//i, '');
  const parts = stripped.split(':');
  if (parts.length < 5) return null;

  const ip = parts[0].trim();
  const dtlsPort = parts[1].trim();
  const localPort = parsePort(parts[3]);
  const tail = parts.slice(4).join(':');

  let name = 'Server';
  const hashIdx = tail.lastIndexOf('#');
  let passwordAndHashes = tail;
  if (hashIdx !== -1) {
    const candidate = decode(tail.slice(hashIdx + 1)).trim();
    if (candidate) name = candidate;
    passwordAndHashes = tail.slice(0, hashIdx);
  }

  const colonIdx = passwordAndHashes.lastIndexOf(':');
  let password: string;
  let hashes: string[] = [];
  if (colonIdx !== -1) {
    password = passwordAndHashes.slice(0, colonIdx);
    hashes = splitHashes(passwordAndHashes.slice(colonIdx + 1));
  } else {
    password = passwordAndHashes;
  }
  password = password.trim();

  if (!ip || !/^\d{1,5}$/.test(dtlsPort) || !password) return null;
  return {
    name,
    host: `${ip}:${dtlsPort}`,
    password,
    hashes,
    port: localPort !== null ? String(localPort) : undefined,
    format: 'legacy',
  };
}

type Listener = (link: WdttLink | null) => void;
let pending: WdttLink | null = null;
const listeners = new Set<Listener>();

export const wdttLinkStore = {
  subscribe: (fn: Listener) => { listeners.add(fn); fn(pending); return () => { listeners.delete(fn); }; },
  set: (link: WdttLink | null) => { pending = link; listeners.forEach(fn => fn(link)); },
  consume: () => { const l = pending; pending = null; listeners.forEach(fn => fn(null)); return l; },
};
