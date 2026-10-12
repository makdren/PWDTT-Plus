import { describe, it, expect, beforeEach } from 'vitest';
import { validSni, connectionModeTitle } from '../lib/utils/connection';
import { settingsStore } from '../lib/store';

describe('Подключение', () => {
  beforeEach(() => localStorage.clear());

  it('SNI', () => {
    expect(validSni('')).toBe(true);
    expect(validSni('ya.ru')).toBe(true);
    expect(validSni(' Ya.Ru ')).toBe(true);
    expect(validSni('1.2.3.4')).toBe(false);
    expect(validSni('localhost')).toBe(false);
    expect(validSni('яндекс.рф')).toBe(false);
  });

  it('названия режимов', () => {
    expect(connectionModeTitle('udp')).toBe('UDP');
    expect(connectionModeTitle('stream')).toBe('TCP/TLS');
  });

  it('миграция старых настроек транспорта', () => {
    localStorage.setItem('wdtt_settings:v1', JSON.stringify({ turnMode: 'auto' }));
    expect(settingsStore.get().turnMode).toBe('udp');
    localStorage.setItem('wdtt_settings:v1', JSON.stringify({ turnMode: 'tcp' }));
    expect(settingsStore.get().turnMode).toBe('stream');
    localStorage.setItem('wdtt_settings:v1', JSON.stringify({ turnTcp: true }));
    expect(settingsStore.get().turnMode).toBe('stream');
    localStorage.setItem('wdtt_settings:v1', JSON.stringify({}));
    expect(settingsStore.get().turnMode).toBe('udp');
    expect(settingsStore.get().turnSni).toBe('');
  });
});
