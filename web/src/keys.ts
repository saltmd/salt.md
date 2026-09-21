import { useEffect, useRef } from 'react';
import { compare } from './format';

// Keyboard shortcuts, in one registry, behind one listener.
//
// One listener, one typing guard, one place to ask what exists — the last of
// which is what the help sheet reads, so the sheet cannot drift from what is
// really bound.

/** Where a shortcut applies. Higher wins: an open modal owns the keyboard, the
 *  editor owns it over the view behind it, and `global` is what nothing else
 *  claimed. Within one scope the most recently registered wins, so stacked
 *  modals behave like the stack they are. */
export type Scope = 'global' | 'view' | 'editor' | 'modal';

const RANK: Record<Scope, number> = { global: 0, view: 1, editor: 2, modal: 3 };

export type Shortcut = {
  /** Stable id. Also what the help sheet keys on, so keep it dotted and
   *  descriptive: `search.open`, `tabs.next`, `checkbox.toggle`. */
  id: string;
  /** Chords, written with `mod` for ⌘ on Apple and Ctrl everywhere else.
   *  Spell `ctrl` literally when you mean the physical Control key on both
   *  platforms — that is a different shortcut, not a spelling variant. */
  keys: string[];
  scope?: Scope;
  /** Fires even while the caret sits in a text field. Off by default; turn it
   *  on only for a shortcut meant to act ON what is being typed, or to leave it
   *  (the search palette). */
  whileTyping?: boolean;
  /** Extra condition, checked at press time. */
  when?: () => boolean;
  /** Evaluated when the help sheet renders, never at registration: a language
   *  change has to reach it, and check-i18n only sees a literal `t('…')` if it
   *  is written at the call site. */
  label?: () => string;
  group?: () => string;
  /** Return `false` to let the key through untouched — for the rare case where
   *  the shortcut decided not to act after all. */
  run: (e: KeyboardEvent) => void | boolean;
};

/** The part of a KeyboardEvent a chord is built from. Narrower than the real
 *  thing so the resolution logic can be tested without a DOM. */
export type KeyEventLike = {
  key?: string;
  code?: string;
  ctrlKey?: boolean;
  metaKey?: boolean;
  altKey?: boolean;
  shiftKey?: boolean;
};

// Canonical modifier order. Any order may be WRITTEN ('alt+mod+n'); everything
// is normalised to this one before comparison, so 'mod+shift+k' and
// 'shift+mod+k' cannot become two different shortcuts.
const ORDER = ['ctrl', 'meta', 'alt', 'shift'];

/** True on Apple hardware, where `mod` means ⌘. Read lazily: navigator.platform
 *  is deprecated (hence the userAgent fallback), and the node check that drives
 *  this module has no navigator at module-load time. */
function isApple(): boolean {
  if (typeof navigator === 'undefined') return false;
  return /Mac|iPhone|iPad|iPod/.test(navigator.platform || navigator.userAgent);
}

const modKey = () => (isApple() ? 'meta' : 'ctrl');

/** The key itself, by physical position wherever there is one: e.key is the
 *  CHARACTER produced and moves with the layout, so on a German board (Z and Y
 *  swapped) `e.key === 'z'` puts undo under a different finger. Named keys —
 *  Enter, Escape, the arrows — have no such problem and come from e.key. */
function baseKey(e: KeyEventLike): string {
  const code = e.code ?? '';
  if (/^Key[A-Z]$/.test(code)) return code.slice(3).toLowerCase();
  if (/^Digit[0-9]$/.test(code)) return code.slice(5);
  const k = (e.key ?? '').toLowerCase();
  return k === ' ' ? 'space' : k;
}

/** An event as a canonical chord string: 'mod+k' pressed on a Mac arrives here
 *  as 'meta+k', and as 'ctrl+k' on everything else. */
export function chordOf(e: KeyEventLike): string {
  const mods = [
    e.ctrlKey ? 'ctrl' : '',
    e.metaKey ? 'meta' : '',
    e.altKey ? 'alt' : '',
    e.shiftKey ? 'shift' : '',
  ].filter(Boolean);
  return [...mods, baseKey(e)].join('+');
}

/** A written chord in the same canonical form, with `mod` resolved for this
 *  platform. This is the only place that knows ⌘ from Ctrl. */
export function normalize(chord: string): string {
  const parts = chord
    .toLowerCase()
    .split('+')
    .map((p) => p.trim())
    .filter(Boolean);
  const key = parts.pop() ?? '';
  const mods = new Set(parts.map((p) => (p === 'mod' ? modKey() : p)));
  return [...ORDER.filter((m) => mods.has(m)), key].join('+');
}

/** True when the keystroke belongs to whatever the reader is writing in.
 *  Nothing is being typed into a checkbox, a radio or a button, so a blanket
 *  "is it an INPUT" test would block shortcuts on exactly those. */
export function isTypingTarget(el: Element | null): boolean {
  if (!el) return false;
  const html = el as HTMLElement;
  if (html.isContentEditable) return true;
  const tag = html.tagName;
  if (tag === 'TEXTAREA' || tag === 'SELECT') return true;
  if (tag === 'INPUT') {
    const type = (html as HTMLInputElement).type;
    return !/^(checkbox|radio|button|submit|reset|file|range|color|image)$/.test(type);
  }
  return false;
}

type Entry = Shortcut & { seq: number; chords: string[] };

const entries: Entry[] = [];
let seq = 0;
let attached = false;

/** Highest-ranked shortcut for a chord, or null. Exported so the check can
 *  drive it with a plain list and no browser. */
