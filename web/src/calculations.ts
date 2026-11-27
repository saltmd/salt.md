import { formatDay, formatNumber } from './format';
import { plural, t } from './i18n';
import type { Calculation, PropDef } from './types';

/** The key the title column goes by in a view's calculations and widths. */
export const TITLE_COL = '__title';

/** A column whose footer shows nothing. */
export const CALC_NONE = 'none';

const COUNTS: Calculation[] = [
  'count_all',
  'count_values',
  'count_unique',
  'count_empty',
  'count_not_empty',
  'percent_empty',
  'percent_not_empty',
];
const NUMBERS: Calculation[] = ['sum', 'avg', 'median', 'min', 'max', 'range'];
const DATES: Calculation[] = ['earliest', 'latest', 'date_range'];
const CHECKS: Calculation[] = ['checked', 'unchecked', 'percent_checked', 'percent_unchecked'];

/** Every calculation, in menu order. */
export const ALL_CALCULATIONS: Calculation[] = [...COUNTS, ...NUMBERS, ...DATES, ...CHECKS];

/** The calculations a column offers, by its property type (none for the
 *  title column). */
export function calculationsFor(p: PropDef | null): Calculation[] {
  switch (p?.type) {
    case 'number':
    case 'rollup':
      return [...COUNTS, ...NUMBERS, ...DATES];
    case 'formula':
      return ALL_CALCULATIONS;
    case 'date':
      return [...COUNTS, ...DATES];
    case 'checkbox':
      return ['count_all', ...CHECKS];
  }
  return COUNTS;
}

/** What a column shows when its view has not chosen anything. */
export function defaultCalculation(p: PropDef | null): Calculation {
  if (!p) return 'count_all';
  if (p.type === 'number' || p.type === 'rollup' || p.type === 'formula') return 'sum';
  if (p.type === 'checkbox') return 'checked';
  return 'count_not_empty';
}

/** The calculation a column shows in a view, or null for none. */
export function columnCalculation(calcs: Record<string, string> | undefined, key: string, p: PropDef | null): Calculation | null {
  const chosen = calcs?.[key];
  if (chosen === CALC_NONE) return null;
  return (chosen as Calculation | undefined) ?? defaultCalculation(p);
}

/** The menu entry for a calculation. A function, not a table: a list of labels
 *  built at import would keep the language the tab started in. */
export function calculationLabel(c: Calculation | typeof CALC_NONE): string {
  switch (c) {
    case CALC_NONE:
      return t('None');
    case 'count_all':
      return t('Count all');
    case 'count_values':
      return t('Count values');
    case 'count_unique':
      return t('Count unique values');
    case 'count_empty':
      return t('Count empty');
    case 'count_not_empty':
      return t('Count not empty');
    case 'percent_empty':
      return t('Percent empty');
    case 'percent_not_empty':
      return t('Percent not empty');
    case 'sum':
      return t('Sum');
    case 'avg':
      return t('Average');
    case 'median':
      return t('Median');
    case 'min':
      return t('Min');
    case 'max':
      return t('Max');
    case 'range':
      return t('Range');
    case 'earliest':
      return t('Earliest date');
    case 'latest':
      return t('Latest date');
    case 'date_range':
      return t('Date range');
    case 'checked':
      return t('Checked');
    case 'unchecked':
      return t('Unchecked');
    case 'percent_checked':
      return t('Percent checked');
    case 'percent_unchecked':
      return t('Percent unchecked');
  }
}

const num = (v: number) => formatNumber(Math.round(v * 1e6) / 1e6);

/** The footer text for a calculated value; empty when there is nothing to
 *  show. `quietZero` leaves a zero count out, for a column nobody chose a
 *  calculation for. */
export function calculationText(c: Calculation, value: unknown, quietZero: boolean, isTitle: boolean): string {
  if (value === null || value === undefined) return '';
  if (typeof value === 'string') {
    const day = formatDay(value, 'date') || value;
    return c === 'earliest' ? t('Earliest {date}', { date: day }) : t('Latest {date}', { date: day });
  }
  const n = Number(value);
  if (quietZero && n === 0) return '';
  const v = num(n);
  switch (c) {
    case 'count_all':
      return isTitle ? plural(n, '{n} row', '{n} rows') : t('Count {n}', { n: v });
    case 'count_values':
      return t('Values {n}', { n: v });
    case 'count_unique':
      return t('Unique {n}', { n: v });
    case 'count_empty':
      return t('Empty {n}', { n: v });
    case 'count_not_empty':
      return t('{n} filled', { n: v });
    case 'percent_empty':
      return t('Empty {n}%', { n: v });
    case 'percent_not_empty':
      return t('Filled {n}%', { n: v });
    case 'sum':
      return 'Σ ' + v;
    case 'avg':
      return t('Average {n}', { n: v });
    case 'median':
      return t('Median {n}', { n: v });
    case 'min':
      return t('Min {n}', { n: v });
    case 'max':
      return t('Max {n}', { n: v });
    case 'range':
      return t('Range {n}', { n: v });
    case 'date_range':
      return plural(n, '{n} day', '{n} days', { n: v });
    case 'checked':
      return t('Checked {n}', { n: v });
    case 'unchecked':
      return t('Unchecked {n}', { n: v });
    case 'percent_checked':
      return t('Checked {n}%', { n: v });
    case 'percent_unchecked':
      return t('Unchecked {n}%', { n: v });
  }
  return v;
}
