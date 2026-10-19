import assert from 'node:assert/strict';
import { eventually, withFixture } from './fixture.mjs';

// Readable URLs: a workspace page carries its slug, a private page keeps the
// bare id (titles must never leak through copied links), and a stale slug
// still opens the page and is corrected without a reload.
await withFixture(async ({ page, api, base, workspace }) => {
  page.setDefaultTimeout(15000);
  const open = await api('POST', '/api/pages', { title: 'Moonlit Garden Notes', type: 'doc', workspaceId: workspace.id });
  const secret = await api('POST', '/api/pages', { title: 'Secret Salaries 2026', type: 'doc', workspaceId: workspace.id });
  await api('PATCH', `/api/pages/${secret.id}`, { visibility: 'private' });

  await page.goto(`${base}/p/${open.id}`);
  await eventually(async () => (await page.url()).includes(`/p/moonlit-garden-notes-${open.id}`));

  await page.goto(`${base}/p/${secret.id}`);
  await eventually(async () => page.url() === `${base}/p/${secret.id}`);
  assert(!page.url().includes('secret'), 'a private URL must carry no title');

  // A stale slug opens the page and is corrected in place.
  await page.goto(`${base}/p/wrong-slug-${open.id}`);
  await eventually(async () => (await page.url()).includes(`/p/moonlit-garden-notes-${open.id}`));

  console.log('Readable URLs: slugs for workspace pages, bare ids for private, stale slugs corrected.');
});
