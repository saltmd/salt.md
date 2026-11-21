import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { eventually, withFixture } from './fixture.mjs';

await withFixture(async ({ page, api, base, workspace, path }) => {
  page.setDefaultTimeout(15000);
  const parent = await api('POST', '/api/pages', { title: 'Project notes', workspaceId: workspace.id });
  const notes = [{ id: 'notes', type: 'paragraph', content: [{ type: 'text', text: 'Keep these notes', styles: {} }] }];
  await api('PATCH', `/api/pages/${parent.id}`, { content: notes });
  await page.goto(`${base}/p/${parent.id}`);
  await page.locator('[data-id="notes"] .bn-inline-content').waitFor();
  const list = page.locator('.child-pages');
  assert.equal(await list.count(), 0, 'childless pages must not show an empty automatic section');

  // Tree changes reach the body without a reload or a document write.
  const first = await api('POST', '/api/pages', { title: 'First proposal draft', parentId: parent.id });
  const second = await api('POST', '/api/pages', { title: 'Second proposal draft', parentId: parent.id });
  await api('POST', '/api/pages', { title: 'Grandchild stays on its own parent', parentId: first.id });
  await list.getByRole('button', { name: second.title, exact: true }).waitFor();
  assert.equal(await list.locator('.bn-toc-title').count(), 0, 'automatic children must be page entries without a Sub-pages heading');
  assert.equal(await list.locator('.child-page-link svg').count(), 2, 'each page entry needs a page icon');
  assert.deepEqual((await list.locator('button').allTextContents()).map((title) => title.trim()), [first.title, second.title]);
  assert.equal(await list.getByText('Grandchild stays on its own parent').count(), 0);
  await api('PATCH', `/api/pages/${second.id}`, { position: 0, title: 'Second draft renamed' });
  await eventually(async () => (await list.locator('button').allTextContents())[0]?.trim() === 'Second draft renamed');
  await page.getByRole('button', { name: 'Show structure', exact: true }).click();
  assert.equal(await list.count(), 1, 'opening Structure must not hide the body list');
  await page.getByRole('button', { name: 'Hide structure', exact: true }).first().click();

  await list.getByRole('button', { name: first.title, exact: true }).focus();
  await page.keyboard.press('Enter');
  await eventually(() => page.url() === `${base}/p/${first.id}`);
  await page.goto(`${base}/p/${parent.id}`);
  await list.getByRole('button', { name: first.title, exact: true }).waitFor();
  assert.equal(await page.locator('[data-id="notes"] .bn-inline-content').textContent(), 'Keep these notes');
  assert.deepEqual((await api('GET', `/api/pages/${parent.id}`)).content, notes,
    'displaying automatic subpages must preserve stored content');

  // A manually placed list, even nested, takes precedence. Removing it brings
  // back the automatic list; no migration or saved placeholder is needed.
  await api('PATCH', `/api/pages/${parent.id}`, { content: [
    { ...notes[0], children: [{ id: 'manual-list', type: 'subpages' }] },
  ] });
  await page.reload();
  await page.locator('[data-id="manual-list"] .bn-subpages').waitFor();
  await eventually(async () => await list.count() === 0);
  assert.equal(await page.locator('.bn-subpages').count(), 1);
  await api('PATCH', `/api/pages/${parent.id}`, { content: notes });
  await page.reload();
  await list.getByRole('button', { name: first.title, exact: true }).waitFor();

  // Collection rows behave like documents; the collection itself retains its
  // existing table instead of acquiring a second row listing.
  const collectionId = path.split('/').at(-1);
  const task = await api('POST', '/api/pages', { title: 'Parent task', parentId: collectionId });
  const child = await api('POST', '/api/pages', { title: 'Task child', parentId: task.id });
  await page.goto(`${base}/p/${task.id}`);
  await list.getByRole('button', { name: child.title, exact: true }).waitFor();
  await page.reload();
  await list.getByRole('button', { name: child.title, exact: true }).waitFor();
  await page.goto(`${base}/p/${collectionId}`);
  await page.locator('.view-tab').first().waitFor();
  assert.equal(await list.count(), 0);

  // Moving/trashing children removes them live, including the final section.
  await page.goto(`${base}/p/${parent.id}`);
  await list.getByRole('button', { name: first.title, exact: true }).waitFor();
  await api('PATCH', `/api/pages/${second.id}`, { parentId: task.id });
  await eventually(async () => await list.locator('button').count() === 1);
  await api('DELETE', `/api/pages/${first.id}`);
  await eventually(async () => await list.count() === 0);

  // Empty parents and narrow screens must still make children easy to find.
  await api('POST', '/api/pages', { title: 'A longer proposal title that wraps on a narrow screen', parentId: task.id });
  await page.goto(`${base}/p/${task.id}`);
  await list.getByRole('button', { name: child.title, exact: true }).waitFor();
  await mkdir(new URL('../../data/checks/', import.meta.url), { recursive: true });
  await page.screenshot({ path: new URL('../../data/checks/subpages-desktop.png', import.meta.url).pathname });
  await page.getByRole('button', { name: /Collapse the sidebar/ }).click();
  await page.mouse.move(380, 800);
  await page.setViewportSize({ width: 390, height: 844 });
  await eventually(async () => {
    const sidebar = await page.locator('.sidebar').boundingBox();
    return sidebar && sidebar.x + sidebar.width <= 1;
  });
  await list.scrollIntoViewIfNeeded();
  const box = await list.boundingBox();
  assert.ok(box && box.x >= 0 && box.x + box.width <= 390, 'the child list must fit a narrow page');
  await page.screenshot({ path: new URL('../../data/checks/subpages-mobile.png', import.meta.url).pathname });
  assert.ok(!(await api('GET', `/api/pages/${task.id}`)).content.some((block) => block.type === 'subpages'));
  console.log('Automatic subpages: documents/tasks, direct children, live rename/order/move/trash, keyboard navigation, manual-list deduplication, unchanged content and narrow layout.');
});
