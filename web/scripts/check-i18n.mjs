// Guards the two rules that keep salt.md translatable, and reports how far the
// text conversion has got.
//
//   node scripts/check-i18n.mjs
//
// The point of this file is that neither rule survives on good intentions. The
// other German-first project this codebase learned from rotted exactly here:
// every new feature added German strings, nothing noticed, and translating
// became an endless chase after other people's commits. A check that fails the
// build is the only version of "we'll remember" that works.
//
// Both sections are enforced. Section 2 became enforceable the moment the last
// string was wrapped: from here on, a bare string or a stale catalog fails the
// build, so rot is mechanically impossible rather than merely discouraged.

import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const src = join(here, '../src');

// format.ts is the one place allowed to touch Intl and build Dates from
// strings; i18n.ts drives it. Everything else asks them.
const FORMAT_OWNERS = new Set(['format.ts', 'i18n.ts']);

// Generated icon tables — data, not interface.
const SKIP = new Set(['mdiSet.ts', 'lucideSet.ts', 'emojiData.ts']);

function walk(dir) {
  const out = [];
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) out.push(...walk(p));
    else if (/\.tsx?$/.test(p) && !SKIP.has(name)) out.push(p);
  }
  return out;
}

const files = walk(src);
const errors = [];
const warnings = [];

/** Text between the parentheses starting at `open`, respecting nesting. Null
 *  when the call runs past the end of the line. */
function balancedArg(code, open) {
  let depth = 0;
  for (let i = open; i < code.length; i++) {
    if (code[i] === '(') depth++;
    else if (code[i] === ')' && --depth === 0) return code.slice(open + 1, i);
  }
  return null;
}

/** Commas that separate arguments, ignoring those inside nested calls. */
function topLevelCommas(arg) {
  let depth = 0;
  let n = 0;
  for (const c of arg) {
    if (c === '(' || c === '[' || c === '{') depth++;
    else if (c === ')' || c === ']' || c === '}') depth--;
    else if (c === ',' && depth === 0) n++;
  }
  return n;
}


/** Blank out comments while leaving everything else — and every line break —
 *  exactly where it was, so line numbers still match.
 *
 *  This walks the text tracking string state instead of pattern-matching,
 *  because pattern-matching got it wrong in a way that mattered:
 *  `accept="image/*"` opened a comment that ran on for 868 characters and
 *  swallowed real JSX, so a bare "Cover" label sat on screen while the check
 *  reported the file clean. A scanner cannot make that mistake. */
function blankComments(text) {
  const out = text.split('');
  let i = 0;
  let quote = null; // ' " ` or null
  while (i < text.length) {
    const c = text[i];
    const d = text[i + 1];
    if (quote) {
      if (c === '\\') i += 2;
      else {
        if (c === quote) quote = null;
        i++;
      }
      continue;
    }
    if (c === "'" || c === '"' || c === '`') {
      quote = c;
      i++;
      continue;
    }
    if (c === '/' && d === '/') {
      while (i < text.length && text[i] !== '\n') out[i++] = ' ';
      continue;
    }
    if (c === '/' && d === '*') {
      const end = text.indexOf('*/', i + 2);
      const stop = end === -1 ? text.length : end + 2;
      for (; i < stop; i++) if (out[i] !== '\n') out[i] = ' ';
      continue;
    }
    i++;
  }
  return out.join('');
}

// ---- Section 1: formatting stays in one place (enforced) ----

