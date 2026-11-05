import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { eventually, withFixture } from './fixture.mjs';

await withFixture(async ({ page, context, api, base, workspace }) => {
  page.setDefaultTimeout(12000);
  const errors = [];
  page.on('pageerror', (e) => errors.push(e.message));
  const password = randomUUID();
  await api('POST', '/api/users', { name: 'Review Colleague', email: 'colleague@example.test', password, workspaces: [{ id: workspace.id, role: 'member' }] });
  const peer = await context.browser().newContext({ viewport: { width: 1200, height: 900 }, locale: 'en-US' });
  try {
    const login = await peer.request.post(base + '/api/login', { data: { email: 'colleague@example.test', password } });
    assert(login.ok());
    const doc = await api('POST', '/api/pages', { title: 'Reply acceptance', workspaceId: workspace.id });
    const send = async (body, replyToId = '', requestId = randomUUID()) => {
      const res = await peer.request.post(`${base}/api/pages/${doc.id}/comments`, { data: { body, replyToId, requestId } });
      assert(res.ok(), await res.text()); return res.json();
    };
    const root = await send('Please add two more clips.');
    await page.goto(`${base}/p/${doc.id}`);
    await page.locator('[title="Show comments"]').click();
    await page.locator('.cp-item').filter({ hasText: 'Please add two more clips.' }).getByRole('button', { name: 'Reply', exact: true }).click();
    const draft = page.getByRole('textbox', { name: 'Write a reply…', exact: true });
    await draft.fill('Added two clips and tightened the opening.');
    // Closing/reopening both the inbox and panel must retain the reply draft.
    await page.getByRole('button', { name: 'Inbox', exact: true }).click();
    await page.getByRole('dialog', { name: 'Inbox', exact: true }).getByRole('button', { name: 'Close', exact: true }).click();
    assert.equal(await draft.inputValue(), 'Added two clips and tightened the opening.');
    await page.locator('.comments-panel').getByRole('button', { name: 'Close', exact: true }).click();
    await page.locator('[title="Show comments"]').click();
    assert.equal(await draft.inputValue(), 'Added two clips and tightened the opening.');
    // A failed submission retains text, target and stable retry key.
    await page.route('**/api/pages/*/comments', async (route) => {
      if (route.request().method() === 'POST') await route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: 'Temporary test failure' }) });
      else await route.continue();
    });
    await page.getByRole('button', { name: 'Send reply', exact: true }).click();
    await page.getByRole('alert').filter({ hasText: 'Temporary test failure' }).waitFor();
    assert.equal(await draft.inputValue(), 'Added two clips and tightened the opening.');
    await page.unroute('**/api/pages/*/comments');
    const other = await peer.newPage();
    other.on('pageerror', (e) => errors.push(e.message));
    await other.goto(`${base}/p/${doc.id}`);
    await other.getByRole('button', { name: 'Inbox', exact: true }).waitFor();
    await page.getByRole('button', { name: 'Send reply', exact: true }).click();
    await eventually(async () => (await api('GET', `/api/pages/${doc.id}/comments`)).length === 2);
    await other.locator('.inbox-badge').waitFor();
    assert.equal(await other.locator('.inbox-badge').innerText(), '1');
    await other.getByRole('button', { name: 'Inbox', exact: true }).click();
    await other.locator('.inbox-item').filter({ hasText: 'Added two clips' }).waitFor();
    let inbox = await (await peer.request.get(base + '/api/notifications')).json();
    assert.equal(inbox.unreadCount, 1, 'opening inbox alone does not mark read');
    await other.locator('.inbox-open').filter({ hasText: 'Added two clips' }).click();
    await other.locator('.cp-item.is-active').filter({ hasText: 'Added two clips' }).waitFor();
    await eventually(async () => (await (await peer.request.get(base + '/api/notifications')).json()).unreadCount === 0);
    assert.equal(await other.locator('.cp-thread').count(), 1);
    await other.reload();
    await other.locator('.cp-item.is-active').filter({ hasText: 'Added two clips' }).waitFor();
    assert.equal(await other.locator('.inbox-badge').count(), 0);
    // Replies missed while offline arrive when the browser comes back online, without a
    // tab focus or reload being needed to repair the conversation.
    await peer.setOffline(true);
    await api('POST', `/api/pages/${doc.id}/comments`, { body: 'Reply sent while the recipient was offline', replyToId: root.id });
    await peer.setOffline(false);
    await other.locator('.cp-body').filter({ hasText: 'Reply sent while the recipient was offline' }).waitFor();
    // Replying directly to a reply alerts its author, not the sender.
    const comments = await api('GET', `/api/pages/${doc.id}/comments`);
    const response = comments.find((c) => c.threadId === root.id);
    await other.locator(`[data-panel-comment="${response.id}"]`).getByRole('button', { name: 'Reply', exact: true }).click();
    await other.getByRole('textbox', { name: 'Write a reply…' }).fill('Perfect, thank you.');
    await other.getByRole('button', { name: 'Send reply', exact: true }).click();
    await page.locator('.inbox-badge').waitFor();
    assert.equal((await api('GET', '/api/notifications')).unreadCount, 1);
    // Resolve/reopen is thread-wide; exact links can reveal resolved replies.
    await page.getByRole('button', { name: 'Resolve thread', exact: true }).click();
    await eventually(async () => (await api('GET', `/api/pages/${doc.id}/comments`)).every((c) => c.resolvedAt));
    await page.getByRole('button', { name: 'Inbox', exact: true }).click();
    await page.locator('.inbox-open').filter({ hasText: 'Perfect, thank you.' }).click();
    await page.locator('.cp-item.is-active').filter({ hasText: 'Perfect, thank you.' }).waitFor();
    await page.getByRole('button', { name: 'Reopen', exact: true }).click();
    // Mark unread/read/all and its cross-tab state are server-owned.
    await page.getByRole('button', { name: 'Inbox', exact: true }).click();
    await page.getByRole('button', { name: 'All', exact: true }).click();
    await page.getByRole('button', { name: 'Mark as unread', exact: true }).first().click();
    await eventually(async () => (await api('GET', '/api/notifications')).unreadCount === 1);
    // Opening the same notification again after marking it unread must clear
    // it, even when its page and reply are already selected.
    await page.locator('.inbox-open').filter({ hasText: 'Perfect, thank you.' }).click();
    await eventually(async () => (await api('GET', '/api/notifications')).unreadCount === 0);
    await page.getByRole('button', { name: 'Inbox', exact: true }).click();
    await page.getByRole('button', { name: 'Mark as unread', exact: true }).first().click();
    await eventually(async () => (await api('GET', '/api/notifications')).unreadCount === 1);
    await page.getByRole('button', { name: 'Mark all as read', exact: true }).click();
    await eventually(async () => (await api('GET', '/api/notifications')).unreadCount === 0);
    await page.keyboard.press('Escape');
    assert.equal(await page.getByRole('dialog', { name: 'Inbox', exact: true }).count(), 0);
    // The entire inbox fits a phone and dark mode without horizontal overflow.
    await other.setViewportSize({ width: 390, height: 844 });
    await other.emulateMedia({ colorScheme: 'dark' });
    await other.getByRole('button', { name: 'Menu', exact: true }).click();
    await other.getByRole('button', { name: 'Inbox', exact: true }).click();
    const bounds = await other.locator('.inbox-dialog').boundingBox();
    assert(bounds.x >= 0 && bounds.x + bounds.width <= 391);
    for (const locale of ['de-DE', 'es-ES', 'zh-CN', 'zh-TW']) {
      const lang = await context.browser().newContext({ locale, viewport: { width: 1200, height: 900 } });
      try {
        assert((await lang.request.post(base + '/api/login', { data: { email: 'colleague@example.test', password } })).ok());
        const localized = await lang.newPage();
        localized.setDefaultTimeout(12000);
        localized.on('pageerror', (e) => errors.push(e.message));
        const note = await api('POST', '/api/pages', { title: 'Localized reply conflict', workspaceId: workspace.id });
        const initial = await api('POST', `/api/pages/${note.id}/comments`, { body: 'Language conflict root' });
        const openComments = await api('GET', `/api/pages/${note.id}/comments`);
        // Hold the visible list at its last open state while a second browser
        // resolves the thread. The POST still reaches the real backend.
        await localized.route(`**/api/pages/${note.id}/comments`, async (route) => {
          if (route.request().method() === 'GET') await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(openComments) });
          else await route.continue();
        });
        await localized.goto(`${base}/p/${note.id}#comment=${initial.id}`);
        await localized.locator('.cp-reply-action').first().click();
        await localized.locator('.cp-compose textarea').fill('Preserve this reply draft');
        await api('POST', `/api/comments/${initial.id}/resolve`, { resolved: true });
        await localized.locator('.cp-compose button[type=submit]').click();
        const translated = {
          'de-DE': 'Öffne diese Diskussion erneut, bevor du antwortest. Dein Entwurf bleibt erhalten.',
          'es-ES': 'Vuelve a abrir este hilo antes de responder. Tu borrador se conserva.',
          'zh-CN': '请先重新打开此讨论再回复。草稿已保留。',
          'zh-TW': '請先重新開啟此討論再回覆。草稿已保留。',
        }[locale];
        await localized.getByRole('alert').filter({ hasText: translated }).waitFor();
        const refreshed = localized.waitForResponse((r) => r.url().endsWith(`/api/pages/${note.id}/comments`) && r.request().method() === 'GET');
        await localized.evaluate(() => window.dispatchEvent(new Event('focus')));
        await refreshed;
        await localized.getByRole('alert').filter({ hasText: translated }).waitFor();
        assert.equal(await localized.locator('.cp-compose textarea').inputValue(), 'Preserve this reply draft');
      } finally { await lang.close(); }
    }
    assert.equal(errors.length, 0, errors.join('\n'));
    console.log('Replies and inbox: two-user live delivery, drafts, failed sends, exact links, unread state, resolve/reopen and mobile passed.');
  } finally { await peer.close(); }
});
