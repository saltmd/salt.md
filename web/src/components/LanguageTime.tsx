import { useEffect, useState } from 'react';
import Portal from './Portal';
import { useExclusiveModal } from '../modal';
import { api } from '../api';
import {
  applyPrefs,
  automaticFormatTag,
  getPrefs,
  LOCALES,
  previewFormat,
  t,
  type Prefs,
} from '../i18n';
import {
  availableTimeZones,
  formatDay,
  formatMoment,
  regionLabel,
  resolvedClock,
  resolvedTimeZone,
  weekdayLabel,
  firstWeekday,
} from '../format';

// Language and time settings (W112).
//
// Until now there was no picker at all: the language came from the browser and
// the only way to change it was to edit localStorage by hand. Everything else —
// date format, timezone, clock, first weekday — was never asked and never
// shown.
//
// Each setting is ONE control, not a toggle plus a control. The first entry is
// "Automatic", and it names what automatic currently resolves to, because a
// setting whose automatic value is invisible is one people switch to manual
// merely to find out what it was. Empty value = automatic is also exactly how
// the column stores it (see server/prefs.go), so there is no third state
// anywhere in the chain to get wrong.

/** Regional formats offered by name. Deliberately a short, curated list rather
 *  than every tag Intl knows: the point is to fix "my laptop says en-US but I
 *  write dates the European way", and a thousand-entry dropdown does not help
 *  anybody do that. */
// prettier-ignore — one tag per line would need one i18n-ok marker per line,
// and the check reads lines. Grouped, four markers cover the lot.
// prettier-ignore
const REGIONS = [
  'de-DE', 'de-AT', 'de-CH',                     // i18n-ok: the tags ARE the setting
  'en-GB', 'en-US', 'en-IE', 'en-AU', 'en-CA',   // i18n-ok: same
  'fr-FR', 'fr-CH', 'it-IT', 'es-ES', 'nl-NL',   // i18n-ok: same
  'pl-PL', 'pt-BR', 'sv-SE', 'da-DK', 'cs-CZ',   // i18n-ok: same
];

