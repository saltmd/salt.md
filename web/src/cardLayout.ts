import type { PropDef } from './types';

// How a board card decides what to show, and in which order (W126).
//
// The old rule was "render every property in schema order, text last". That
// makes the card grow with the SCHEMA rather than with what matters, and it
// produced the wildwuchs: two unlabelled dates side by side, a naked 55, the
// same colleague twice because two person fields both printed a full name,
// and four lines of address, mail and phone that nobody reads on a card.
//
// The rule now: a fixed order by ROLE, so a card looks the same everywhere no
// matter how the schema grew. Nothing is hidden by cleverness — the order is
// deterministic, the overflow is visible as a count, opens in place, and can
// be turned off per view.

export type CardZone =
  /** Coloured chips: select/multiselect. The colour IS the label. */
  | 'chip'
  /** Numbers, dates, checkboxes, checklists, rollups, formulas — shown WITH
      their field name, because a bare "55" or a second date means nothing. */
  | 'fact'
  /** People — a stack of faces in the card's top right corner. */
  | 'person'
  /** One short text, clamped. */
  | 'note'
  /** Mail, phone, address, link: on a card these are "exists", not content. */
  | 'contact'
  /** Never on a card. */
  | 'hidden';

/** Values that carry no information but still occupy a line. Imports are full
    of them — a Trello field emptied by hand becomes "-", not "". */
const FILLER = new Set(['-', '–', '—', '/', 'n/a', 'na', 'k.a.', 'keine', 'none']);

export function isBlank(v: unknown): boolean {
  if (v === undefined || v === null) return true;
  if (Array.isArray(v)) return v.length === 0;
  if (typeof v === 'string') {
    const s = v.trim();
    return s === '' || FILLER.has(s.toLowerCase());
  }
  return false;
}

