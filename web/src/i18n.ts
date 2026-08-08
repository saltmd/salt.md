import { useEffect, useState } from 'react';
import { setFormatLocale, setFormatPrefs } from './format';

// Translation. The English source text IS the key:
//
//     t('Manage users')
//     plural(n, '{n} page', '{n} pages')
//
// Symbolic keys ('users.manage') were the other option and were rejected: when
// a translation is missing, a symbolic key shows the user gibberish, while an
// English key shows them English. salt.md is written in English first, so the
// fallback is always a correct sentence — a missing translation degrades to
// "not translated yet", never to "broken".
//
// The cost of this choice is that editing English source text orphans its
// translations. scripts/check-i18n.mjs reports those orphans instead of leaving
// them to rot, which is the trade we want: loud and mechanical beats silent.

export const LOCALES: Record<string, string> = {
  en: 'English',
  de: 'Deutsch',
};

/** Plural categories as Intl names them. Which ones a language actually uses
 *  is the language's business: English has 2, German 2, Polish 3, Arabic 6. */
type PluralCategory = 'zero' | 'one' | 'two' | 'few' | 'many' | 'other';

/** A catalog maps English source text to its translation. Plural entries hold
 *  one string per category the target language needs. */
type Entry = string | Partial<Record<PluralCategory, string>>;
type Catalog = Record<string, Entry>;

// Every file under locales/ is picked up automatically, so adding a language
// means dropping in one JSON file — no code change, which is the point of the
// translation tool.
const files = import.meta.glob<{ default: Catalog }>('./locales/*.json');

let locale = 'en';
let catalog: Catalog = {};
const listeners = new Set<() => void>();

// ---- reading ----

/** Translate a source string. Unknown text falls through as-is, which is why
 *  the source must be English. */
export function t(source: string, vars?: Record<string, string | number>): string {
  const hit = catalog[source];
  const s = typeof hit === 'string' ? hit : source;
  return vars ? fill(s, vars) : s;
}

/** Translate a counted string. `other` doubles as the catalog key.
 *
 *      plural(n, '{n} page', '{n} pages')
 *
 *  Intl.PluralRules picks the category, so Polish gets its three forms and
 *  Japanese its one without any of that leaking into the call site. The
 *  hand-rolled `n === 1 ? '' : 'n'` this replaces was only ever right for
 *  English and German. */
export function plural(
  n: number,
  one: string,
  other: string,
  vars?: Record<string, string | number>,
): string {
  const hit = catalog[other];
  let s: string;
  if (hit && typeof hit === 'object') {
    const cat = new Intl.PluralRules(locale).select(n) as PluralCategory;
    s = hit[cat] ?? hit.other ?? other;
  } else if (typeof hit === 'string') {
    // A language with a single form (Japanese, Chinese) may store a plain
    // string — no categories to choose between.
    s = hit;
  } else {
    s = n === 1 ? one : other;
  }
  return fill(s, { n, ...vars });
}

/** Replace {name} placeholders. Unknown names are left standing so a typo in a
 *  catalog is visible rather than silently blank. */
function fill(s: string, vars: Record<string, string | number>): string {
  return s.replace(/\{(\w+)\}/g, (m, k) => (k in vars ? String(vars[k]) : m));
}

export function getLocale(): string {
  return locale;
}

// ---- switching ----

/** The account's language and time preferences (W112).
 *
 *  Every field is optional and empty means AUTOMATIC — follow the browser,
 *  which is what happened before there was a setting. See server/prefs.go for
 *  why the absence of a decision and the automatic mode are one state. */
export type Prefs = {
  language?: string;
  region?: string;
  timeZone?: string;
  clock?: string;
  weekStart?: string;
};

let prefs: Prefs = {};

export function getPrefs(): Prefs {
  return prefs;
}

/** The cache key. It holds a COPY of the account's settings, never the truth:
 *  the account is the truth (see prefs.go). This exists so the first frame
 *  after a reload is not briefly in the wrong language while /api/me is still
 *  in flight — and so the login screen, where there is no account yet, comes up
 *  the way this person last had it. */
const CACHE = 'salt-prefs';

function cached(): Prefs {
  try {
    const raw = localStorage.getItem(CACHE);
    if (raw) return JSON.parse(raw) as Prefs;
  } catch {
    /* a hand-edited or truncated cache is not worth a broken app */
  }
  // Migration from the single key this replaces. Somebody who chose German
  // before W112 keeps German instead of being silently reset to the browser.
  const old = localStorage.getItem('salt-locale');
  return old && old in LOCALES ? { language: old } : {};
}

/** Which language to translate into: the account's choice, else the browser's
 *  preference, else English. */
function preferred(p: Prefs): string {
  if (p.language && p.language in LOCALES) return p.language;
  for (const tag of navigator.languages ?? [navigator.language]) {
    const base = tag.split('-')[0];
    if (base in LOCALES) return base;
  }
  return 'en';
}

