import assert from 'node:assert/strict';
import { eventually, withFixture } from './fixture.mjs';

await withFixture(async ({ page, api, base, workspace }) => {
  page.setDefaultTimeout(15000);
  const text = 'Select these words to leave a comment.';
  const doc = await api('POST', '/api/pages', { title: 'Selection comments', workspaceId: workspace.id });
  const content = [{ id: 'passage', type: 'paragraph', props: {}, content: [{ type: 'text', text, styles: {} }], children: [] }];
  await api('PATCH', `/api/pages/${doc.id}`, { content });
  const comments = () => api('GET', `/api/pages/${doc.id}/comments`);
  const select = async (label = 'Comment on selected text') => {
    const passage = page.locator('[data-id="passage"] .bn-inline-content');
    await passage.click({ clickCount: 3 });
    const button = page.getByRole('button', { name: label, exact: true });
    await button.waitFor();
    await eventually(async () => button.isEnabled());
    await button.click();
    await page.locator('.cp-pending').waitFor();
    assert.equal(await page.locator('.cp-pending span').textContent(), text);
  };
  const draft = page.locator('.cp-compose textarea');
  const send = page.locator('.cp-compose button[type="submit"]');
  await page.goto(`${base}/p/${doc.id}`);
  await select();
  await draft.fill('Please clarify this passage.');
  await send.click();
  await page.locator('.cp-item').waitFor();
  const [saved] = await comments();
  assert.equal(saved.anchor.quote, text);
  assert(saved.anchor.from && saved.anchor.to, 'the browser still needs its Yjs positions');
  await page.locator('.salt-comment-highlight').waitFor();
  await page.reload();
  await page.locator('.salt-comment-highlight').waitFor();
  assert.equal(await page.locator('.cp-quote span').textContent(), text);

  await page.locator('.cp-act[title="Mark as resolved"]').click();
  await eventually(async () => (await comments())[0].resolvedAt);
  await eventually(async () => (await page.locator('.salt-comment-highlight').count()) === 0);
  await page.locator('.cp-toggle input').check();
  await page.getByRole('button', { name: 'Reopen', exact: true }).click();
  await page.locator('.salt-comment-highlight').waitFor();

  // The same editor binding must work in both themes and in the narrow drawer.
  for (const colorScheme of ['light', 'dark']) {
    await page.emulateMedia({ colorScheme });
    await page.setViewportSize({ width: 390, height: 844 });
    await page.locator('.cp-quote').click();
    await eventually(async () => (await page.locator('.cp-compose').count()) === 0);
    const highlight = page.locator('.salt-comment-highlight');
    await highlight.click();
    await page.locator('.cp-quote').waitFor();
    assert.equal(await page.locator('.cp-quote span').textContent(), text);
  }
  await page.setViewportSize({ width: 1280, height: 900 });

  // A stale anchor reaches the ordinary API error translator. Test the real
  // 409 response and retained draft in each shipped language, without mocks.
  for (const [language, label, message] of [
    ['de', 'Ausgewählten Text kommentieren', 'Die Seite wurde geändert. Wähle den Text erneut aus.'],
    ['es', 'Comentar el texto seleccionado', 'La página ha cambiado. Selecciona el texto de nuevo.'],
    ['zh-Hans', '评论所选文本', '页面已更改。请重新选择文本。'],
  ]) {
    await api('PATCH', `/api/pages/${doc.id}`, { content });
    await api('PUT', '/api/me/prefs', { language });
    await page.reload();
    await select(label);
    await draft.fill(`Keep this draft (${language}).`);
    await api('PATCH', `/api/pages/${doc.id}`, { content });
    const response = page.waitForResponse((r) => r.url().endsWith(`/api/pages/${doc.id}/comments`) && r.request().method() === 'POST');
    await send.click();
    const rejected = await response;
    assert.equal(rejected.status(), 409);
    assert.equal((await rejected.json()).code, 'comment_anchor_stale');
    await page.locator('.toast').filter({ hasText: message }).waitFor();
    assert.equal(await draft.inputValue(), `Keep this draft (${language}).`);
    assert.equal((await comments()).length, 1, 'a rejected draft must not create a comment');
  }

  await api('PUT', '/api/me/prefs', { language: 'en' });
  await page.reload();
  await page.getByText('Original text no longer available', { exact: true }).waitFor();
  assert.equal(await page.locator('.cp-quote').isDisabled(), true);
  await page.locator('.cp-act[title="Delete"]').click();
  await eventually(async () => (await comments()).length === 0);
  console.log('Inline comments: create, reload, resolve/reopen, narrow light/dark navigation, translated 409s, retained drafts, detachment and deletion.');
});
