import assert from 'node:assert/strict';
import { eventually, withFixture } from './fixture.mjs';

// Relation chips: a click EDITS (opens the picker below), the icon beside
// the chip opens the row. A click must never navigate, and buttons must
// never nest — the picker toggle used to swallow a pencil button, which is
// invalid HTML and vanished behind long titles on cards.
await withFixture(async ({ page, api, base, workspace, path }) => {
  page.setDefaultTimeout(15000);
  const collectionId = path.split('/').pop();
  const target = await api('POST', '/api/pages', { title: 'Targets', type: 'collection', workspaceId: workspace.id });
  const alpha = await api('POST', '/api/pages', { parentId: target.id, title: 'Alpha', props: {} });
  await api('POST', '/api/pages', { parentId: target.id, title: 'Beta', props: {} });
  await api('PUT', path, {
    schema: [{ id: 'rel', name: 'Rel', type: 'relation', relationCollection: target.id }],
    views: [{ id: 'table', name: 'Table', type: 'table' }],
  });
  await api('POST', '/api/pages', { parentId: collectionId, title: 'Linked', props: { rel: [alpha.id] } });

  await page.goto(`${base}/p/${collectionId}`);
  const chip = page.locator('.relation-chip-wrap', { hasText: 'Alpha' }).first();
  await chip.waitFor();
  // No button inside a button anywhere in the relation cells.
  assert.equal(await page.locator('.relation-value button button').count(), 0);

  // Clicking the chip edits: the picker opens, the page stays put.
  const url = page.url();
  await chip.locator('.relation-chip').click();
  await page.locator('.relation-menu').waitFor();
  assert.equal(page.url(), url);

  // The icon opens the row in a new tab, titled with the row.
  const tabs = page.locator('.tab-bar .tab');
  await chip.locator('.relation-open-btn').click();
  await eventually(async () => (await tabs.filter({ hasText: 'Alpha' }).count()) === 1);

  console.log('Relation chips: click edits, the icon opens the row, no nested buttons.');
});
