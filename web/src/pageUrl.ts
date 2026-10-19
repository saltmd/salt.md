// Canonical page URLs: /p/<slug>-<id>, with the slug optional.
//
// The id (32 hex chars) is the only identifier; the slug is a transliterated
// title prefix for readable links, in the style of Outline. It changes with
// the title and is never used for lookup, so a stale slug still opens the
// same page and is corrected to the fresh one via replaceState.

import type { PageMeta } from './types';

const RU: Record<string, string> = {
  а: 'a', б: 'b', в: 'v', г: 'g', д: 'd', е: 'e', ё: 'yo',
  ж: 'zh', з: 'z', и: 'i', й: 'y', к: 'k', л: 'l', м: 'm',
  н: 'n', о: 'o', п: 'p', р: 'r', с: 's', т: 't', у: 'u',
  ф: 'f', х: 'h', ц: 'ts', ч: 'ch', ш: 'sh', щ: 'shch',
  ъ: '', ы: 'y', ь: '', э: 'e', ю: 'yu', я: 'ya',
  // Ukrainian + Belarusian letters missing from the Russian set.
  і: 'i', ї: 'yi', є: 'ye', ґ: 'g', ў: 'u',
};

// Latin letters NFKD does not fold to ASCII: without these a word with a
// sharp s would come out "gro-e". Applied before the NFKD pass, like the
// Cyrillic map.
const LATIN: Record<string, string> = {
  ß: 'ss', æ: 'ae', œ: 'oe', ø: 'o', å: 'a', ł: 'l', // i18n-ok: transliteration table, not prose
  đ: 'd', þ: 'th', ð: 'd', ħ: 'h', ı: 'i', ŋ: 'n', ſ: 's',
};

const MAX_SLUG = 80;

export function slugifyTitle(title: string): string {
  // Transliterate Cyrillic FIRST on the raw string: NFKD would decompose
  // YO into YE + combining diaeresis, and stripping that first would turn
  // YO into E. Latin diacritics are folded afterwards.
  const raw = (title ?? '').toLowerCase();
  let latin = '';
  for (const ch of raw) {
    const tr = RU[ch] ?? LATIN[ch];
    latin += tr !== undefined ? tr : ch;
  }
  latin = latin.normalize('NFKD').replace(/[\u0300-\u036f]/g, '');
  let out = '';
  for (const ch of latin) {
    const code = ch.codePointAt(0) ?? 0;
    // a-z, 0-9 pass through; everything else becomes a separator run.
    if ((code >= 97 && code <= 122) || (code >= 48 && code <= 57)) out += ch;
    else out += '-';
  }
  out = out.replace(/-+/g, '-').replace(/^-+|-+$/g, '');
  if (out.length > MAX_SLUG) {
    out = out.slice(0, MAX_SLUG).replace(/-+$/g, '');
  }
  return out;
}

export function pagePath(id: string, title?: string): string {
  const slug = title ? slugifyTitle(title) : '';
  return slug ? `/p/${slug}-${id}` : `/p/${id}`;
}

// The title a page may show in its URL: only a page the whole workspace sees.
// A private page's title must never reach the address bar (it is copied into
// chats, tickets and proxy logs), and the same holds for a restricted one;
// both keep the bare /p/<id>.
export function pageSlugTitle(meta?: Pick<PageMeta, 'title' | 'visibility'>): string {
  if (!meta || meta.visibility !== 'workspace') return '';
  return meta.title;
}

// The id is always the trailing 32 hex chars; anything before the last dash
// is a slug and is ignored. Bare /p/<id> (old links, API messages) matches too.
// Offline-created pages carry temp-<uuid> until the outbox flush swaps them
// for server ids: never valid server-side, but the reload path must resolve
// them, or the tab bounces to the first page and the draft strands.
export function pageIdFromPathname(pathname: string): string | null {
  const m = pathname.match(/^\/p\/([A-Za-z0-9-]+)\/?$/);
  if (!m) return null;
  const tail = m[1].toLowerCase();
  if (tail.startsWith('temp-') && tail.length <= 64) return tail;
  // Legacy bare ids of any length (the previous frontend accepted them).
  if (/^[0-9a-f]+$/.test(tail)) return tail;
  const id = tail.slice(-32);
  if (!/^[0-9a-f]{32}$/.test(id)) return null;
  if (tail[tail.length - 33] !== '-') return null;
  return id;
}
