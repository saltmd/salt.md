// Asserts how a board card sorts its properties into zones (src/cardLayout.ts).
//
//   node scripts/check-cardlayout.mjs
//
// No test framework, same shape as check-format.mjs: esbuild is here anyway as
// part of vite, so the module is bundled once and imported.
//
// Why this exists: the rules are guesses about VALUES, not just types — a text
// property holding "+49 6202 93560" is a phone number and belongs on an icon,
// the same property holding a sentence is a note and belongs on its own line.
// Those guesses are exactly the kind of thing that quietly stops being true.

import { execFileSync } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const web = join(here, '..');
const tmp = mkdtempSync(join(tmpdir(), 'salt-card-'));
const bundle = join(tmp, 'cardLayout.mjs');

try {
  try {
    execFileSync(
      join(web, 'node_modules/esbuild/bin/esbuild'),
      [join(web, 'src/cardLayout.ts'), '--bundle', '--format=esm', '--outfile=' + bundle],
      { stdio: ['ignore', 'ignore', 'pipe'] },
    );
  } catch (e) {
    console.error(String(e.stderr ?? e));
    process.exit(1);
  }

  const cl = await import(bundle);
  const out = [];
  const check = (name, got, want) => out.push({ name, got: String(got), want: String(want) });
  const def = (id, type, name = id) => ({ id, name, type });

  // ---- Filler values ----
  // The lone "-" under a card came from a Trello import: a field emptied by
  // hand is not an empty string, and it cost a full row on every card.
  for (const v of ['', '   ', '-', '–', '—', 'n/a', 'K.A.', 'keine']) {
    check(`isBlank(${JSON.stringify(v)})`, cl.isBlank(v), true);
  }
  for (const v of ['0', 'Koeln', '-1']) {
    check(`isBlank(${JSON.stringify(v)})`, cl.isBlank(v), false);
  }
  check('isBlank([])', cl.isBlank([]), true);
  check('isBlank(undefined)', cl.isBlank(undefined), true);
  check('isBlank(0)', cl.isBlank(0), false);

  // ---- Zones by type ----
  check('select is a chip', cl.zoneOf(def('a', 'select'), 'x'), 'chip');
  check('multiselect is a chip', cl.zoneOf(def('a', 'multiselect'), ['x']), 'chip');
  check('date is a fact', cl.zoneOf(def('a', 'date'), '2026-01-01'), 'fact');
  check('number is a fact', cl.zoneOf(def('a', 'number'), 55), 'fact');
  check('checklist is a fact', cl.zoneOf(def('a', 'checklist'), []), 'fact');
  // A computed value is an OBJECT, and the text branch turns one into
  // "[object Object]" on every card. That shipped once, so every computed type
  // gets a line here.
  check(
    'last activity is a fact, not text',
    cl.zoneOf(def('a', 'lastActivity'), { at: '2026-01-01T10:00:00Z', by: 'Ada' }),
    'fact',
  );
  check('person is a person', cl.zoneOf(def('a', 'person'), 'u1'), 'person');
  check('url is a contact', cl.zoneOf(def('a', 'url'), 'https://trello.com/c/x'), 'contact');
  // A relation used to be banned from cards. That held while a board could
  // only group by a select: you saw the status in the column heading and
  // nothing on the card said which system the task belonged to. Now that a
  // board can group by the relation itself, the card may show it — and on a
  // board grouped BY it the property is dropped anyway, so it never repeats
  // its own column heading.
  check('relation is a chip on the card', cl.zoneOf(def('a', 'relation'), ['p1']), 'chip');
  // Its reverse is a different matter: on a system row that is every task
  // pointing at it. Fine in a table, far too much for a card.
  check('backrelation stays off the card', cl.zoneOf(def('a', 'backrelation'), ['p1']), 'hidden');

  // ---- Zones by VALUE: the part that is a guess ----
  const text = def('a', 'text');
  check('a mail address is a contact', cl.zoneOf(text, 'info@example.de'), 'contact');
  check('a phone number is a contact', cl.zoneOf(text, '+49 6202 93560'), 'contact');
  check('a phone with a slash is a contact', cl.zoneOf(text, '06202/935631'), 'contact');
  check('a postal line is a contact', cl.zoneOf(text, '68723 Plankstadt'), 'contact');
  check('a sentence is a note', cl.zoneOf(text, 'Bitte um Rueckruf ab 14 Uhr'), 'note');
  // A short number must not be mistaken for a phone number.
  check('a bare 42 is a note', cl.zoneOf(text, '42'), 'note');

  // Reported from a real documentation workspace: every IP on every card was
  // drawn as a telephone receiver, with the address itself hidden behind the
  // icon. Digits separated by dots are exactly what the phone pattern matches,
  // so the address shape has to be tested first and win.
  check('an IP is a fact, not a contact', cl.zoneOf(text, '192.168.99.245'), 'fact');
  check('a subnet is a fact', cl.zoneOf(text, '10.0.0.0/8'), 'fact');
  check('a gateway is a fact', cl.zoneOf(text, '10.0.0.1'), 'fact');
  check('an IP never gets a phone icon', cl.contactKind(text, '192.168.99.245'), 'address');
  // And the shape still has to be an address: four groups, none above 255.
  // Not an address by shape, and still not a phone number: a dotted list of
  // three or more groups is neither, so it falls through to plain text.
  check('999.1.1.1 is neither', cl.zoneOf(text, '999.1.1.1'), 'note');
  check('a version number is neither', cl.zoneOf(text, '1.6.11'), 'note');
  check('a dotted date is neither', cl.zoneOf(text, '2026.08.03'), 'note');
  // The reason this was hard to see: real phone numbers must keep working.
  check('a dotted phone number is still a contact', cl.zoneOf(text, '06202.935631'), 'contact');

  check('mail icon', cl.contactKind(text, 'x@y.de'), 'mail');
  check('phone icon', cl.contactKind(text, '+49 221 139870'), 'phone');
  check('address icon', cl.contactKind(text, '50996 Koeln'), 'address');
  check('link icon', cl.contactKind(def('a', 'url'), 'https://trello.com'), 'link');

  // ---- The card from the screenshot, sorted ----
  const defs = [
    def('status', 'select', 'Status'),
    def('kind', 'multiselect', 'Anlage'),
    def('owner', 'person', 'Verantwortlich'),
    def('team', 'person', 'Team'),
    def('size', 'number', 'Mitarbeiter'),
    def('followup', 'date', 'Wiedervorlage'),
    def('created', 'date', 'Erstkontakt'),
    def('mail', 'text', 'E-Mail'),
    def('phone', 'text', 'Telefon'),
    def('city', 'text', 'Ort'),
    def('link', 'url', 'Trello'),
    def('note', 'text', 'Notiz'),
  ];
  const props = {
    status: '11-20',
    kind: ['Cloud-TK-Anlage'],
    owner: 'Jonas Czwalina',
    team: 'Jonas Czwalina',
    size: 55,
    followup: '2026-03-12',
    created: '2025-12-01',
    mail: 'info@example.de',
    phone: '+49 6202 93560',
    city: '68723 Plankstadt',
    link: 'https://trello.com/c/x',
    note: 'Frau Kruse bittet um Rueckruf',
  };
  const ids = (list) => list.map((d) => d.id).join(',');
  const plan = cl.planCard(defs, (d) => ({ def: d, value: props[d.id] }));
  check('chips', ids(plan.chips), 'status,kind');
  check('people', ids(plan.people), 'owner,team');
  check('facts', ids(plan.facts), 'size,followup,created');
  check('one note', ids(plan.notes), 'note');
  // Four lines of mail, phone, address and link become four icons.
  check('contacts', ids(plan.contacts), 'mail,phone,city,link');
  check('nothing left over', plan.overflow.length, 0);

  // ---- The guard, and what it must never do ----
  // Cards MAY differ in height — the guard is only there so a schema with
  // thirty fields does not become a wall. What it holds back is COUNTED, not
  // dropped: that is what makes "+n" answerable when one of them matters.
  const many = Array.from({ length: cl.CARD_FACT_LIMIT + 4 }, (_, i) => def('n' + i, 'number', 'Zahl ' + i));
  const manyProps = Object.fromEntries(many.map((d, i) => [d.id, i + 1]));
  const big = cl.planCard(many, (d) => ({ def: d, value: manyProps[d.id] }));
  check('the guard caps the printed facts', big.facts.length, cl.CARD_FACT_LIMIT);
  check('the rest is counted, not dropped', big.facts.length + big.overflow.length, many.length);

  const twoNotes = [def('a', 'text', 'A'), def('b', 'text', 'B')];
  const notePlan = cl.planCard(twoNotes, (d) => ({
    def: d,
    value: { a: 'Ein laengerer Hinweis', b: 'Noch ein Hinweis' }[d.id],
  }));
  check('one note is printed', ids(notePlan.notes), 'a');
  check('the second note is counted', ids(notePlan.overflow), 'b');

  let fail = 0;
  for (const c of out) {
    if (c.got !== c.want) {
      fail++;
      console.log(`  FAIL ${c.name}: got ${JSON.stringify(c.got)}, want ${JSON.stringify(c.want)}`);
    }
  }
  console.log(`\n  card layout: ${out.length - fail} passed, ${fail} failed`);
  process.exit(fail ? 1 : 0);
} finally {
  rmSync(tmp, { recursive: true, force: true });
}
