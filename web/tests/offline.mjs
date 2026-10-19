import assert from 'node:assert/strict';
import { eventually, withFixture } from './fixture.mjs';

// Offline: opened pages render from snapshots while disconnected, edits made
// offline sync on reconnect, and nothing is duplicated or dropped.
await withFixture(async ({ page, context, api, base, workspace }) => {
  page.setDefaultTimeout(15000);
  const created = await api('POST', '/api/pages', { title: 'Offline Probe', type: 'doc', workspaceId: workspace.id });
  await page.goto(`${base}/p/${created.id}`);
  await eventually(async () => (await page.locator('.editor-loading').count()) === 0);
  // A second online load: the service worker takes control and caches the
  // API snapshots only for requests it sees.
  await page.reload();
  await eventually(async () => (await page.locator('.editor-loading').count()) === 0);

  // Drop the network: the snapshot renders with an offline badge.
  await context.setOffline(true);
  await page.reload();
  await eventually(async () => (await page.locator('.editor-loading').count()) === 0);
  assert((await page.locator('body').textContent()).includes('Offline Probe'));
  assert.equal(await page.locator('.offline-banner').count(), 1);

  // Type while offline, come back: the text syncs and survives a reload.
  await page.locator('.bn-editor[contenteditable="true"]').first().click();
  await page.keyboard.type('written while offline');
  await context.setOffline(false);
  // Wait for the journal to reach the server before reloading: the banner
  // only tracks connectivity, not the sync itself.
  await eventually(async () => {
    const got = await api('GET', `/api/pages/${created.id}`);
    return JSON.stringify(got.content ?? got).includes('written while offline');
  });
  await page.reload();
  await eventually(async () => (await page.locator('body').textContent()).includes('written while offline'));

  console.log('Offline: snapshots render disconnected, edits sync on reconnect.');
});
