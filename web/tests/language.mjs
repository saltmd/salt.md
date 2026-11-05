import assert from 'node:assert/strict';
import { eventually, withFixture } from './fixture.mjs';

// Which language a browser gets, that a choice survives a reload, and that a
// text field leaves Enter alone while an input method is composing a word.
await withFixture(async ({ page, context, api, base }) => {
  page.setDefaultTimeout(15000);
  const htmlLang = (tab) => tab.evaluate(() => document.documentElement.lang);

  // A catalog is matched by language AND script: zh-TW asks for Traditional
  // characters, which is not what 简体中文 is. German and Spanish stay what they
  // were, so a regional tag still reaches the catalog of its language.
  for (const [locale, want] of [
    ['zh-CN', 'zh-Hans'], ['zh', 'zh-Hans'], ['zh-SG', 'zh-Hans'],
    ['zh-TW', 'en'], ['zh-HK', 'en'],
    ['de-AT', 'de'], ['es-MX', 'es'], ['fr-FR', 'en'],
  ]) {
    const visitor = await context.browser().newContext({ locale });
    const tab = await visitor.newPage();
    await tab.goto(base);
    await tab.waitForSelector('.login-card');
    assert.equal(await htmlLang(tab), want, locale);
    await visitor.close();
  }

  // The server used to lower the whole tag, and 'zh-hans' answered to no catalog.
  await page.click('.user-menu-btn');
  await page.click('.menu >> text=Language and time');
  await page.locator('.dialog select').first().selectOption('zh-Hans');
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await eventually(async () => (await htmlLang(page)) === 'zh-Hans');
  await page.reload();
  await page.waitForSelector('.sidebar');
  await eventually(async () => (await htmlLang(page)) === 'zh-Hans');
  assert.equal((await api('GET', '/api/me')).prefs.language, 'zh-Hans');

  // These lists were constants, so no language ever reached them.
  await page.getByRole('button', { name: /^筛选/ }).waitFor();
  await page.getByRole('button', { name: '属性', exact: true }).click();
  const types = await page.locator('.schema-add select option').allTextContents();
  assert(types.includes('文本') && types.includes('公式'), types.join(' '));

  // Enter finishes the word being composed. Taken for a command it stored the
  // half-typed Latin letters as a property.
  const name = page.getByPlaceholder('新属性名称');
  const rows = page.locator('.schema-row');
  const before = await rows.count();
  const cdp = await context.newCDPSession(page);
  const enter = { key: 'Enter', code: 'Enter', windowsVirtualKeyCode: 13 };
  await name.click();
  await cdp.send('Input.imeSetComposition', { text: 'wen', selectionStart: 3, selectionEnd: 3 });
  await cdp.send('Input.dispatchKeyEvent', { type: 'rawKeyDown', ...enter });
  await cdp.send('Input.dispatchKeyEvent', { type: 'keyUp', ...enter });
  await page.waitForTimeout(200);
  assert.equal(await rows.count(), before, 'Enter inside a composition added a property');

  // Once the word is finished, Enter means what it always did.
  await cdp.send('Input.insertText', { text: '文本' });
  await name.press('Enter');
  await eventually(async () => (await rows.count()) === before + 1);
});
