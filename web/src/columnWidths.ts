// How wide each column of a Columns block is.
//
// Its own module, with no imports, so scripts/check-columnwidths.mjs can run
// the arithmetic without a browser. The server reads the same prop with the
// same rule (server/columns.go) — change one, change the other.
//
// A layout is the widths in PERCENT of the row, one per column, written as
// "70,30" on the block's `widths` prop. Empty means equal columns, which is also
// what every block written before this existed has. Percent rather than pixels
// because a column row follows the page: a page that is narrower on a phone, or
// printed on A4, keeps its proportions.

/** No column is dragged narrower than this. At the page's usual width it is
 *  still a readable ~130px; below it a column is a sliver you cannot grab back. */
export const MIN_PCT = 15;

/** The layouts offered on the block's bar. Two or three columns, because count
 *  is 2 or 3 everywhere (the editor, the print view, the Markdown spelling). */
export const PRESETS: readonly (readonly number[])[] = [
  [50, 50],
  [70, 30],
  [30, 70],
  [33, 33, 34],
  [25, 50, 25],
];

export function equalWidths(count: number): number[] {
  const base = Math.floor(100 / count);
  const w = Array<number>(count).fill(base);
  w[count - 1] += 100 - base * count; // the remainder goes to the last: 33,33,34 reads as thirds
  return w;
}

/** The stored string as numbers, or null when it is empty, belongs to another
 *  column count, or is not a layout — in which case the columns are equal. */
export function parseWidths(raw: unknown, count: number): number[] | null {
  if (typeof raw !== 'string' || raw === '') return null;
  const parts = raw.split(',').map((s) => Number(s));
  if (parts.length !== count) return null;
  if (!parts.every((n) => Number.isFinite(n) && n >= 5 && n <= 95)) return null;
  return parts;
}

/** What gets stored: nothing at all for equal columns, so the common case stays
 *  a block with no extra prop to carry. */
export function formatWidths(w: readonly number[]): string {
  const eq = equalWidths(w.length);
  return w.every((n, i) => n === eq[i]) ? '' : w.join(',');
}

export function sameWidths(a: readonly number[], b: readonly number[]): boolean {
  return a.length === b.length && a.every((n, i) => n === b[i]);
}

/** Move the divider after column i by `delta` percent. The two columns beside it
 *  trade width, the rest stay put, and neither goes under MIN_PCT. */
export function moveDivider(w: readonly number[], i: number, delta: number): number[] {
  const out = [...w];
  const d = Math.max(MIN_PCT - w[i], Math.min(w[i + 1] - MIN_PCT, Math.round(delta)));
  out[i] = w[i] + d;
  out[i + 1] = w[i + 1] - d;
  return out;
}

/** CSS for the row, from the widths. `fr` rather than %, because the row also
 *  has gaps and percentages would overflow by exactly their sum. */
export function gridTemplate(w: readonly number[]): string {
  return w.map((n) => `minmax(0,${n}fr)`).join(' ');
}
