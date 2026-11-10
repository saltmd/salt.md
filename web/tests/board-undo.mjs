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
  const revertIds = [];
  page.on('request', (r) => {
    if (r.method() === 'PATCH' && r.url().endsWith(rowPath)) saves++;
    const m = r.method() === 'POST' && r.url().match(/\/api\/audit\/(\d+)\/revert$/);
    if (m) { reverts++; revertIds.push(Number(m[1])); }
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

  // Two presses, and the first fails for a reason a retry can fix: the
  // second, meant for the older move, is cancelled rather than run against a
  // card the newer move still holds (where it would be skipped and lost).
  await drag('todo', 'third');
  await expectStatus('third');
  await drag('third', 'done');
  await expectStatus('done');
  let releaseRevert;
  const revertGate = new Promise((resolve) => { releaseRevert = resolve; });
  let failedOnce = false;
  await page.route('**/api/audit/*/revert', async (route) => {
    if (failedOnce) return route.continue();
    failedOnce = true;
    await revertGate;
    await route.fulfill({ status: 503, contentType: 'application/json', body: '{"error":"Temporarily unavailable"}' });
  });
  const beforeFailure = reverts;
  await page.keyboard.press(undoKey);
  await page.keyboard.press(undoKey);
  await eventually(async () => reverts === beforeFailure + 1);
  releaseRevert();
  await page.locator('.toast').filter({ hasText: 'That could not be taken back.' }).last().waitFor();
  await page.waitForTimeout(300);
  assert.equal(reverts, beforeFailure + 1, 'the press queued behind a failed undo is cancelled');
  await expectStatus('done');
  await page.unroute('**/api/audit/*/revert');
  await page.keyboard.press(undoKey);
  await expectStatus('third');
  await page.keyboard.press(undoKey);
  await expectStatus('todo');
  await cardIn('todo').waitFor();

  // An undo pressed while a move is saving belongs to that move. If the save
  // then fails, the undo is dropped instead of reverting the move before it.
  await drag('todo', 'third');
  await expectStatus('third');
  let releaseFail;
  let failStarted;
  const failGate = new Promise((resolve) => { releaseFail = resolve; });
  const failReceived = new Promise((resolve) => { failStarted = resolve; });
  await page.route(`**${rowPath}`, async (route) => {
    if (route.request().method() !== 'PATCH') return route.continue();
    failStarted();
    await failGate;
    await route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"Test save failure"}' });
  });
  await drag('third', 'done');
  await failReceived;
  const beforeFailedMoveUndo = reverts;
  await page.keyboard.press(undoKey);
  releaseFail();
  await page.locator('.toast').filter({ hasText: 'Change not saved' }).waitFor();
  await page.waitForTimeout(300);
  await page.unroute(`**${rowPath}`);
  assert.equal(reverts, beforeFailedMoveUndo, 'an undo for a failed save must not revert an older move');
  await expectStatus('third');
  // The older move stays on the stack for the next deliberate press.
  await page.keyboard.press(undoKey);
  await expectStatus('todo');
  await cardIn('todo').waitFor();

  // A permanent refusal leaves the stack, so older moves stay reachable.
  for (const status of [403, 404]) {
    await drag('todo', 'third');
    await expectStatus('third');
    await drag('third', 'done');
    await expectStatus('done');
    let refusedAnswered;
    const answered = new Promise((resolve) => { refusedAnswered = resolve; });
    await page.route('**/api/audit/*/revert', async (route) => {
      await route.fulfill({ status, contentType: 'application/json', body: '{"error":"Test refusal","code":"forbidden"}' });
      refusedAnswered();
    });
    const beforeRefusal = revertIds.length;
    await page.keyboard.press(undoKey);
    await answered;
    await page.waitForTimeout(150);
    assert.equal(revertIds.length, beforeRefusal + 1);
    const refused = revertIds.at(-1);
    await page.unroute('**/api/audit/*/revert');
    await api('PATCH', rowPath, { propsPatch: { status: 'third' } });
    await page.keyboard.press(undoKey);
    await eventually(async () => revertIds.at(-1) !== refused);
    assert(revertIds.at(-1) < refused, `after a ${status}, undo moves on to the older move`);
    await expectStatus('todo');
    await cardIn('todo').waitFor();
  }

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

  // An undo that answers after the reader switched views reloads the view
  // shown then: a failure must not paint the old view's filter into it.
  await reset('todo', [
    { id: 'board', name: 'Board', type: 'board', groupBy: 'status', filters: [{ property: 'status', op: 'is', value: 'todo' }] },
    { id: 'all', name: 'All rows', type: 'board', groupBy: 'status' },
  ]);
  await drag('todo', 'done');
  await expectStatus('done');
  let releaseLate;
  const lateGate = new Promise((resolve) => { releaseLate = resolve; });
  let lateStarted;
  const lateReceived = new Promise((resolve) => { lateStarted = resolve; });
  await page.route('**/api/audit/*/revert', async (route) => {
    lateStarted();
    await lateGate;
    await route.fulfill({ status: 503, contentType: 'application/json', body: '{"error":"Temporarily unavailable"}' });
  });
  await page.keyboard.press(undoKey);
  await lateReceived;
  await page.locator('.view-tab', { hasText: 'All rows' }).click();
  await board.locator('.board-card').filter({ hasText: 'Second row' }).waitFor();
  releaseLate();
  await page.locator('.toast').filter({ hasText: 'That could not be taken back.' }).last().waitFor();
  await page.waitForTimeout(400);
  await page.unroute('**/api/audit/*/revert');
  assert.equal(await board.locator('.board-card').filter({ hasText: 'Second row' }).count(), 1, 'the view shown keeps its rows');
  assert.equal(await board.locator('.board-card').filter({ hasText: 'First row' }).count(), 1);

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

  // A text field the reader was in (the page title) does not keep the focus
  // through a drag: Ctrl+Z afterwards takes back the move, not the typing.
  await reset();
  await page.locator('textarea.page-title').click();
  await drag('todo', 'done');
  await expectStatus('done');
  await page.keyboard.press(undoKey);
  await expectStatus('todo');
  await cardIn('todo').waitFor();
  await drag('todo', 'done');
  await expectStatus('done');

  // A board that is a page of its own leaves mouse presses alone, so card
  // text stays selectable and pickers keep their focus.
  await page.evaluate(() => {
    window.boardPressPrevented = null;
    window.addEventListener('pointerdown', (e) => { window.boardPressPrevented = e.defaultPrevented; }, { once: true });
  });
  await board.locator('[data-col="done"] .board-col-head-left').click();
  assert.equal(await page.evaluate(() => window.boardPressPrevented), false,
    'a standalone board must not cancel mouse presses');

  // A press inside a card's picker belongs to the picker: dragging from it
  // into another column must not move the card.
  await cardIn('done').locator('.card-prop-edit .select-cell-value').first().click();
  const picker = page.locator('.select-menu');
  await picker.waitFor();
  const from = await picker.boundingBox();
  const into = await board.locator('[data-col="todo"]').boundingBox();
  const pickerSaves = saves;
  await page.mouse.move(from.x + 3, from.y + 3);
  await page.mouse.down();
  await page.mouse.move(into.x + 80, into.y + 85, { steps: 12 });
  await page.mouse.up();
  await page.waitForTimeout(200);
  assert.equal(await board.locator('.board-drag-ghost').count(), 0, 'a picker press must not start a drag');
  assert.equal(saves, pickerSaves, 'a picker press must not move the card');
  await expectStatus('done');
  await page.keyboard.press('Escape');
  await picker.waitFor({ state: 'hidden' }).catch(() => page.locator('.select-backdrop').click());

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
