// Asserts the column-width arithmetic (src/columnWidths.ts).
//
//   node scripts/check-columnwidths.mjs
//
// Same shape as check-treemode.mjs: esbuild bundles the module once and it is
// imported. No framework.
//
// What it pins is the part that quietly corrupts a page: a stored layout that
// no longer matches its column count, or a drag that leaves a column too narrow
// to grab back. The server applies the same rule (server/columns_test.go).

import { execFileSync } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const web = join(here, '..');
const tmp = mkdtempSync(join(tmpdir(), 'salt-colw-'));
const bundle = join(tmp, 'columnWidths.mjs');

try {
  try {
    execFileSync(
      join(web, 'node_modules/esbuild/bin/esbuild'),
      [join(web, 'src/columnWidths.ts'), '--bundle', '--format=esm', '--outfile=' + bundle],
      { stdio: ['ignore', 'ignore', 'pipe'] },
    );
  } catch (e) {
    console.error(String(e.stderr ?? e));
    process.exit(1);
  }

  const cw = await import(bundle);
  const out = [];
  const check = (name, got, want) => out.push({ name, got: JSON.stringify(got), want: JSON.stringify(want) });

  // ---- Equal columns ----
  check('two equal', cw.equalWidths(2), [50, 50]);
  check('three equal sum to 100', cw.equalWidths(3).reduce((a, b) => a + b, 0), 100);
  check('equal layouts store nothing', cw.formatWidths([50, 50]), '');
  check('equal thirds store nothing', cw.formatWidths(cw.equalWidths(3)), '');
  check('a real layout is stored', cw.formatWidths([70, 30]), '70,30');

  // ---- Reading what is stored ----
  check('empty means equal', cw.parseWidths('', 2), null);
  check('missing means equal', cw.parseWidths(undefined, 2), null);
  check('a layout', cw.parseWidths('70,30', 2), [70, 30]);
  check('a layout for another count is ignored', cw.parseWidths('70,30', 3), null);
  check('text is ignored', cw.parseWidths('a,b', 2), null);
  check('a sliver is ignored', cw.parseWidths('99,1', 2), null);

  // ---- Dragging a divider ----
  check('move right', cw.moveDivider([50, 50], 0, 20), [70, 30]);
  check('move left', cw.moveDivider([50, 50], 0, -20), [30, 70]);
  check('cannot squeeze the right column', cw.moveDivider([50, 50], 0, 90), [85, 15]);
  check('cannot squeeze the left column', cw.moveDivider([50, 50], 0, -90), [15, 85]);
  check('the middle divider leaves the first column alone', cw.moveDivider([25, 50, 25], 1, 10), [25, 60, 15]);
  check('the total never changes', cw.moveDivider([33, 33, 34], 0, 7.4).reduce((a, b) => a + b, 0), 100);

  // ---- Every preset is a layout the rest accepts ----
  for (const p of cw.PRESETS) {
    check(`preset ${p.join('/')} sums to 100`, p.reduce((a, b) => a + b, 0), 100);
    check(`preset ${p.join('/')} survives a round trip`,
      cw.parseWidths(cw.formatWidths(p), p.length) ?? cw.equalWidths(p.length), [...p]);
  }

  check('grid template', cw.gridTemplate([70, 30]), 'minmax(0,70fr) minmax(0,30fr)');

  let fail = 0;
  for (const c of out) {
    if (c.got !== c.want) {
      fail++;
      console.log(`  FAIL ${c.name}: got ${c.got}, want ${c.want}`);
    }
  }
  console.log(`\n  column widths: ${out.length - fail} passed, ${fail} failed`);
  process.exit(fail ? 1 : 0);
} finally {
  rmSync(tmp, { recursive: true, force: true });
}
