// Asserts which children each sidebar section shows (src/treeMode.ts).
//
//   node scripts/check-treemode.mjs
//
// Same shape as check-cardlayout.mjs: esbuild bundles the module once and it is
// imported. No framework.
//
// This check exists because the rule has been got wrong TWICE, in opposite
// directions, and neither time did anything fail:
//
//   1. A database filed under a document appeared nowhere — Documents dropped
//      it as a database, Collections only listed roots.
//   2. The split-mode filter kept running in MIXED mode, where the one section
//      is the only section. A database under a document vanished from the
//      interface while the page count went on including it. Reported from real
//      use: "hab auf Baumansicht umgestellt aber sehe die Datenbanken nicht
//      mehr".
//
// Both failures look identical from the outside — a tree that quietly omits a
// page still looks like a tree. That is why they are worth pinning.

import { execFileSync } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const web = join(here, '..');
const tmp = mkdtempSync(join(tmpdir(), 'salt-tree-'));
const bundle = join(tmp, 'treeMode.mjs');

try {
  try {
    execFileSync(
      join(web, 'node_modules/esbuild/bin/esbuild'),
      [join(web, 'src/treeMode.ts'), '--bundle', '--format=esm', '--outfile=' + bundle],
      { stdio: ['ignore', 'ignore', 'pipe'] },
    );
  } catch (e) {
    console.error(String(e.stderr ?? e));
    process.exit(1);
  }

  const tm = await import(bundle);
  const out = [];
  const check = (name, got, want) => out.push({ name, got: String(got), want: String(want) });
  const names = (list) => list.map((x) => x.id).join(',');

  const doc = { id: 'doc', type: 'doc' };
  const db = { id: 'db', type: 'collection' };
  const row = { id: 'row' }; // a row carries no type in the tree payload
  const kids = [doc, db, row];

  // ---- Split mode: two sections, so Documents may hide a database ----
  check('split: docs hides the database', names(tm.childrenForSection(kids, 'docs', false)), 'doc,row');
  check('split: the dbs section shows everything', names(tm.childrenForSection(kids, 'dbs', false)), 'doc,db,row');
  check('split: a top-level database is left to Collections',
    names(tm.topLevelForDocs(kids, false)), 'doc,row');
  check('split: there IS a Collections section', tm.hasSeparateDbSection(false), true);

  // ---- Mixed mode: ONE section, so nothing may be hidden ----
  // This is the whole bug. Every one of these was false when it was reported.
  check('mixed: docs shows the database too', names(tm.childrenForSection(kids, 'mixed' && 'docs', true)), 'doc,db,row');
  check('mixed: a top-level database shows', names(tm.topLevelForDocs(kids, true)), 'doc,db,row');
  check('mixed: there is NO second section', tm.hasSeparateDbSection(true), false);
  // The section a caller passes must not matter in mixed mode — there is only
  // one, and a stray 'dbs' must not change the answer either.
  check('mixed: the section name is irrelevant',
    names(tm.childrenForSection(kids, 'dbs', true)), 'doc,db,row');

  // ---- Whatever the mode, nothing is ever ADDED or reordered ----
  check('nothing is invented (split)', tm.childrenForSection([], 'docs', false).length, 0);
  check('nothing is invented (mixed)', tm.childrenForSection([], 'docs', true).length, 0);
  check('order is preserved', names(tm.childrenForSection([row, db, doc], 'docs', true)), 'row,db,doc');

  let fail = 0;
  for (const c of out) {
    if (c.got !== c.want) {
      fail++;
      console.log(`  FAIL ${c.name}: got ${JSON.stringify(c.got)}, want ${JSON.stringify(c.want)}`);
    }
  }
  console.log(`\n  tree sections: ${out.length - fail} passed, ${fail} failed`);
  process.exit(fail ? 1 : 0);
} finally {
  rmSync(tmp, { recursive: true, force: true });
}
