// Checks the keyboard registry's resolution rules (src/keys.ts).
//
//   node scripts/check-keys.mjs
//
// No test framework: esbuild is already here as part of vite, and the parts of
// keys.ts worth checking are pure — a chord in, a decision out. The module is
// imported twice, once with a Mac navigator and once with a Windows one,
// because HALF of what this file guards is the difference between the two.
//
// Why it exists: the shortcuts this replaced were three hand-written listeners
// that each re-decided what ⌘ means, what counts as "typing", and who wins.
// They disagreed — ⌥N fired while a page title was being typed, which on a Mac
// keyboard is the dead key for ˜ — and nothing anywhere could have noticed.
// These assertions are that noticing.

import { execFileSync } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const web = join(here, '..');

const out = [];
const check = (name, got, want) => out.push({ name, got: String(got), want: String(want), ok: String(got) === String(want) });

/** Pretend to be a platform before importing the module. keys.ts reads
 *  navigator lazily on every call precisely so this is possible — and so a
 *  browser that reports its platform late cannot freeze the wrong answer. */
function pretendPlatform(platform) {
  Object.defineProperty(globalThis, 'navigator', {
    value: { platform, userAgent: platform },
    configurable: true,
    writable: true,
  });
}

const tmp = mkdtempSync(join(tmpdir(), 'salt-keys-'));
const bundle = join(tmp, 'keys.mjs');
try {
  try {
    execFileSync(
      join(web, 'node_modules/esbuild/bin/esbuild'),
      [join(web, 'src/keys.ts'), '--bundle', '--format=esm', '--outfile=' + bundle],
      { stdio: ['ignore', 'ignore', 'pipe'] },
    );
  } catch (e) {
    console.error(String(e.stderr ?? e));
    process.exit(1);
  }

  pretendPlatform('MacIntel');
  const keys = await import(bundle);
  const { chordOf, normalize, isTypingTarget, register, resolve } = keys;

  // ---- the key itself comes from the physical position ----
  //
  // THE assertion this section exists for: a German keyboard swaps Z and Y, so
  // reading e.key would move undo to a different finger there — and an AZERTY
  // layout needs Shift for the digits, so a shortcut on "1" would need Shift
  // too. e.code says where the key sits, which is what a shortcut means.
  check('layout does not move a letter', chordOf({ code: 'KeyZ', key: 'y', metaKey: true }), 'meta+z');
  check('layout does not move a digit', chordOf({ code: 'Digit1', key: '&', ctrlKey: true }), 'ctrl+1');
  check('named keys come from e.key', chordOf({ key: 'Enter', ctrlKey: true }), 'ctrl+enter');
  check('the space bar has a name', chordOf({ key: ' ' }), 'space');
  check('arrows keep their name', chordOf({ key: 'ArrowRight', ctrlKey: true, altKey: true }), 'ctrl+alt+arrowright');

  // ---- modifiers are ordered, never spelled twice ----
  check('written order does not matter', normalize('shift+alt+K'), normalize('alt+shift+k'));
  check('canonical order', chordOf({ code: 'KeyK', ctrlKey: true, altKey: true, shiftKey: true, metaKey: true }), 'ctrl+meta+alt+shift+k');

  // ---- `mod` is ⌘ here, Ctrl there ----
  check('mod is Command on a Mac', normalize('mod+k'), 'meta+k');
  check('ctrl stays Control on a Mac', normalize('ctrl+k'), 'ctrl+k');
  // The tab-cycling shortcut depends on this: ⌘⌥← is the browser's own tab
  // switch on macOS, so a literal `ctrl` must NOT match a pressed ⌘.
  check('Command does not answer for Control', normalize('ctrl+alt+arrowleft') === chordOf({ key: 'ArrowLeft', metaKey: true, altKey: true }), 'false');

  // ---- what counts as typing ----
  //
  // A checkbox is an INPUT, and the guard this replaced tested the tag name
  // alone — it would have blocked shortcuts on the very element the toggle is
  // for. Nothing is typed into a checkbox.
  check('text input is typing', isTypingTarget({ tagName: 'INPUT', type: 'text' }), 'true');
  check('textarea is typing', isTypingTarget({ tagName: 'TEXTAREA' }), 'true');
  check('contenteditable is typing', isTypingTarget({ isContentEditable: true, tagName: 'DIV' }), 'true');
  check('checkbox is NOT typing', isTypingTarget({ tagName: 'INPUT', type: 'checkbox' }), 'false');
  check('radio is NOT typing', isTypingTarget({ tagName: 'INPUT', type: 'radio' }), 'false');
  check('button is NOT typing', isTypingTarget({ tagName: 'BUTTON' }), 'false');
  check('nothing focused is NOT typing', isTypingTarget(null), 'false');

  // ---- who wins ----
  const fired = [];
  const run = (id) => () => fired.push(id);

  // The modal registers FIRST here, on purpose. Registered last it would win on
  // recency alone, and this assertion would pass with the scope ranking gone —
  // which is exactly what happened the first time it was written.
  const offModal = register({ id: 'm', keys: ['mod+e'], scope: 'modal', run: run('m') });
  const offGlobal = register({ id: 'g', keys: ['mod+e'], run: run('g') });
  check('a modal outranks global whenever it registered', resolve(normalize('mod+e'), false)?.id, 'm');
  offModal();
  check('and hands it back when it closes', resolve(normalize('mod+e'), false)?.id, 'g');

  const offView1 = register({ id: 'v1', keys: ['mod+e'], scope: 'view', run: run('v1') });
  const offView2 = register({ id: 'v2', keys: ['mod+e'], scope: 'view', run: run('v2') });
  check('the latest registration shadows the earlier', resolve(normalize('mod+e'), false)?.id, 'v2');
  offView2();
  check('unregistering reveals the one underneath', resolve(normalize('mod+e'), false)?.id, 'v1');
  offView1();
  offGlobal();
  check('nothing is left behind', resolve(normalize('mod+e'), false), 'null');

  // ---- the typing guard, which is the whole point ----
  const offQuiet = register({ id: 'quiet', keys: ['alt+n'], run: run('quiet') });
  check('silent while typing by default', resolve(normalize('alt+n'), true), 'null');
  check('and live otherwise', resolve(normalize('alt+n'), false)?.id, 'quiet');
  offQuiet();

  const offLoud = register({ id: 'loud', keys: ['mod+k'], whileTyping: true, run: run('loud') });
  check('whileTyping opts back in', resolve(normalize('mod+k'), true)?.id, 'loud');
  offLoud();

  // ---- when() ----
  let armed = false;
  const offWhen = register({ id: 'w', keys: ['mod+enter'], when: () => armed, run: run('w') });
  check('a disarmed shortcut does not answer', resolve(normalize('mod+enter'), false), 'null');
  armed = true;
  check('an armed one does', resolve(normalize('mod+enter'), false)?.id, 'w');
  offWhen();

  // ---- the same module, on Windows ----
  pretendPlatform('Win32');
  check('mod is Ctrl off Apple', normalize('mod+k'), 'ctrl+k');
  check('and Ctrl is still Ctrl', normalize('ctrl+k'), 'ctrl+k');
  // Consequence worth stating: `mod+k` and `ctrl+k` are the SAME shortcut here
  // and two different ones on a Mac. Registering both is how ⌘K and Ctrl+K can
  // both open the search without inventing a second entry off Apple.
  check('mod and ctrl collapse off Apple', normalize('mod+k') === normalize('ctrl+k'), 'true');
} finally {
  rmSync(tmp, { recursive: true, force: true });
}

const bad = out.filter((c) => !c.ok);
for (const c of out) if (!c.ok) console.log(`    FAIL ${c.name}: expected ${c.want}, got ${c.got}`);
console.log(`  ${bad.length === 0 ? 'ok  ' : 'FAIL'} keyboard registry ${out.length - bad.length}/${out.length}`);
if (bad.length) process.exit(1);
