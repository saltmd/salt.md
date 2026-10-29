import { compare, isReadableDay, isUnfinishedRelativeDay, resolveSmartDate } from './format';
import type { Filter, FilterOp, User } from './types';

function isEmptyVal(v: unknown): boolean {
  if (Array.isArray(v)) return v.length === 0;
  return v === undefined || v === '' || v === null || v === false;
}

/** What a condition compares against: the set if there is one, else the single
 *  value. Never both — the same rule the server applies (rowFilter.vals). */
export function filterValues(f: Filter): string[] {
  if (f.values?.length) return f.values;
  return f.value ? [f.value] : [];
}

/** Is this condition finished enough to mean anything? An unfinished one must
 *  not filter: comparing against the empty string emptied the table the moment
 *  somebody added "Date is …", before they had typed a thing, which is what
 *  made the date filter look broken. `is_empty` is the deliberate version.
 *  A half-typed relative day ("today-") waits the same way, and on a date
 *  property so does anything that is not a day yet (see liveValue). */
export function filterIsArmed(f: Filter, isDate = false): boolean {
  const op = f.op ?? (f.value ? 'is' : 'is_not_empty');
  if (op === 'is_empty' || op === 'is_not_empty') return true;
  const live = (v: string) => liveValue(v, op, isDate);
  if (op === 'between') return !!f.value && (f.value2 ?? '') !== '' && live(f.value ?? '') && live(f.value2 ?? '');
  const vals = filterValues(f);
  return vals.length > 0 && vals.some(live);
}

/** May this value take part in the comparison? On a date property only a day
 *  or a relative day that resolves can (isReadableDay), except under
 *  "contains", which looks for text. Elsewhere only the dangling sign of a
 *  half-typed relative day waits. The server drops the same values. */
function liveValue(v: string, op: FilterOp, isDate: boolean): boolean {
  return isDate && op !== 'contains' ? isReadableDay(v) : !isUnfinishedRelativeDay(v);
}

export function matchesFilter(
  row: { props: Record<string, unknown> },
  f: Filter,
  isDate: boolean,
  isNumber = false,
  isPerson = false,
  currentUser: User | null = null,
): boolean {
  const v = row.props[f.property];
  const op = f.op ?? (f.value ? 'is' : 'is_not_empty');
  if (!filterIsArmed(f, isDate)) return true;
  // A relative day names a different date every day. Resolve it now — the
  // same way the server resolves it per query — or the rows the server sent
  // and the rows shown here would disagree. Date properties only: a status
  // option named "Today" must keep matching the option, not the date.
  const day = (s: string) => (isDate ? (resolveSmartDate(s) ?? s) : s);
  const live = (s: string) => liveValue(s, op, isDate);
  const value = day(f.value ?? '');
  const rawVals = filterValues(f).filter(live);
  const vals: string[] = [];
  for (const item of rawVals) {
    if (isPerson && item === '@me') {
      if (currentUser?.id) vals.push(currentUser.id);
      if (currentUser?.name && currentUser.name !== currentUser.id) vals.push(currentUser.name);
      if (!currentUser?.id) vals.push('__nobody__');
    } else {
      vals.push(day(item));
    }
  }
  // One value or several, the question is the same: does the cell hold any of
  // them. A cell may itself be a list (multiselect, relation, person).
  const holdsAny = () =>
    Array.isArray(v)
      ? vals.some((x) => v.includes(x))
      : vals.some((x) =>
          isNumber && v !== undefined && v !== null && v !== ''
            ? Number(v) === Number(x)
            : x === 'true' || x === 'false' ? String(v === true) === x : String(v ?? '') === x,
        );
  switch (op) {
    case 'is_empty':
      return isEmptyVal(v);
    case 'is_not_empty':
      return !isEmptyVal(v);
    case 'contains': {
      let needle = value.toLowerCase();
      if (isPerson && needle === '@me') {
        needle = currentUser?.name?.toLowerCase() || currentUser?.id?.toLowerCase() || '@me';
      }
      if (Array.isArray(v)) return v.some((x) => String(x).toLowerCase().includes(needle));
      return String(v ?? '').toLowerCase().includes(needle);
    }
    case 'gt':
    case 'lt': {
      const nv = Number(v);
      const nf = Number(value);
      const cmp = !Number.isNaN(nv) && !Number.isNaN(nf) ? nv - nf : compare(String(v ?? ''), value);
      return op === 'gt' ? cmp > 0 : cmp < 0;
    }
    case 'between': {
      // Inclusive at both ends — a range named by two dates contains them.
      // ISO dates compare correctly as text, so only numbers need Number().
      const hi = f.value2 === undefined ? '' : isDate ? (resolveSmartDate(f.value2) ?? f.value2) : f.value2;
      const nv = Number(v);
      if (!Number.isNaN(nv) && !Number.isNaN(Number(value)) && !Number.isNaN(Number(hi))) {
        return nv >= Number(value) && nv <= Number(hi);
      }
      const sv = String(v ?? '');
      return sv !== '' && sv >= value && sv <= hi;
    }
    case 'is_not':
      return !holdsAny();
    case 'is':
    default:
      // Checkbox filters compare against a boolean; a never-toggled box has no
      // stored value, which must count as "false" (unchecked), not "no match".
      return holdsAny();
  }
}

