import { useState } from 'react';
import { ChevronDown } from 'lucide-react';
import { t } from './i18n';
import type { SpanMode } from './types';

// A first-look dot on a view's settings button, once per kind of view.
// Same localStorage key family the interface store uses, so a later shared
// helper reads what this wrote.
const HINT_PREFIX = 'salt:hint:';

export function useSettingsHint(name: string): [boolean, () => void] {
  const [show, setShow] = useState(() => {
    try {
      return localStorage.getItem(HINT_PREFIX + name) !== '1';
    } catch {
      return false;
    }
  });
  const seen = () => {
    if (!show) return;
    try {
      localStorage.setItem(HINT_PREFIX + name, '1');
    } catch {
      /* best-effort persistence */
    }
    setShow(false);
  };
  return [show, seen];
}

// Day/Week/Month/2-months/Year switch shared by the calendar and the
// timeline headers: same control, same place, same labels.
export function ModePicker({ mode, onChange }: { mode: SpanMode; onChange: (m: SpanMode) => void }) {
  const [open, setOpen] = useState(false);
  const label =
    mode === 'day'
      ? t('Day')
      : mode === 'week'
        ? t('Week')
        : mode === 'year'
          ? t('Year')
          : mode === '2months'
            ? t('2 months')
            : t('Month');
  const pick = (m: SpanMode) => {
    setOpen(false);
    if (m !== mode) onChange(m);
  };
  return (
    <div className="cal-range-wrap">
      <button className="btn-sm" onClick={() => setOpen((o) => !o)} aria-haspopup="listbox" aria-expanded={open}>
        {label} <ChevronDown size={14} />
      </button>
      {open && (
        <>
          <div className="fs-backdrop" onClick={() => setOpen(false)} />
          <div className="menu view-tab-menu" role="listbox">
            {(['day', 'week', 'month', '2months', 'year'] as const).map((m) => (
              <button
                key={m}
                role="option"
                aria-selected={mode === m}
                onClick={() => pick(m)}
              >
                {m === 'day'
                  ? t('Day')
                  : m === 'week'
                    ? t('Week')
                    : m === 'year'
                      ? t('Year')
                      : m === '2months'
                        ? t('2 months')
                        : t('Month')}
              </button>
            ))}
          </div>
        </>
      )}
    </div>
  );
}
