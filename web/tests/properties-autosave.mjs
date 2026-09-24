import assert from 'node:assert/strict';
import { eventually, withFixture } from './fixture.mjs';

await withFixture(async ({ page, path, open, read, assertRows }) => {
  await open();
  const field = page.locator('.schema-row .prop-input').first();
  const heading = page.getByRole('heading', { name: 'Collection properties' });
  const close = page.getByRole('button', { name: 'Close', exact: true });
  await field.fill('Renamed status');
  await eventually(async () => (await read()).schema[0].name === 'Renamed status');
  await field.fill('Immediately closed');
  await close.click();
  await heading.waitFor({ state: 'hidden' });
  assert.equal((await read()).schema[0].name, 'Immediately closed');
  await page.reload();
  await open();

  // Hold the first write, then edit while Close is awaiting it. The second
  // draft must be drained too, even before its debounce timer has fired.
  let release;
  let started;
  const gate = new Promise(resolve => { release = resolve; });
  const began = new Promise(resolve => { started = resolve; });
  let first = true;
  await page.route(`**${path}`, async route => {
    if (route.request().method() === 'PUT' && first) {
      first = false;
      started();
      await gate;
    }
    await route.continue();
  });
  await field.fill('First draft');
  await close.click();
  await began;
  await field.fill('Latest draft');
  release();
  await heading.waitFor({ state: 'hidden' });
  assert.equal((await read()).schema[0].name, 'Latest draft');
  await page.unroute(`**${path}`);

  // Saving failure must not silently close the dialog or report success.
  await open();
  await page.route(`**${path}`, route => route.request().method() === 'PUT'
    ? route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"Test failure"}' })
    : route.continue());
  await field.fill('Retry draft');
  await page.getByRole('button', { name: 'Retry', exact: true }).waitFor();
  await close.click();
  await heading.waitFor();
  assert.equal((await read()).schema[0].name, 'Latest draft');
  await field.fill('Latest draft');
  await close.click();
  await heading.waitFor({ state: 'hidden' });
  await page.getByText('Latest draft', { exact: true }).waitFor();
  await open();
  await field.fill('Retry draft');
  await page.getByRole('button', { name: 'Retry', exact: true }).waitFor();
  await page.unroute(`**${path}`);
  await page.getByRole('button', { name: 'Retry', exact: true }).click();
  await eventually(async () => (await read()).schema[0].name === 'Retry draft');

  await page.locator('.schema-row button[title="Delete property"]').last().click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  assert.equal((await read()).schema.length, 2);
  await page.locator('.schema-row button[title="Delete property"]').last().click();
  await dialog.getByRole('button', { name: 'Delete property', exact: true }).click();
  await eventually(async () => (await read()).schema.length === 1);
  await assertRows();
  // Deleting and recreating a property must not restore removed view settings.
  await page.locator('.schema-row button[title="Delete property"]').first().click();
  await dialog.getByRole('button', { name: 'Delete property', exact: true }).click();
  await eventually(async () => (await read()).schema.length === 0);
  await page.getByPlaceholder('New property name', { exact: true }).fill('Status');
  await page.getByRole('button', { name: 'Add', exact: true }).click();
  await eventually(async () => (await read()).schema.length === 1);
  assert.deepEqual((await read()).views.find(view => view.id === 'table').filters, []);
  await close.click();
  await heading.waitFor({ state: 'hidden' });
});
console.log('Properties autosave browser regressions passed.');
