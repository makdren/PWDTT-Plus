import { useEffect, useState, useRef } from 'react';
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import Layout from './components/Layout';
import Connect from './pages/Connect';
import Logs from './pages/Logs';
import Toast from './components/Toast';
import UpdateModal from './modals/UpdateModal';
import { wdttLinkStore, parseWdttLink } from './lib/utils/wdttLink';
import { toastStore } from './lib/stores/toastStore';
import { logStore } from './lib/stores/logStore';
import { tunnelStore } from './lib/stores/tunnelStore';
import type { LogLevel } from './lib/stores/logStore';
import { EventsOn } from '../wailsjs/runtime/runtime';
import { CheckUpdate, TakePendingLink, ShowWindow } from '../wailsjs/go/backend/App';

function importLinkText(text: string) {
  const r = parseWdttLink(text);
  if (!r.ok) { toastStore.show(r.error, 5000); return; }
  wdttLinkStore.set(r.link);
}

// Ссылки, переданные системой (клик по wdtt:// в браузере или Telegram).
function useSystemLinks() {
  useEffect(() => {
    const take = () => {
      TakePendingLink().then(link => {
        if (!link) return;
        ShowWindow().catch(() => {});
        importLinkText(link);
      }).catch(() => {});
    };
    take();
    return EventsOn('wdtt_link', take);
  }, []);
}

function useWdttPaste() {
  useEffect(() => {
    const handler = (e: ClipboardEvent) => {
      const text = e.clipboardData?.getData('text') ?? '';
      const looksLikeLink = /q?wdtt:\/\//i.test(text) || /^https:\/\/[^/\s]+\/c\/[A-Za-z0-9_-]{6,64}/i.test(text.trim());
      if (!looksLikeLink) return;
      const tag = (document.activeElement as HTMLElement)?.tagName;
      if (tag === 'INPUT' || tag === 'TEXTAREA') return;
      e.preventDefault();
      importLinkText(text);
    };
    document.addEventListener('paste', handler);
    document.body.tabIndex = 0;
    return () => document.removeEventListener('paste', handler);
  }, []);
}

function useWailsEvents() {
  useEffect(() => {
    const offs = [
      EventsOn('log', (level: unknown, msg: unknown) => {
        logStore.push((level as LogLevel) ?? 'INFO', String(msg ?? ''));
      }),
      EventsOn('log_batch', (batch: unknown) => {
        if (!Array.isArray(batch)) return;
        for (const item of batch) {
          if (Array.isArray(item)) logStore.push((item[0] as LogLevel) ?? 'INFO', String(item[1] ?? ''));
        }
      }),
      EventsOn('error', (msg: unknown) => {
        const s = String(msg ?? '');
        logStore.push('ERROR', s);
        toastStore.show(s, 5000);
      }),
      EventsOn('state_changed', (status: unknown) => {
        const s = String(status ?? '');
        if (s === 'connected' || s === 'running') { tunnelStore.set('connected'); logStore.push('INFO', '✓ Туннель активен'); }
        else if (s === 'connecting') { tunnelStore.set('connecting'); logStore.clear(); logStore.push('INFO', '⟳ Подключение...'); }
        else if (s === 'stopped' || s === 'error' || s === 'disconnected') { tunnelStore.set('idle'); logStore.push('INFO', '— Отключено'); }
      }),
      // Окно в трее: ставим анимации на паузу, чтобы WebView2 не рисовал зря.
      EventsOn('window_visible', (visible: unknown) => {
        document.documentElement.classList.toggle('app-hidden', visible === false);
      }),
      EventsOn('event', (name: unknown) => {
        if (name === 'wg_config') tunnelStore.set('connected');
      }),
    ];
    return () => offs.forEach(off => off());
  }, []);
}

interface UpdateState { version: string; body: string; url: string; pageUrl?: string; canInstall?: boolean }

// Интервал повторной проверки: 5 минут
const RETRY_MS = 5 * 60 * 1000;

function useUpdateChecker() {
  const [updateInfo, setUpdateInfo] = useState<UpdateState | null>(null);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    const check = () => {
      CheckUpdate().then(info => {
        if (info.available) {
          setUpdateInfo({ version: info.version, body: info.body, url: info.url, pageUrl: info.pageUrl, canInstall: info.canInstall });
        }
      }).catch(() => {
        timerRef.current = setTimeout(check, RETRY_MS);
      });
    };
    check();
    // Проверка из настроек («Проверить обновления») открывает то же окно.
    const off = EventsOn('show_update', (info: unknown) => setUpdateInfo(info as UpdateState));
    return () => { off(); if (timerRef.current) clearTimeout(timerRef.current); };
  }, []);

  return { updateInfo, closeUpdate: () => setUpdateInfo(null) };
}

export default function App() {
  useWailsEvents();
  useWdttPaste();
  useSystemLinks();
  const { updateInfo, closeUpdate } = useUpdateChecker();

  return (
    <BrowserRouter>
      <Routes>
        <Route element={<Layout />}>
          <Route path="/" element={<Connect />} />
          <Route path="/logs" element={<Logs />} />
        </Route>
      </Routes>
      <Toast />
      {updateInfo && (
        <UpdateModal
          version={updateInfo.version}
          body={updateInfo.body}
          url={updateInfo.url}
          pageUrl={updateInfo.pageUrl}
          canInstall={updateInfo.canInstall}
          onClose={closeUpdate}
        />
      )}
    </BrowserRouter>
  );
}
