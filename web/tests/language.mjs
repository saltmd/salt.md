import assert from 'node:assert/strict';
import { eventually, withFixture } from './fixture.mjs';

// Which language a browser gets, that a choice survives a reload, and that a
// text field leaves Enter alone while an input method is composing a word.
await withFixture(async ({ page, context, api, base, workspace }) => {
  page.setDefaultTimeout(15000);
  const htmlLang = (tab) => tab.evaluate(() => document.documentElement.lang);

  // A catalog is matched by language AND script: zh-TW asks for Traditional
  // characters, which is not what 简体中文 is. German and Spanish stay what they
  // were, so a regional tag still reaches the catalog of its language.
  for (const [locale, want] of [
    ['zh-CN', 'zh-Hans'], ['zh', 'zh-Hans'], ['zh-SG', 'zh-Hans'],
    ['zh-Hans', 'zh-Hans'], ['zh-Hans-TW', 'zh-Hans'],
    ['zh-TW', 'zh-Hant'], ['zh-HK', 'zh-Hant'], ['zh-MO', 'zh-Hant'],
    ['zh-Hant', 'zh-Hant'], ['zh-Hant-TW', 'zh-Hant'],
    ['de-AT', 'de'], ['es-MX', 'es'], ['fr-FR', 'en'],
  ]) {
    const visitor = await context.browser().newContext({ locale });
    const tab = await visitor.newPage();
    await tab.goto(base);
    await tab.waitForSelector('.login-card');
    assert.equal(await htmlLang(tab), want, locale);
    if (want === 'zh-Hant') await tab.getByRole('button', { name: '登入', exact: true }).waitFor();
    await visitor.close();
  }

  const collectionURL = page.url();
  await page.click('.user-menu-btn');
  await page.click('.menu >> text=Language and time');
  await page.locator('.dialog select').first().selectOption('zh-Hant');
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await eventually(async () => (await htmlLang(page)) === 'zh-Hant');
  await page.reload();
  await page.waitForSelector('.sidebar');
  await eventually(async () => (await htmlLang(page)) === 'zh-Hant');
  assert.equal((await api('GET', '/api/me')).prefs.language, 'zh-Hant');
  await page.getByRole('button', { name: /^篩選/ }).waitFor();

  // The editor has a separate dictionary; the interface catalog cannot reach it.
  const doc = await api('POST', '/api/pages', { title: 'Traditional editor', workspaceId: workspace.id });
  await api('PATCH', `/api/pages/${doc.id}`, { content: [{ id: 'intro', type: 'paragraph', props: {}, content: [], children: [] }] });
  await page.goto(`${base}/p/${doc.id}`);
  await page.locator('[data-id="intro"] .bn-inline-content').click();
  await page.keyboard.type('/');
  const menu = page.locator('.bn-suggestion-menu');
  await menu.getByText('一級標題', { exact: true }).waitFor();
  const groups = await menu.locator('.bn-suggestion-menu-label').allTextContents();
  assert(groups.length > 1 && new Set(groups).size === groups.length, groups.join(' '));
  await page.goto(collectionURL);

  // The server used to lower the whole tag, and 'zh-hans' answered to no catalog.
  await page.click('.user-menu-btn');
  await page.click('.menu >> text=語言與時間');
  await page.locator('.dialog select').first().selectOption('zh-Hans');
  await page.getByRole('button', { name: '儲存', exact: true }).click();
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
