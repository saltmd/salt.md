// Checks the focus-navigation rules (src/nav.ts).
//
//   node scripts/check-nav.mjs
//
// Same shape as check-keys.mjs, and for the same reason: the decisions worth
// guarding are pure. Where do I land from here, who inherits the focus when the
// row under it is deleted, and is the caret allowed to leave this field — three
// questions that take a list and a number and answer without a browser.
//
// Why it exists: the arrow keys are the one feature where a wrong answer is
// invisible in a screenshot and obvious in the hand. The two that would cost the
// most are the ones nothing else would catch:
//
//   - the edge. A list that wraps from the last row back to the first takes away
//     the only thing a keyboard gives you, which is knowing where you are
//     without looking.
//   - the heir. Delete the focused row without handing the focus on and it drops
//     to <body>, where no arrow does anything and the way back in is the mouse.

import { execFileSync } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const web = join(here, '..');

const out = [];
const check = (name, got, want) =>
  out.push({ name, got: String(got), want: String(want), ok: String(got) === String(want) });

const tmp = mkdtempSync(join(tmpdir(), 'salt-nav-'));
const bundle = join(tmp, 'nav.mjs');
try {
  // nav.ts imports React and i18n; bundling reaches them the same way vite does.
  // A `navigator` has to exist first — keys.ts reads it lazily, but the bundle
  // evaluates i18n on import.
  Object.defineProperty(globalThis, 'navigator', {
    value: { platform: 'MacIntel', userAgent: 'MacIntel' },
    configurable: true,
    writable: true,
  });
  try {
    execFileSync(
      join(web, 'node_modules/esbuild/bin/esbuild'),
      [join(web, 'src/nav.ts'), '--bundle', '--format=esm', '--outfile=' + bundle],
      { stdio: ['ignore', 'ignore', 'pipe'] },
    );
  } catch (e) {
    console.error(String(e.stderr ?? e));
    process.exit(1);
  }

  const { neighbour, heir, exitsDown, exitsStart, entryRegion } = await import(bundle);

  // ---- stepping ----
  check('down goes down', neighbour(5, 2, 1), '3');
  check('up goes up', neighbour(5, 2, -1), '1');
  // THE assertion of this section: no wrap-around, in either direction. -1 means
  // "nobody", and the caller hands the keystroke back to the browser instead of
  // teleporting the reader to the other end of the list.
  check('the last row is the last row', neighbour(5, 4, 1), '-1');
  check('the first row is the first row', neighbour(5, 0, -1), '-1');
  // From nowhere (nothing focused yet) the arrows enter the list by the end they
  // came from, which is what makes ↓ a way IN and not only a way along.
  check('down enters at the top', neighbour(5, -1, 1), '0');
  check('up enters at the bottom', neighbour(5, -1, -1), '4');
  check('an empty list has no step', neighbour(0, -1, 1), '-1');

  // ---- getting in from outside ----
  //
  // The state every keyboard reader STARTS in is "focus is nowhere": the page
  // has just loaded, or a dialog closed, or they clicked in the margin. Every
  // arrow binding a region makes is conditioned on already being in one, so
  // without these the feature could only be reached with the mouse first —
  // which is the same as not having it.
  const two = ['sidebar.tree', 'content'];

  // ← and → name a SIDE of the screen and are read literally. Where the reader
  // was last has no say: they are pointing at a half of the window.
  check('left enters the left region', entryRegion('left', two, null), 'sidebar.tree');
  check('right enters the right region', entryRegion('right', two, null), 'content');
  check('left ignores where you were', entryRegion('left', two, 'content'), 'sidebar.tree');
  check('right ignores where you were', entryRegion('right', two, 'sidebar.tree'), 'content');

  // ↑ and ↓ name no side, so they resume instead.
  check('down resumes where you were', entryRegion('down', two, 'content'), 'content');
  check('up resumes where you were', entryRegion('up', two, 'sidebar.tree'), 'sidebar.tree');
  // THE assertion of this section: with nowhere to resume, ↓ enters the FIRST
  // region, not the last. Entering the sidebar lands on a row; entering the
  // document lands a caret in the page title, so a wrong guess there means the
  // next thing typed is typed into a title. The guess is made in the direction
  // that cannot damage anything.
  check('with no history, down takes the safe region', entryRegion('down', two, null), 'sidebar.tree');
  // A remembered region that is no longer on screen (the editor unmounted) is
  // not a place to send anybody.
  check('a region that is gone is not resumed', entryRegion('down', ['sidebar.tree'], 'content'), 'sidebar.tree');
  // Nothing on screen: the keystroke goes back to the browser rather than
  // being swallowed for a move that cannot happen.
  check('no regions, no entry', entryRegion('left', [], 'content'), 'null');
  check('one region answers every direction', entryRegion('right', ['content'], null), 'content');

  // ---- who inherits the focus ----
  check('the row below inherits', heir(5, 2), '3');
  check('the last row hands back up', heir(5, 4), '3');
  check('a list of one leaves nobody', heir(1, 0), '-1');
  check('nothing focused, nobody inherits', heir(5, -1), '-1');

  // ---- leaving a text field ----
  //
  // These two are why the document's arrows can be heard with a caret in them at
  // all: everywhere except the very edge, the keystroke belongs to the text.
  check('the end of the title lets go', exitsDown({ start: 9, end: 9, length: 9 }), 'true');
  check('the middle does not', exitsDown({ start: 4, end: 4, length: 9 }), 'false');
  // A selection means the reader is working on the text, not leaving it — and
  // shift+↓ extending a selection must never navigate away.
  check('a selection never leaves', exitsDown({ start: 0, end: 9, length: 9 }), 'false');
  check('an empty title lets go at once', exitsDown({ start: 0, end: 0, length: 0 }), 'true');
  check('the start lets go leftwards', exitsStart({ start: 0, end: 0 }), 'true');
  check('one character in, it does not', exitsStart({ start: 1, end: 1 }), 'false');
  check('a selection from the start stays', exitsStart({ start: 0, end: 3 }), 'false');
} finally {
  rmSync(tmp, { recursive: true, force: true });
}

const bad = out.filter((c) => !c.ok);
for (const c of out) if (!c.ok) console.log(`    FAIL ${c.name}: expected ${c.want}, got ${c.got}`);
console.log(`  ${bad.length === 0 ? 'ok  ' : 'FAIL'} focus navigation ${out.length - bad.length}/${out.length}`);
if (bad.length) process.exit(1);