export function resolve(chord: string, typing: boolean, list: Entry[] = entries): Entry | null {
  let best: Entry | null = null;
  for (const e of list) {
    if (!e.chords.includes(chord)) continue;
    if (typing && !e.whileTyping) continue;
    if (e.when && !e.when()) continue;
    if (!best || rankOf(e) > rankOf(best)) best = e;
  }
  return best;
}

// Scope first, registration order second — a later registration in the same
// scope shadows an earlier one rather than fighting it.
const rankOf = (e: Entry) => RANK[e.scope ?? 'global'] * 1e6 + e.seq;

function onKeyDown(e: KeyboardEvent) {
  // Mid-composition keys belong to the input method: while a Japanese or
  // Chinese reader is composing, Enter and the arrows pick a candidate.
  if (e.isComposing) return;
  const hit = resolve(chordOf(e), isTypingTarget(document.activeElement));
  if (!hit) return;
  if (hit.run(e) === false) return;
  // Both, deliberately: preventDefault stops the browser's meaning of the
  // chord, stopPropagation stops the widget underneath from ALSO acting on it.
  e.preventDefault();
  e.stopPropagation();
}

function attach() {
  if (attached || typeof window === 'undefined') return;
  attached = true;
  // Capture, not bubble: the editor and several block widgets call
  // stopPropagation, so a bubbling listener never hears anything pressed inside
  // them — which is where these shortcuts are most needed.
  window.addEventListener('keydown', onKeyDown, true);
}

/** Register a shortcut; returns the unregister function. */
export function register(s: Shortcut): () => void {
  const entry: Entry = { ...s, seq: seq++, chords: s.keys.map(normalize) };
  entries.push(entry);
  attach();
  return () => {
    const i = entries.indexOf(entry);
    if (i >= 0) entries.splice(i, 1);
  };
}

/** Register for as long as the component is mounted. Callbacks are read through
 *  a ref, so a re-render never churns the registry and no stale closure can
 *  creep in. */
export function useShortcut(s: Shortcut) {
  const ref = useRef(s);
  ref.current = s;
  const deps = [s.id, s.keys.join('|'), s.scope, s.whileTyping] as const;
  useEffect(
    () =>
      register({
        id: s.id,
        keys: s.keys,
        scope: s.scope,
        whileTyping: s.whileTyping,
        run: (e) => ref.current.run(e),
        when: () => (ref.current.when ? ref.current.when() : true),
        label: () => (ref.current.label ? ref.current.label() : ''),
        group: () => (ref.current.group ? ref.current.group() : ''),
      }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    deps,
  );
}

const SYMBOLS: Record<string, string> = { ctrl: '⌃', meta: '⌘', alt: '⌥', shift: '⇧' };
const WORDS: Record<string, string> = { ctrl: 'Ctrl', meta: 'Win', alt: 'Alt', shift: 'Shift' };
const KEY_NAMES: Record<string, string> = {
  arrowup: '↑',
  arrowdown: '↓',
  arrowleft: '←',
  arrowright: '→',
  enter: '↩',
  space: 'Space',
  escape: 'Esc',
};

/** A chord as a reader sees it: ⌘⇧K on a Mac, Ctrl+Shift+K elsewhere.
 *
 *  Key names stay in Latin script and untranslated on purpose — they name what
 *  is printed on the keycap, and the keycap does not change language. */
export function formatChord(chord: string): string {
  const parts = normalize(chord).split('+');
  const key = parts.pop() ?? '';
  const apple = isApple();
  const mods = parts.map((m) => (apple ? SYMBOLS[m] : WORDS[m]) ?? m);
  const name = KEY_NAMES[key] ?? (key.length === 1 ? key.toUpperCase() : key);
  return apple ? mods.join('') + name : [...mods, name].join('+');
}

/** The chord to advertise for `id`, or '' when it is not bound right now. For
 *  putting the shortcut next to the action in a menu, where it is actually
 *  learnt. Highest-ranked binding wins, same as a press. */
export function chordFor(id: string): string {
  let best: Entry | null = null;
  for (const e of entries) {
    if (e.id !== id) continue;
    if (e.when && !e.when()) continue;
    if (!best || rankOf(e) > rankOf(best)) best = e;
  }
  return best ? formatChord(best.chords[0] ?? '') : '';
}

/** A button's tooltip, with its live keybind appended when one is bound right
 *  now — the same source chordFor uses for a menu row, so the two can never
 *  disagree. */
export function hint(label: string, id: string): string {
  const chord = chordFor(id);
  return chord ? `${label} (${chord})` : label;
}

/** Everything bound HERE, for the help sheet, sorted by group then label.
 *
 *  Both filters follow from the sheet's promise — "what can I press on this
 *  screen": `when` is honoured, because an instruction that does nothing when
 *  followed is worse than silence; and one row per id, because the same
 *  'nav.down' is deliberately registered once per region. */
export function shortcutList(): { id: string; keys: string; label: string; group: string }[] {
  const seen = new Set<string>();
  return [...entries]
    .sort((a, b) => rankOf(b) - rankOf(a))
    .filter((e) => (e.when ? e.when() : true))
    .filter((e) => !seen.has(e.id) && (seen.add(e.id), true))
    .map((e) => ({
      id: e.id,
      keys: e.chords.map(formatChord)[0] ?? '',
      label: e.label ? e.label() : '',
      group: e.group ? e.group() : '',
    }))
    .filter((e) => e.label !== '')
    .sort((a, b) => compare(a.group, b.group) || compare(a.label, b.label));
}
