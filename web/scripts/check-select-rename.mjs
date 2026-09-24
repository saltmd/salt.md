// A rename must leave stored select/multi-select values and filters resolvable.
import assert from 'node:assert/strict';
import { build } from 'esbuild';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const tmp = mkdtempSync(join(tmpdir(), 'salt-select-rename-'));
try {
  const outfile = join(tmp, 'rename.mjs');
  await build({
    entryPoints: [fileURLToPath(new URL('../src/renameSelectOption.ts', import.meta.url))],
    bundle: true, format: 'esm', outfile,
  });
  const { renameSelectOption, moveSelectOption, reorderSelectOption } = await import(pathToFileURL(outfile).href);
  const options = Object.freeze([
    Object.freeze({ id: 'in-progress', name: 'In progress', color: '#337ea9' }),
    Object.freeze({ id: 'done', name: 'Done', color: '#448361' }),
  ]);
  assert.deepEqual(moveSelectOption(options, 'in-progress', 1), [options[1], options[0]]);
  assert.deepEqual(moveSelectOption(options, 'done', -1), [options[1], options[0]]);
  assert.equal(moveSelectOption(options, 'in-progress', -1), options);
  assert.equal(moveSelectOption(options, 'done', 1), options);
  assert.equal(moveSelectOption(options, 'missing', 1), options);
  const three = Object.freeze([...options, Object.freeze({id: 'third', name: 'Third', color: '#123456'})]);
  assert.deepEqual(reorderSelectOption(three, 'in-progress', 'third', true), [three[1], three[2], three[0]]);
  assert.deepEqual(reorderSelectOption(three, 'third', 'in-progress', false), [three[2], three[0], three[1]]);
  assert.deepEqual(reorderSelectOption(three, 'in-progress', 'third', false), [three[1], three[0], three[2]]);
  assert.deepEqual(reorderSelectOption(three, 'third', 'in-progress', true), [three[0], three[2], three[1]]);
  assert.equal(reorderSelectOption(three, 'missing', 'third', true), three);
  assert.equal(reorderSelectOption(three, 'third', 'missing', true), three);
  assert.equal(reorderSelectOption(three, 'third', 'third', true), three);
  const renamed = renameSelectOption(options, 'in-progress', '  In review  ');
  assert.deepEqual(renamed, [
    { id: 'in-progress', name: 'In review', color: '#337ea9' }, options[1],
  ]);
  const rows = ['in-progress', 'done', ['in-progress', 'done']];
  assert.deepEqual(rows.map(value => (Array.isArray(value) ? value : [value])
    .map(id => renamed.find(option => option.id === id)?.name)),
    [['In review'], ['Done'], ['In review', 'Done']]);
  assert.equal(renamed.find(option => option.id === 'in-progress').id, options[0].id);
  for (const invalid of ['', '   ', 'Done', ' done ']) {
    assert.equal(renameSelectOption(options, 'in-progress', invalid), null);
  }
  assert.equal(renameSelectOption(options, 'missing', 'Review'), null);
  assert.equal(renameSelectOption(options, 'in-progress', 'IN PROGRESS')[0].name, 'IN PROGRESS');
  assert.equal(renameSelectOption(options, 'in-progress', 'Review – revised')[0].id, 'in-progress');
  assert.equal(options[0].name, 'In progress');
  console.log('Select rename: stored selections, stable ids/colours, validation and immutability passed.');
} finally {
  rmSync(tmp, { recursive: true, force: true });
}
