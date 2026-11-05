import { filterIsArmed, filterValues, matchesFilter } from './collectionFilters';
import { resolveSmartDate } from './format';
import type { Filter, PropDef, User } from './types';

/** Infer only values fixed by equality conditions. Alternatives are not a
 * choice we can make for the author; derived properties cannot be assigned.
 * Board/calendar presets are explicit choices and take precedence.
 *
 * "@me" on a person is the one who makes the row. Stored as written it would
 * name whoever looks at it, so everybody would see the row as their own. */
export function collectionDefaults(
  schema: PropDef[],
  filters: Filter[],
  presets: Record<string, unknown> = {},
  currentUser: User | null = null,
): Record<string, unknown> {
  const props: Record<string, unknown> = {};
  for (const prop of schema) {
    if (['formula', 'rollup', 'backrelation', 'lastActivity', 'checklist'].includes(prop.type)) continue;
    const conditions = filters.filter(f => f.property === prop.id && filterIsArmed(f, prop.type === 'date') &&
      (f.op ?? (f.value === '' ? 'is_not_empty' : 'is')) === 'is');
    const normalize = (value: string) => {
      if (prop.type === 'date') return resolveSmartDate(value) ?? value;
      if (prop.type === 'number' && value.trim() !== '' && Number.isFinite(Number(value))) return String(Number(value));
      // Nobody signed in: nobody to assign, so the value drops out.
      if (prop.type === 'person' && value === '@me') return currentUser?.id ?? null;
      return value;
    };
    const sets = conditions.map(f => filterValues(f).map(normalize).filter((value): value is string => value !== null))
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
  // Reuse the view's evaluator rather than inventing a second set of rules.
  // Contradictory or restrictive conditions must not produce misleading defaults.
  for (const prop of schema) {
    if (filters.some(f => f.property === prop.id &&
      !matchesFilter({ props }, f, prop.type === 'date', prop.type === 'number', prop.type === 'person', currentUser))) {
      delete props[prop.id];
    }
  }
  return { ...props, ...presets };
}
