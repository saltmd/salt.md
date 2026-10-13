import assert from 'node:assert/strict';
import { eventually, withFixture } from './fixture.mjs';

// Page access: the lock button opens the access menu, choosing the private
// mode narrows the page, and the choice persists. Cross-user reads and the
// grant limits are covered server-side (page_access_test.go).
await withFixture(async ({ page, api, base, workspace }) => {
  page.setDefaultTimeout(15000);
  const created = await api('POST', '/api/pages', { title: 'Grantable', type: 'doc', workspaceId: workspace.id });
  await page.goto(`${base}/p/${created.id}`);

  await page.locator('.access-wrap button').click();
  await page.locator('.access-menu').waitFor();
  assert.equal(await page.locator('.access-menu input[name="page-access"]').count(), 2);

  // Choosing the private mode narrows the page; the member picker appears
  // with it (it lives in the private branch, so click first).
  await page.locator('.access-menu label', { hasText: 'Only me and chosen members' }).click();
  await eventually(async () => page.locator('.access-menu input[name="page-access"][value="private"]').isChecked());
  await page.locator('.access-menu select[name="share-member"]').waitFor();
  await eventually(async () => {
    const res = await api('GET', `/api/pages/${created.id}`);
    return res.visibility === 'private';
  });

  // The member picker is there for the owner; choosing persists a reload.
  await page.reload();
  await page.locator('.access-wrap button').click();
  await page.locator('.access-menu').waitFor();
  assert((await page.locator('.access-menu').textContent()).includes('Nobody yet'));

  console.log('Page access: the menu narrows the page and the choice persists.');
});
