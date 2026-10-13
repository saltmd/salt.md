import { useState } from 'react';
import Portal from './Portal';
import { useExclusiveModal } from '../modal';
import { api } from '../api';
import { applyPrefs, getPrefs, t, type Prefs } from '../i18n';

// Whether collection rows open in a new tab (W127).
//
// A plain click on a row navigates the current tab in place, like the
// sidebar, so looking at a task never loses the collection behind it. The
// account can opt into always opening rows in a new tab instead; ⌘/Ctrl
// opens a new tab either way, as in a browser. The choice is an account
// preference, like the language. The phone and the laptop must not disagree
// about what a click does.
export function RowOpenModal({ onClose }: { onClose: () => void }) {
  useExclusiveModal(onClose);
  const [prefs, setPrefs] = useState<Prefs>(getPrefs());
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  // Absent is automatic, which means off: rows navigate unless the account
  // explicitly opted into new tabs.
  const checked = prefs.openRowsNewTab === true;

  const save = async () => {
    setSaving(true);
    setError('');
    try {
      const stored = await api.putPrefs({ ...prefs, openRowsNewTab: checked });
      await applyPrefs(stored);
      onClose();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      setSaving(false);
    }
  };

  return (
    <Portal>
      <div
        className="modal-overlay"
        onMouseDown={(e) => {
          if (e.target === e.currentTarget) onClose();
        }}
      >
        <div className="dialog" role="dialog" aria-modal="true" aria-label={t('Open rows in new tab')}>
          <h2>{t('Open rows in new tab')}</h2>
          <p className="dialog-hint">
            {t(
              'A plain click on a row opens it in a new tab instead of the current one. Holding ⌘ or Ctrl always opens a new tab, as in a browser.',
            )}
          </p>
          <label className="check-label">
            <input
              type="checkbox"
              checked={checked}
              onChange={(e) => setPrefs({ ...prefs, openRowsNewTab: e.target.checked })}
            />
            {t('Open collection rows in a new tab')}
          </label>
          {error && <div className="login-error">{error}</div>}
          <div className="dialog-actions">
            <button className="btn" onClick={onClose}>
              {t('Cancel')}
            </button>
            <button className="btn primary" onClick={() => void save()} disabled={saving}>
              {saving ? t('Saving…') : t('Save')}
            </button>
          </div>
        </div>
      </div>
    </Portal>
  );
}
