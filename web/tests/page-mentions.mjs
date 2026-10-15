import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { withFixture, eventually } from './fixture.mjs';

await withFixture(async ({ page, context, api, base, workspace }) => {
  page.setDefaultTimeout(15000);
  const password = randomUUID();
  const recipient = await api('POST', '/api/users', { name: 'Mario Rossi', email: 'page-mario@example.test', password,
    workspaces: [{ id: workspace.id, role: 'member' }] });
  const doc = await api('POST', '/api/pages', { title: 'Page mentions', workspaceId: workspace.id });
  const linked = await api('POST', '/api/pages', { title: 'Reference brief', workspaceId: workspace.id });
  await api('PATCH', `/api/pages/${doc.id}`, { content: [{ id: 'intro', type: 'paragraph', props: {}, content: [{ type: 'text', text: 'Review: ', styles: {} }], children: [] }] });
  await page.goto(`${base}/p/${doc.id}`);
  const paragraph = page.locator('[data-id="intro"] .bn-inline-content');
  await paragraph.click();
  await page.keyboard.press('End');
  await page.keyboard.type('@Mario');
  await page.locator('.bn-suggestion-menu').getByText('Mario Rossi', { exact: true }).click();
  const chip = page.locator('.page-person-mention');
  await chip.waitFor();
  assert.equal(await chip.innerText(), '@Mario Rossi');
  await eventually(async () => JSON.stringify((await api('GET', `/api/pages/${doc.id}`)).content).includes('userMention'));
  const saved = await api('GET', `/api/pages/${doc.id}`);
  const node = saved.content[0].content.find(n => n.type === 'userMention');
  assert.equal(node.props.userId, recipient.id);
  assert.equal(node.props.originPageId, doc.id);
  const occurrence = node.props.mentionId;
  await page.keyboard.type('@Reference');
  await page.locator('.bn-suggestion-menu').getByText('Reference brief', { exact: true }).click();
  await page.locator('.page-link', { hasText: 'Reference brief' }).waitFor();
  await eventually(async () => JSON.stringify((await api('GET', `/api/pages/${doc.id}`)).content).includes(linked.id));
  await page.reload();
  await chip.waitFor();
  assert.equal(await chip.getAttribute('data-person-mention-id'), occurrence);
  if (process.env.PAGE_MENTION_SCREENSHOT) await page.screenshot({ path: process.env.PAGE_MENTION_SCREENSHOT });

  const other = await context.browser().newContext({ viewport: { width: 1200, height: 900 }, locale: 'en-US' });
  try {
    const response = await other.request.post(`${base}/api/login`, { data: { email: 'page-mario@example.test', password } });
    assert(response.ok());
    const inbox = () => other.request.get(`${base}/api/notifications`).then(r => r.json());
    await eventually(async () => (await inbox()).unread === 1);
    assert.equal((await inbox()).items.length, 1);
    const receiver = await other.newPage();
    await receiver.goto(base);
    await receiver.getByRole('button', { name: /Notifications/ }).click();
    await receiver.locator('.notification-item').click();
    await receiver.locator('.page-person-mention.is-mentioned').waitFor();
    assert.equal(await receiver.locator('.page-person-mention.is-mentioned').innerText(), '@Mario Rossi');
    assert.equal((await inbox()).unread, 0);
    const current = (await api('GET', `/api/pages/${doc.id}`)).content;
    await api('PATCH', `/api/pages/${doc.id}?materialize=1`, { content: current });
    await api('PATCH', `/api/pages/${doc.id}?materialize=1`, { content: current });
    assert.equal((await inbox()).items.length, 1);
    assert.equal((await inbox()).unread, 0);
    const noMention = structuredClone(current);
    noMention[0].content = noMention[0].content.filter(n => n.type !== 'userMention');
    await api('PATCH', `/api/pages/${doc.id}`, { content: noMention });
    assert.equal((await inbox()).items.length, 0);
    await api('PATCH', `/api/pages/${doc.id}`, { content: current });
    assert.equal((await inbox()).items.length, 1);
    assert.equal((await inbox()).unread, 0);
  } finally { await other.close(); }
  console.log('Page mentions: @ person selection, page links preserved, saved chip, recipient navigation, stable read state and removal/undo deduplication.');
});
