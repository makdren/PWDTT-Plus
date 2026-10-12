export interface Server {
  id: string;
  name: string;
  host: string;
  password: string;
  deviceId?: string;
  ping?: number;
  icon?: string;
  hashes?: [string, string, string, string];
  power?: number;
  listen?: string;     // локальный порт (local из ссылки WDTT-Plus)
  maxWorkers?: number; // предел мощности от владельца сервера (max_workers)
}

export interface AppSettings {
  autoStart: boolean;
  obfsMode: 'audio' | 'video';
  obfsAccepted: boolean;
  turnTcp: boolean;
  turnMode: TurnMode;
  turnSni: string;
  exclusions: string[];
}

// «Подключение» как в WDTT Plus: UDP (запасной TCP/TLS) или только TCP/TLS.
export type TurnMode = 'udp' | 'stream';

export type TunnelState = 'idle' | 'connecting' | 'connected' | 'disconnecting';

export const DEFAULT_SETTINGS: AppSettings = {
  autoStart: true,
  obfsMode: 'audio',
  obfsAccepted: false,
  turnTcp: false,
  turnMode: 'udp',
  turnSni: '',
  exclusions: [],
};
