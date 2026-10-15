import assert from 'node:assert/strict';
import { eventually, withFixture } from './fixture.mjs';

// A collection embedded in a document (#22, #26): the embed keeps its filters
// to itself, two embeds of one collection disagree as they please, the
// collection's own view never notices, and the embed shows a lean toolbar.
// The fixture's table view is saved with "Status is To do", so it shows one of
// the two rows.
await withFixture(async ({ page, api, base, workspace, path, read }) => {
  page.setDefaultTimeout(15000);
  const collectionId = path.split('/').pop();
  const embedIn = async (title) => {
    const doc = await api('POST', '/api/pages', { title, workspaceId: workspace.id });
    await api('PATCH', `/api/pages/${doc.id}`, {
      content: [{ id: 'db', type: 'database', props: { collectionId }, content: [], children: [] }],
    });
    return doc;
  };
  const a = await embedIn('Project A');
  const b = await embedIn('Project B');
  const rowsIn = (scope) => page.locator(`${scope} .db-table tbody tr`).filter({ hasText: /row/ });

  // Project A: the embed starts out as the collection's view says.
  await page.goto(`${base}/p/${a.id}`);
  await eventually(async () => (await rowsIn('.bn-db-embed').count()) === 1);

  // #22: a window onto the rows, not a second copy of the collection.
  const embed = page.locator('.bn-db-embed');
  assert.equal(await embed.getByRole('button', { name: 'Properties', exact: true }).count(), 0);
  assert.equal(await embed.locator('.view-add').count(), 0);
  assert.equal(await embed.getByRole('button', { name: 'View options', exact: true }).count(), 0);

  // #26: taking the filter away here shows both rows HERE...
  const before = (await read()).views;
  await embed.locator('.fs-controls button', { hasText: 'Filter' }).first().click();
  await page.locator('.fs-remove').first().click();
  await page.keyboard.press('Escape');
  await eventually(async () => (await rowsIn('.bn-db-embed').count()) === 2);
  // ...and nowhere else: the collection's saved view is untouched.
  assert.deepEqual((await read()).views, before, "the collection's views must not change");

  // It is kept with the document.
  await eventually(async () => {
    await page.reload();
    await rowsIn('.bn-db-embed').first().waitFor();
    return (await rowsIn('.bn-db-embed').count()) === 2;
  });

  // Project B embeds the same collection and still sees it filtered.
  await page.goto(`${base}/p/${b.id}`);
  await eventually(async () => (await rowsIn('.bn-db-embed').count()) === 1);

  // So does the collection's own page.
  await page.goto(`${base}${path.replace('/api/collections', '/p')}`);
  await eventually(async () => (await rowsIn('.collection-scroll').count()) === 1);

  // Legacy embeds inherit column visibility until it is changed locally.
  const shared = await read();
  shared.views[0].hidden = ['labels'];
  await api('PUT', path, shared);
  await page.goto(`${base}/p/${a.id}`);
  const headers = (scope) => scope.locator('.db-table thead th').allTextContents();
  const expectHeaders = async (scope, names) => eventually(async () =>
    JSON.stringify((await headers(scope)).map(s => s.trim())) === JSON.stringify(names));
  const columns = async (scope, action) => {
    await scope.getByRole('button', { name: /^Columns/ }).click();
    await action(page.locator('.col-popover'));
    await page.locator('.fs-backdrop').click();
  };
  await expectHeaders(embed, ['Name', 'Status']);
  // An explicit [] must override the shared hidden list, preserving A's filter.
  await columns(embed, pop => pop.getByRole('button', { name: 'Show all', exact: true }).click());
  await expectHeaders(embed, ['Name', 'Status', 'Labels']);
  await columns(embed, pop => pop.getByRole('button', { name: 'Status', exact: true }).click());
  await expectHeaders(embed, ['Name', 'Labels']);
  assert.deepEqual((await read()).views, shared.views);
  await eventually(async () => {
    const doc = await api('GET', `/api/pages/${a.id}`);
    const local = JSON.parse(doc.content.find(block => block.type === 'database').props.local || '{}');
    return JSON.stringify(local.views?.table?.hidden) === JSON.stringify(['status']);
  });
  await page.reload();
  await expectHeaders(embed, ['Name', 'Labels']);
  assert.equal(await rowsIn('.bn-db-embed').count(), 2, 'column changes preserve local filters');

  // Two instances in the SAME document also keep independent column settings.
  await api('PATCH', `/api/pages/${b.id}`, {
    content: ['db-one', 'db-two'].map(id => ({
      id, type: 'database', props: { collectionId }, content: [], children: [],
    })),
  });
  await page.goto(`${base}/p/${b.id}`);
  const first = page.locator('.bn-db-embed').nth(0);
  const second = page.locator('.bn-db-embed').nth(1);
  await expectHeaders(first, ['Name', 'Status']);
  await expectHeaders(second, ['Name', 'Status']);
  await columns(first, pop => pop.getByRole('button', { name: 'Show all', exact: true }).click());
  await expectHeaders(first, ['Name', 'Status', 'Labels']);
  await expectHeaders(second, ['Name', 'Status']);
  await columns(second, pop => pop.getByRole('button', { name: 'Hide all', exact: true }).click());
  await expectHeaders(second, ['Name']);
  await eventually(async () => {
    const doc = await api('GET', `/api/pages/${b.id}`);
    const locals = doc.content.filter(block => block.type === 'database')
      .map(block => JSON.parse(block.props.local || '{}'));
    return locals.length === 2 && locals[0].views?.table?.hidden?.length === 0 &&
      locals[1].views?.table?.hidden?.length === 2;
  });
  await page.reload();
  await expectHeaders(first, ['Name', 'Status', 'Labels']);
  await expectHeaders(second, ['Name']);
  await page.goto(`${base}/p/${a.id}`);
  await expectHeaders(embed, ['Name', 'Labels']);
  await page.goto(`${base}${path.replace('/api/collections', '/p')}`);
  await expectHeaders(page.locator('.collection-scroll'), ['Name', 'Status']);
  assert.deepEqual((await read()).views, shared.views);

  // Long view names used to push New beyond the document's clipped edge.
  const toolbarConfig = await read();
  toolbarConfig.views[0].name = 'Table with a deliberately long view name';
  toolbarConfig.views[1].name = 'Board of responsible people';
  await api('PUT', path, toolbarConfig);
  await page.goto(`${base}/p/${b.id}`);
  for (const width of [960, 480]) {
    await page.setViewportSize({ width, height: 1000 });
    const backdrop = page.locator('.sidebar-backdrop');
    if (await backdrop.isVisible()) {
      await backdrop.click({ position: { x: width - 10, y: 100 } });
    }
    await page.mouse.move(0, 0);
    for (const instance of [first, second]) {
      const toolbar = instance.locator('.collection-toolbar');
      await toolbar.getByRole('button', { name: 'New', exact: true }).waitFor();
      await eventually(() => toolbar.evaluate(bar => {
        const outer = bar.closest('.bn-db-embed').getBoundingClientRect();
        const bounds = bar.getBoundingClientRect();
        return [...bar.querySelectorAll('button')].every(button => {
          const rect = button.getBoundingClientRect();
          return rect.width > 0 && rect.left >= outer.left && rect.right <= outer.right &&
            rect.left >= 0 && rect.right <= window.innerWidth &&
            rect.top >= bounds.top && rect.bottom <= bounds.bottom &&
            Number(getComputedStyle(button.closest('.collection-actions') || button).opacity) === 1;
        });
      }));
    }
  }
  console.log('Embeds: independent filters/columns; toolbar actions fit at 960px and 480px.');
});
