import { test } from 'node:test';
import assert from 'node:assert/strict';
import { once } from 'node:events';
import { copyFile, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createMockServer, listMachines } from './server.js';

const testdata = fileURLToPath(new URL('../testdata/', import.meta.url));

async function withServer(fn, options) {
  const server = createMockServer(options);
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const base = `http://127.0.0.1:${server.address().port}`;
  try { await fn((path, init) => fetch(base + path, init)); }
  finally { server.closeAllConnections(); await new Promise((resolve) => server.close(resolve)); }
}

test('机台目录由数据目录推导，配方列表与文件一一对应', async () => {
  await withServer(async (request) => {
    const health = await request('/healthz');
    assert.deepEqual(await health.json(), { status: 'ok' });
    const response = await request('/api/machines');
    assert.equal(response.status, 200);
    const machines = await response.json();
    assert.deepEqual(machines, [
      { id: 'DEMO01', name: 'DEMO01', format: 'pgs', process: '' },
      { id: 'DEMO02', name: 'DEMO02', format: 'dcex', process: '' },
    ]);
    for (const [machine, count] of [[machines[0], 1], [machines[1], 1]]) {
      const catalog = await (await request(`/api/machines/${machine.id}/recipes`)).json();
      assert.equal(catalog.length, count);
      assert.ok(catalog.every((item) => item.name.endsWith(`.${machine.format}`)));
      assert.deepEqual(catalog.map((item) => item.name), catalog.map((item) => item.name).sort());
    }
  }, { root: testdata });
});

test('按字节原样返回文件，保留编码与换行', async () => {
  await withServer(async (request) => {
    for (const machine of ['DEMO01', 'DEMO02']) {
      const catalog = await (await request(`/api/machines/${machine}/recipes`)).json();
      for (const item of catalog) {
        const response = await request(`/api/machines/${machine}/recipes/${encodeURIComponent(item.id)}/content`);
        assert.equal(response.status, 200);
        const actual = Buffer.from(await response.arrayBuffer());
        const expected = await readFile(join(testdata, machine, item.name));
        assert.deepEqual(actual, expected);
        assert.equal(Number(response.headers.get('content-length')), expected.length);
      }
    }
  }, { root: testdata });
});

test('拒绝写入、未知路由、缺失配方与路径穿越', async () => {
  await withServer(async (request) => {
    const write = await request('/api/machines', { method: 'POST' });
    assert.equal(write.status, 405);
    assert.equal(write.headers.get('allow'), 'GET');
    for (const path of [
      '/unknown', '/api/machines/UNKNOWN/recipes',
      '/api/machines/DEMO01/recipes/missing.pgs/content',
      '/api/machines/DEMO01/recipes/..%2F..%2Fpackage.json/content',
      '/api/machines/..%2F..%2F/recipes',
    ]) assert.equal((await request(path)).status, 404, path);
    assert.equal((await request('/api/machines/%ZZ/recipes')).status, 400);
  }, { root: testdata });
});

test('数据目录不可用时返回有界错误', async () => {
  await withServer(async (request) => {
    const response = await request('/api/machines/DEMO01/recipes');
    assert.equal(response.status, 500);
    assert.deepEqual(await response.json(), { error: '机台数据读取失败' });
  }, { root: '/nonexistent-machine-data' });
});

test('忽略隐藏目录、空目录与混合格式目录', async (t) => {
  const root = await mkdtemp(join(tmpdir(), 'recipe-data-'));
  t.after(() => rm(root, { recursive: true, force: true }));
  await mkdir(join(root, 'DEMO01'));
  await copyFile(join(testdata, 'DEMO01', 'DEMO-001_8.pgs'), join(root, 'DEMO01', 'DEMO-001_8.pgs'));
  await mkdir(join(root, 'MIXED'));
  await copyFile(join(testdata, 'DEMO01', 'DEMO-001_8.pgs'), join(root, 'MIXED', 'a.pgs'));
  await copyFile(join(testdata, 'DEMO02', 'DEMO-002_8.dcex'), join(root, 'MIXED', 'b.dcex'));
  await mkdir(join(root, 'EMPTY'));
  await writeFile(join(root, 'EMPTY', 'notes.txt'), 'no recipe here');
  await mkdir(join(root, '.hidden'));
  await copyFile(join(testdata, 'DEMO01', 'DEMO-001_8.pgs'), join(root, '.hidden', 'DEMO-001_8.pgs'));

  assert.deepEqual((await listMachines(root)).map((machine) => machine.id), ['DEMO01']);
});
