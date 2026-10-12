import { useState } from 'react';
import { IconX, IconHelpCircle, IconChevronDown } from '@tabler/icons-react';
import { settingsStore } from '../lib/store';
import type { TurnMode } from '../lib/types';
import { validSni } from '../lib/utils/connection';

interface Props {
  onClose: () => void;
}

// Как «Подключение» в WDTT Plus: UDP с запасным TCP/TLS или только TCP/TLS.
const MODES: { mode: TurnMode; title: string; subtitle: string }[] = [
  { mode: 'udp', title: 'UDP', subtitle: 'UDP, при недоступности — TCP/TLS' },
  { mode: 'stream', title: 'TCP/TLS', subtitle: 'Без внешнего UDP' },
];

export default function Connection({ onClose }: Props) {
  const [mode, setMode] = useState<TurnMode>(() => settingsStore.get().turnMode);
  const [sni, setSni] = useState(() => settingsStore.get().turnSni);
  const [extraOpen, setExtraOpen] = useState(() => settingsStore.get().turnSni !== '');
  const [help, setHelp] = useState(false);
  const sniOk = validSni(sni);

  // Изменения сохраняются сразу и используются при следующем подключении.
  const save = (next: { mode?: TurnMode; sni?: string }) => {
    const cur = settingsStore.get();
    const m = next.mode ?? cur.turnMode;
    const s = next.sni ?? cur.turnSni;
    settingsStore.save({ ...cur, turnMode: m, turnTcp: m === 'stream', turnSni: validSni(s) ? s.trim().toLowerCase() : cur.turnSni });
  };

  return (
    <>
      <style>{`
        .cn-overlay { position: fixed; inset: 0; background: var(--overlay-bg); backdrop-filter: blur(4px); display: flex; align-items: center; justify-content: center; z-index: 200; animation: overlay-in 0.3s ease-out; }
        .cn-modal { background: var(--surface); border-radius: 16px; padding: 20px; width: 400px; max-width: 95vw; box-shadow: var(--shadow); border: 1px solid var(--border); animation: modal-in 0.3s ease-out; }
        .cn-header { display: flex; align-items: center; gap: 8px; margin-bottom: 14px; color: var(--text); }
        .cn-title { font-size: 18px; font-weight: 700; color: var(--accent); }
        .cn-icon-btn { background: none; border: none; cursor: pointer; color: var(--text-3); padding: 2px; display: flex; }
        .cn-icon-btn:hover { color: var(--text); }
        .cn-close { margin-left: auto; }
        .cn-hint { font-size: 12px; color: var(--text-3); margin-bottom: 12px; line-height: 1.5; }
        .cn-option { width: 100%; display: flex; align-items: center; gap: 12px; padding: 12px 14px; border-radius: 12px; border: 1.5px solid var(--border); background: var(--bg-2); color: var(--text); cursor: pointer; margin-bottom: 8px; text-align: left; font-family: 'Geist', sans-serif; }
        .cn-option--active { border-color: var(--accent); background: var(--bg-3); }
        .cn-radio { width: 18px; height: 18px; border-radius: 50%; border: 2px solid var(--text-3); flex: 0 0 auto; display: flex; align-items: center; justify-content: center; }
        .cn-option--active .cn-radio { border-color: var(--accent); }
        .cn-option--active .cn-radio::after { content: ''; width: 8px; height: 8px; border-radius: 50%; background: var(--accent); }
        .cn-opt-title { font-size: 14px; font-weight: 600; }
        .cn-opt-sub { font-size: 12px; color: var(--text-3); margin-top: 2px; }
        .cn-extra { width: 100%; display: flex; align-items: center; justify-content: space-between; padding: 12px 14px; border-radius: 12px; border: 1.5px solid var(--border); background: var(--bg-2); color: var(--text); cursor: pointer; font-family: 'Geist', sans-serif; text-align: left; }
        .cn-extra-body { padding: 12px 2px 2px; }
        .cn-input { width: 100%; padding: 10px 12px; border: 1.5px solid var(--input-border); border-radius: 10px; font-size: 13px; outline: none; box-sizing: border-box; color: var(--text); background: var(--input-bg); font-family: 'Geist', sans-serif; }
        .cn-input--bad { border-color: #ef4444; }
        .cn-label { font-size: 12px; color: var(--text-3); margin: 0 2px 6px; }
        .cn-help { font-size: 12px; color: var(--text-2); background: var(--bg-2); border-radius: 10px; padding: 10px 12px; margin-bottom: 12px; line-height: 1.55; }
      `}</style>
      <div className="cn-overlay" onClick={onClose}>
        <div className="cn-modal" onClick={e => e.stopPropagation()}>
          <div className="cn-header">
            <span className="cn-title">Подключение</span>
            <button type="button" className="cn-icon-btn" onClick={() => setHelp(h => !h)} aria-label="Справка"><IconHelpCircle size={20} /></button>
            <button type="button" className="cn-icon-btn cn-close" onClick={onClose} aria-label="Закрыть"><IconX size={20} /></button>
          </div>

          {help && (
            <div className="cn-help">
              <b>UDP</b> — все потоки идут к серверам VK по UDP. На TCP/TLS клиент переходит, только когда рабочих UDP-каналов не осталось; новые проверки UDP после этого откладываются на 5 минут. Отказ отдельного канала не отключает остальные.<br />
              <b>TCP/TLS</b> — внешний UDP не используется: сначала TLS, затем TCP. Для сетей, где UDP режут. Обычно медленнее UDP.<br />
              В обоих режимах канал считается рабочим, только когда по нему отвечает ваш сервер.
            </div>
          )}

          <div className="cn-hint">Изменения сохраняются сразу и будут использованы при следующем подключении.</div>

          {MODES.map(m => (
            <button key={m.mode} type="button" className={`cn-option${mode === m.mode ? ' cn-option--active' : ''}`}
              onClick={() => { setMode(m.mode); save({ mode: m.mode }); }}>
              <span className="cn-radio" />
              <span>
                <div className="cn-opt-title">{m.title}</div>
                <div className="cn-opt-sub">{m.subtitle}</div>
              </span>
            </button>
          ))}

          <button type="button" className="cn-extra" onClick={() => setExtraOpen(o => !o)}>
            <span>
              <div className="cn-opt-title">Дополнительно</div>
              <div className="cn-opt-sub">{sni ? `SNI ${sni}` : 'Для сетей с ограничениями'}</div>
            </span>
            <IconChevronDown size={18} style={{ transform: extraOpen ? 'rotate(180deg)' : undefined, transition: 'transform 0.2s' }} />
          </button>
          {extraOpen && (
            <div className="cn-extra-body">
              <div className="cn-label">Внешний SNI для TURN/TLS</div>
              <input
                className={`cn-input${sniOk ? '' : ' cn-input--bad'}`}
                placeholder="ya.ru"
                value={sni}
                spellCheck={false}
                onChange={e => { setSni(e.target.value); if (validSni(e.target.value)) save({ sni: e.target.value }); }}
              />
              <div className="cn-opt-sub" style={{ margin: '6px 2px 0', color: sniOk ? undefined : '#ef4444' }}>
                {sniOk
                  ? 'Имя сайта из белого списка вашей сети. Сертификат TURN всё равно проверяется по настоящему имени. Пусто — без подмены.'
                  : 'Нужен домен латиницей, например ya.ru'}
              </div>
            </div>
          )}
        </div>
      </div>
    </>
  );
}
