import assert from 'node:assert/strict';
import { eventually, withFixture } from './fixture.mjs';

await withFixture(async ({ page, api, base, workspace, path, read }) => {
  const collectionId = path.split('/').pop();
  const events = await api('POST', '/api/pages', {
    title: 'Events', type: 'collection', workspaceId: workspace.id,
  });
  const eventA = await api('POST', '/api/pages', { title: 'Event A', parentId: events.id });
  const eventB = await api('POST', '/api/pages', { title: 'Event B', parentId: events.id });
  const config = await read();
  config.schema.push({ id: 'event', name: 'Event', type: 'relation', relationCollection: events.id });
  await api('PUT', path, config);
  const doc = await api('POST', '/api/pages', { title: 'Filtered TODOs', workspaceId: workspace.id });
  await api('PATCH', `/api/pages/${doc.id}`, {
    content: [eventA, eventB].map((event, index) => ({
      id: `embed-${index}`, type: 'database', content: [], children: [],
      props: { collectionId, local: JSON.stringify({ viewId: 'table', views: {
        table: { filters: [
          { property: 'event', op: 'is', value: event.id },
          { property: 'status', op: 'is', value: index ? 'todo' : 'done' },
        ] },
      } }) },
    })),
  });
  await page.goto(`${base}/p/${doc.id}`);
  const newButton = page.locator('.bn-db-embed').first().getByRole('button', { name: 'New', exact: true });
  await newButton.waitFor();
  let writes = 0;
  let release;
  const held = new Promise(resolve => { release = resolve; });
  await page.route('**/api/pages', async route => {
    if (route.request().method() !== 'POST') return route.continue();
    writes++;
    await held;
    await route.continue();
  });
  await newButton.click();
  await eventually(() => newButton.isDisabled());
  // A second activation during the pending request must not create another row.
  await newButton.evaluate(button => button.click());
  release();
  await page.waitForURL(url => url.pathname !== `/p/${doc.id}`);
  assert.equal(writes, 1);
  const createdId = new URL(page.url()).pathname.split('/').pop();
  const created = await api('GET', `/api/pages/${createdId}`);
  assert.equal(created.parentId, collectionId);
  assert.deepEqual(created.props, { event: [eventA.id], status: 'done' });
  await page.locator('.page-title').fill('New event A TODO');
  await eventually(async () => (await api('GET', `/api/pages/${createdId}`)).title === 'New event A TODO');
  await page.unroute('**/api/pages');
  assert.deepEqual((await read()).views, config.views, 'creation must not change shared filters');

  await page.goto(`${base}/p/${doc.id}`);
  await eventually(async () => (await page.locator('.bn-db-embed').first()
    .locator('.db-table tbody tr').filter({ hasText: 'New event A TODO' }).count()) === 1);
  assert.equal(await page.locator('.bn-db-embed').nth(1)
    .locator('.db-table tbody tr').filter({ hasText: 'New event A TODO' }).count(), 0);

  // A failed creation is reported, releases the guard and leaves no hidden row.
  await page.route('**/api/pages', route => route.request().method() === 'POST'
    ? route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: 'Creation unavailable' }) })
    : route.continue());
  await newButton.click();
  await page.locator('.toast').filter({ hasText: 'Could not be created' }).waitFor();
  await eventually(async () => !(await newButton.isDisabled()));
  await page.unroute('**/api/pages');
  await newButton.click();
  await page.waitForURL(url => url.pathname !== `/p/${doc.id}`);
  const retry = await api('GET', `/api/pages/${new URL(page.url()).pathname.split('/').pop()}`);
  assert.deepEqual(retry.props, created.props);
  console.log('Collection creation: local relation/status defaults, visible row, opened editor, duplicate guard and retry passed.');
});
