// Checks the wiki against the code (wiki/*.md).
//
//   node scripts/check-wiki.mjs
//
// Documentation is the one artefact nothing tests. It is written once, it reads
// plausibly forever, and it goes wrong the moment somebody renames a tool — at
// which point it is worse than nothing, because a reader trusts it.
//
// So the parts that CAN be checked mechanically are:
//
//   1. Every MCP tool the wiki names exists.
//   2. Every MCP tool that exists is documented somewhere.
//   3. Every /api/ path the wiki mentions is a real route.
//   4. Every property type and view type is covered.
//   5. Every relative link between wiki pages resolves.
//
// What it cannot check is whether a sentence is TRUE. Nothing can. But the
// class of error this catches — a name that quietly stopped existing — is the
// one that makes a reader stop trusting the rest.

import { readdirSync, readFileSync, existsSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';

const here = dirname(fileURLToPath(import.meta.url));
const repo = join(here, '../..');
const wikiDir = join(repo, 'wiki');

const files = readdirSync(wikiDir).filter((f) => f.endsWith('.md'));
const pages = new Map(files.map((f) => [f, readFileSync(join(wikiDir, f), 'utf8')]));
const all = [...pages.values()].join('\n');

// Words that look like a tool name but are prose or a shell command. Kept
// short and explicit: the point of the rule is to catch a tool that quietly
// stopped existing, and a long allow-list would defeat it.
const ALLOWED_NON_TOOLS = new Set(['list_pages', 'sha256sum', 'create_rows_per_row']);

const errors = [];
const notes = [];

// ---- 1 + 2: the MCP tool catalogue ----------------------------------------

const mcp = readFileSync(join(repo, 'server/mcp.go'), 'utf8');
const tools = new Set([...mcp.matchAll(/"name":\s*"([a-z_]+)"/g)].map((m) => m[1]));

// Tools the wiki names, as `backticked` words. Only words that look like tool
// names are considered, so ordinary prose in backticks is not mistaken for one.
const named = new Set();
for (const [file, text] of pages) {
  for (const m of text.matchAll(/`([a-z][a-z_]{2,})\s*(?:\(|`)/g)) {
    const word = m[1];
    if (tools.has(word)) named.add(word);
    else if (/^(get|set|create|update|delete|list|query|save|write|import|propose|duplicate|embed|upload|search|note|whoami|working)_?/.test(word)
             && !ALLOWED_NON_TOOLS.has(word)) {
      errors.push(`${file}: \`${word}\` looks like a tool and is not one`);
    }
  }
}
for (const t of tools) {
  if (!named.has(t)) errors.push(`no wiki page documents the tool \`${t}\``);
}

// ---- 3: /api/ paths --------------------------------------------------------

