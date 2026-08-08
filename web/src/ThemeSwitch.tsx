import { Sun, Moon, Monitor } from 'lucide-react';
import { t } from './i18n';

// Light / Automatic / Dark as a three-way choice rather than a toggle.
//
// The old button knew only two states: on first run it copied the system
// setting and froze it, so anyone who later changed their system was stuck on
// the old theme with no way to ask for "follow the system" again. That is what
// the third option is for.
export type ThemePref = 'light' | 'dark' | 'auto';

export default function ThemeSwitch({
  value,
  onChange,
  size = 15,
}: {
  value: ThemePref;
  onChange: (v: ThemePref) => void;
  size?: number;
}) {
  // Built inside the render, not as a module constant: a constant would call
  // t() once at import time and keep the first language forever.
  const OPTIONS: { value: ThemePref; icon: typeof Sun; label: string }[] = [
    { value: 'light', icon: Sun, label: t('Light') },
    { value: 'auto', icon: Monitor, label: t('Automatic — follows the system') },
    { value: 'dark', icon: Moon, label: t('Dark') },
  ];
  return (
    <div className="theme-switch" role="radiogroup" aria-label={t('Appearance')}>
      {OPTIONS.map(({ value: v, icon: Icon, label }) => (
        <button
          key={v}
          type="button"
          role="radio"
          aria-checked={value === v}
          aria-label={label}
          title={label}
          className={'theme-switch-opt' + (value === v ? ' on' : '')}
          onClick={() => onChange(v)}
        >
          <Icon size={size} />
        </button>
      ))}
    </div>
  );
}
