import assert from 'node:assert/strict';
import { build } from 'esbuild';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL, fileURLToPath } from 'node:url';

const temp = mkdtempSync(join(tmpdir(), 'salt-collection-defaults-'));
try {
  const outfile = join(temp, 'defaults.mjs');
  await build({ entryPoints: [fileURLToPath(new URL('../src/collectionDefaults.ts', import.meta.url))],
    outfile, bundle: true, platform: 'node', format: 'esm' });
  const { collectionDefaults } = await import(pathToFileURL(outfile));
  const schema = [
    { id: 'event', type: 'relation' }, { id: 'status', type: 'select' },
    { id: 'labels', type: 'multiselect' }, { id: 'done', type: 'checkbox' },
    { id: 'effort', type: 'number' }, { id: 'due', type: 'date' },
    { id: 'owner', type: 'person' }, { id: 'team', type: 'person', personMultiple: true },
    { id: 'computed', type: 'formula' },
  ];
  const is = (property, value) => ({ property, op: 'is', value });
  assert.deepEqual(collectionDefaults(schema, [
    is('event', 'event-a'), is('status', 'todo'), is('labels', 'urgent'),
    is('labels', 'client'), is('done', 'false'), is('effort', '4.5'),
    is('due', '2026-10-15'), is('owner', 'user-a'), is('team', 'user-b'),
    is('computed', 'read-only'),
  ]), { event: ['event-a'], status: 'todo', labels: ['urgent', 'client'],
    done: false, effort: 4.5, due: '2026-10-15', owner: 'user-a', team: ['user-b'] });
  assert.deepEqual(collectionDefaults(schema, [
    { property: 'status', values: ['todo', 'done'], value: '', op: 'is' },
    { property: 'event', values: ['a', 'b'], value: '', op: 'is' },
    { property: 'effort', value: '10', op: 'gt' },
  ]), {}, 'do not pick arbitrary alternatives or inequality values');
  assert.deepEqual(collectionDefaults(schema, [
    { property: 'status', values: ['todo', 'done'], value: '', op: 'is' },
    is('status', 'done'),
  ]), { status: 'done' }, 'intersect equality conditions');
  assert.deepEqual(collectionDefaults(schema, [is('status', 'todo'), is('status', 'done')]), {},
    'conflicting scalar conditions have no default');
  assert.deepEqual(collectionDefaults(schema, [
    { property: 'status', value: '', op: 'is' }, is('effort', 'NaN'), is('done', 'invalid'),
    { property: 'event', value: '', op: 'is_empty' },
  ]), {});
  assert.deepEqual(collectionDefaults(schema, [is('status', 'todo')], { status: 'done', due: '2026-10-16' }),
    { status: 'done', due: '2026-10-16' }, 'explicit board/calendar choices take precedence');
  console.log('Collection defaults: typed values, relations, alternatives, conflicts and explicit presets passed.');
} finally {
  rmSync(temp, { recursive: true, force: true });
}
