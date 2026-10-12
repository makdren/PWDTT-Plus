import { useState } from 'react';
import { IconRouteOff, IconX } from '@tabler/icons-react';
import { settingsStore } from '../lib/store';
import { toastStore } from '../lib/stores/toastStore';
import { UpdateExclusions } from '../../wailsjs/go/backend/App';

interface Props {
  onClose: () => void;
}

// Сайты, IP и подсети, которые открываются напрямую, минуя туннель.
export default function Exclusions({ onClose }: Props) {
  const [text, setText] = useState(() => settingsStore.get().exclusions.join('\n'));
  const [saving, setSaving] = useState(false);

  const save = async () => {
    if (saving) return;
    setSaving(true);
    const lines = text.split(/\r?\n/).map(l => l.trim()).filter(Boolean);
    try {
      const entries = (await UpdateExclusions(lines)) ?? [];
      const values = entries.map(e => e.value);
      settingsStore.save({ ...settingsStore.get(), exclusions: values });
      const skipped = lines.filter(l => !l.startsWith('#')).length - values.length;
      toastStore.show(
        skipped > 0
          ? `Сохранено: ${values.length}. Не распознано или повторы: ${skipped}`
          : `Исключений: ${values.length}`,
        3500,
      );
      onClose();
    } catch {
      toastStore.show('Не удалось сохранить исключения', 3000);
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <style>{`
        .ex-overlay { position: fixed; inset: 0; background: var(--overlay-bg); backdrop-filter: blur(4px); display: flex; align-items: center; justify-content: center; z-index: 200; animation: overlay-in 0.3s ease-out; }
        .ex-modal { background: var(--surface); border-radius: 14px; padding: 20px; width: 420px; max-width: 95vw; box-shadow: var(--shadow); border: 1px solid var(--border); animation: modal-in 0.3s ease-out; }
        .ex-header { display: flex; align-items: center; gap: 10px; margin-bottom: 12px; color: var(--text); }
        .ex-title { font-size: 16px; font-weight: 600; flex: 1; }
        .ex-close { background: none; border: none; cursor: pointer; color: var(--text); padding: 0; line-height: 1; }
        .ex-hint { font-size: 12px; color: var(--text-4); margin-bottom: 10px; line-height: 1.45; }
        .ex-area { width: 100%; height: 220px; resize: vertical; padding: 11px 14px; border: 1.5px solid var(--input-border); border-radius: 10px; font-size: 13px; font-family: ui-monospace, Consolas, monospace; outline: none; box-sizing: border-box; color: var(--text); background: var(--input-bg); }
        .ex-btn { width: 100%; padding: 13px; border: none; border-radius: 10px; background: var(--accent); color: var(--accent-fg); font-size: 14px; font-family: 'Geist', sans-serif; font-weight: 600; cursor: pointer; margin-top: 10px; }
        .ex-btn:disabled { opacity: 0.4; cursor: not-allowed; }
      `}</style>
      <div className="ex-overlay">
        <div className="ex-modal" onClick={e => e.stopPropagation()}>
          <div className="ex-header">
            <IconRouteOff stroke={2} size={22} />
            <span className="ex-title">Исключения из туннеля</span>
            <button type="button" className="ex-close" onClick={onClose} aria-label="Закрыть"><IconX size={18} /></button>
          </div>
          <div className="ex-hint">
            По одному на строку: сайт (<code>gosuslugi.ru</code>, ссылка тоже подойдёт), IP (<code>1.2.3.4</code>) или подсеть (<code>10.0.0.0/8</code>).
            Трафик к ним пойдёт напрямую. Поддомены добавляйте отдельно. Изменения применяются сразу, даже при активном подключении.
          </div>
          <textarea
            className="ex-area"
            value={text}
            onChange={e => setText(e.target.value)}
            placeholder={'sberbank.ru\nhttps://www.gosuslugi.ru\n192.168.0.0/16'}
            spellCheck={false}
          />
          <button type="button" className="ex-btn" onClick={save} disabled={saving}>
            {saving ? 'Сохранение...' : 'Сохранить'}
          </button>
        </div>
      </div>
    </>
  );
}
