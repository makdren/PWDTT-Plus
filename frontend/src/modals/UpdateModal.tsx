import { useEffect, useState } from 'react';
import { IconX, IconDownload, IconRocket, IconExternalLink } from '@tabler/icons-react';
import { BrowserOpenURL, EventsOn } from '../../wailsjs/runtime/runtime';
import { InstallUpdate } from '../../wailsjs/go/backend/App';
import { renderMarkdown } from '../lib/utils/markdown';

interface Props {
  version: string;
  body: string;
  url: string;
  pageUrl?: string;
  canInstall?: boolean;
  onClose: () => void;
}

const RELEASES_URL = 'https://github.com/makdren/PWDTT-Plus/releases/latest';

export default function UpdateModal({ version, body, url, pageUrl, canInstall, onClose }: Props) {
  const sanitizedHtml = renderMarkdown(body);
  const [progress, setProgress] = useState<number | null>(null);
  const [error, setError] = useState('');
  const busy = progress !== null && !error;

  useEffect(() => EventsOn('update_progress', (p: unknown) => setProgress(Number(p) || 0)), []);

  const install = async () => {
    setError('');
    setProgress(0);
    try {
      await InstallUpdate();
      setProgress(100); // приложение перезапустится само
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : String(e));
    }
  };

  const openPage = () => { BrowserOpenURL(pageUrl || url || RELEASES_URL); onClose(); };

  return (
    <>
      <style>{`
        .upd-overlay { position: fixed; inset: 0; background: var(--overlay-bg); backdrop-filter: blur(6px); display: flex; align-items: center; justify-content: center; z-index: 200; animation: overlay-in 0.25s ease-out; }
        .upd-modal { background: var(--surface); border-radius: 16px; padding: 24px; width: 420px; max-width: 92vw; box-shadow: var(--shadow); animation: modal-in 0.3s ease-out; border: 1px solid var(--border); position: relative; overflow: hidden; }
        .upd-modal::before { content: ''; position: absolute; top: 0; left: 0; right: 0; height: 3px; background: linear-gradient(90deg, var(--accent), #22c55e); }
        .upd-header { display: flex; align-items: center; gap: 10px; margin-bottom: 16px; }
        .upd-icon { width: 40px; height: 40px; border-radius: 12px; background: rgba(34, 197, 94, 0.12); display: flex; align-items: center; justify-content: center; color: #22c55e; flex-shrink: 0; }
        .upd-title { font-size: 16px; font-weight: 700; color: var(--text); }
        .upd-version { font-size: 13px; color: var(--text-3); margin-top: 2px; }
        .upd-close { position: absolute; top: 16px; right: 16px; background: none; border: none; cursor: pointer; color: var(--text-3); padding: 4px; border-radius: 6px; transition: color 0.15s, background 0.15s; }
        .upd-close:hover { color: var(--text); background: var(--bg-3); }
        .upd-body { font-size: 13px; color: var(--text-2); line-height: 1.6; max-height: 200px; overflow-y: auto; padding: 12px 14px; background: var(--bg-2); border-radius: 10px; margin-bottom: 16px; word-break: break-word; }
        .upd-body h3 { font-size: 14px; font-weight: 700; color: var(--text); margin: 12px 0 6px; }
        .upd-body h3:first-child { margin-top: 0; }
        .upd-body ul { margin: 4px 0; padding-left: 18px; }
        .upd-body li { margin: 2px 0; }
        .upd-body code { font-size: 12px; background: var(--bg-3); padding: 1px 5px; border-radius: 4px; font-family: monospace; }
        .upd-body a { color: var(--accent); text-decoration: none; }
        .upd-body a:hover { text-decoration: underline; }
        .upd-actions { display: flex; gap: 10px; }
        .upd-btn { flex: 1; display: flex; align-items: center; justify-content: center; gap: 6px; padding: 11px; border-radius: 10px; font-size: 13px; font-weight: 600; cursor: pointer; transition: all 0.15s; font-family: 'Geist', sans-serif; }
        .upd-btn--primary { background: var(--accent); color: var(--accent-fg); border: none; }
        .upd-btn--primary:hover { opacity: 0.9; box-shadow: 0 4px 12px rgba(0,0,0,0.2); }
        .upd-btn--secondary { background: transparent; color: var(--text-3); border: 1px solid var(--border); }
        .upd-btn--secondary:hover { border-color: var(--text-3); color: var(--text); }
      `}</style>
      <div className="upd-overlay" onClick={busy ? undefined : onClose}>
        <div className="upd-modal" onClick={e => e.stopPropagation()}>
          {!busy && <button type="button" className="upd-close" onClick={onClose} aria-label="Закрыть"><IconX size={18} /></button>}

          <div className="upd-header">
            <div className="upd-icon"><IconRocket size={20} /></div>
            <div>
              <div className="upd-title">Доступно обновление</div>
              <div className="upd-version">v{version}</div>
            </div>
          </div>

          {body && <div className="upd-body" dangerouslySetInnerHTML={{ __html: sanitizedHtml }} />}

          {busy && (
            <div style={{ marginBottom: 14 }}>
              <div style={{ height: 6, borderRadius: 3, background: 'var(--bg-3)', overflow: 'hidden' }}>
                <div style={{ width: `${progress}%`, height: '100%', background: 'var(--accent)', transition: 'width 0.2s' }} />
              </div>
              <div style={{ fontSize: 12, color: 'var(--text-3)', marginTop: 6 }}>
                {progress! < 100 ? `Скачивание… ${progress}%` : 'Установка и перезапуск… VPN будет переподключён вручную'}
              </div>
            </div>
          )}
          {error && <div style={{ fontSize: 12, color: '#ef4444', marginBottom: 12 }}>{error}</div>}

          <div className="upd-actions">
            {canInstall ? (
              <>
                <button type="button" className="upd-btn upd-btn--secondary" onClick={error ? openPage : onClose} disabled={busy}>
                  {error ? <><IconExternalLink size={16} />Страница релиза</> : 'Позже'}
                </button>
                <button type="button" className="upd-btn upd-btn--primary" onClick={install} disabled={busy}>
                  <IconDownload size={16} />
                  {error ? 'Повторить' : 'Обновить'}
                </button>
              </>
            ) : (
              <>
                <button type="button" className="upd-btn upd-btn--secondary" onClick={onClose}>
                  Позже
                </button>
                <button type="button" className="upd-btn upd-btn--primary" onClick={openPage}>
                  <IconDownload size={16} />
                  Скачать
                </button>
              </>
            )}
          </div>
        </div>
      </div>
    </>
  );
}
