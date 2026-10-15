import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { withFixture, eventually } from './fixture.mjs';

await withFixture(async ({ page, context, api, base, workspace }) => {
  const password = randomUUID();
  const recipient = await api('POST', '/api/users', {
    name: 'Mario Rossi', email: 'mario@example.test', password,
    workspaces: [{ id: workspace.id, role: 'member' }],
  });
  const doc = await api('POST', '/api/pages', { title: 'Mention review', workspaceId: workspace.id });
  await page.goto(`${base}/p/${doc.id}`);
  await page.getByRole('button', { name: 'Show comments', exact: true }).click();
  const box = page.getByRole('textbox', { name: 'Write a comment…' });
  await box.fill('@Mar');
  await page.getByRole('option', { name: 'Mario Rossi' }).click();
  assert.equal(await box.inputValue(), '@Mario Rossi ');
  await box.fill('👋 ' + await box.inputValue());
  await box.fill(await box.inputValue() + 'puoi verificare?');
  // LAN HTTP does not expose randomUUID; getRandomValues remains available.
  await page.evaluate(() => Object.defineProperty(crypto, 'randomUUID', { value: undefined, configurable: true }));
  let dropped = false;
  const path = `**/api/pages/${doc.id}/comments`;
  await page.route(path, async route => {
    if (route.request().method() === 'POST' && !dropped) {
      dropped = true;
      await route.fetch();
      await route.abort('connectionreset');
    } else await route.continue();
  });
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  await eventually(async () => dropped && await box.isEnabled());
  assert.match(await box.inputValue(), /puoi verificare/);
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  await eventually(async () => (await box.inputValue()) === '');
  await page.unroute(path);
  const comments = await api('GET', `/api/pages/${doc.id}/comments`);
  assert.equal(comments.length, 1);
  assert.equal(comments[0].mentions[0].userId, recipient.id);
  assert.equal(comments[0].mentions[0].start, 3);
  assert.equal(await page.locator('.person-mention').innerText(), '@Mario Rossi');
  await api('POST', `/api/comments/${comments[0].id}/resolve`, { resolved: true });

  const other = await context.browser().newContext({ viewport: { width: 1200, height: 900 }, locale: 'en-US' });
  try {
    const login = await other.request.post(`${base}/api/login`, { data: { email: 'mario@example.test', password } });
    assert(login.ok(), await login.text());
    const receiver = await other.newPage();
    await receiver.goto(base);
    const inbox = () => other.request.get(`${base}/api/notifications`).then(r => r.json());
    let notifications = await inbox();
    assert.equal(notifications.unread, 1);
    assert.equal(notifications.items.length, 1);
    await receiver.getByRole('button', { name: /Notifications/ }).click();
    await receiver.getByRole('dialog', { name: 'Notifications' }).waitFor();
    await receiver.locator('.notification-item').click();
    await receiver.locator('.cp-item.is-mentioned').waitFor();
    assert.match(await receiver.locator('.cp-item.is-mentioned').innerText(), /puoi verificare/);
    assert.equal(await receiver.locator('.person-mention').innerText(), '@Mario Rossi');
    assert.equal((await inbox()).unread, 0);
    await receiver.reload();
    await receiver.getByRole('button', { name: /Notifications/ }).click();
    await receiver.locator('.notification-item').waitFor();
    assert.equal(await receiver.locator('.notification-item.is-unread').count(), 0);
    if (process.env.MENTION_SCREENSHOT) await receiver.screenshot({ path: process.env.MENTION_SCREENSHOT });
    await api('PATCH', `/api/pages/${doc.id}`, { visibility: 'private' });
    notifications = await inbox();
    assert.equal(notifications.items.length, 0);
    assert.equal(notifications.unread, 0);
  } finally { await other.close(); }
  console.log('Mentions: Unicode offsets, selected account, failed-response retry, one notification, resolved-comment navigation, read persistence and revoked access.');
});
