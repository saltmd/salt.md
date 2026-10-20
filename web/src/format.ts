// The one place that turns machine values into text a human reads.
//
// Nothing outside this file may call toLocaleDateString, toLocaleString,
// toLocaleTimeString, Intl.* or `new Date(...)` for display purposes. That is
// not a style rule — it is what keeps the two kinds of date apart:
//
//   A MOMENT      an instant on the world clock ("last edited 14:30").
//                 Stored UTC, shown in the viewer's own timezone. Two people
//                 in Berlin and Tokyo see different clock faces for the SAME
//                 instant, and that is correct.
//
//   A DAY         a calendar date with no timezone ("due 2026-07-18").
//                 A deadline on the 18th is the 18th in Tokyo and in Los
//                 Angeles. Converting it moves it, and a contract expires a
//                 day early.
//
// The trap is that JavaScript makes the wrong one easy: `new Date('2026-07-18')`
// parses as UTC midnight, so west of Greenwich it renders as the 17th. Every
// date bug in this codebase has been a variant of that. Hence formatMoment and
// formatDay as two separate functions — you cannot confuse what you cannot mix.

type Style = 'time' | 'dayMonth' | 'date' | 'datetime' | 'full';

let locale = 'en';

// ---- Preferences (W112) ----
//
// Each of these has an AUTOMATIC state, and automatic is the default: the empty
// string, or null, means "whatever the region implies" — which is what happened
// before there was a setting. Only a deliberate choice ever differs.

/** IANA zone for MOMENTS. '' is the machine's own zone. */
let zone = '';
/** true = 12 hours, false = 24, undefined = let the region decide. */
let hour12: boolean | undefined;
/** 0 = Sunday … 6 = Saturday, null = let the region decide. */
let weekStartOverride: number | null = null;

/** Switch the language used for all formatting. Called by i18n.ts; the two
 *  always move together. */
export function setFormatLocale(next: string) {
  locale = next;
  fmtCache.clear();
  collator = null;
}

export function formatLocale(): string {
  return locale;
}

const WEEK_START: Record<string, number> = { sun: 0, mon: 1, sat: 6 };

/** Apply the account's preferences. Anything unset stays automatic.
 *
 *  Called from i18n.ts alongside setFormatLocale, so the two can never disagree
 *  about which settings the interface is currently showing. */
export function setFormatPrefs(p: { timeZone?: string; clock?: string; weekStart?: string }) {
  zone = p.timeZone ?? '';
  hour12 = p.clock === '12' ? true : p.clock === '24' ? false : undefined;
  weekStartOverride = p.weekStart && p.weekStart in WEEK_START ? WEEK_START[p.weekStart] : null;
  fmtCache.clear();
}

/** The zone moments are shown in, resolved — never '', so a settings dialog can
 *  say "Automatic (Europe/Berlin)" without having to work it out itself. */
export function resolvedTimeZone(): string {
  if (zone) return zone;
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
  } catch {
    return 'UTC';
  }
}

// Intl objects are expensive to build — measurably so inside a list that
// renders a timestamp per row. Cache them per locale + option set.
const fmtCache = new Map<string, Intl.DateTimeFormat>();

function build(opts: Intl.DateTimeFormatOptions): Intl.DateTimeFormat {
  const full = hour12 === undefined || opts.hour === undefined ? opts : { ...opts, hour12 };
  // The separator is written as an escape: a literal NUL byte made git and
  // GitHub treat this whole file as binary, so its diffs could not be read.
  const key = locale + '\u0000' + JSON.stringify(full);
  let f = fmtCache.get(key);
  if (!f) {
    try {
      f = new Intl.DateTimeFormat(locale, full);
    } catch {
      // An unknown timezone makes the constructor throw. The server checks only
      // the SHAPE of a stored zone — it carries no tzdata, see prefs.go — so an
      // unusable one can reach here, and one bad setting must never be able to
      // blank out every timestamp in the app. Drop the zone, keep the rest.
      const { timeZone: _unused, ...rest } = full;
      f = new Intl.DateTimeFormat(locale, rest);
    }
    fmtCache.set(key, f);
  }
  return f;
}

