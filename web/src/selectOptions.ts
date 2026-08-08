import { t } from './i18n';

// Colours + id helper for database select / multi-select options (wave 29).
// Notion-style named palette (stored as hex on each option so existing data and
// the chip renderer keep working unchanged).
export const OPTION_HEXES = [
  '#787774', // gray
  '#9f6b53', // brown
  '#d9730d', // orange
  '#cb912f', // yellow
  '#448361', // green
  '#337ea9', // blue
  '#9065b0', // purple
  '#c14c8a', // pink
  '#d44c47', // red
];

// A function rather than a constant, so t() runs at render time — a constant
// would resolve once at import and hold the first language forever. The hex is
// what gets stored, so the name is display only and safe to translate.
export const optionPalette = (): { name: string; hex: string }[] => [
  { name: t('Gray'), hex: OPTION_HEXES[0] },
  { name: t('Brown'), hex: OPTION_HEXES[1] },
  { name: t('Orange'), hex: OPTION_HEXES[2] },
  { name: t('Yellow'), hex: OPTION_HEXES[3] },
  { name: t('Green'), hex: OPTION_HEXES[4] },
  { name: t('Blue'), hex: OPTION_HEXES[5] },
  { name: t('Purple'), hex: OPTION_HEXES[6] },
  { name: t('Pink'), hex: OPTION_HEXES[7] },
  { name: t('Red'), hex: OPTION_HEXES[8] },
];

let idc = 0;

// optionSlug derives a stable id from a name, suffixing to avoid collisions with
// existing options (e.g. "In Progress" vs "in-progress" both slug to the same).
export function optionSlug(name: string, existing: { id: string }[]): string {
  let oid =
    name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') || `o${++idc}`;
  while (existing.some((o) => o.id === oid)) oid += '_';
  return oid;
}
