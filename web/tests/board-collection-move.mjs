import assert from 'node:assert/strict';
import { eventually, withFixture } from './fixture.mjs';

await withFixture(async ({ page, context, api, base, workspace, path, schema }) => {
  const sourceId = path.split('/').pop();
  const createCollection = (title, workspaceId = workspace.id) =>
    api('POST', '/api/pages', { title, type: 'collection', workspaceId });
  const destination = await createCollection('Destination');
  // The same properties: a move that carries everything over asks nothing.
  await api('PUT', `/api/collections/${destination.id}`, { schema });
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
    if (route.request().method() === 'POST') await gate;
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

  // A destination that would not show the card's values, and a relation
  // that keeps pointing at it: the move lists both and asks first.
  await api('POST', `/api/collections/${destination.id}/rows/move`, { rowId: original.id, collectionId: sourceId });
  await sameParent(sourceId);
  const elsewhere = await createCollection('Elsewhere');
  await api('PUT', `/api/collections/${elsewhere.id}`, { schema: [
    { id: 'status', name: 'Status', type: 'select', options: [{ id: 'open', name: 'Open', color: '#337ea9' }] },
  ] });
  const projects = await createCollection('Projects');
  await api('PUT', `/api/collections/${projects.id}`, { schema: [
    { id: 'tasks', name: 'Tasks', type: 'relation', relationCollection: sourceId },
  ] });
  await api('POST', '/api/pages', { parentId: projects.id, title: 'Launch', props: { tasks: [original.id] } });
  await openBoard(page, sourceId);
  await drop(elsewhere.id);
  const dialog = page.locator('.confirm-dialog');
  await dialog.waitFor();
  const message = await dialog.locator('.confirm-message').innerText();
  assert.match(message, /“Elsewhere” will not show these values of the card:\s+Status, Labels/);
  assert.match(message, /Projects › Tasks/);
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await page.waitForTimeout(200);
  assert.equal((await read()).parentId, sourceId, 'cancelling leaves the card where it is');
  await drop(elsewhere.id);
  await dialog.getByRole('button', { name: 'Move anyway' }).click();
  await sameParent(elsewhere.id);
  await card(page).waitFor({ state: 'hidden' });
  await press();
  await sameParent(sourceId);
  assert.deepEqual((await read()).props, { ...original.props, status: 'done' }, 'moving back restores every value');
  const activity = await api('GET', `/api/audit?limit=5`);
  const moves = activity.filter((e) => e.action === 'move_to_collection');
  assert(moves.some((e) => e.detail === 'Option editing test → Elsewhere'), 'the move is in the activity log');
  assert(moves.some((e) => e.detail === 'Elsewhere → Option editing test'), 'its undo is in the activity log');

  // Without the live stream the board still drops the moved card.
  await page.route('**/api/events*', (route) => route.abort());
  await page.goto(`${base}/p/${sourceId}`);
  await page.locator('.view-tab').filter({ hasText: /^Board$/ }).click();
  await card(page).waitFor();
  await drop(destination.id);
  // Projects still relates to the card, so the move asks first.
  await page.locator('.confirm-dialog').getByRole('button', { name: 'Move anyway' }).click();
  await sameParent(destination.id);
  await card(page).waitFor({ state: 'hidden' });
  await page.unroute('**/api/events*');
  await api('POST', `/api/collections/${destination.id}/rows/move`, { rowId: original.id, collectionId: sourceId });
  await sameParent(sourceId);

  // Two warnings at once (two embedded boards): the one replaced is answered
  // as cancelled, so the board waiting on it is not stuck for good.
  await page.evaluate(() => {
    window.dialogAnswers = [];
    for (const n of [1, 2]) {
      window.dispatchEvent(new CustomEvent('salt:dialog', { detail: {
        kind: 'confirm', message: `Question ${n}`, resolve: (v) => window.dialogAnswers.push([n, v]),
      } }));
    }
  });
  await page.locator('.confirm-message').filter({ hasText: 'Question 2' }).waitFor();
  assert.deepEqual(await page.evaluate(() => window.dialogAnswers), [[1, false]]);
  await page.locator('.confirm-dialog').getByRole('button', { name: 'Cancel' }).click();
  assert.deepEqual(await page.evaluate(() => window.dialogAnswers), [[1, false], [2, false]]);

  console.log('Sidebar collection moves: drag target, live updates, persistence, immediate undo, stale undo, the warning before a lossy move, its activity entries and replaced dialogs passed.');
});
