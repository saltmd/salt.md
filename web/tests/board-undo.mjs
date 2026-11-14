import assert from 'node:assert/strict';
import { eventually, withFixture } from './fixture.mjs';

await withFixture(async ({ page, api, base, workspace, path, schema }) => {
  page.setDefaultTimeout(15000);
  await page.setViewportSize({ width: 1900, height: 1100 });
  const collectionId = path.split('/').at(-1);
  const first = (await api('GET', `${path}/rows`)).rows.find((r) => r.title === 'First row');
  const rowPath = `/api/pages/${first.id}`;
  const undoKey = process.platform === 'darwin' ? 'Meta+z' : 'Control+z';
  const props = async () => (await api('GET', rowPath)).props;
  const board = page.locator('.board');
  const cardIn = (col, scope = board) => scope.locator(`[data-col="${col}"] .board-card`).filter({ hasText: 'First row' });
  let saves = 0;
  let reverts = 0;
  page.on('request', (r) => {
    if (r.method() === 'PATCH' && r.url().endsWith(rowPath)) saves++;
    if (r.method() === 'POST' && /\/api\/audit\/\d+\/revert$/.test(r.url())) reverts++;
  });
  const openBoard = async () => {
    await page.goto(`${base}/p/${collectionId}`);
    await page.locator('.view-tab', { hasText: 'Board' }).click();
    await board.waitFor();
  };
  const reset = async (status = 'todo', views) => {
    await api('PATCH', rowPath, { propsPatch: { status, labels: ['todo', 'done'] } });
    await api('PUT', path, { schema, views: views ?? [
      { id: 'board', name: 'Board', type: 'board', groupBy: 'status' },
    ] });
    await openBoard();
  };
  // Pointer events exercise the real drag hook, including threshold, ghost,
  // column hit testing and pointer release. HTML5 dragTo bypasses that path.
  const drag = async (from, to, { scope = board, cancel = false } = {}) => {
    const source = cardIn(from, scope).first();
    await source.scrollIntoViewIfNeeded();
    const a = await source.boundingBox();
    const b = await scope.locator(`[data-col="${to}"]`).boundingBox();
    assert(a && b, 'drag endpoints must be visible');
    await page.mouse.move(a.x + 60, a.y + 28);
    await page.mouse.down();
    await page.mouse.move(b.x + 80, b.y + 85, { steps: 12 });
    await scope.locator('.board-drag-ghost').waitFor();
    if (cancel) await page.evaluate(() => window.dispatchEvent(new PointerEvent('pointercancel')));
    await page.mouse.up();
  };
  const expectStatus = (status) => eventually(async () => (await props()).status === status);
  const menuMove = async (from, name, scope = board) => {
    await cardIn(from, scope).first().locator('.card-move-btn').click();
    await page.locator('.card-move-menu').getByRole('button', { name, exact: true }).click();
  };

  await openBoard();
  await drag('todo', 'done');
  await expectStatus('done');
  await page.keyboard.press(undoKey);
  await expectStatus('todo');
  await cardIn('todo').waitFor();
  assert.deepEqual((await props()).labels, ['todo', 'done'], 'undo preserves other properties');

  // Mixed move mechanisms share the same stack, including two key presses
  // before either undo request has answered.
  await drag('todo', 'third');
  await expectStatus('third');
  await menuMove('third', 'Done');
  await expectStatus('done');
  await page.keyboard.press(undoKey);
  await page.keyboard.press(undoKey);
  await expectStatus('todo');
  await cardIn('todo').waitFor();

  const noops = saves;
  await drag('todo', 'todo');
  await drag('todo', 'done', { cancel: true });
  assert.equal(saves, noops, 'same-column and cancelled drags must not save');
  const noUndo = reverts;
  await page.keyboard.press(undoKey);
  await page.waitForTimeout(150);
  assert.equal(reverts, noUndo, 'cancelled drags must not add history');

  // Undo while saving must be queued rather than lost or applied to an older
  // entry. Hold the real network request until after the key has been pressed.
  let release;
  let received;
  const gate = new Promise((resolve) => { release = resolve; });
  const started = new Promise((resolve) => { received = resolve; });
  await page.route(`**${rowPath}`, async (route) => {
    if (route.request().method() === 'PATCH') { received(); await gate; }
    await route.continue();
  });
  await drag('todo', 'done');
  await started;
  await page.keyboard.press(undoKey);
  release();
  await expectStatus('todo');
  await cardIn('todo').waitFor();
  await page.unroute(`**${rowPath}`);

  // A rejected save restores server truth and contributes no undo entry.
  await page.route(`**${rowPath}`, async (route) => {
    if (route.request().method() === 'PATCH') {
      await route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"Test save failure"}' });
    } else await route.continue();
  });
  await drag('todo', 'done');
  await page.locator('.toast').filter({ hasText: 'Change not saved' }).waitFor();
  await cardIn('todo').waitFor();
  await expectStatus('todo');
  const failedSaveUndo = reverts;
  await page.keyboard.press(undoKey);
  await page.waitForTimeout(150);
  assert.equal(reverts, failedSaveUndo);
  await page.unroute(`**${rowPath}`);

  // A rejected undo remains retryable, with no optimistic false restoration.
  await drag('todo', 'done');
  await expectStatus('done');
  await page.route('**/api/audit/*/revert', (route) => route.abort());
  await page.keyboard.press(undoKey);
  await page.locator('.toast').filter({ hasText: 'That could not be taken back.' }).waitFor();
  await expectStatus('done');
  await page.unroute('**/api/audit/*/revert');
  await page.keyboard.press(undoKey);
  await expectStatus('todo');
  await cardIn('todo').waitFor();

  await drag('todo', 'done');
  await expectStatus('done');
  await api('PATCH', rowPath, { propsPatch: { status: 'third', labels: ['third'] } });
  await page.keyboard.press(undoKey);
  await page.locator('.toast').filter({ hasText: 'Nothing was taken back: every value has been changed since.' }).waitFor();
  assert.deepEqual(await props(), { status: 'third', labels: ['third'] }, 'later edits survive undo');

  // An absent status and a multi-select must recover their precise old shape.
  await reset(null);
  await drag('__unset__', 'todo');
  await expectStatus('todo');
  await page.keyboard.press(undoKey);
  await eventually(async () => !('status' in await props()));
  await cardIn('__unset__').waitFor();
  await reset('todo', [{ id: 'board', name: 'Board', type: 'board', groupBy: 'labels' }]);
  await drag('todo', 'third');
  await eventually(async () => JSON.stringify((await props()).labels) === '["third"]');
  await page.keyboard.press(undoKey);
  await eventually(async () => JSON.stringify((await props()).labels) === '["todo","done"]');

  await reset('todo', [{ id: 'board', name: 'Board', type: 'board', groupBy: 'status',
    filters: [{ property: 'status', op: 'is', value: 'todo' }] }]);
  await drag('todo', 'done');
  await expectStatus('done');
  await eventually(async () => await board.locator('.board-card').filter({ hasText: 'First row' }).count() === 0);
  await page.keyboard.press(undoKey);
  await expectStatus('todo');
  await cardIn('todo').waitFor();

  // Modal ownership must hold even when a button, rather than a text field,
  // owns focus. A reload clears browser history but preserves the saved move.
  await reset();
  await drag('todo', 'done');
  await expectStatus('done');
  await page.getByRole('button', { name: 'Properties', exact: true }).click();
  const blocked = reverts;
  await page.keyboard.press(undoKey);
  await page.waitForTimeout(150);
  assert.equal(reverts, blocked, 'modal owns the keyboard');
  await page.keyboard.press('Escape');
  await page.reload();
  await cardIn('done').waitFor();
  await page.mouse.click(1800, 30);
  await page.keyboard.press(undoKey);
  await page.waitForTimeout(150);
  assert.equal(reverts, blocked, 'reload clears undo history');
  await expectStatus('done');

  // Two boards embedded together: the last interacted board owns undo, and
  // the text editor must receive its own undo key while it has the caret.
  await reset();
  const second = await api('POST', '/api/pages', { title: 'Other board', type: 'collection', workspaceId: workspace.id });
  await api('PUT', `/api/collections/${second.id}`, { schema,
    views: [{ id: 'board', name: 'Board', type: 'board', groupBy: 'status' }] });
  const otherRow = await api('POST', '/api/pages', { parentId: second.id, title: 'First row', props: { status: 'todo' } });
  const doc = await api('POST', '/api/pages', { title: 'Embedded undo', workspaceId: workspace.id });
  await api('PATCH', `/api/pages/${doc.id}`, { content: [
    { id: 'notes', type: 'paragraph', props: {}, content: [{ type: 'text', text: 'Notes', styles: {} }], children: [] },
    { id: 'a', type: 'database', props: { collectionId }, content: [], children: [] },
    { id: 'b', type: 'database', props: { collectionId: second.id }, content: [], children: [] },
  ] });
  await page.goto(`${base}/p/${doc.id}`);
  await eventually(async () => await board.count() === 2);
  await menuMove('todo', 'Done', board.nth(0));
  await expectStatus('done');
  await menuMove('todo', 'Third', board.nth(1));
  await eventually(async () => (await api('GET', `/api/pages/${otherRow.id}`)).props.status === 'third');
  await page.keyboard.press(undoKey);
  await eventually(async () => (await api('GET', `/api/pages/${otherRow.id}`)).props.status === 'todo');
  await expectStatus('done');
  await board.nth(0).locator('[data-col="done"] .board-col-head-left').click();
  assert.equal(await page.evaluate(() => document.activeElement.isContentEditable), false,
    'clicking an embedded board must move focus out of the document editor');
  await page.keyboard.press(undoKey);
  await expectStatus('todo');
  await menuMove('todo', 'Done', board.nth(0));
  await expectStatus('done');
  const text = page.locator('[data-id="notes"] .bn-inline-content');
  await text.click();
  await page.keyboard.insertText(' typed');
  await eventually(async () => (await text.innerText()).includes('typed'));
  await page.evaluate(() => {
    window.boardUndoReachedEditor = false;
    document.activeElement.addEventListener('keydown', (event) => {
      if (event.code === 'KeyZ') window.boardUndoReachedEditor = !event.defaultPrevented;
    }, { capture: true });
  });
  const beforeTypingUndo = reverts;
  await page.keyboard.press(undoKey);
  assert.equal(await page.evaluate(() => window.boardUndoReachedEditor), true,
    'the undo key must reach the text editor without being prevented');
  assert.equal(reverts, beforeTypingUndo, 'text undo must not undo a board move');
  await expectStatus('done');
  await drag('done', 'third', { scope: board.nth(0) });
  await expectStatus('third');
  await page.keyboard.press(undoKey);
  await expectStatus('done');

  // Navigating away and back creates a fresh collection session. Exercise
  // physical Ctrl+Z as well as the host's normal modifier.
  await openBoard();
  await page.addInitScript(() => Object.defineProperty(navigator, 'platform', { get: () => 'Win32' }));
  await page.reload();
  await drag('done', 'todo');
  await expectStatus('todo');
  await page.keyboard.press('Control+z');
  await expectStatus('done');
  const afterNavigation = reverts;
  await page.keyboard.press('Control+z');
  await page.waitForTimeout(150);
  assert.equal(reverts, afterNavigation, 'leaving a collection clears its earlier moves');

  console.log('Board undo: pointer/menu moves, queued undo, failures, conflicts, typed values, filters, modal/editor ownership, embedded boards and session reset passed.');
});
