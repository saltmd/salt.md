import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { withFixture, eventually } from './fixture.mjs';

await withFixture(async ({ page, context, api, base, workspace }) => {
  const doc = await api('POST', '/api/pages', { title: 'Inline checkbox test', workspaceId: workspace.id });
  await api('PATCH', `/api/pages/${doc.id}`, { content: [{ id: 'grid', type: 'table', props: {}, content: {
    type: 'tableContent', rows: [
      { cells: [[{ type: 'text', text: 'Task', styles: {} }], [{ type: 'text', text: 'Done', styles: {} }]] },
      { cells: [[{ type: 'text', text: 'Review brief', styles: {} }], []] },
    ],
  }, children: [] }] });
  await page.goto(`${base}/p/${doc.id}`);
  const cell = page.locator('[data-id="grid"] tr').nth(1).locator('td').nth(1);
  await cell.click();
  const errors = [];
  page.on('console', message => { if (message.type() === 'error') errors.push(message.text()); });
  await page.keyboard.type('/');
  const menu = page.locator('.bn-suggestion-menu');
  await menu.waitFor();
  const headings = () => menu.locator('.bn-suggestion-menu-label').allTextContents();
  const allGroups = await headings();
  assert.equal(new Set(allGroups).size, allGroups.length, 'Each group appears once in the full menu');
  await page.keyboard.type('ch');
  await eventually(async () => (await headings()).length === 1);
  assert.deepEqual(await headings(), ['Basic blocks']);
  assert(await menu.getByText('Check List', { exact: true }).isVisible());
  assert(await menu.getByText('Inline checkbox', { exact: true }).isVisible());
  await page.keyboard.press('Backspace');
  await page.keyboard.press('Backspace');
  await eventually(async () => (await headings()).length === allGroups.length);
  assert.equal(new Set(await headings()).size, allGroups.length);
  await page.keyboard.type('zzzznomatch');
  await eventually(async () => (await headings()).length === 0);
  for (let index = 0; index < '/zzzznomatch'.length; index++) await page.keyboard.press('Backspace');
  await page.keyboard.type('/checkbox');
  await eventually(async () => (await headings()).length === 1);
  assert.deepEqual(await headings(), ['Basic blocks']);
  assert(!errors.some(error => /same key|unique.*key/i.test(error)), 'No duplicate React keys in suggestion headings');
  await page.locator('.bn-suggestion-menu').getByText('Inline checkbox', { exact: true }).click();
  const checkbox = cell.getByRole('checkbox', { name: 'Inline checkbox', exact: true });
  await checkbox.waitFor();
  assert.equal(await checkbox.isChecked(), false);
  const read = () => api('GET', `/api/pages/${doc.id}`);
  const checked = async () => JSON.stringify((await read()).content).includes('"checked":true');
  await page.waitForTimeout(650);
  await checkbox.click();
  assert.equal(await checkbox.isChecked(), true);
  await eventually(checked);
  const saved = await read();
  assert.equal(saved.content.length, 1, 'No checklist block should be inserted outside the table');
  assert.equal(saved.content[0].type, 'table');
  await page.keyboard.press('Meta+z');
  await eventually(async () => !(await checkbox.isChecked()));
  await page.keyboard.press('Meta+Shift+z');
  await eventually(async () => checkbox.isChecked());
  await eventually(checked);
  await page.reload();
  await checkbox.waitFor();
  assert.equal(await checkbox.isChecked(), true);
  const peer = await context.newPage();
  await peer.goto(`${base}/p/${doc.id}`);
  const peerCheckbox = peer.getByRole('checkbox', { name: 'Inline checkbox', exact: true });
  await peerCheckbox.waitFor();
  await checkbox.click();
  await eventually(async () => !(await peerCheckbox.isChecked()));
  await checkbox.click();
  await eventually(async () => peerCheckbox.isChecked());
  await eventually(checked);
  for (const format of ['md', 'html']) {
    const response = await context.request.get(`${base}/api/export/${doc.id}?format=${format}`);
    assert(response.ok());
    assert((await response.text()).includes('☑'));
  }
  if (process.env.INLINE_CHECKBOX_SCREENSHOT) await page.screenshot({ path: process.env.INLINE_CHECKBOX_SCREENSHOT });
  const password = randomUUID();
  await api('POST', '/api/users', { name: 'Reader', email: 'checkbox-reader@example.test', password,
    workspaces: [{ id: workspace.id, role: 'viewer' }] });
  const viewer = await context.browser().newContext({ locale: 'en-US' });
  try {
    const login = await viewer.request.post(`${base}/api/login`, { data: { email: 'checkbox-reader@example.test', password } });
    assert(login.ok());
    const reader = await viewer.newPage();
    await reader.goto(`${base}/p/${doc.id}`);
    const readOnlyCheckbox = reader.getByRole('checkbox', { name: 'Inline checkbox', exact: true });
    await readOnlyCheckbox.waitFor();
    assert.equal(await readOnlyCheckbox.isDisabled(), true);
    assert.equal(await readOnlyCheckbox.isChecked(), true);
  } finally { await viewer.close(); }
  await peer.close();
  console.log('Inline checkbox: insertion inside table, saved toggle, undo/redo, reload, collaboration and Markdown/HTML export passed.');
});