const server = readFileSync(join(repo, 'server/server.go'), 'utf8');
const routes = [...server.matchAll(/m\.HandleFunc\("(?:[A-Z]+ )?([^"]+)"/g)].map((m) => m[1]);
/** A route pattern matches a mentioned path when their segments line up,
 *  treating {placeholders} as wildcards. */
const routeMatches = (mentioned) => {
  const want = mentioned.replace(/^\/+/, '').split('/');
  return routes.some((r) => {
    const have = r.replace(/^\/+/, '').split('/');
    if (have.length !== want.length) return false;
    return have.every((seg, i) => seg.startsWith('{') || seg === want[i]);
  });
};
for (const [file, text] of pages) {
  for (const m of text.matchAll(/`(\/api\/[A-Za-z0-9_{}/<>-]*)`/g)) {
    const path = m[1].replace(/<[^>]+>/g, '{x}');
    // A bare `/api/` in prose names the surface, not an endpoint.
    if (path === '/api/' || path === '/api') continue;
    if (!routeMatches(path)) errors.push(`${file}: \`${m[1]}\` is not a route`);
  }
}

// ---- 4: every type is covered ---------------------------------------------

// [a-zA-Z], not [a-z]: a camelCase type (lastActivity) slipped straight past
// the lowercase pattern, so the one check meant to force a wiki section for
// every property type would have stayed silent about it.
const propTypes = new Set(
  [...readFileSync(join(repo, 'web/src/components/PropertyValue.tsx'), 'utf8')
    .matchAll(/case '([a-zA-Z]+)':/g)].map((m) => m[1]),
);
for (const t of propTypes) {
  if (!new RegExp('###\\s+' + t + '\\b').test(all)) {
    errors.push(`properties.md has no "### ${t}" section`);
  }
}

const viewTypes = ['table', 'board', 'list', 'gallery', 'calendar', 'form', 'timeline'];
const views = pages.get('views.md') ?? '';
for (const v of viewTypes) {
  if (!views.includes('`' + v + '`')) errors.push(`views.md never names \`${v}\``);
}

// ---- 5: links between wiki pages resolve -----------------------------------

for (const [file, text] of pages) {
  for (const m of text.matchAll(/\]\(([a-z-]+\.md)(#[a-z0-9-]+)?\)/g)) {
    if (!pages.has(m[1])) errors.push(`${file}: links to ${m[1]}, which does not exist`);
  }
  if (file !== 'README.md' && !(pages.get('README.md') ?? '').includes(file)) {
    notes.push(`${file} is not listed in README.md`);
  }
}

// ---- 6: nothing real in the examples --------------------------------------
//
// This wiki goes on a public website. Examples written while looking at a live
// instance carry real customer names, real hostnames and real addresses
// straight into it — which is how a customer list gets published by somebody
// being helpful. It happened: a tree diagram named three of the owner's
// customers, and it took a human reading it to notice.
//
// A rule cannot recognise a company name, so it cannot catch that class. What
// it CAN pin down is every identifier that is unambiguously real: an address, a
// hostname, an email. Those are the ones that get copied without thinking.
//
// The name class is covered by a written rule instead — see README.md — and by
// the fact that examples here have to be obviously invented.

// Subdomains of the documentation domains count too — `salt.example.com` is
// exactly the invented example this rule wants people to use.
const ALLOWED_HOSTS = /^(([a-z0-9-]+\.)*example\.(com|org|net)|localhost|github\.com|raw\.githubusercontent\.com|ghcr\.io|mermaid\.js\.org|your-instance)$/;
// Reserved for documentation (RFC 5737) plus the loopback address.
// 169.254.169.254 is the cloud metadata endpoint — a well-known constant that
// the webhook page names precisely because deliveries must never reach it. It
// belongs to nobody.
const ALLOWED_IPS = /^(127\.0\.0\.1|0\.0\.0\.0|169\.254\.169\.254|192\.0\.2\.\d+|198\.51\.100\.\d+|203\.0\.113\.\d+)$/;

for (const [file, text] of pages) {
  for (const m of text.matchAll(/\b(\d{1,3}(?:\.\d{1,3}){3})\b/g)) {
    if (!ALLOWED_IPS.test(m[1])) {
      errors.push(`${file}: ${m[1]} is a real-looking IP address — use 192.0.2.x`);
    }
  }
  for (const m of text.matchAll(/[A-Za-z0-9._%+-]+@([A-Za-z0-9.-]+\.[A-Za-z]{2,})/g)) {
    if (!ALLOWED_HOSTS.test(m[1])) {
      errors.push(`${file}: an email at ${m[1]} — use example.com`);
    }
  }
  for (const m of text.matchAll(/https?:\/\/([A-Za-z0-9.-]+\.[A-Za-z]{2,})/g)) {
    const host = m[1].replace(/^www\./, '');
    if (!ALLOWED_HOSTS.test(host) && !host.endsWith('salt.md') && host !== 'saltmd.github.io') {
      errors.push(`${file}: links to ${host} — is that ours, or somebody's real instance?`);
    }
  }
}

// ---- 7: coverage, as a NOTE rather than a gate ----------------------------
//
// "Does the wiki cover the whole system?" is answerable, roughly: every family
// of routes the server exposes should be findable somewhere in the wiki.
//
// Roughly, because the match is by word and documentation says "two-factor"
// where a route says "2fa". A fuzzy rule must not fail a build — a check that
// cries wolf gets switched off, and this one is worth keeping. So it reports.
//
// It answered a real question once: asked whether the wiki covered everything,
// this printed thirteen unmentioned families in one second, of which three
// (favourites, the audit log, the health endpoint) were genuinely missing.

const SAID_DIFFERENTLY = {
  '2fa': 'two-factor', 'comment-counts': 'comment', 'import-zip': 'archive',
  library: 'blueprint', logout: 'sign', oauth: 'sign in with',
  'reindex-siblings': 'rebuild', signup: 'register', 'signup-policy': 'register',
  'tag-colors': 'colour', me: 'account', 'public-base': 'public base',
  presence: 'working_on', ics: 'calendar', favorites: 'favourite',
};
const lower = all.toLowerCase();
const families = [...new Set(
  [...server.matchAll(/m\.HandleFunc\("(?:[A-Z]+ )?(\/api\/[^"{]*)/g)]
    .map((m) => m[1].split('/')[2]).filter(Boolean),
)].sort();
const uncovered = families.filter((f) =>
  !lower.includes(f) && !lower.includes(f.replace(/-/g, ' ')) &&
  !lower.includes(SAID_DIFFERENTLY[f] ?? '\u0000'));
if (uncovered.length) {
  notes.push(`${uncovered.length} of ${families.length} route families are never mentioned: ${uncovered.join(', ')}`);
} else {
  notes.push(`all ${families.length} route families are mentioned somewhere`);
}

// ---- 8: screenshots, and knowing when one has started lying ---------------
//
// A picture is a claim, and it is the claim nobody re-reads. It cannot be
// checked for truth — but it CAN be tied to the code it shows, and then the one
// question that matters is answerable mechanically: has that code changed since
// the picture was taken?
//
// So wiki/screenshots.json records, per shot, which source files it shows and
// the commit it was taken at. A page references a shot by its id and nothing
// else, which is what makes swapping one a matter of replacing a file.
//
// Retake with: node scripts/shoot-wiki.mjs <id>

const manifestPath = join(repo, 'wiki/screenshots.json');
let manifest = null;
try {
  manifest = JSON.parse(readFileSync(manifestPath, 'utf8'));
} catch {
  notes.push('wiki/screenshots.json is missing or unreadable — no screenshots checked');
}

if (manifest) {
  const byId = new Map(manifest.shots.map((s) => [s.id, s]));

  // Referenced but not described, and described but never referenced. Both are
  // the same failure as an undocumented tool: a name with nothing behind it.
  const referenced = new Set();
  for (const [file, text] of pages) {
    for (const m of text.matchAll(/!\[[^\]]*\]\(img\/([a-z0-9-]+)\.png\)/g)) {
      const id = m[1];
      referenced.add(id);
      const shot = byId.get(id);
      if (!shot) errors.push(`${file}: shows img/${id}.png, which screenshots.json does not describe`);
      else if (shot.page !== file) errors.push(`${file}: shows ${id}, which screenshots.json files under ${shot.page}`);
    }
  }

  for (const s of manifest.shots) {
    if (!pages.has(s.page)) errors.push(`screenshots.json: ${s.id} names ${s.page}, which is not a wiki page`);
    for (const suffix of ['', '-dark']) {
      if (!existsSync(join(repo, `wiki/img/${s.id}${suffix}.png`)))
        errors.push(`screenshots.json: ${s.id} has no ${suffix ? 'dark' : 'light'} image — run scripts/shoot-wiki.mjs ${s.id}`);
    }
    for (const f of s.shows ?? []) {
      if (!existsSync(join(repo, f))) errors.push(`screenshots.json: ${s.id} says it shows ${f}, which does not exist`);
    }
    if (!s.takenAt || typeof s.takenAt !== 'object')
      errors.push(`screenshots.json: ${s.id} was never taken — run scripts/shoot-wiki.mjs ${s.id}`);
  }

  const unused = manifest.shots.filter((s) => !referenced.has(s.id)).map((s) => s.id);
  if (unused.length) notes.push(`${unused.length} screenshot(s) taken but never shown: ${unused.join(', ')}`);

  // The staleness question, answered from CONTENT rather than from history.
  //
  // A commit stamp was the first design and it was wrong in the ordinary case:
  // change a component, retake the picture, then commit — and the commit that
  // changed the component is newer than the stamp, so a freshly taken picture
  // reports itself stale. Hashing what the shot SHOWS cannot be wrong about
  // that, fires the moment the file is edited rather than a commit later, and
  // needs no git — which matters, because the Docker image builds from copied
  // source with no repository in it.
  const stale = [];
  for (const s of manifest.shots) {
    const stamped = s.takenAt;
    if (!stamped || typeof stamped !== 'object') continue; // already reported above
    for (const [file, was] of Object.entries(stamped)) {
      let now = 'missing';
      try {
        now = createHash('sha256').update(readFileSync(join(repo, file))).digest('hex').slice(0, 12);
      } catch { /* the file is gone; reported separately */ }
      if (now !== was) {
        stale.push({ id: s.id, file });
        break;
      }
    }
  }
  if (stale.length) {
    errors.push(
      `${stale.length} screenshot(s) show code that has changed since they were taken:\n` +
      stale.map((x) => `          ${x.id} — ${x.file}`).join('\n') +
      `\n        retake them: node scripts/shoot-wiki.mjs ${stale.map((x) => x.id).join(' ')}`,
    );
  } else if (manifest.shots.length) {
    notes.push(`all ${manifest.shots.length} screenshots are current`);
  }
}

// ---- report ----------------------------------------------------------------

console.log(`\n  wiki: ${files.length} pages, ${tools.size} tools, ${routes.length} routes`);
for (const n of notes) console.log(`  note  ${n}`);
for (const e of errors) console.log(`  FAIL  ${e}`);
if (errors.length) {
  console.log(`\n  FAILED — the wiki disagrees with the code in ${errors.length} place(s).\n`);
  process.exit(1);
}
console.log('  ok — every tool, route and type in the wiki still exists\n');