const MAIL = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
// Phone numbers as people write them: +49 6202 93560, 06202/935631, …
const PHONE = /^[+(]?[\d][\d\s()/.-]{5,}$/;
// A postal line: a postcode followed by a town.
const POSTAL = /^\d{4,5}\s+\S/;

// Three or more dot-separated groups of digits: an IP address, a version, a
// dotted date. Never a telephone number — nobody writes one as 1.6.11.
//
// PHONE alone accepts all of them, because dots are a separator people do use
// in phone numbers. Reported from a real documentation workspace: every IP on
// every card was drawn as a telephone receiver, with the address itself hidden
// behind the icon. One dot still reads as a phone number (06202.935631), which
// is the case that made this hard to see.
const DOTTED_GROUPS = /^\d+(\.\d+){2,}$/;
const isPhone = (s: string) => PHONE.test(s) && !DOTTED_GROUPS.test(s);

// An IPv4 address, with an optional prefix length — 192.168.99.245, 10.0.0.0/8.
// Spelled out by shape (four groups, none above 255) because that is the only
// thing separating it from any other dotted number.
const IPV4 =
  /^((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)\.){3}(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\/(3[0-2]|[12]?\d))?$/;

/** Which zone a property belongs to. The value is consulted only for text —
    the same "text" property can hold a phone number on one card and a real
    note on the next, and the card should treat those differently. */
export function zoneOf(def: PropDef, value: unknown): CardZone {
  switch (def.type) {
    case 'select':
    case 'multiselect':
      return 'chip';
    case 'number':
    case 'date':
    case 'checkbox':
    case 'checklist':
    case 'rollup':
    case 'formula':
      return 'fact';
    case 'person':
      return 'person';
    case 'url':
      return 'contact';
    // A relation names what this row belongs to — the customer, the system,
    // the deal. It used to be banned from cards outright, which was defensible
    // while a board could only group by a select: you saw the status in the
    // column and nothing told you which project the card belonged to.
    //
    // It costs nothing on a board that groups BY this relation, because the
    // grouped property is dropped from every card anyway (see BoardCardProps).
    // So it shows where it informs and stays quiet where it would repeat.
    case 'relation':
      return 'chip';
    // A backrelation is a list of everything pointing here — on a system row
    // that is every task it has. Useful in a table, far too much for a card.
    case 'backrelation':
      return 'hidden';
    case 'text':
    default: {
      const s = String(value ?? '').trim();
      // An address is a fact, not a contact: the contact zone means "this
      // exists" and shows an icon INSTEAD of the value, which is right for a
      // phone number and useless for an address — the whole point of an address
      // is the digits. And it needs its field name, because "internal",
      // "external" and "gateway" are the same four numbers otherwise.
      if (IPV4.test(s)) return 'fact';
      if (MAIL.test(s) || isPhone(s) || POSTAL.test(s)) return 'contact';
      return 'note';
    }
  }
}

/** The icon a contact value gets — its whole presence on the card. */
export type ContactKind = 'mail' | 'phone' | 'address' | 'link';

export function contactKind(def: PropDef, value: unknown): ContactKind {
  if (def.type === 'url') return 'link';
  const s = String(value ?? '').trim();
  if (MAIL.test(s)) return 'mail';
  // Same rule as zoneOf, deliberately: this function is reachable from anywhere
  // that already decided "contact", and it must not disagree with the zone that
  // sent the value here.
  if (isPhone(s)) return 'phone';
  return 'address';
}

/** A fact worth showing without its label: a date field actually called
    "Datum", a number called "Anzahl". Repeating the name would be noise. */
export function needsLabel(def: PropDef, factCount: number): boolean {
  if (factCount < 2) {
    const n = def.name.trim().toLowerCase();
    if (n === 'datum' || n === 'date' || n === 'zahl' || n === 'number') return false;
  }
  return true;
}

/** How many facts a card prints before the rest collapses into "+n".
 *
 *  Deliberately generous: cards are ALLOWED to differ in height — what makes
 *  a board look messy is disorder, not variation, and the zones above supply
 *  the order. So this is not a design cap, it is a guard against the one case
 *  that really becomes a wall: a schema with thirty fields on every card. In
 *  practice a card stays well under it, because contact fields moved to icons,
 *  people collapsed into one stack, and filler values count as empty. */
export const CARD_FACT_LIMIT = 8;

export interface CardPlan<T> {
  chips: T[];
  facts: T[];
  people: T[];
  notes: T[];
  contacts: T[];
  /** Facts and notes beyond the cap — shown as a count, opened in place. */
  overflow: T[];
}

/** Sort properties into the zones, applying the cap. Everything that is
    dropped ends up in `overflow` and nowhere else, so a card can always
    account for every field it did not print. */
export function planCard<T>(
  entries: T[],
  of: (e: T) => { def: PropDef; value: unknown },
): CardPlan<T> {
  const plan: CardPlan<T> = { chips: [], facts: [], people: [], notes: [], contacts: [], overflow: [] };
  for (const e of entries) {
    const { def, value } = of(e);
    const zone = zoneOf(def, value);
    if (zone === 'hidden') continue;
    if (zone === 'chip') plan.chips.push(e);
    else if (zone === 'person') plan.people.push(e);
    else if (zone === 'contact') plan.contacts.push(e);
    else if (zone === 'fact') plan.facts.push(e);
    else plan.notes.push(e);
  }
  // The guard applies to the two zones that grow without bound and cost a
  // full row each. A note is worth more than a ninth date, so notes go first.
  const capped = [...plan.notes.slice(0, 1), ...plan.facts];
  if (capped.length > CARD_FACT_LIMIT) {
    const keep = new Set(capped.slice(0, CARD_FACT_LIMIT));
    plan.overflow = [...plan.notes, ...plan.facts].filter((e) => !keep.has(e));
    plan.facts = plan.facts.filter((e) => keep.has(e));
    plan.notes = plan.notes.filter((e) => keep.has(e));
  } else {
    plan.overflow = plan.notes.slice(1);
    plan.notes = plan.notes.slice(0, 1);
  }
  return plan;
}
