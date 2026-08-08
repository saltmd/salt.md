import type { ReactNode } from 'react';
import { LUCIDE_SET } from './lucideSet';
import { mdiPath, useMdi } from './mdiLoader';

// A page icon can be one of four things, all stored in the single `icon`
// string on a page:
//   • an emoji            — "🚀"                (rendered as text, CSS-sized)
//   • a Lucide icon       — "lucide:Rocket" or "lucide:Rocket:#e03131"
//   • an MDI icon         — "mdi:Rocket"    or "mdi:Rocket:#e03131"
//   • an uploaded image   — "/files/abc.png" (or an http/data URL)
// PageIcon renders whichever it is; everywhere a page icon appears uses it so
// every icon kind works uniformly across the whole app.

export function isImageIcon(icon?: string | null): boolean {
  return !!icon && (icon.startsWith('/') || icon.startsWith('http') || icon.startsWith('data:'));
}
export function isLucideIcon(icon?: string | null): boolean {
  return !!icon && icon.startsWith('lucide:');
}
export function isMdiIcon(icon?: string | null): boolean {
  return !!icon && icon.startsWith('mdi:');
}

// "lucide:Name[:color]" / "mdi:Name[:color]". A trailing ":fill" segment from
// the old filled-Lucide variants is tolerated and ignored, so icons picked back
// then still resolve to the same (now outline) glyph instead of vanishing.
export function parseIconRef(icon: string): { name: string; color?: string } {
  const parts = icon.split(':');
  const color = parts[2] && parts[2] !== 'fill' ? parts[2] : undefined;
  return { name: parts[1] || '', color };
}
export function makeLucide(name: string, color?: string): string {
  return color ? `lucide:${name}:${color}` : `lucide:${name}`;
}
export function makeMdi(name: string, color?: string): string {
  return color ? `mdi:${name}:${color}` : `mdi:${name}`;
}

export function PageIcon({
  icon,
  size = 18,
  fallback = null,
}: {
  icon?: string | null;
  size?: number;
  fallback?: ReactNode;
}) {
  // Hooks must run on every render, so this comes before any early return.
  const wantsMdi = isMdiIcon(icon);
  const mdiReady = useMdi(wantsMdi);

  if (!icon) return <>{fallback}</>;

  if (wantsMdi) {
    const { name, color } = parseIconRef(icon);
    const d = mdiReady ? mdiPath(name) : undefined;
    // Until the lazy chunk lands (or if the name is unknown) fall back quietly.
    if (!d) return <>{fallback}</>;
    return (
      <svg width={size} height={size} viewBox="0 0 24 24" className="page-icon-mdi" aria-hidden="true">
        <path d={d} fill={color || 'currentColor'} />
      </svg>
    );
  }

  if (isLucideIcon(icon)) {
    const { name, color } = parseIconRef(icon);
    const Cmp = LUCIDE_SET[name];
    if (!Cmp) return <>{fallback}</>;
    return (
      <Cmp
        size={size}
        color={color || 'currentColor'}
        fill="none"
        strokeWidth={2}
        className="page-icon-lucide"
      />
    );
  }

  if (isImageIcon(icon)) {
    return <img src={icon} alt="" className="page-icon-img" style={{ width: size, height: size }} />;
  }

  // Emoji / plain text: parent's font-size controls the size (unchanged behavior).
  return <>{icon}</>;
}
