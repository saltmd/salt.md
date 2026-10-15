import assert from 'node:assert/strict';
import { eventually, withFixture } from './fixture.mjs';

await withFixture(async ({ page, api, base, workspace }) => {
  const doc = await api('POST', '/api/pages', { title: 'Diagram regression', workspaceId: workspace.id });
  await api('PATCH', `/api/pages/${doc.id}`, { content: [
    { id: 'flowchart', type: 'mermaid', props: { code: 'graph TD\n  A[Start] --> B[Done]', svg: '', rev: 0 }, children: [] },
    { id: 'sequence', type: 'mermaid', props: { code: 'sequenceDiagram\n  Alice->>Bob: Hello', svg: '', rev: 0 }, children: [] },
  ] });
  await page.goto(`${base}/p/${doc.id}`);
  await eventually(async () => await page.locator('.bn-mermaid img').count() === 2);
  for (const img of await page.locator('.bn-mermaid img').all()) {
    const src = await img.getAttribute('src');
    assert(src?.startsWith('data:image/svg+xml'), 'diagrams remain inert SVG images');
    assert(await img.evaluate(node => node.complete && node.naturalWidth > 0));
  }
  await eventually(async () => JSON.stringify((await api('GET', `/api/pages/${doc.id}`)).content).includes('<svg'));
  const exported = await page.request.get(`${base}/api/export/${doc.id}?format=html`);
  assert(exported.ok());
  const html = await exported.text();
  assert(html.includes('Start') && html.includes('Hello'), 'stored diagrams survive HTML export');
  await page.reload();
  await eventually(async () => await page.locator('.bn-mermaid img').count() === 2);
  console.log('Mermaid: flowchart and sequence rendering, inert SVG images, persistence and HTML export passed.');
});
