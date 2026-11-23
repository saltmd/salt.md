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
        // What the app sends on reconnect and on returning to a hidden window.
        await localized.evaluate(() => window.dispatchEvent(new Event('salt:comments-refresh')));
        await refreshed;
        await localized.getByRole('alert').filter({ hasText: translated }).waitFor();
        assert.equal(await localized.locator('.cp-compose textarea').inputValue(), 'Preserve this reply draft');
      } finally { await lang.close(); }
    }
    // An item in another workspace opens there in one history entry, and
    // Back returns both the page and the sidebar's workspace.
    await other.close();
    const second = await api('POST', '/api/workspaces', { name: 'Second space' });
    await api('POST', `/api/workspaces/${second.id}/members`, { email: 'colleague@example.test', role: 'member' });
    const away = await api('POST', '/api/pages', { title: 'Away page', workspaceId: second.id });
    const awayRoot = await api('POST', `/api/pages/${away.id}/comments`, { body: 'Question in the second space' });
    const awayReply = await peer.request.post(`${base}/api/pages/${away.id}/comments`, { data: { body: 'Answer in the second space', replyToId: awayRoot.id, requestId: randomUUID() } });
    assert(awayReply.ok());
    await page.setViewportSize({ width: 1200, height: 900 });
    await page.goto(`${base}/p/${doc.id}`);
    const switcher = page.locator('.ws-btn').first();
    await switcher.waitFor();
    const historyBefore = await page.evaluate(() => history.length);
    const homeName = (await switcher.innerText()).trim();
    await page.getByRole('button', { name: 'Inbox', exact: true }).click();
    await page.locator('.inbox-open').filter({ hasText: 'Answer in the second space' }).click();
    await page.locator('.cp-item.is-active').filter({ hasText: 'Answer in the second space' }).waitFor();
    assert.equal(await page.evaluate(() => history.length), historyBefore + 1, 'one history entry');
    await eventually(async () => (await switcher.innerText()).includes('Second space'));
    await page.goBack();
    await eventually(async () => new URL(page.url()).pathname === `/p/${doc.id}`);
    await eventually(async () => (await switcher.innerText()).trim() === homeName);
    // Open the other workspace's page through Search, switch the sidebar to
    // that workspace while the page stays, open an item from the first one,
    // and go Back: the sidebar returns to the workspace it was switched to.
    await page.locator('.sidebar-search:not(.inbox-trigger)').first().click();
    await page.locator('.search-input').fill('Away page');
    await page.locator('.search-results').getByText('Away page').first().waitFor();
    await page.keyboard.press('Enter');
    await eventually(async () => new URL(page.url()).pathname === `/p/${away.id}`);
    await switcher.click();
    await page.locator('.ws-menu-item').filter({ hasText: 'Second space' }).click();
    await eventually(async () => (await switcher.innerText()).includes('Second space'));
    assert.equal(new URL(page.url()).pathname, `/p/${away.id}`);
    await page.getByRole('button', { name: 'Inbox', exact: true }).click();
    await page.getByRole('button', { name: 'All', exact: true }).click();
    await page.locator('.inbox-open').filter({ hasText: 'Perfect, thank you.' }).click();
    await eventually(async () => (await switcher.innerText()).trim() === homeName);
    await page.goBack();
    await eventually(async () => new URL(page.url()).pathname === `/p/${away.id}`);
    await eventually(async () => (await switcher.innerText()).includes('Second space'));

    // A send that answers after its page was left still clears its draft.
    const openPanel = async () => {
      const toggle = page.locator('[title="Show comments"], [title="Hide comments"]').first();
      await toggle.waitFor();
      if ((await toggle.getAttribute('title')) === 'Show comments') await toggle.click();
      await page.locator('.cp-compose textarea').waitFor();
    };
    await page.goto(`${base}/p/${doc.id}`);
    await openPanel();
    let releaseSend;
    const sendGate = new Promise((resolve) => { releaseSend = resolve; });
    await page.route(`**/api/pages/${doc.id}/comments`, async (route) => {
      if (route.request().method() === 'POST') await sendGate;
      await route.continue();
    });
    const sentText = 'Sent while leaving the page';
    await page.locator('.cp-compose textarea').fill(sentText);
    const posted = page.waitForResponse((r) => r.url().endsWith(`/api/pages/${doc.id}/comments`) && r.request().method() === 'POST');
    await page.locator('.cp-compose button[type=submit]').click();
    await page.locator('.sidebar-search:not(.inbox-trigger)').first().click();
    await page.locator('.search-input').fill('Away page');
    await page.locator('.search-results').getByText('Away page').first().waitFor();
    await page.keyboard.press('Enter');
    await eventually(async () => new URL(page.url()).pathname === `/p/${away.id}`);
    releaseSend();
    await posted;
    await page.unroute(`**/api/pages/${doc.id}/comments`);
    await page.goBack();
    await eventually(async () => new URL(page.url()).pathname === `/p/${doc.id}`);
    await openPanel();
    await page.locator('.cp-body').filter({ hasText: sentText }).waitFor();
    assert.equal(await page.locator('.cp-compose textarea').inputValue(), '', 'the sent text must not return to the box');

    // The same when the reader is back on the page before the send answers.
    let releaseBack;
    const backGate = new Promise((resolve) => { releaseBack = resolve; });
    await page.route(`**/api/pages/${doc.id}/comments`, async (route) => {
      if (route.request().method() === 'POST') await backGate;
      await route.continue();
    });
    const backText = 'Sent, left and came back';
    await page.locator('.cp-compose textarea').fill(backText);
    const backPosted = page.waitForResponse((r) => r.url().endsWith(`/api/pages/${doc.id}/comments`) && r.request().method() === 'POST');
    await page.locator('.cp-compose button[type=submit]').click();
    await page.locator('.sidebar-search:not(.inbox-trigger)').first().click();
    await page.locator('.search-input').fill('Away page');
    await page.locator('.search-results').getByText('Away page').first().waitFor();
    await page.keyboard.press('Enter');
    await eventually(async () => new URL(page.url()).pathname === `/p/${away.id}`);
    await page.goBack();
    await eventually(async () => new URL(page.url()).pathname === `/p/${doc.id}`);
    await openPanel();
    assert.equal(await page.locator('.cp-compose textarea').inputValue(), backText, 'an unsent draft is kept on return');
    releaseBack();
    await backPosted;
    await page.unroute(`**/api/pages/${doc.id}/comments`);
    await page.locator('.cp-body').filter({ hasText: backText }).waitFor();
    await eventually(async () => (await page.locator('.cp-compose textarea').inputValue()) === '');
    // An author who becomes a viewer keeps Delete on their own comment.
    const members = await api('GET', `/api/workspaces/${workspace.id}/members`);
    const colleague = (members.members ?? members).find((m) => m.email === 'colleague@example.test');
    await api('PATCH', `/api/workspaces/${workspace.id}/members/${colleague.id ?? colleague.userId}`, { role: 'viewer' });
    const viewer = await peer.newPage();
    await viewer.goto(`${base}/p/${doc.id}`);
    const viewerToggle = viewer.locator('[title="Show comments"], [title="Hide comments"]').first();
    await viewerToggle.waitFor();
    if ((await viewerToggle.getAttribute('title')) === 'Show comments') await viewerToggle.click();
    const own = viewer.locator('.cp-item').filter({ hasText: 'Please add two more clips.' }).first();
    await own.waitFor();
    assert.equal(await viewer.locator('.cp-compose textarea').count(), 0, 'a viewer does not compose');
    assert.equal(await own.getByTitle('Delete', { exact: true }).count(), 1, 'the author keeps Delete');
    await viewer.close();
    await api('PATCH', `/api/workspaces/${workspace.id}/members/${colleague.id ?? colleague.userId}`, { role: 'member' });
    assert.equal(errors.length, 0, errors.join('\n'));
    console.log('Replies and inbox: two-user live delivery, drafts, failed sends, exact links, unread state, resolve/reopen, mobile, cross-workspace navigation, late sends and viewer authors passed.');
  } finally { await peer.close(); }
});
