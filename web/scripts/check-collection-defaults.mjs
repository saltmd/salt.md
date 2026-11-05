// Defaults must be unambiguous and use the same comparisons as the view.
import assert from 'node:assert/strict';
import { build } from 'esbuild';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const tmp = mkdtempSync(join(tmpdir(), 'salt-collection-defaults-'));
try {
  const outfile = join(tmp, 'defaults.mjs');
  await build({ entryPoints: [fileURLToPath(new URL('../src/collectionDefaults.ts', import.meta.url))], bundle: true, format: 'esm', outfile });
  const { collectionDefaults: defaults } = await import(pathToFileURL(outfile).href);
  const filter = (value, op = 'is') => ({ property: 'p', value, op });
  const schema = (type, extra = {}) => [{ id: 'p', name: 'Property', type, ...extra }];
  const cases = [
    ['text', [filter('Done')], { p: 'Done' }],
    ['url', [filter('https://example.org')], { p: 'https://example.org' }],
    ['number', [filter('12')], { p: 12 }],
    ['number', [filter('5.0')], { p: 5 }],
    ['number', [filter('5.0'), filter('5')], { p: 5 }],
    ['number', [filter('NaN')], {}],
    ['checkbox', [filter('true')], { p: true }],
    ['checkbox', [filter('false')], { p: false }],
    ['select', [{ ...filter(''), values: ['a', 'b'] }], {}],
    ['select', [filter('a'), filter('b')], {}],
    ['select', [{ ...filter(''), values: ['a', 'b'] }, filter('b')], { p: 'b' }],
    ['multiselect', [{ ...filter(''), values: ['a', 'b'] }], {}],
    ['multiselect', [filter('a'), filter('a', 'is_not')], {}],
    ['select', [filter('Today')], { p: 'Today' }],
    ['select', [filter('a'), filter('a', 'is_not')], {}],
    ['text', [filter('abc'), filter('z', 'contains')], {}],
    ['text', [filter('abc'), filter('b', 'contains')], { p: 'abc' }],
    ['text', [filter('abc', 'contains')], {}],
    ['text', [filter('')], {}],
    ['multiselect', [filter('a'), filter('b')], { p: ['a', 'b'] }],
    ['relation', [filter('a')], { p: ['a'] }],
    ['person', [filter('a')], { p: 'a' }],
    ['date', [filter('2026-10-28')], { p: '2026-10-28' }],
    ['date', [filter('today-')], {}],
    ['formula', [filter('a')], {}],
    ['rollup', [filter('a')], {}],
    ['backrelation', [filter('a')], {}],
    ['lastActivity', [filter('a')], {}],
    ['checklist', [filter('a')], {}],
  ];
  for (const [type, filters, expected] of cases) {
    assert.deepEqual(defaults(schema(type), filters), expected, JSON.stringify([type, filters]));
  }
  assert.deepEqual(defaults(schema('person', { personMultiple: true }), [filter('a')]), { p: ['a'] });
  // "@me" is the one making the row, never the marker itself: stored as
  // written, every reader would see the row as their own (#120).
  const me = { id: 'u_ada', name: 'Ada Lovelace' };
  const mine = { ...filter(''), values: ['@me'] };
  assert.deepEqual(defaults(schema('person'), [mine], {}, me), { p: 'u_ada' });
  assert.deepEqual(defaults(schema('person'), [filter('@me')], {}, me), { p: 'u_ada' });
  assert.deepEqual(defaults(schema('person', { personMultiple: true }), [mine], {}, me), { p: ['u_ada'] });
  assert.deepEqual(defaults(schema('person'), [{ ...filter(''), values: ['@me', 'u_bob'] }], {}, me), {});
  assert.deepEqual(defaults(schema('person'), [mine]), {});
  assert.deepEqual(defaults(schema('text'), [filter('@me')], {}, me), { p: '@me' });
  assert.deepEqual(defaults(schema('select'), [filter('a')], { p: '' }), { p: '' });
  assert.deepEqual(defaults(schema('select'), [filter('a')], { p: 'b' }), { p: 'b' });
  const relative = defaults(schema('date'), [filter('today')]);
  assert.match(relative.p, /^\d{4}-\d{2}-\d{2}$/);
  const filters = Object.freeze([Object.freeze(filter('a'))]);
  const presets = Object.freeze({ other: 'b' });
  assert.deepEqual(defaults(schema('select'), filters, presets), { p: 'a', other: 'b' });
  console.log('Collection defaults: all checks passed');
} finally {
  rmSync(tmp, { recursive: true, force: true });
}
