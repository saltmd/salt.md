import type { PageMeta } from './types';
import { compare } from './format';

// Tag suggestions. Without them you type the same tag for the tenth time
// slightly differently and the workspace frays into "project", "Projects",
// "project-a". The server de-duplicates tags only INSIDE one page and only by
// lower case — across the workspace nothing stops it. That is exactly the gap
// the suggestion list closes.

/** Every tag used in the workspace with its frequency, most frequent first. */
export function collectTags(pages: Iterable<PageMeta>): { tag: string; count: number }[] {
  // Group by lower case but show the most frequent spelling — otherwise it
  // suggests "Project" while 12 pages carry "project".
  const byKey = new Map<string, Map<string, number>>();
  for (const p of pages) {
    if (p.trashed) continue;
    for (const t of p.tags ?? []) {
      const key = t.toLowerCase();
      const variants = byKey.get(key) ?? new Map<string, number>();
      variants.set(t, (variants.get(t) ?? 0) + 1);
      byKey.set(key, variants);
    }
  }
  const out: { tag: string; count: number }[] = [];
  for (const variants of byKey.values()) {
    let best = '';
    let bestN = 0;
    let total = 0;
    for (const [name, n] of variants) {
      total += n;
      if (n > bestN) {
        best = name;
        bestN = n;
      }
    }
    out.push({ tag: best, count: total });
  }
  return out.sort((a, b) => b.count - a.count || compare(a.tag, b.tag));
}

/** Levenshtein distance, abandoned as soon as it goes past `max`. */
function editDistance(a: string, b: string, max: number): number {
  if (Math.abs(a.length - b.length) > max) return max + 1;
  let prev = Array.from({ length: b.length + 1 }, (_, i) => i);
  for (let i = 1; i <= a.length; i++) {
    const cur = [i];
    let rowMin = i;
    for (let j = 1; j <= b.length; j++) {
      const cost = a[i - 1] === b[j - 1] ? 0 : 1;
      cur[j] = Math.min(cur[j - 1] + 1, prev[j] + 1, prev[j - 1] + cost);
      if (cur[j] < rowMin) rowMin = cur[j];
    }
    if (rowMin > max) return max + 1; // the whole row is already too far away
    prev = cur;
  }
  return prev[b.length];
}

export interface TagSuggestion {
  tag: string;
  count: number;
  /** true = kein Treffer im Text, sondern ein Tippfehler-Verdacht. */
  similar?: boolean;
}

/**
 * Suggestions for an input, in this order: prefix hits, then substring hits,
 * then near misses (typos). With no input the most frequent tags come back —
 * so the first click already shows what exists at all.
 */
export function suggestTags(
  all: { tag: string; count: number }[],
  draft: string,
  already: string[],
  limit = 8,
): TagSuggestion[] {
  const used = new Set(already.map((t) => t.toLowerCase()));
  const pool = all.filter((t) => !used.has(t.tag.toLowerCase()));
  // Normalise the way the server does, so "my tag" also finds "my-tag".
  const q = draft.trim().replace(/^#/, '').replace(/\s+/g, '-').toLowerCase();
  if (!q) return pool.slice(0, limit);

  const prefix: TagSuggestion[] = [];
  const infix: TagSuggestion[] = [];
  const similar: TagSuggestion[] = [];
  // Do not "correct" short inputs — at 2 characters everything is similar.
  const maxDist = q.length >= 5 ? 2 : q.length >= 3 ? 1 : 0;

  for (const t of pool) {
    const k = t.tag.toLowerCase();
    if (k === q) continue; // typed exactly — no suggestion needed for that
    if (k.startsWith(q)) prefix.push(t);
    else if (k.includes(q)) infix.push(t);
    else if (maxDist > 0 && editDistance(q, k, maxDist) <= maxDist)
      similar.push({ ...t, similar: true });
  }
  return [...prefix, ...infix, ...similar].slice(0, limit);
}