for (const file of files) {
  const name = file.split('/').pop();
  if (FORMAT_OWNERS.has(name)) continue;
  const rel = relative(join(here, '..'), file);
  const lines = readFileSync(file, 'utf8').split('\n');

  lines.forEach((line, i) => {
    const at = `${rel}:${i + 1}`;
    // A line may opt out with `// i18n-ok: why` — for the handful of places
    // that compare IDs or timestamps rather than anything a human reads. The
    // reason is mandatory so the exemption has to be argued for, not just
    // taken.
    if (/\/\/\s*i18n-ok:\s*\S/.test(line)) return;
    const code = line.replace(/\/\/.*$/, '');
    if (/\.toLocale(String|DateString|TimeString)\s*\(/.test(code)) {
      errors.push(`${at}  toLocale* outside format.ts — use formatMoment/formatDay`);
    }
    if (/\bIntl\.\w+/.test(code)) {
      errors.push(`${at}  Intl.* outside format.ts — add a helper there instead`);
    }
    if (/['"][a-z]{2}-[A-Z]{2}['"]/.test(code) && !/lang=|locale/i.test(code)) {
      errors.push(`${at}  hardcoded locale tag — formatting follows the user's language`);
    }
    if (/\.localeCompare\s*\(/.test(code)) {
      errors.push(`${at}  localeCompare — use compare() so sorting follows the language`);
    }
    // `new Date(x)` with a single non-numeric argument parses a string, and
    // `new Date('2026-07-18')` lands on UTC midnight — the off-by-one-day bug
    // this whole module exists to prevent. Multi-argument and empty forms are
    // local by construction and fine.
    const at2 = code.indexOf('new Date(');
    if (at2 >= 0) {
      const arg = balancedArg(code, at2 + 'new Date'.length);
      if (arg !== null && arg.trim() !== '' && topLevelCommas(arg) === 0) {
        const a = arg.trim();
        const numeric = /^[\d.\s+*/-]+$/.test(a) || /Date\.now\(\)|getTime\(\)|\* ?864/.test(a);
        if (!numeric) {
          warnings.push(`${at}  new Date(${a.slice(0, 40)}) parses a string — formatDay() if it is a calendar date`);
        }
      }
    }
  });
}

// ---- Section 2: strings are wrapped, catalogs are current (enforced) ----

// Names that are the same in every language, because they belong to somebody
// else. Translating "Microsoft" or "nginx" would be wrong, not merely odd.
const BRANDS = new Set([
  'Google', 'Microsoft', 'Gmail', 'Outlook', 'Cloudflare', 'Caddy', 'nginx',
  'Notion', 'Markdown', 'GitHub', 'salt.md', 'salt.md', 'MCP', 'API', 'DB',
  'md', 'JSON', 'CSV', 'ICS', 'SMTP', 'OAuth', 'HTTPS', 'URL', 'PWA',
]);

/** True for example values, identifiers, commands and brand names — anything
 *  that is not prose and must therefore stay as it is.
 *
 *  Without this the check cries wolf about `smtp.example.com` and
 *  `00000000-0000-…`, and a check that cries wolf gets switched off. */
function isTechnical(s) {
  const v = s.trim();
  if (BRANDS.has(v)) return true;
  // A single token holding a dot, slash, at-sign or underscore: hostname, path,
  // email, env var. "Delete user" has a space and none of those.
  if (!/\s/.test(v) && /[./@_]/.test(v)) return true;
  // No lowercase word of three letters or more anywhere — nothing to translate.
  if (!/[a-z]{3}/.test(v)) return true;
  return false;
}

const CALL = /\bt\(\s*(['"`])((?:\\.|(?!\1)[^\\])*)\1/g;
const PLURAL = /\bplural\(\s*[^,]+,\s*(['"`])(?:\\.|(?!\1)[^\\])*\1\s*,\s*(['"`])((?:\\.|(?!\2)[^\\])*)\2/g;

const used = new Set();
let unwrapped = 0;
const unwrappedSample = [];
const allBare = [];

for (const file of files) {
  const rel = relative(join(here, '..'), file);
  const text = readFileSync(file, 'utf8');
  // Strip comments before collecting keys: i18n.ts documents t() with an
  // example, and an example is not a string the app ships.
  if (!FORMAT_OWNERS.has(file.split('/').pop())) {
    const bare = blankComments(text);
    for (const m of bare.matchAll(CALL)) used.add(m[2]);
    for (const m of bare.matchAll(PLURAL)) used.add(m[3]);
  }

  if (!file.endsWith('.tsx')) continue;

  // Scanned over the WHOLE file, not line by line. JSX puts text and its
  // closing tag on separate lines all the time:
  //
  //     <button>
  //       <ImageIcon /> Cover
  //     </button>
  //
  // A per-line scan never sees the `<` and reports the file as clean. It did
  // exactly that, and "Cover" sat untranslated on screen while the check said
  // zero. Comments are blanked rather than removed so line numbers survive.
  const blanked = blankComments(text).replace(/<code[^>]*>[\s\S]*?<\/code>/g, (m) =>
    m.replace(/[^\n]/g, ' '),
  );
  const lineOf = (idx) => blanked.slice(0, idx).split('\n').length;
  const rawLines = text.split('\n');
  const exempt = (idx) => /i18n-ok:\s*\S/.test(rawLines[lineOf(idx) - 1] ?? '');

  // Text between JSX tags, tolerating one line break on either side.
  const JSX_TEXT = /(^|[^=!<>-])>[ \t]*\r?\n?[ \t]*([^<>{}\n]*[A-Za-z][^<>{}\n]*?)[ \t]*\r?\n?[ \t]*</g;
  for (const m of blanked.matchAll(JSX_TEXT)) {
    const s2 = m[2].trim();
    if (s2.length < 3) continue;
    if (/^[\d\s.,:;|/·—–-]+$/.test(s2)) continue;
    if (/=>|&&|\|\||\?\?|===|!==|[[\]();:]/.test(s2)) continue;
    const before = blanked.slice(Math.max(0, m.index - 40), m.index + 1);
    if (/\b(Map|Set|Array|Promise|Record|Partial|Awaited|React|useState|useRef|useMemo)\s*$/.test(before)) continue;
    if (isTechnical(s2)) continue;
    if (exempt(m.index)) continue;
    unwrapped++;
    allBare.push(`${rel}:${lineOf(m.index)}  ${s2.slice(0, 70)}`);
    if (unwrappedSample.length < 8) unwrappedSample.push(`${rel}:${lineOf(m.index)}  ${s2.slice(0, 52)}`);
  }

  // Attributes a human reads.
  for (const m of blanked.matchAll(/\b(placeholder|title|aria-label|alt)=(["'])([^"']{2,})\2/g)) {
    if (isTechnical(m[3])) continue;
    if (exempt(m.index)) continue;
    unwrapped++;
    allBare.push(`${rel}:${lineOf(m.index)}  ${m[1]}="${m[3].slice(0, 55)}"`);
    if (unwrappedSample.length < 8) unwrappedSample.push(`${rel}:${lineOf(m.index)}  ${m[1]}="${m[3].slice(0, 40)}"`);
  }
}

// ---- Section 3: the source itself is English (enforced) ----
//
// Sections 1 and 2 watch what a USER reads. Nothing watched what a DEVELOPER
// reads, and it showed: 269 German comment lines were sitting in .ts, .tsx and
// .css while both sections reported clean. Comments carry the *why*, and unlike
// the interface they will never get a translation layer — a German comment
// stays German for good.
//
// It also catches what the JSX rule above cannot. That regex tolerates one line
// break and no `{}` or `<>` inside the text, so a German paragraph running over
// three lines with a `<code>` in the middle walked straight past it. Two did,
// and both were on screen: the index hint and the emergency-access dialog.
//
// The rule is the same crude one the Go side uses (server/language_test.go): an
// umlaut, or two words from a list with no English homographs. Plus the
// ae/oe/ue spellings, because much of the German here was written that way.
//
// The trailing boundary is `(?![\w])` and NOT `(?![\w-])`. A hyphen has to be
// allowed to follow, because German compounds are written with one — the first
// version of this rule read straight past "Nicht-Admins" and "Instanz-Setting"
// and reported types.ts as clean. A LEADING hyphen or word character still
// blocks the match, so `--border-color` and `.header-bar` stay quiet.
const GERMAN_WORDS =
  /(?<![\w-])(und|nicht|wird|werden|sind|eine|einen|einem|einer|kann|muss|soll|sollen|beim|des|dem|der|das|den|für|fuer|aus|nach|über|ueber|ohne|damit|weil|wenn|dann|noch|nur|schon|sich|hier|aber|oder|bitte|kein|keine|wurde|wurden|diese|dieser|dieses|jeder|jede|mehr|sehr|immer|wieder|zwischen|während|waehrend|deshalb|sonst|bereits|jetzt|etwas|nichts|alles|ihre|seine|unser|gibt|geben|machen|macht|lassen|bleibt|steht|liegt|dabei|darauf|dafür|dafuer|dadurch|daher|sowie|zum|zur|vom|ist|sein|haben|hoehe|höhe|breite|farbe|zeile|zeilen|spalte|spalten|duerfen|koennen|muessen|gehoert|eigene|eigenen|jeweils|nichts)(?![\w])/giu;
const UMLAUT = /[äöüßÄÖÜ]/;

function readsGerman(line) {
  if (/i18n-ok:\s*\S/.test(line)) return false;
  if (UMLAUT.test(line)) return true;
  return (line.match(GERMAN_WORDS) ?? []).length >= 2;
}

const germanLines = [];
{
  const stack = [src];
  while (stack.length) {
    const d = stack.pop();
    for (const name of readdirSync(d)) {
      const p = join(d, name);
      if (statSync(p).isDirectory()) {
        // locales/ IS the German — that is the entire point of it.
        if (name !== 'locales') stack.push(p);
      } else if (/\.(tsx?|css)$/.test(p) && !SKIP.has(name)) {
        const rel = relative(join(here, '..'), p);
        readFileSync(p, 'utf8')
          .split('\n')
          .forEach((line, i) => {
            if (readsGerman(line)) germanLines.push(`${rel}:${i + 1}  ${line.trim().slice(0, 72)}`);
          });
      }
    }
  }
}

// A STRING LITERAL is judged more harshly than a line: ONE unambiguous German
// word is enough. That is the same bargain the Go side struck in
// TestUserFacingStringsAreEnglish, and for the same reason — short interface
// text is exactly the shape the line rule cannot see. "Teilen fehlgeschlagen"
// carries no umlaut and only one word from the list above, so it read as clean
// while sitting in a toast for months. Four of these were found by hand on one
// day; this is what stops the fifth.
//
// The words here must not exist in English and must not appear in a class name,
// an id or a URL — anything ambiguous belongs in the line list above, not here.
// Verbs and participles — what a BUTTON says.
const GERMAN_STRONG =
  /(?<![\w-])(fehlgeschlagen|umbenennen|umbenannt|verschieben|verschoben|loeschen|löschen|geloescht|gelöscht|gespeichert|speichern|hochladen|hochgeladen|anlegen|angelegt|erstellen|erstellt|schliessen|schließen|abbrechen|hinzufuegen|hinzufügen|entfernen|entfernt|bearbeiten|suchen|senden|gesendet|teilen|geteilt|beitritt|importierter|einsammeln|referenzierte|auswaehlen|auswählen|verbinden|verbunden|zurueck|zurück|weiter|fertig|ungueltig|ungültig|vorhanden|erforderlich|aufklappen|zuklappen|einklappen|ausklappen|umbenannt|gespeicherte|geoeffnet|geöffnet|geschlossen|verworfen|uebernehmen|übernehmen|bestaetigen|bestätigen|wiederherstellen|zuruecksetzen|zurücksetzen)(?![\w])/iu;

// Nouns and adjectives — what a MENU ENTRY or a description says, and the class
// both other rules were blind to. "Hervorgehobener Hinweis mit Emoji" sat in
// the slash menu, on screen, through every pass: no umlaut, no verb from the
// list above, and a JSX rule cannot see a value in an object literal.
//
// Only words that are not also English and not plausible as an identifier or a
// file name — a false positive here fails somebody's build for no reason, and
// the answer to that is a smaller list, not a disabled check.
const GERMAN_NOUNS =
  /(?<![\w-])(hinweis|hinweise|ueberschrift|überschrift|inhaltsverzeichnis|einstellung|einstellungen|verzeichnis|datei|dateien|ordner|benutzer|kennwort|passwort|auswahl|vorlage|vorlagen|eintrag|eintraege|einträge|spalte|spalten|zeile|zeilen|tabelle|sammlung|sammlungen|arbeitsbereich|papierkorb|kommentar|kommentare|abschnitt|anhang|hervorgehoben|hervorgehobener|hervorgehobene|ungelesen|verfuegbar|verfügbar|nachricht|nachrichten|beschreibung|karte|karten)(?![\w])/iu;

/** A STRING reads as German if one unambiguous word is in it. One is enough
 *  here and not enough for a whole line, because interface text is short: "Nicht
 *  gefunden" has no umlaut and two words, and the line rule would never see it. */
const stringReadsGerman = (s) => GERMAN_STRONG.test(s) || GERMAN_NOUNS.test(s);

// Every quoted string in the source, JSX text included via the line rule above.
const germanStrings = [];
{
  const stack = [src];
  const STR = /(['"`])((?:\\.|(?!\1)[^\\])*?)\1/g;
  while (stack.length) {
    const d = stack.pop();
    for (const name of readdirSync(d)) {
      const p2 = join(d, name);
      if (statSync(p2).isDirectory()) {
        if (name !== 'locales') stack.push(p2);
      } else if (/\.tsx?$/.test(p2) && !SKIP.has(name)) {
        const rel = relative(join(here, '..'), p2);
        readFileSync(p2, 'utf8')
          .split('\n')
          .forEach((line, i) => {
            if (/i18n-ok:\s*\S/.test(line)) return;
            for (const m of line.matchAll(STR)) {
              const text = m[2];
              if (!/[A-Za-zÄÖÜäöüß]{3}/.test(text)) continue;
              if (stringReadsGerman(text)) {
                germanStrings.push(`${rel}:${i + 1}  ${text.slice(0, 60)}`);
                break;
              }
            }
          });
      }
    }
  }
}

// `--german` lists them grouped by file, so the sweep can be worked through one
// file at a time — the same shape as `--bare`.
if (process.argv.includes('--german')) {
  const byFile = new Map();
  for (const s of germanLines) {
    const f = s.slice(0, s.indexOf(':'));
    if (!byFile.has(f)) byFile.set(f, []);
    byFile.get(f).push(s);
  }
  for (const [f, list] of [...byFile.entries()].sort((a, b) => b[1].length - a[1].length)) {
    console.log(`\n${f}  (${list.length})`);
    for (const s of list) console.log('  ' + s);
  }
  console.log(`\n${germanLines.length} total`);
  process.exit(0);
}

// Catalogs: an entry nobody asks for any more is an orphan (usually the source
// text was edited); a source string with no entry is simply untranslated.
const localeDir = join(src, 'locales');
const report = [];
for (const name of readdirSync(localeDir).filter((f) => f.endsWith('.json'))) {
  const cat = JSON.parse(readFileSync(join(localeDir, name), 'utf8'));
  const keys = Object.keys(cat);
  const orphans = keys.filter((k) => !used.has(k));
  const missing = [...used].filter((k) => !(k in cat));
  report.push({ name, have: keys.length, orphans, missing: missing.length });
}

// `--bare` lists every unwrapped string with its location, grouped by file, so
// the conversion can be worked through file by file instead of guessed at.
// Searching for umlauts is not enough — plenty of German has none ("Senden",
// "Kommentare", "Liste").
if (process.argv.includes('--bare')) {
  const byFile = new Map();
  for (const s of allBare) {
    const f = s.slice(0, s.indexOf(':'));
    if (!byFile.has(f)) byFile.set(f, []);
    byFile.get(f).push(s);
  }
  for (const [f, list] of [...byFile.entries()].sort((a, b) => b[1].length - a[1].length)) {
    console.log(`\n${f}  (${list.length})`);
    for (const s of list) console.log('  ' + s);
  }
  console.log(`\n${allBare.length} total`);
  process.exit(0);
}

// `--missing de` prints the source strings that locale has no entry for, as a
// JSON object ready to be filled in or handed to a translator. This is the
// seed of the translation tool: a language is a file, and this is how the file
// gets its list of what to say.
const missingFor = process.argv.indexOf('--missing');
if (missingFor > 0) {
  const loc = process.argv[missingFor + 1];
  const path = join(localeDir, `${loc}.json`);
  // A locale that does not exist yet has nothing translated, so every key is
  // missing. That is how translate.mjs asks for the full list.
  const cat = existsSync(path) ? JSON.parse(readFileSync(path, 'utf8')) : {};
  const todo = [...used].filter((k) => !(k in cat)).sort();
  console.log(JSON.stringify(Object.fromEntries(todo.map((k) => [k, ''])), null, 2));
  process.exit(0);
}

// ---- output ----

console.log('  Formatting confined to format.ts');
if (errors.length === 0) console.log('    ok   no stray Intl, toLocale, locale tag or localeCompare');
for (const e of errors) console.log(`    FAIL ${e}`);
for (const w of warnings) console.log(`    warn ${w}`);

console.log();
console.log('  String conversion');
console.log(`    ${used.size} wrapped in t()/plural(), ~${unwrapped} still bare`);
for (const s of unwrappedSample) console.log(`      ${s}`);
if (unwrapped > unwrappedSample.length) console.log(`      … and ${unwrapped - unwrappedSample.length} more`);

console.log();
console.log('  Catalogs');
for (const r of report) {
  console.log(`    ${r.name}: ${r.have} entries, ${r.missing} untranslated, ${r.orphans.length} orphaned`);
  for (const o of r.orphans.slice(0, 5)) console.log(`      orphan: ${o.slice(0, 60)}`);
}

console.log();
console.log('  Source language');
if (germanLines.length === 0) console.log('    ok   no German in .ts, .tsx or .css');
else {
  console.log(`    ${germanLines.length} line(s) read as German`);
  for (const s of germanLines.slice(0, 8)) console.log(`      ${s}`);
  if (germanLines.length > 8) console.log(`      … and ${germanLines.length - 8} more`);
}
if (germanStrings.length === 0) console.log('    ok   no German inside a string literal');
else {
  console.log(`    ${germanStrings.length} string literal(s) read as German`);
  for (const s of germanStrings.slice(0, 12)) console.log(`      ${s}`);
  if (germanStrings.length > 12) console.log(`      … and ${germanStrings.length - 12} more`);
}

console.log();
const stale = report.filter((r) => r.missing > 0 || r.orphans.length > 0);
if (errors.length || unwrapped > 0 || stale.length || germanLines.length || germanStrings.length) {
  if (errors.length) console.log(`  FAILED — ${errors.length} formatting violation(s)`);
  if (unwrapped > 0)
    console.log(`  FAILED — ${unwrapped} user-visible string(s) not wrapped in t()`);
  for (const r of stale)
    console.log(`  FAILED — ${r.name}: ${r.missing} untranslated, ${r.orphans.length} orphaned`);
  if (germanLines.length)
    console.log(`  FAILED — ${germanLines.length} German line(s) in the source`);
  if (germanStrings.length)
    console.log(`  FAILED — ${germanStrings.length} German string literal(s) — one word is enough here`);
  console.log('\n  Fix, or justify the line with `i18n-ok: <reason>`.');
  console.log('  For a catalog: node scripts/check-i18n.mjs --missing <locale>');
  console.log('  For the German list: node scripts/check-i18n.mjs --german');
  process.exit(1);
}
console.log('  ok — every string wrapped, every catalog current, source is English');
