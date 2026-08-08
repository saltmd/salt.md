// Fills a throwaway instance with the invented content the wiki screenshots show.
//
//   SALT_DATA=/tmp/salt-shots SALT_ADDR=:8421 go run .      # in another shell
//   node scripts/seed-wiki-instance.mjs                     # then this
//   node scripts/shoot-wiki.mjs                             # then the pictures
//
// Without this the screenshot manifest is only half a mechanism. The recipes say
// which buttons to press, and said nothing about what should be on the screen
// when they are pressed — so "retake that shot" was a thing only the person who
// happened to have the right instance lying around could do, and the pictures
// would drift apart the first time somebody else tried.
//
// Everything here is invented, and has to be. This content is photographed and
// published on a public website; the first draft of the wiki named three real
// customers in a diagram, and it took a person to catch it. Acme, Northwind and
// Ada Lovelace are not anybody.
//
// NEVER point this at an instance with real content: it creates the first
// account and writes pages. It refuses an instance that already has one.

const BASE = process.env.SALT_SEED_URL ?? 'http://127.0.0.1:8421';
const EMAIL = process.env.SALT_SEED_EMAIL ?? 'ada@example.com';
const PASSWORD = process.env.SALT_SEED_PASSWORD ?? 'WikiPruefung2026!';

let cookie = '';

async function call(method, path, body) {
  const res = await fetch(BASE + path, {
    method,
    headers: { 'Content-Type': 'application/json', ...(cookie ? { cookie } : {}) },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const set = res.headers.get('set-cookie');
  if (set) cookie = set.split(';')[0];
  const text = await res.text();
  if (!res.ok) throw new Error(`${method} ${path} → ${res.status} ${text.slice(0, 160)}`);
  return text.trim().startsWith('{') || text.trim().startsWith('[') ? JSON.parse(text) : {};
}

const me = await call('GET', '/api/me');
if (!me.setupRequired) {
  console.error(`\n  ${BASE} already has an account — seed a FRESH instance, never one with real content.\n`);
  process.exit(1);
}

await call('POST', '/api/setup', { name: 'Ada Lovelace', email: EMAIL, password: PASSWORD });
await call('POST', '/api/login', { email: EMAIL, password: PASSWORD });

// English, and a fixed region and zone: a screenshot whose dates read 08/20/2026
// in one run and 20.08.2026 in the next is a screenshot that changes for no
// reason, which makes its staleness check meaningless.
await call('PUT', '/api/me/prefs', {
  language: 'en',
  region: 'en-GB',
  timeZone: 'Europe/Berlin',
  clock: '24',
  weekStart: 'monday',
});

const ws = (await call('GET', '/api/workspaces'))[0].id;
await call('PATCH', `/api/workspaces/${ws}`, { name: 'Northwind Product' });

const page = (title, parentId = null, type = 'doc', props) =>
  call('POST', '/api/pages', { parentId, title, type, workspaceId: ws, props });

const handbook = await page('Team handbook');
for (const t of ['How we write', 'Onboarding', 'Release checklist', 'Meeting notes']) {
  await page(t, handbook.id);
}
await page('Product roadmap');
await page('Design principles');

// A second collection to relate to, so the relation property has somewhere to
// point and the wiki can photograph one that is actually filled in.
const systems = (await page('Systems', null, 'collection')).id;
await call('PUT', `/api/collections/${systems}`, {
  schema: [
    {
      id: 'kind',
      name: 'Kind',
      type: 'select',
      options: [
        { id: 'service', name: 'Service', color: '#2f7d4f' },
        { id: 'library', name: 'Library', color: '#3b6ea5' },
      ],
    },
  ],
  views: [{ id: 'table', name: 'Table', type: 'table' }],
});
const sysRows = [];
for (const n of ['Billing service', 'Search index', 'Design system']) {
  sysRows.push((await page(n, systems, 'doc', { kind: 'service' })).id);
}

// The main collection carries one property of nearly every type, because
// properties.md needs a picture where each of them can be seen doing its job.
const tasks = (await page('Tasks', null, 'collection')).id;
await call('PUT', `/api/collections/${tasks}`, {
  schema: [
    { id: 'status', name: 'Status', type: 'select', options: [
      { id: 'todo', name: 'To do', color: '#c4554d' },
      { id: 'doing', name: 'In progress', color: '#b58a3b' },
      { id: 'done', name: 'Done', color: '#2f7d4f' },
    ] },
    { id: 'area', name: 'Area', type: 'multiselect', options: [
      { id: 'ui', name: 'Interface', color: '#3b6ea5' },
      { id: 'api', name: 'API', color: '#8a4fa8' },
      { id: 'docs', name: 'Docs', color: '#b58a3b' },
    ] },
    { id: 'owner', name: 'Owner', type: 'person' },
    { id: 'due', name: 'Due', type: 'date' },
    { id: 'effort', name: 'Effort (days)', type: 'number' },
    { id: 'blocked', name: 'Blocked', type: 'checkbox' },
    { id: 'link', name: 'Ticket', type: 'url' },
    { id: 'steps', name: 'Steps', type: 'checklist' },
    { id: 'system', name: 'System', type: 'relation', relationCollection: systems },
  ],
  views: [
    { id: 'board', name: 'Board', type: 'board', groupBy: 'status' },
    { id: 'table', name: 'Table', type: 'table' },
    { id: 'cal', name: 'Calendar', type: 'calendar', dateProp: 'due' },
  ],
});

// Fixed dates, not "today + n": a screenshot taken next month must look like the
// one taken today, or the staleness check reports differences nobody caused.
const rows = [
  ['Rewrite the onboarding flow', 'doing', ['ui'], '2026-08-20', 3, false, 0],
  ['Index PDF attachments', 'done', ['api'], '2026-08-12', 5, false, 1],
  ['Document the webhook payload', 'todo', ['docs', 'api'], '2026-08-29', 1, false, -1],
  ['Board cards lose their colour', 'todo', ['ui'], '2026-08-15', 2, true, 2],
  ['Retire the old export path', 'todo', ['api'], '2026-09-04', 2, false, -1],
  ['Translate the settings dialog', 'doing', ['ui', 'docs'], '2026-08-22', 4, false, -1],
];
for (const [title, status, area, due, effort, blocked, sys] of rows) {
  await page(title, tasks, 'doc', {
    status, area, due, effort, blocked,
    link: 'https://example.com/tickets/' + title.split(' ')[0].toLowerCase(),
    system: sys >= 0 ? [sysRows[sys]] : [],
  });
}

const pages = await call('GET', '/api/pages');
console.log(`\n  seeded ${BASE}: ${pages.length} pages, 2 collections, ${rows.length} rows`);
console.log(`  now: node scripts/shoot-wiki.mjs\n`);