export function LanguageTimeModal({ onClose }: { onClose: () => void }) {
  useExclusiveModal(onClose);
  const [prefs, setPrefs] = useState<Prefs>(getPrefs());
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  // The FORMAT settings preview as you pick them; the LANGUAGE waits for Save.
  //
  // Not a style choice — changing the language remounts the whole tree (see
  // main.tsx), which would destroy this dialog mid-edit and re-run App's mount
  // effect, and that re-fetches /api/me and re-applies the still unsaved value.
  // The first version did exactly that: picking English put everything back to
  // German a frame later and closed the dialog on the way.
  const change = (patch: Prefs) => {
    const next = { ...prefs, ...patch };
    setPrefs(next);
    previewFormat(next);
  };

  // Leaving without saving must not leave the preview behind. Covers Cancel,
  // Esc and the click outside, because all three end in an unmount.
  useEffect(() => () => previewFormat(getPrefs()), []);

  const save = async () => {
    setSaving(true);
    setError('');
    try {
      // The server answers with the CLEANED set, and that is what we adopt —
      // if it refused something, the dialog now shows what is really stored
      // rather than what was asked for.
      const stored = await api.putPrefs(prefs);
      setPrefs(stored);
      // Full apply, language included. This is the one moment a remount is
      // wanted: everything is saved, so the re-fetch that follows finds the new
      // values and agrees with them.
      await applyPrefs(stored);
      onClose();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      setSaving(false);
    }
  };

  const zones = availableTimeZones();
  // A moment and a day, side by side: the whole point is that the first one
  // moves with the timezone and the second one never does.
  const sampleMoment = '2026-07-18T22:30:00Z';
  const sampleDay = '2026-07-18';

  return (
    <Portal>
      <div
        className="modal-overlay"
        onMouseDown={(e) => {
          if (e.target === e.currentTarget) onClose();
        }}
      >
        <div className="dialog" role="dialog" aria-modal="true" aria-label={t('Language and time')}>
          <h2>{t('Language and time')}</h2>
          <p className="dialog-hint">
            {t(
              'Automatic follows this browser and is right most of the time. Set a value yourself when it is not — a laptop on the wrong continent, or a language you read but do not want your dates in.',
            )}
          </p>

          <label className="profile-label">{t('Language')}</label>
          <select
            className="prop-input profile-input"
            value={prefs.language ?? ''}
            onChange={(e) => change({ language: e.target.value })}
          >
            <option value="">
              {t('Automatic ({value})', {
                value: LOCALES[localeNow()] ?? localeNow(),
              })}
            </option>
            {Object.entries(LOCALES).map(([code, name]) => (
              <option key={code} value={code}>
                {name}
              </option>
            ))}
          </select>
          {/* Said out loud rather than left to be guessed: the other four take
              effect in the preview straight away, and a language that visibly
              does nothing until Save reads as broken. */}
          {(prefs.language ?? '') !== (getPrefs().language ?? '') && (
            <span className="dialog-hint">{t('The language changes when you save.')}</span>
          )}

          <label className="profile-label">{t('Date and number format')}</label>
          <select
            className="prop-input profile-input"
            value={prefs.region ?? ''}
            onChange={(e) => change({ region: e.target.value })}
          >
            <option value="">
              {t('Automatic ({value})', { value: regionLabel(automaticFormatTag(prefs)) })}
            </option>
            {REGIONS.map((tag) => (
              <option key={tag} value={tag}>
                {regionLabel(tag)} — {tag}
              </option>
            ))}
          </select>

          <label className="profile-label">{t('Time zone')}</label>
          <select
            className="prop-input profile-input"
            value={prefs.timeZone ?? ''}
            onChange={(e) => change({ timeZone: e.target.value })}
          >
            <option value="">{t('Automatic ({value})', { value: resolvedTimeZone() })}</option>
            {zones.map((z) => (
              <option key={z} value={z}>
                {z}
              </option>
            ))}
          </select>

          <label className="profile-label">{t('Clock')}</label>
          <select
            className="prop-input profile-input"
            value={prefs.clock ?? ''}
            onChange={(e) => change({ clock: e.target.value })}
          >
            <option value="">
              {t('Automatic ({value})', {
                value: resolvedClock() === '12' ? t('12-hour') : t('24-hour'),
              })}
            </option>
            <option value="24">{t('24-hour')}</option>
            <option value="12">{t('12-hour')}</option>
          </select>

          <label className="profile-label">{t('Week starts on')}</label>
          <select
            className="prop-input profile-input"
            value={prefs.weekStart ?? ''}
            onChange={(e) => change({ weekStart: e.target.value })}
          >
            <option value="">
              {t('Automatic ({value})', {
                value: weekdayLabel(firstWeekday()),
              })}
            </option>
            <option value="mon">{weekdayLabel(1)}</option>
            <option value="sun">{weekdayLabel(0)}</option>
            <option value="sat">{weekdayLabel(6)}</option>
          </select>

          <div className="lt-preview">
            <div>
              <span className="lt-preview-label">{t('A moment')}</span>
              <strong>{formatMoment(sampleMoment, 'full')}</strong>
              <span className="dialog-hint">{t('Shown in your time zone.')}</span>
            </div>
            <div>
              <span className="lt-preview-label">{t('A deadline')}</span>
              <strong>{formatDay(sampleDay, 'date')}</strong>
              <span className="dialog-hint">
                {t('A calendar date never moves, whatever the time zone says.')}
              </span>
            </div>
          </div>

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

// The language currently APPLIED, which is what "Automatic" means for the
// language row: automatic follows the browser, and the browser is what is on
// screen right now.
function localeNow(): string {
  return document.documentElement.lang || 'en';
}
