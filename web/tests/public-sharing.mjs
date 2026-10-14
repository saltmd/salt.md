import assert from 'node:assert/strict';
import { eventually, withFixture } from './fixture.mjs';

// Public collection shares: anonymous readers see exactly the shared view —
// no private rows, no filtered-out rows, no hidden columns — and row detail
// obeys the same gate.
await withFixture(async ({ page, context, api, base, workspace }) => {
  page.setDefaultTimeout(15000);
  const owner = await api('POST', '/api/pages', { title: 'Source', type: 'collection', workspaceId: workspace.id });
  const collectionId = owner.id;
  await api('PUT', `/api/collections/${collectionId}`, {
    schema: [
      { id: 'status', name: 'Status', type: 'text' },
      { id: 'secret', name: 'Secret', type: 'text' },
    ],
    views: [{
      id: 'v1', name: 'Done', type: 'table',
      filters: [{ property: 'status', op: 'is', value: 'done' }],
      hidden: ['secret'],
    }],
  });
  await api('POST', '/api/pages', { parentId: collectionId, title: 'Done row', props: { status: 'done', secret: 'shh' } });
  await api('POST', '/api/pages', { parentId: collectionId, title: 'Todo row', props: { status: 'todo' } });
  const priv = await api('POST', '/api/pages', { parentId: collectionId, title: 'Private row', props: { status: 'done' } });
  await api('PATCH', `/api/pages/${priv.id}`, { visibility: 'private' });
  const minted = await api('POST', `/api/pages/${collectionId}/share`, { allowedViews: ['v1'], allowDetail: true });
  const token = minted.token ?? minted.url.split('/').pop();

  // Anonymous: a fresh context with no account.
  const anon = await context.browser().newContext();
  const visitor = await anon.newPage();
  await visitor.goto(`${base}/public/${token}`);
  const rows = visitor.locator('.public-collection-body tbody tr');
  await eventually(async () => (await rows.count()) === 1);
  assert((await rows.first().textContent()).includes('Done row'));
  assert.equal(await visitor.locator('.public-collection-body th', { hasText: 'Secret' }).count(), 0);

  // Row detail obeys the same gate and hides the secret column too.
  await rows.first().locator('.db-title-link').click();
  await visitor.locator('.public-detail').waitFor();
  assert(!(await visitor.locator('.public-detail').textContent()).includes('shh'));
  await visitor.locator('.public-detail-backdrop').click({ position: { x: 5, y: 5 } });

  await anon.close();
  console.log('Public shares: anonymous readers see exactly the shared view.');
});
