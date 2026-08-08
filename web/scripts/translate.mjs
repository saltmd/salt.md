// Adds a language, or tops up an existing one.
//
//   node scripts/translate.mjs --list        # which languages exist, how complete
//   node scripts/translate.mjs fr            # write or extend src/locales/fr.json
//   node scripts/translate.mjs fr --dry      # show what is missing, write nothing
//
// The rules that make this safe to run at any time:
//
//   Only missing keys are touched. A line somebody corrected by hand is never
//   overwritten, so re-running this on a language a native speaker has been
//   through does not undo their work.
//
//   Machine-written entries are recorded in <locale>.machine.json, so it stays
//   answerable which lines nobody has read yet. Correct an entry, drop its key
//   from that list, and it counts as human from then on.
//
//   Plural keys get one form per category the target language actually uses.
//   Intl.PluralRules decides which those are — Polish needs three, Arabic six,
//   and no English-speaking author would guess either.
//
// Without an API key it writes nothing and prints the missing keys as JSON plus
// a ready-made prompt. That path matters more than the automatic one: somebody
// contributing Portuguese should not need an account anywhere.

import { execFileSync } from 'node:child_process';
import { existsSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const localeDir = join(here, '../src/locales');
const i18nFile = join(here, '../src/i18n.ts');

/** Every string the app uses, asked of check-i18n.mjs rather than re-derived —
 *  two scripts with two ideas of what counts as a key would drift apart, and
 *  the drift would show up as mystery gaps in a catalog. */
function keysInUse() {
  const out = execFileSync(
    process.execPath,
    [join(here, 'check-i18n.mjs'), '--missing', '__none__'],
    { encoding: 'utf8' },
  );
  return Object.keys(JSON.parse(out));
}

function machineList(loc) {
  const p = join(localeDir, `${loc}.machine.json`);
  return existsSync(p) ? JSON.parse(readFileSync(p, 'utf8')) : [];
}

/** Which plural categories this language distinguishes. Asked, not assumed. */
function pluralCategories(loc) {
  const pr = new Intl.PluralRules(loc);
  const seen = new Set();
  for (const n of [0, 1, 2, 3, 5, 11, 21, 100, 101, 1.5]) seen.add(pr.select(n));
  return [...seen];
}

/** Plural keys are the ones holding {n} — they came from plural(). */
const isPluralKey = (k) => /\{n\}/.test(k);

function promptFor(loc, cats) {
  let name = loc;
  try {
    name = new Intl.DisplayNames(['en'], { type: 'language' }).of(loc) ?? loc;
  } catch {
    /* unknown tag — the code itself is instruction enough */
  }
  return [
    `Translate the values of this JSON into ${name}. Keep every key exactly as it is.`,
    `Placeholders such as {name} and {n} must survive unchanged.`,
    `This is the interface of a note-taking app: keep it short and plain, the way software speaks.`,
    `Keys containing {n} are plural forms — give an object with these categories: ${cats.join(', ')}.`,
    `Answer with JSON and nothing else.`,
    '',
  ].join('\n');
}

async function translateBatch(keys, loc, cats) {
  const res = await fetch('https://api.anthropic.com/v1/messages', {
    method: 'POST',
    headers: {
      'content-type': 'application/json',
      'x-api-key': process.env.ANTHROPIC_API_KEY,
      'anthropic-version': '2023-06-01',
    },
    body: JSON.stringify({
      model: process.env.SALT_TRANSLATE_MODEL || 'claude-sonnet-5',
      max_tokens: 16384,
      messages: [
        {
          role: 'user',
          content:
            promptFor(loc, cats) +
            JSON.stringify(Object.fromEntries(keys.map((k) => [k, ''])), null, 2),
        },
      ],
    }),
  });
  if (!res.ok) {
    console.error(`Translation request failed: ${res.status}\n${await res.text()}`);
    process.exit(1);
  }
  const data = await res.json();
  const text = data.content.map((c) => c.text ?? '').join('');
  const json = text.slice(text.indexOf('{'), text.lastIndexOf('}') + 1);
  return JSON.parse(json);
}

// ---- main ----

const argv = process.argv.slice(2);
const target = argv.find((a) => !a.startsWith('-'));
const dry = argv.includes('--dry');
const keys = keysInUse();

if (!target || argv.includes('--list')) {
  console.log(`${keys.length} strings in use.\n`);
  const files = readdirSync(localeDir).filter((n) => n.endsWith('.json') && !n.endsWith('.machine.json'));
  for (const f of files) {
    const loc = f.replace(/\.json$/, '');
    const cat = JSON.parse(readFileSync(join(localeDir, f), 'utf8'));
    const done = keys.filter((k) => k in cat).length;
    const unread = machineList(loc).length;
    console.log(
      `  ${loc.padEnd(6)} ${String(Math.round((done / keys.length) * 100)).padStart(3)}%  ${done}/${keys.length}` +
        (unread ? `  (${unread} machine-written, unreviewed)` : ''),
    );
  }
  console.log('\n  node scripts/translate.mjs <locale>   to add or top up a language');
  process.exit(0);
}

const path = join(localeDir, `${target}.json`);
const existing = existsSync(path) ? JSON.parse(readFileSync(path, 'utf8')) : {};
const missing = keys.filter((k) => !(k in existing));
const cats = pluralCategories(target);

console.log(`${target}: ${Object.keys(existing).length} translated, ${missing.length} missing.`);
if (missing.length === 0) {
  console.log('Nothing to do.');
  process.exit(0);
}

if (dry || !process.env.ANTHROPIC_API_KEY) {
  if (!dry) {
    console.log('\nNo ANTHROPIC_API_KEY set — nothing written.');
    console.log('Hand the block below to a translator, or paste it into any chat with:\n');
    console.log(promptFor(target, cats));
  }
  console.log(JSON.stringify(Object.fromEntries(missing.map((k) => [k, ''])), null, 2));
  const n = missing.filter(isPluralKey).length;
  if (n) {
    console.log(`\n# ${n} of these are plural keys.`);
    console.log(`# ${target} needs these forms: ${cats.join(', ')} — give them as an object.`);
  }
  process.exit(0);
}

const translated = await translateBatch(missing, target, cats);
const merged = { ...existing, ...translated };
writeFileSync(
  path,
  JSON.stringify(Object.fromEntries(Object.keys(merged).sort().map((k) => [k, merged[k]])), null, 2) + '\n',
);

const unread = [...new Set([...machineList(target), ...Object.keys(translated)])].sort();
writeFileSync(join(localeDir, `${target}.machine.json`), JSON.stringify(unread, null, 2) + '\n');

console.log(`Wrote ${Object.keys(translated).length} entries to ${target}.json.`);
console.log(`${unread.length} entries are machine-written and unreviewed (${target}.machine.json).`);
if (!readFileSync(i18nFile, 'utf8').includes(`  ${target}:`)) {
  console.log(`\nOne step left: add "${target}" to LOCALES in src/i18n.ts so it appears in the picker.`);
}
