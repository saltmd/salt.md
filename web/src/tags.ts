// Tag colours (Welle 27/28). A tag can be given an explicit colour (stored per
// workspace, Notion-style); without one it falls back to an automatic colour
// derived from the (lower-cased) tag name so tags aren't all the same colour.
// The classes tag-<name> carry the actual light/dark palette (see styles.css).
export const TAG_PALETTE = [
  'gray',
  'brown',
  'orange',
  'yellow',
  'green',
  'blue',
  'purple',
  'pink',
  'red',
] as const;

export function autoTagColor(name: string): string {
  let h = 0;
  const s = name.toLowerCase();
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) >>> 0;
  return TAG_PALETTE[h % TAG_PALETTE.length];
}

// tagColorClass resolves the CSS class for a tag: an explicit override from the
// workspace colour map wins, otherwise the automatic colour.
export function tagColorClass(name: string, colors?: Record<string, string>): string {
  const override = colors?.[name.toLowerCase()];
  return 'tag-' + (override && override !== 'default' ? override : autoTagColor(name));
}
