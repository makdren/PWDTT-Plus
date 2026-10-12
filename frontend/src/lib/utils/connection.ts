import type { TurnMode } from '../types';

export function connectionModeTitle(mode: TurnMode): string {
  return mode === 'stream' ? 'TCP/TLS' : 'UDP';
}

// То же правило, что в ядре (NormalizeFrontSNI): домен латиницей, не IP.
export function validSni(value: string): boolean {
  const host = value.trim().toLowerCase();
  if (!host) return true;
  if (host.length > 253 || /^\d+\.\d+\.\d+\.\d+$/.test(host)) return false;
  const labels = host.split('.');
  return labels.length >= 2 && labels.every(l => /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/.test(l));
}