/** Show a set of settings WITHOUT changing the language.
 *
 *  The settings dialog needs a live preview, and a live preview must not change
 *  the language — because changing the language remounts the whole tree
 *  (main.tsx keys the root on it), which destroys the dialog mid-edit AND
 *  re-runs App's mount effect, which re-fetches /api/me and applies the still
 *  unsaved value. The result was a dropdown that appeared to do nothing: pick
 *  English, the tree remounts, the server says German, everything is German
 *  again, one frame later, dialog gone.
 *
 *  So the split: format settings preview immediately, because nothing has to
 *  remount for them — setFormatLocale and setFormatPrefs notify nobody. The
 *  LANGUAGE waits for Save, where a remount is exactly what you want. */
export function previewFormat(next: Prefs): void {
  setFormatLocale(next.region || automaticFormatTag(next));
  setFormatPrefs(next);
}

/** The regional tag "automatic" resolves to for a given set of settings.
 *
 *  Takes the settings rather than reading the applied ones, because the dialog
 *  has to label its Automatic entry for what is PENDING: pick English while the
 *  region stays automatic and the answer becomes English, one moment before the
 *  language itself is applied. Reading global state there said "German" while
 *  the preview beside it already showed 07/18/2026. */
export function automaticFormatTag(p: Prefs = prefs): string {
  return formattingTag(preferred(p));
}

/** Take the account's settings and show them, language included. Called once
 *  /api/me has answered, and again whenever the settings dialog saves.
 *
 *  Writes the cache as a side effect, so the next first paint already matches
 *  and the change survives a reload even before /api/me returns. */
export async function applyPrefs(next: Prefs): Promise<void> {
  prefs = next ?? {};
  try {
    localStorage.setItem(CACHE, JSON.stringify(prefs));
    localStorage.removeItem('salt-locale');
  } catch {
    /* private mode, quota — the account still has the settings */
  }
  await setLocale(preferred(prefs));
}

/** The tag to FORMAT with, which is not the same as the language to translate
 *  into.
 *
 *  salt.md ships one catalog per language ('en', 'de'), because writing one per
 *  region would mean maintaining British and American copies of the same
 *  sentences. But dates and numbers really are regional: bare 'en' means
 *  American to Intl, so an English-reading user in Dublin or Sydney would get
 *  07/18/2026 instead of 18/07/2026, and an Austrian gets a different thousands
 *  separator from a German.
 *
 *  So: translate by language, format by whichever regional variant of that
 *  language the browser already asked for. Their operating system settled this
 *  question long ago and got it right. */
function formattingTag(lang: string): string {
  for (const tag of navigator.languages ?? [navigator.language]) {
    if (tag.split('-')[0] === lang) return tag;
  }
  return lang;
}

/** Load a language and tell everyone. English needs no catalog — its keys are
 *  already the text. */
export async function setLocale(next: string): Promise<void> {
  if (!(next in LOCALES)) next = 'en';
  const path = `./locales/${next}.json`;
  let loaded: Catalog = {};
  if (next !== 'en' && files[path]) {
    try {
      loaded = (await files[path]()).default;
    } catch {
      // A broken or missing catalog must not take the app down; English is
      // always a working fallback.
      loaded = {};
    }
  }
  locale = next;
  catalog = loaded;
  // Region beats language for FORMATTING: somebody may read English and still
  // want 18.07.2026. Unset, the browser's own regional variant decides, which
  // is what formattingTag has always done.
  setFormatLocale(prefs.region || formattingTag(next));
  setFormatPrefs(prefs);
  applyDocumentLanguage(next);
  listeners.forEach((fn) => fn());
}

/** Tell the browser what it is rendering. Beyond screen readers this drives
 *  hyphenation, quotation marks and spell-check, and `dir` decides whether the
 *  page lays out right-to-left. */
function applyDocumentLanguage(next: string) {
  document.documentElement.lang = next;
  let dir = 'ltr';
  try {
    const loc = new Intl.Locale(next) as Intl.Locale & {
      getTextInfo?: () => { direction: string };
      textInfo?: { direction: string };
    };
    const info = typeof loc.getTextInfo === 'function' ? loc.getTextInfo() : loc.textInfo;
    if (info?.direction) dir = info.direction;
  } catch {
    /* older browser — left-to-right is right for every language we ship today */
  }
  document.documentElement.dir = dir;
}

/** Called once before the first render, so a German user never sees a flash of
 *  English while the catalog is still in flight.
 *
 *  Runs off the CACHE, because at this point nobody has signed in yet and the
 *  account's settings are still a request away. applyPrefs corrects it a moment
 *  later if they differ. */
export function initLocale(): Promise<void> {
  prefs = cached();
  return setLocale(preferred(prefs));
}

// ---- React ----

/** Re-render this component when the language changes. Components that call
 *  t() need it; the same tiny store pattern as presence.ts, for the same
 *  reason — no provider has to sit in between. */
export function useLocale(): string {
  const [, bump] = useState(0);
  useEffect(() => {
    const fn = () => bump((n) => n + 1);
    listeners.add(fn);
    return () => {
      listeners.delete(fn);
    };
  }, []);
  return locale;
}