/** Formatter for a CALENDAR value. Carries NO timezone, by construction — a day
 *  is the day it says it is, everywhere. */
function dtf(opts: Intl.DateTimeFormatOptions): Intl.DateTimeFormat {
  return build(opts);
}

/** Formatter for a MOMENT, in the viewer's zone.
 *
 *  Two functions rather than one with a flag, for the same reason formatMoment
 *  and formatDay are two functions: a timezone must be impossible to attach to
 *  a calendar date by accident. Nothing that formats a DAY may call this. */
function dtfZoned(opts: Intl.DateTimeFormatOptions): Intl.DateTimeFormat {
  return build(zone ? { ...opts, timeZone: zone } : opts);
}

const STYLES: Record<Style, Intl.DateTimeFormatOptions> = {
  time: { hour: '2-digit', minute: '2-digit' },
  dayMonth: { day: 'numeric', month: 'short' },
  date: { day: '2-digit', month: '2-digit', year: 'numeric' },
  datetime: { day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit' },
  full: { day: 'numeric', month: 'long', year: 'numeric', hour: '2-digit', minute: '2-digit' },
};

// ---- Moments: an instant, shown in the viewer's timezone ----

/** Format a UTC timestamp from the server (RFC3339) in the viewer's timezone —
 *  the machine's own, or the one the account chose. */
export function formatMoment(iso: string | null | undefined, style: Style = 'datetime'): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  return dtfZoned(STYLES[style]).format(d);
}

/** "5 minutes ago", "yesterday", "now" — in whatever language is active.
 *
 *  Intl does the wording, including the languages where "yesterday" is not a
 *  number-plus-unit at all. The hand-rolled German version this replaced could
 *  only ever say one thing. */
export function formatRelative(iso: string | null | undefined): string {
  if (!iso) return '';
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return '';
  const secs = Math.round((t - Date.now()) / 1000);
  const abs = Math.abs(secs);
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' });
  if (abs < 45) return rtf.format(0, 'second');
  if (abs < 3600) return rtf.format(Math.round(secs / 60), 'minute');
  if (abs < 86400) return rtf.format(Math.round(secs / 3600), 'hour');
  if (abs < 7 * 86400) return rtf.format(Math.round(secs / 86400), 'day');
  // Beyond a week a relative phrase stops being useful ("3 weeks ago" tells
  // you less than the date does). Within this year the day and month are
  // enough; older than that the year has to come along, or "18 Jul" could
  // mean any of several.
  const sameYear = new Date(t).getFullYear() === new Date().getFullYear();
  return formatMoment(iso, sameYear ? 'dayMonth' : 'date');
}

// ---- Days: a calendar date, never converted ----

/** Split "2026-07-18" or "2026-07-18T14:30" into its parts. Deliberately
 *  hand-parsed: handing the string to `new Date()` is the bug this module
 *  exists to prevent. */
function parseDay(day: string): { y: number; m: number; d: number; hh: number; mm: number; timed: boolean } | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})(?:T(\d{2}):(\d{2}))?/.exec(day);
  if (!m) return null;
  return {
    y: +m[1],
    m: +m[2],
    d: +m[3],
    hh: m[4] ? +m[4] : 0,
    mm: m[5] ? +m[5] : 0,
    timed: m[4] !== undefined,
  };
}

/** Format a stored calendar date. The day shown is always the day stored — no
 *  timezone maths happens here, by construction.
 *
 *  This holds WHATEVER the timezone setting says. That is the one place where a
 *  timezone preference could do real damage: a deadline on the 18th is the 18th
 *  for a viewer who has set their zone to Auckland, exactly as it is for one who
 *  has set it to Los Angeles. Hence dtf() and not dtfZoned(), and hence the
 *  assertions in check-format.mjs that pin it. */
