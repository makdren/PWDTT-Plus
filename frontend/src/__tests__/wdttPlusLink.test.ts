import { describe, it, expect } from 'vitest';
import { parseWdttLink, parseWdttUrl, extractWdttLink, normalizeVkHash } from '../lib/utils/wdttLink';

// Ссылки в том виде, в каком их строит WDTT-Plus (TransferCodec.buildConnectionLink):
// URLEncoder.encode(...).replace("+", "%20").
const full =
  'wdtt://connect?v=1&host=1.2.3.4&dtls=56000&wg=56001&local=9000' +
  '&password=Ab%2Bc%3D%26d&hashes=AbCdEfGhIjKlMnOp%2CQrStUvWxYz123456' +
  '&name=%D0%9C%D0%BE%D0%B9%20%D1%81%D0%B5%D1%80%D0%B2%D0%B5%D1%80&max_workers=36';

describe('ссылки WDTT-Plus wdtt://connect?', () => {
  it('разбирает все поля', () => {
    const r = parseWdttLink(full);
    expect(r.ok).toBe(true);
    if (!r.ok) return;
    expect(r.link.host).toBe('1.2.3.4:56000');
    expect(r.link.password).toBe('Ab+c=&d');
    expect(r.link.hashes).toEqual(['AbCdEfGhIjKlMnOp', 'QrStUvWxYz123456']);
    expect(r.link.name).toBe('Мой сервер');
    expect(r.link.port).toBe('9000');
    expect(r.link.maxWorkers).toBe(36);
    expect(r.link.format).toBe('wdtt-plus');
  });

  it('доступ без VK-хешей допустим', () => {
    const link = parseWdttUrl('wdtt://connect?v=1&host=vpn.example.com&dtls=56000&wg=56001&local=9000&password=secret&hashes=');
    expect(link).not.toBeNull();
    expect(link!.host).toBe('vpn.example.com:56000');
    expect(link!.hashes).toEqual([]);
    expect(link!.maxWorkers).toBeUndefined();
    expect(link!.name).toBe('Server');
  });

  it('хеши в виде ссылок VK-звонка нормализуются', () => {
    const link = parseWdttUrl('wdtt://connect?v=1&host=1.2.3.4&dtls=1&wg=2&local=3&password=p' +
      '&hashes=' + encodeURIComponent('https://vk.ru/call/join/AbC_dEf-123?x=1,vk.com/call/join/Zz9'));
    expect(link!.hashes).toEqual(['AbC_dEf-123', 'Zz9']);
  });

  it('извлекается из текста сообщения', () => {
    const msg = `Ваш доступ: ${full}. Удачи!`;
    expect(extractWdttLink(msg)).toBe(full);
    expect(parseWdttUrl(msg)!.host).toBe('1.2.3.4:56000');
  });

  it('другая версия → ошибка', () => {
    expect(parseWdttLink(full.replace('v=1', 'v=2')).ok).toBe(false);
  });

  it('нет порта → ошибка', () => {
    expect(parseWdttLink('wdtt://connect?v=1&host=1.2.3.4&dtls=56000&local=9000&password=p').ok).toBe(false);
  });

  it('адрес с портом или схемой → ошибка', () => {
    expect(parseWdttLink('wdtt://connect?v=1&host=1.2.3.4:5&dtls=1&wg=2&local=3&password=p').ok).toBe(false);
    expect(parseWdttLink('wdtt://connect?v=1&host=https%3A%2F%2Fa.b&dtls=1&wg=2&local=3&password=p').ok).toBe(false);
  });

  it('max_workers не кратно 9 → ошибка', () => {
    expect(parseWdttLink(full.replace('max_workers=36', 'max_workers=40')).ok).toBe(false);
  });

  it('короткая ссылка бота — понятная ошибка', () => {
    const r = parseWdttLink('https://example.org/c/AbCdEf123');
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.error).toMatch(/Android/);
  });
});

describe('старый формат WDTT-Plus (6 полей)', () => {
  it('host:dtls:wg:local:password:hashes', () => {
    const link = parseWdttUrl('wdtt://vpn.example.com:56000:56001:9000:secret:AbCdEfGhIjKlMnOp');
    expect(link!.host).toBe('vpn.example.com:56000');
    expect(link!.password).toBe('secret');
    expect(link!.hashes).toEqual(['AbCdEfGhIjKlMnOp']);
    expect(link!.port).toBe('9000');
  });

  it('пустые хеши', () => {
    const link = parseWdttUrl('wdtt://1.2.3.4:56000:56001:9000:secret:');
    expect(link!.password).toBe('secret');
    expect(link!.hashes).toEqual([]);
  });
});

describe('normalizeVkHash', () => {
  it('ссылки и мусор', () => {
    expect(normalizeVkHash(' <https://vk.com/call/join/abc_DEF-1/> ')).toBe('abc_DEF-1');
    expect(normalizeVkHash('https://example.com/x')).toBe('');
    expect(normalizeVkHash('плохой')).toBe('');
  });
});

describe('ссылка WDTT-Plus в начале сообщения', () => {
  it('текст после пробела отбрасывается', () => {
    const link = parseWdttUrl('wdtt://connect?v=1&host=1.2.3.4&dtls=1&wg=2&local=3&password=p&max_workers=18 — ваш доступ');
    expect(link!.maxWorkers).toBe(18);
  });
});
