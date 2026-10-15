import assert from 'node:assert/strict';
import { eventually, withFixture } from './fixture.mjs';

await withFixture(async ({ page, api, base, workspace, path }) => {
  page.setDefaultTimeout(15000);
  const collectionId = path.split('/').at(-1);
  const unrelated = await api('POST', '/api/pages', { title: 'Unrelated page', workspaceId: workspace.id });
  await page.goto(`${base}/p/${unrelated.id}`);
  await page.goto(`${base}/p/${collectionId}`);
  await page.locator('.view-tab', { hasText: 'Board' }).click();
  await page.locator('.board-card', { hasText: 'First row' }).click();
  const rowId = page.url().split('/').at(-1);
  await page.getByRole('button', { name: 'More actions', exact: true }).click();
  await page.getByRole('button', { name: 'Move to trash', exact: true }).click();
  await eventually(async () => page.url() === `${base}/p/${collectionId}`);
  await page.locator('.view-tab', { hasText: 'Board' }).waitFor();
  assert.equal((await api('GET', `/api/pages/${rowId}`)).trashed, true);
  assert.equal(await page.locator('.tab-bar .tab', { hasText: 'First row' }).count(), 0);

  const parent = await api('POST', '/api/pages', { title: 'Parent document', workspaceId: workspace.id });
  const child = await api('POST', '/api/pages', { title: 'Child document', parentId: parent.id });
  await page.goto(`${base}/p/${child.id}`);
  await page.getByRole('button', { name: 'More actions', exact: true }).click();
  await page.getByRole('button', { name: 'Move to trash', exact: true }).click();
  await eventually(async () => page.url() === `${base}/p/${parent.id}`);
  console.log('Trash navigation: collection rows and child documents return to their parent and remove the deleted tab.');
});
