import assert from 'node:assert/strict';
import { eventually, withFixture } from './fixture.mjs';

await withFixture(async ({ page, context, api, base, workspace, path }) => {
  const sourceId = path.split('/').pop();
  const createCollection = (title, workspaceId = workspace.id) =>
    api('POST', '/api/pages', { title, type: 'collection', workspaceId });
  const destination = await createCollection('Destination');
  const other = await api('POST', '/api/workspaces', { name: 'Other workspace' });
  const outside = await createCollection('Outside', other.id);
  const { rows } = await api('GET', `${path}/rows`);
  const original = rows.find((row) => row.title === 'First row');
  const movePath = `${path}/rows/move`;
  const rowPath = `/api/pages/${original.id}`;
  const read = () => api('GET', rowPath);
  const sameParent = (id) => eventually(async () => (await read()).parentId === id);
  const card = (tab) => tab.locator('.board-card').filter({ hasText: 'First row' });
  const target = (id) => page.locator(`[data-board-drop-collection="${id}"]`);
  const press = () => page.keyboard.press('Control+z');
  const openBoard = async (tab, id) => {
    await tab.goto(`${base}/p/${id}`);
    const boardTab = tab.locator('.view-tab').filter({ hasText: /^Board$/ });
    await boardTab.click();
    await tab.locator('.board').waitFor();
  };
  const drop = async (id, highlighted = true) => {
    const from = await card(page).boundingBox();
    const to = await target(id).boundingBox();
    assert(from && to, 'card and sidebar collection must be visible');
    await page.mouse.move(from.x + 45, from.y + 16);
    await page.mouse.down();
    await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 12 });
    await page.locator('.board-drag-ghost').waitFor();
    const highlight = target(id).and(page.locator('[data-board-drop-active]'));
    if (highlighted) await highlight.waitFor();
    else assert.equal(await highlight.count(), 0);
    await page.mouse.up();
    await highlight.waitFor({ state: 'hidden' });
  };

  await openBoard(page, sourceId);
  await drop(sourceId, false);
  assert.equal((await read()).parentId, sourceId);
  const forbidden = await context.request.post(base + movePath, {
    data: { rowId: original.id, collectionId: outside.id },
  });
  assert.equal(forbidden.status(), 404);
  assert.equal((await forbidden.json()).code, 'collection_move_unavailable');

  const observer = await context.newPage();
  await openBoard(observer, destination.id);
  await drop(destination.id);
  await card(page).waitFor({ state: 'hidden' });
  await card(observer).waitFor();
  await observer.reload();
  await card(observer).waitFor();
  assert.equal((await read()).parentId, destination.id);

  // Undo only changes collection membership and order, even after a property edit.
  await api('PATCH', rowPath, { propsPatch: { status: 'done' } });
  await press();
  await card(page).waitFor();
  await card(observer).waitFor({ state: 'hidden' });
  const restored = await read();
  assert.equal(restored.parentId, sourceId);
  assert.equal(restored.position, original.position);
  assert.deepEqual(restored.props, { ...original.props, status: 'done' });

  let release;
  const gate = new Promise((resolve) => { release = resolve; });
  await page.route(`**${movePath}`, async (route) => {
    await gate;
    await route.continue();
  });
  const undoRequest = page.waitForRequest((req) => req.method() === 'POST' &&
    req.url().endsWith(`/api/collections/${destination.id}/rows/move`));
  await drop(destination.id);
  await press();
  release();
  await undoRequest;
  await sameParent(sourceId);
  await page.unroute(`**${movePath}`);

  await card(page).waitFor();
  await drop(destination.id);
  await card(observer).waitFor();
  const moved = await read();
  await api('PATCH', rowPath, { position: moved.position + 10 });
  const revert = page.waitForResponse((res) => res.url().endsWith(`/api/collections/${destination.id}/rows/move`) &&
    res.request().method() === 'POST');
  await press();
  const rejected = await revert;
  assert.equal(rejected.status(), 409);
  assert.equal((await rejected.json()).code, 'collection_move_stale');
  await page.locator('.toast').filter({ hasText: 'The card moved again. This move could not be undone.' }).waitFor();
  const after = await read();
  assert.equal(after.parentId, destination.id);
  assert.equal(after.position, moved.position + 10);

  console.log('Sidebar collection moves: drag target, live updates, persistence, immediate undo and stale undo passed.');
});
