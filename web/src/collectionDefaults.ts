import type { Filter, PropDef } from './types';

/** Infer only values fixed by equality conditions. Alternatives are not a
 * choice we can make for the author; derived properties cannot be assigned.
 * Board/calendar presets are explicit choices and take precedence. */
export function collectionDefaults(
  schema: PropDef[],
  filters: Filter[],
  presets: Record<string, unknown> = {},
): Record<string, unknown> {
  const props: Record<string, unknown> = {};
  for (const prop of schema) {
    if (['formula', 'rollup', 'backrelation', 'lastActivity', 'checklist'].includes(prop.type)) continue;
    const conditions = filters.filter(f => f.property === prop.id &&
      (f.op ?? (f.value === '' ? 'is_not_empty' : 'is')) === 'is');
    const sets = conditions.map(f => f.values?.length ? f.values : f.value === '' ? [] : [f.value])
      .filter(values => values.length > 0);
    if (!sets.length) continue;
    const multiple = prop.type === 'relation' || prop.type === 'multiselect' ||
      (prop.type === 'person' && prop.personMultiple);
    if (multiple) {
      // Each singleton condition is required (AND); alternatives stay unassigned.
      const required = [...new Set(sets.filter(values => values.length === 1).map(values => values[0]))];
      if (required.length && sets.every(values => required.some(value => values.includes(value)))) {
        props[prop.id] = required;
      }
      continue;
    }
    const candidates = [...new Set(sets[0])].filter(value => sets.every(values => values.includes(value)));
    if (candidates.length !== 1) continue;
    const value = candidates[0];
    if (prop.type === 'checkbox') {
      if (value === 'true' || value === 'false') props[prop.id] = value === 'true';
    } else if (prop.type === 'number') {
      if (value.trim() !== '' && Number.isFinite(Number(value))) props[prop.id] = Number(value);
    } else {
      props[prop.id] = value;
    }
  }
  return { ...props, ...presets };
}
