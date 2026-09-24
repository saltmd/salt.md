import assert from 'node:assert/strict';
import { spawn, spawnSync } from 'node:child_process';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:net';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { randomUUID } from 'node:crypto';
import { chromium } from 'playwright';

export async function eventually(check) {
  for (let attempt = 0; attempt < 100; attempt++) {
    if (await check()) return;
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  throw new Error('Expected state did not arrive within 10 seconds');
}

// Always build and start our own loopback server with disposable data. There is
// intentionally no URL or data-directory override pointing at an existing site.
export async function withFixture(test) {
  const root = resolve(dirname(fileURLToPath(import.meta.url)), '../..');
  const temp = await mkdtemp(join(tmpdir(), 'salt-browser-test-'));
  let server;
  let browser;
  try {
    const binary = join(temp, process.platform === 'win32' ? 'salt.exe' : 'salt');
    const build = spawnSync(process.env.GO_BINARY || 'go', ['build', '-o', binary, '.'], {
      cwd: root, encoding: 'utf8',
    });
    assert.equal(build.status, 0, build.stderr || String(build.error || 'Build failed'));
    const listener = createServer();
    listener.listen(0, '127.0.0.1');
    await once(listener, 'listening');
    const port = listener.address().port;
    await new Promise(resolve => listener.close(resolve));
    const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith('SALT_')));
    server = spawn(binary, [], {
      env: { ...env, SALT_DATA: join(temp, 'data'), SALT_ADDR: `127.0.0.1:${port}` },
      stdio: 'ignore',
    });
    const base = `http://127.0.0.1:${port}`;
    await eventually(async () => {
      if (server.exitCode !== null) throw new Error('Test server exited before becoming ready');
      try { return (await fetch(`${base}/api/health`)).ok; } catch { return false; }
    });
    browser = await chromium.launch({
      headless: true,
      ...(process.env.PLAYWRIGHT_CHANNEL ? { channel: process.env.PLAYWRIGHT_CHANNEL } : {}),
    });
    const context = await browser.newContext({ viewport: { width: 1200, height: 900 } });
    async function api(method, path, data) {
      const response = await context.request.fetch(base + path, { method, data });
      assert(response.ok(), `${method} ${path}: HTTP ${response.status()}`);
      return response.json();
    }
    await api('POST', '/api/setup', {
      name: 'Browser Tester', email: 'browser@example.test', password: randomUUID(),
    });
    const workspace = (await api('GET', '/api/workspaces'))[0];
    const collection = await api('POST', '/api/pages', {
      title: 'Option editing test', type: 'collection', workspaceId: workspace.id,
    });
    const schema = ['select', 'multiselect'].map((type, index) => ({
      id: index === 0 ? 'status' : 'labels', name: index === 0 ? 'Status' : 'Labels', type,
      options: [
        { id: 'todo', name: 'To do', color: '#337ea9' },
        { id: 'done', name: 'Done', color: '#448361' },
        { id: 'third', name: 'Third', color: '#123456' },
      ],
    }));
    const path = `/api/collections/${collection.id}`;
    await api('PUT', path, { schema, views: [
      { id: 'table', name: 'Table', type: 'table' },
      { id: 'board', name: 'Board', type: 'board', groupBy: 'status' },
    ] });
    const rows = [];
    for (const [title, props] of [
      ['First row', { status: 'todo', labels: ['todo', 'done'] }],
      ['Second row', { status: 'done', labels: ['done'] }],
    ]) rows.push(await api('POST', '/api/pages', { parentId: collection.id, title, props }));
    const page = await context.newPage();
    await page.goto(`${base}/p/${collection.id}`);
    const open = () => page.getByRole('button', { name: 'Properties', exact: true }).click();
    const chip = (index, name) => page.locator('.schema-options').nth(index)
      .getByRole('button', { name, exact: true });
    const field = page.getByRole('textbox', { name: 'Option name' });
    const read = () => api('GET', path);
    const assertRows = async () => {
      for (const row of rows) assert.deepEqual((await api('GET', `/api/pages/${row.id}`)).props, row.props);
    };
    await test({ page, context, path, open, chip, field, read, assertRows, schema });
  } finally {
    await browser?.close();
    if (server && server.exitCode === null) {
      const exited = once(server, 'exit');
      server.kill('SIGTERM');
      const timer = setTimeout(() => server.kill('SIGKILL'), 2000);
      await exited;
      clearTimeout(timer);
    }
    await rm(temp, { recursive: true, force: true });
  }
}