export function formatDay(day: string | null | undefined, style: Style = 'date'): string {
  if (!day) return '';
  const p = parseDay(day);
  if (!p) return '';
  // Built with local constructor arguments, so the Date lands on local midnight
  // and formatting reads back the same calendar day everywhere on earth.
  const d = new Date(p.y, p.m - 1, p.d, p.hh, p.mm);
  if (p.timed && style !== 'time') {
    return dtf({ ...STYLES[style], hour: '2-digit', minute: '2-digit' }).format(d);
  }
  return dtf(STYLES[style]).format(d);
}

/** A month heading ("Jul 26", "July 2026"). Same rule as formatDay: the month
 *  asked for is the month shown. */
export function formatMonth(year: number, monthIndex: number, style: 'short' | 'long' = 'short'): string {
  const d = new Date(year, monthIndex, 1);
  return dtf(
    style === 'short' ? { month: 'short', year: '2-digit' } : { month: 'long', year: 'numeric' },
  ).format(d);
}

export function formatMonthYear(year: number, monthIndex: number): string {
  const d = new Date(year, monthIndex, 1);
  if (year === new Date().getFullYear()) return dtf({ month: 'short' }).format(d);
  return dtf({ month: 'short', year: 'numeric' }).format(d);
}

/** Whole days from today to a stored calendar date; negative when it has
 *  passed. Both sides are built as local midnight, so "due today" stays true
 *  all day and does not flip at 01:00 the way a UTC comparison would.
 *
 *  "Today" comes from today(), so it follows the timezone setting — and both
 *  sides of the subtraction are then calendar days in the same reckoning. */
export function daysUntil(day: string): number | null {
  const p = parseDay(day);
  if (!p) return null;
  const n = parseDay(today());
  if (!n) return null;
  const due = new Date(p.y, p.m - 1, p.d);
  const midnight = new Date(n.y, n.m - 1, n.d);
  return Math.round((due.getTime() - midnight.getTime()) / 86400000);
}

/** Today as "YYYY-MM-DD" in the viewer's timezone — the string form used for
 *  storing and comparing calendar dates.
 *
 *  This one DOES follow the timezone setting, and it has to: "what day is it
 *  now" is a question about a moment, not about a stored date. Somebody who
 *  says they are in Tokyo wants "today" to be Tokyo's today, or a task created
 *  just after midnight there gets yesterday's date.
 *
 *  Note the direction: the setting decides which day *now* falls on. It never
 *  touches a day that is already stored. */
