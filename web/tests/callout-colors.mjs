import assert from 'node:assert/strict';
import { eventually, withFixture } from './fixture.mjs';

await withFixture(async ({ page, api, base, workspace }) => {
  page.setDefaultTimeout(15000);
  const icons = ['💡', '⚠️', '❗', '✅', '📌', '🔥', 'ℹ️'];
  const tones = ['neutral', 'warning', 'danger', 'success', 'neutral', 'amber', 'info'];
  const labels = ['Idea', 'Warning', 'Important', 'Completed', 'Pinned', 'Urgent', 'Information'];
  const document = await api('POST', '/api/pages', { title: 'Callout colors', workspaceId: workspace.id });
  await api('PATCH', `/api/pages/${document.id}`, { content: icons.map((emoji, i) => ({
    id: `callout-${i}`, type: 'callout', props: { emoji },
    content: [{ type: 'text', text: labels[i], styles: {} }], children: [],
  })) });
  await page.goto(`${base}/p/${document.id}`);
  const callouts = page.locator('.bn-callout');
  await callouts.last().waitFor();
  await page.getByRole('radio', { name: 'Light', exact: true }).click();
  assert.deepEqual(await callouts.evaluateAll(nodes => nodes.map(n => n.dataset.tone)), tones);
  const colors = await callouts.evaluateAll(nodes => nodes.map(n => getComputedStyle(n).backgroundColor));
  assert.equal(colors[0], colors[4]);
  assert.equal(new Set(colors).size, 6);
  // Changing an existing block's icon immediately changes its background.
  for (let i = 1; i <= icons.length; i++) {
    await callouts.first().locator('.bn-callout-emoji').click();
    assert.equal(await callouts.first().getAttribute('data-tone'), tones[i % icons.length]);
    assert.equal(await callouts.first().evaluate(n => getComputedStyle(n).backgroundColor), colors[i % icons.length]);
  }
  await page.getByRole('radio', { name: 'Dark', exact: true }).click();
  const dark = await callouts.evaluateAll(nodes => nodes.map(n => getComputedStyle(n).backgroundColor));
  assert.equal(dark[0], dark[4]);
  assert.equal(new Set(dark).size, 6);
  dark.forEach((color, i) => assert.notEqual(color, colors[i]));
  const contrasts = await callouts.evaluateAll(nodes => nodes.map(node => {
    const luminance = color => {
      const rgb = color.match(/[\d.]+/g).slice(0, 3).map(Number).map(v => {
        v /= 255;
        return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
      });
      return rgb[0] * 0.2126 + rgb[1] * 0.7152 + rgb[2] * 0.0722;
    };
    const text = luminance(getComputedStyle(node.querySelector('.bn-callout-content')).color);
    const background = luminance(getComputedStyle(node).backgroundColor);
    return (Math.max(text, background) + 0.05) / (Math.min(text, background) + 0.05);
  }));
  contrasts.forEach(ratio => assert(ratio >= 4.5, `Text contrast ${ratio}`));
  await callouts.first().locator('.bn-callout-emoji').click();
  await eventually(async () => (await api('GET', `/api/pages/${document.id}`)).content[0].props.emoji === '⚠️');
  await page.reload();
  await callouts.first().waitFor();
  assert.equal(await callouts.first().getAttribute('data-tone'), 'warning');
  assert.equal(await callouts.first().locator('.bn-callout-content').innerText(), 'Idea');
});
console.log('Callout colors: seven icons, cycling, light/dark themes and reload persistence passed.');
