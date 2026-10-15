import assert from 'node:assert/strict';
import { eventually, withFixture } from './fixture.mjs';

await withFixture(async ({ page, context, api, base, workspace, path }) => {
  page.setDefaultTimeout(15000);
  const source = path.replace('/api/collections', '/p');
  const newPage = async () => {
    await page.getByRole('button', { name: 'New', exact: true }).click();
    await page.waitForURL(url => url.pathname !== source);
    const id = new URL(page.url()).pathname.split('/').pop();
    await page.locator('.page-title').waitFor();
    await page.locator('.bn-editor').waitFor();
    return id;
  };
  const first = await newPage();
  // Advance past a phone call. A page still open must not be discarded.
  await page.clock.install();
  await page.clock.fastForward(65000);
  assert.equal((await api('GET', `/api/pages/${first}`)).trashed, false);
  await page.goBack();
  await eventually(async () => (await api('GET', `/api/pages/${first}`)).trashed);

  // A pre-existing blank page is not a draft simply because it is Untitled.
  const old = await api('POST', '/api/pages', { title: 'Untitled', workspaceId: workspace.id });
  await page.goto(`${base}/p/${old.id}`);
  await page.locator('.page-title').waitFor();
  await page.goto(`${base}${source}`);
  assert.equal((await api('GET', `/api/pages/${old.id}`)).trashed, false);

  // Editing the title then leaving immediately beats the 500ms metadata debounce.
  const named = await newPage();
  await page.locator('.page-title').fill('Keep this TODO');
  await page.goBack();
  await eventually(async () => (await api('GET', `/api/pages/${named}`)).title === 'Keep this TODO');
  assert.equal((await api('GET', `/api/pages/${named}`)).trashed, false);

  const written = await newPage();
  await page.locator('.bn-editor').click();
  await page.keyboard.type('A note worth keeping');
  await page.goBack();
  await eventually(async () => JSON.stringify((await api('GET', `/api/pages/${written}`)).content)
    .includes('A note worth keeping'));
  assert.equal((await api('GET', `/api/pages/${written}`)).trashed, false);

  // Another editor holds a live connection: leaving this browser cannot discard it.
  const shared = await newPage();
  const other = await context.newPage();
  await other.goto(`${base}/p/${shared}`);
  await other.locator('.bn-editor').waitFor();
  await page.goBack();
  await other.locator('.page-title').fill('Edited in another tab');
  await eventually(async () => (await api('GET', `/api/pages/${shared}`)).title === 'Edited in another tab');
  assert.equal((await api('GET', `/api/pages/${shared}`)).trashed, false);
  await other.close();
  console.log('Empty pages: no inactivity deletion, discard only on leave, old pages retained, immediate title/body edits and another editor protected.');
});