export function today(): string {
  if (!zone) return toDayString(new Date());
  // Read the calendar parts out of the zone rather than shifting a Date around
  // — an offset in hours cannot express the answer for every zone, and getting
  // it wrong here would move every due date by a day.
  const parts = dtfZoned({ year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(
    new Date(),
  );
  const get = (t: string) => parts.find((p) => p.type === t)?.value ?? '';
  const y = get('year');
  const mo = get('month');
  const d = get('day');
  return y && mo && d ? `${y}-${mo}-${d}` : toDayString(new Date());
}

export function toDayString(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}

// ---- Calendars ----

/** Which day the week starts on, 0 = Sunday … 6 = Saturday.
 *
 *  Monday in most of Europe, Sunday in the US and Japan, Saturday in much of
 *  the Arab world. Intl knows; the calendar view used to hardcode Monday. */
export function firstWeekday(): number {
  if (weekStartOverride !== null) return weekStartOverride;
  try {
    const loc = new Intl.Locale(locale) as Intl.Locale & {
      getWeekInfo?: () => { firstDay: number };
      weekInfo?: { firstDay: number };
    };
    const info = typeof loc.getWeekInfo === 'function' ? loc.getWeekInfo() : loc.weekInfo;
    // Intl counts Monday = 1 … Sunday = 7; JavaScript's getDay() counts
    // Sunday = 0 … Saturday = 6.
    if (info?.firstDay) return info.firstDay % 7;
  } catch {
    /* older browser without weekInfo — fall through */
  }
  return SUNDAY_FIRST.has(locale.slice(0, 2)) ? 0 : 1;
}

// Fallback for browsers without weekInfo (Firefox until recently). Not
// exhaustive; it only has to be better than "Monday, always".
const SUNDAY_FIRST = new Set(['en', 'ja', 'ko', 'zh', 'pt', 'es', 'he', 'ar', 'th', 'id']);

/** Weekday headers for a calendar grid, already rotated so index 0 is the
 *  locale's first day. */
export function weekdayNames(style: 'short' | 'narrow' = 'short'): string[] {
  const f = dtf({ weekday: style });
  const start = firstWeekday();
  const out: string[] = [];
  // 2026-02-01 was a Sunday, so offsetting from it gives a clean week.
  for (let i = 0; i < 7; i++) out.push(f.format(new Date(2026, 1, 1 + ((start + i) % 7))));
  return out;
}

// ---- What "automatic" currently resolves to ----
//
// The settings dialog has to be able to say "Automatic (Europe/Berlin)" rather
// than just "Automatic". A setting whose automatic value is invisible is a
// setting people flip to manual just to find out what it was.
//
// These live here and not in the dialog because everything Intl belongs in this
// file — that is the rule the whole module exists for, and a picker full of
// timezone names is no reason to make an exception.

/** Every IANA zone this browser knows, sorted. Empty on a browser too old for
 *  supportedValuesOf, in which case the dialog offers automatic only — which is
 *  correct behaviour, not a broken list. */
export function availableTimeZones(): string[] {
  try {
    const f = (Intl as unknown as { supportedValuesOf?: (k: string) => string[] })
      .supportedValuesOf;
    return typeof f === 'function' ? f('timeZone') : [];
  } catch {
    return [];
  }
}

/** '12' or '24' — what the active region implies when nobody has chosen. */
export function resolvedClock(): '12' | '24' {
  try {
    return new Intl.DateTimeFormat(locale, { hour: 'numeric' }).resolvedOptions().hour12
      ? '12'
      : '24';
  } catch {
    return '24';
  }
}

/** The name of a weekday, 0 = Sunday, in the active language. */
export function weekdayLabel(index: number): string {
  // 2026-02-01 was a Sunday, so adding the index lands on the day asked for.
  return dtf({ weekday: 'long' }).format(new Date(2026, 1, 1 + index));
}

/** A regional tag as a person would recognise it. Falls back to the tag
 *  itself, which is still better than nothing.
 *  Example: "Deutsch (Österreich)" — i18n-ok: a German example. */
export function regionLabel(tag: string): string {
  try {
    const dn = new Intl.DisplayNames([locale], { type: 'region' });
    const parts = new Intl.Locale(tag);
    const lang = new Intl.DisplayNames([locale], { type: 'language' }).of(parts.language);
    const reg = parts.region ? dn.of(parts.region) : '';
    return reg ? `${lang} (${reg})` : lang || tag;
  } catch {
    return tag;
  }
}

// ---- Numbers ----

export function formatNumber(n: number, opts?: Intl.NumberFormatOptions): string {
  return new Intl.NumberFormat(locale, opts).format(n);
}

/** Byte sizes with the locale's own decimal separator — "1,5 MB" in German,
 *  "1.5 MB" in English. The unit itself is not translated; MB is MB. */
export function formatBytes(n: number): string {
  if (n >= 1 << 30) return formatNumber(n / (1 << 30), { maximumFractionDigits: 2 }) + ' GB';
  if (n >= 1 << 20) return formatNumber(n / (1 << 20), { maximumFractionDigits: 1 }) + ' MB';
  if (n >= 1 << 10) return formatNumber(n / (1 << 10), { maximumFractionDigits: 0 }) + ' KB';
  return formatNumber(n) + ' B';
}

// ---- Sorting ----

let collator: Intl.Collator | null = null;

/** Compare two strings the way the active language sorts them.
 *
 *  Plain `<` compares UTF-16 code units — i18n-ok: "Ärger" lands after "Zebra"
 *  under that — and every lowercase word after every uppercase one.
 *  Intl.Collator knows that German files Ä with A (i18n-ok: the letter is the
 *  example), Swedish files it at the end, and Czech sorts Ch as its own. */
export function compare(a: string, b: string): number {
  if (!collator) collator = new Intl.Collator(locale, { numeric: true, sensitivity: 'base' });
  return collator.compare(a || '', b || '');
}
