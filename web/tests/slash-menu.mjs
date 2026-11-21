import assert from 'node:assert/strict';
import { eventually, withFixture } from './fixture.mjs';

// The slash menu keys its headings by group, and our own entries came after
// BlockNote's, so a group could appear twice: "Basic blocks" three times on
// 1.1.1, and after typing "ch" and clearing it, headings that were left behind.
// Each group is one run of entries now, in the order it first appears.
await withFixture(async ({ page, api, base, workspace }) => {
  page.setDefaultTimeout(15000);
  const doc = await api('POST', '/api/pages', { title: 'Slash', workspaceId: workspace.id });
  await api('PATCH', `/api/pages/${doc.id}`, { content: [{ id: 'intro', type: 'paragraph', props: {}, content: [], children: [] }] });
  await page.goto(`${base}/p/${doc.id}`);
  await page.locator('[data-id="intro"] .bn-inline-content').click();
  await page.keyboard.type('/');
  const menu = page.locator('.bn-suggestion-menu');
  await menu.waitFor();
  const headings = () => menu.locator('.bn-suggestion-menu-label').allTextContents();
  const unique = (list) => new Set(list).size === list.length;

  const full = await headings();
  assert.ok(full.length > 1, `no groups at all: ${full}`);
  assert.ok(unique(full), `a group heading repeats: ${full}`);
  await page.keyboard.type('ch');
  await page.waitForTimeout(400);
  assert.ok(unique(await headings()), 'a heading repeats while filtering');
  await page.keyboard.press('Backspace');
  await page.keyboard.press('Backspace');
  await page.waitForTimeout(400);
  assert.deepEqual(await headings(), full, 'clearing the query left other headings than before');
});

await withFixture(async ({ page, api, base, workspace, path }) => {
  page.setDefaultTimeout(15000);
  const collectionId = path.split('/').at(-1);
  const document = await api('POST', '/api/pages', { title: 'Parent document', workspaceId: workspace.id });
  const row = await api('POST', '/api/pages', { title: 'Parent task', parentId: collectionId });
  const seed = (id) => [
    { id: `notes-${id}`, type: 'paragraph', content: [{ type: 'text', text: 'Keep these notes', styles: {} }] },
    { id: `children-${id}`, type: 'subpages' },
    { id: `command-${id}`, type: 'paragraph', content: [] },
  ];
  const openCommand = async (parent) => {
    await page.goto(`${base}/p/${parent.id}`);
    await page.locator(`[data-id="command-${parent.id}"] .bn-inline-content`).click();
    await page.keyboard.type('/page');
    await page.locator('.bn-suggestion-menu').getByText('Create a subpage', { exact: true }).waitFor();
    assert.equal(await page.locator('.bn-suggestion-menu').getByText('Sub-pages', { exact: true }).count(), 1,
      'the existing Sub-pages listing must remain available');
  };
  const creates = [];
  page.on('request', (request) => {
    if (request.method() === 'POST' && new URL(request.url()).pathname === '/api/pages') creates.push(request.postDataJSON());
  });

  // Documents and collection rows both parent the child; Enter and pointer
  // selection follow the same create-and-open path and focus the new title.
  for (const [parent, mouse] of [[document, false], [row, true]]) {
    await api('PATCH', `/api/pages/${parent.id}`, { content: seed(parent.id) });
    await openCommand(parent);
    if (mouse) await page.locator('.bn-suggestion-menu').getByText('Page', { exact: true }).click();
    else await page.keyboard.press('Enter');
    await eventually(async () => page.url() !== `${base}/p/${parent.id}`);
    const childId = page.url().split('/').at(-1);
    const child = await api('GET', `/api/pages/${childId}`);
    assert.equal(child.parentId, parent.id);
    assert.equal(child.workspaceId, workspace.id);
    assert.equal(child.type, 'doc');
    await eventually(() => page.locator('.page-title').evaluate((el) => el === document.activeElement));
    const title = `Child of ${parent.title}`;
    await page.keyboard.type(title);
    await eventually(async () => (await api('GET', `/api/pages/${childId}`)).title === title);
    await page.reload();
    assert.equal(await page.locator('.page-title').inputValue(), title);

    await page.goto(`${base}/p/${parent.id}`);
    await page.locator('.bn-subpages').getByText(title, { exact: true }).waitFor();
    assert.equal(await page.locator(`[data-id="notes-${parent.id}"] .bn-inline-content`).textContent(), 'Keep these notes');
    assert.equal(creates.filter((request) => request.parentId === parent.id).length, 1, 'one selection must create exactly one child');
  }

  // A server refusal is visible and leaves the user and their notes on the
  // parent. Reusing the shared callback must not bypass its error handling.
  const before = (await api('GET', '/api/pages')).length;
  await page.route('**/api/pages', async (route) => {
    if (route.request().method() !== 'POST') return route.continue();
    await route.fulfill({ status: 403, contentType: 'application/json', body: JSON.stringify({ error: 'Page creation denied for test' }) });
  });
  await openCommand(document);
  await page.keyboard.press('Enter');
  await page.locator('.toast').filter({ hasText: 'Page creation denied for test' }).waitFor();
  assert.equal(page.url(), `${base}/p/${document.id}`);
  assert.equal((await api('GET', '/api/pages')).length, before);
  assert.equal(await page.locator(`[data-id="notes-${document.id}"] .bn-inline-content`).textContent(), 'Keep these notes');

  // The action joins the editor's translated Basic blocks group rather than
  // introducing an English heading into a translated menu.
  for (const [language, title, description] of [
    ['de', 'Seite', 'Eine Unterseite erstellen'],
    ['es', 'Página', 'Crear una subpágina'],
    ['zh-Hans', '页面', '创建子页面'],
    ['zh-Hant', '頁面', '建立子頁面'],
  ]) {
    await api('PUT', '/api/me/prefs', { language });
    await page.goto(`${base}/p/${document.id}`);
    await page.locator(`[data-id="command-${document.id}"] .bn-inline-content`).click();
    await page.keyboard.type('/page');
    const menu = page.locator('.bn-suggestion-menu');
    await menu.getByText(description, { exact: true }).waitFor();
    await menu.getByText(title, { exact: true }).waitFor();
    assert.ok(!(await menu.locator('.bn-suggestion-menu-label').allTextContents()).includes('Basic blocks'),
      `${language}: the Page action introduced an English group`);
  }
  console.log('Page command: document/row parenting, inherited workspace, keyboard/mouse selection, title focus, persistence, visible creation refusal and translated menus.');
});
