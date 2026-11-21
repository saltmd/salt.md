import assert from 'node:assert/strict';
import { eventually, withFixture } from './fixture.mjs';

// Tab in the page editor (#144). Tab indents a block under the one above it
// and Shift+Tab lifts it out again. Where neither is possible, the key used to
// leave the editor: Tab on the first line moved the focus to whatever came
// next on the page. It now stays in the text and changes nothing.
await withFixture(async ({ page, api, base, workspace }) => {
  page.setDefaultTimeout(15000);
  const doc = await api('POST', '/api/pages', { title: 'Tab test', workspaceId: workspace.id });
  const para = (id, text) => ({ id, type: 'paragraph', props: {}, content: [{ type: 'text', text, styles: {} }], children: [] });
  await api('PATCH', `/api/pages/${doc.id}`, { content: [para('first', 'First line'), para('second', 'Second line')] });
  const line = (id) => page.locator(`[data-id="${id}"] .bn-inline-content`).first();
  const inEditor = () => page.evaluate(() => !!document.activeElement?.closest('.bn-editor'));
  const nested = () => page.evaluate(() => !!document.querySelector('[data-id="first"] [data-id="second"]'));
  const caretInto = async (id) => {
    await line(id).click();
    await line(id).evaluate((el) => {
      const range = document.createRange();
      range.selectNodeContents(el);
      range.collapse(true);
      getSelection().removeAllRanges();
      getSelection().addRange(range);
    });
    // The editor takes over a selection set from outside on the next
    // selectionchange event, which comes a moment later.
    await page.waitForTimeout(200);
  };

  await page.goto(`${base}/p/${doc.id}`);
  await eventually(async () => (await line('second').textContent()) === 'Second line');

  // The first block has nothing to go under: the focus stays and nothing moves.
  await caretInto('first');
  await page.keyboard.press('Tab');
  assert.equal(await inEditor(), true, 'Tab on the first line left the editor');
  assert.equal(await nested(), false);
  await page.keyboard.type('x');
  await eventually(async () => (await line('first').textContent()) === 'xFirst line');

  // The second one goes under the first, as before.
  await caretInto('second');
  await page.keyboard.press('Tab');
  await eventually(nested);
  assert.equal(await inEditor(), true);

  // Lifted out again, and once more at the top level, where there is nothing
  // left to leave: the focus stays there too.
  await page.keyboard.press('Shift+Tab');
  await eventually(async () => !(await nested()));
  await page.keyboard.press('Shift+Tab');
  assert.equal(await inEditor(), true, 'Shift+Tab on a top-level block left the editor');
  await page.keyboard.type('y');
  await eventually(async () => (await line('second').textContent()) === 'ySecond line');
});
