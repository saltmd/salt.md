import assert from 'node:assert/strict';
import { eventually, withFixture } from './fixture.mjs';

// Undo in the page editor (#145). Since 1.1.6 the comment highlighting was
// added to the editor after it had started, and that silently switched off
// the editor's undo history: Ctrl+Z did nothing on Linux. On a Mac the
// browser's own undo partly covered for it, which is why it went unnoticed.
//
// The first run pretends to be a Linux machine, so the editor binds Ctrl and
// nothing of the browser can stand in for the editor's own history. The
// second run uses this machine's real key.
await withFixture(async ({ page, context, api, base, workspace }) => {
  page.setDefaultTimeout(15000);
  const doc = await api('POST', '/api/pages', { title: 'Undo test', workspaceId: workspace.id });
  const content = [{ id: 'line', type: 'paragraph', props: {}, content: [{ type: 'text', text: 'Start', styles: {} }], children: [] }];
  await api('PATCH', `/api/pages/${doc.id}`, { content });
  const line = (p) => p.locator('[data-id="line"] .bn-inline-content');

  async function check(p, undoKey, redoKey) {
    await p.goto(`${base}/p/${doc.id}`);
    await eventually(async () => (await line(p).textContent()) === 'Start');
    // Caret after "Start". The End key would not do on a Mac, where it scrolls
    // instead of moving the caret, so the selection is set directly.
    await line(p).click();
    await line(p).evaluate((el) => {
      const range = document.createRange();
      range.selectNodeContents(el);
      range.collapse(false);
      getSelection().removeAllRanges();
      getSelection().addRange(range);
    });
    await p.keyboard.type(' and more');
    await eventually(async () => (await line(p).textContent()) === 'Start and more');
    // Past the history's grouping window, so the typing is one step of its own.
    await p.waitForTimeout(700);
    await p.keyboard.press(undoKey);
    await eventually(async () => (await line(p).textContent()) === 'Start');
    await p.keyboard.press(redoKey);
    await eventually(async () => (await line(p).textContent()) === 'Start and more');
    // Leave the page as it was found for the next run.
    await p.keyboard.press(undoKey);
    await eventually(async () => (await line(p).textContent()) === 'Start');
  }

  const linux = await context.newPage();
  await linux.addInitScript(() => {
    Object.defineProperty(Navigator.prototype, 'platform', { get: () => 'Linux x86_64' });
  });
  linux.setDefaultTimeout(15000);
  await check(linux, 'Control+z', 'Control+Shift+z');
  await linux.close();

  await check(page, 'ControlOrMeta+z', 'ControlOrMeta+Shift+z');
  assert.ok(true);
});
