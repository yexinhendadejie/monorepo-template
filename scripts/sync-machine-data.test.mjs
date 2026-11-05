import { after, before, test } from 'node:test';
import assert from 'node:assert/strict';
import { FtpSrv } from 'ftp-srv';
import bunyan from 'bunyan';
import { mkdtemp, mkdir, readFile, rm, stat, utimes, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { readConfig, resolveTarget, sync } from './sync-machine-data.mjs';

let workspace;
let remoteRoot;
let localRoot;
let ftp;
let port;

async function put(relative, content, when) {
  const target = path.join(remoteRoot, relative);
  await mkdir(path.dirname(target), { recursive: true });
  await writeFile(target, content);
  if (when) await utimes(target, when, when);
}

async function startFtp(root) {
  const server = new FtpSrv({
    url: 'ftp://127.0.0.1:0',
    anonymous: true,
    pasv_url: '127.0.0.1',
    pasv_min: 20000,
    pasv_max: 20100,
    log: bunyan.createLogger({ name: 'ftp-srv-test', level: 'fatal' }),
  });
  server.on('login', ({ connection }, resolve) => resolve({ root }));
  await server.listen();
  return { server, port: server.server.address().port };
}

before(async () => {
  workspace = await mkdtemp(path.join(os.tmpdir(), 'recipe-sync-'));
  remoteRoot = path.join(workspace, 'remote');
  localRoot = path.join(workspace, 'local');
  await put('ABMCPT01/NP4M-1200.pgs', 'IGSS,IGSS1,1\r\n', new Date('2024-03-01T02:03:04Z'));
  await put('ABMCPT03/NP3D-2920.dcex', '[INFO PLAN]\r\nName=x\r\n', new Date('2024-03-02T03:04:05Z'));
  await put('ABMCPT03/backup/older.dcex', 'a,b\r\n', new Date('2024-03-03T04:05:06Z'));
  await put('ABMCPT03/.DS_Store', 'junk');
  const started = await startFtp(remoteRoot);
  ftp = started.server;
  port = started.port;
});

after(async () => {
  await ftp.close();
  await rm(workspace, { recursive: true, force: true });
});

const config = () => ({
  host: '127.0.0.1',
  port,
  user: 'anonymous',
  password: '',
  secure: false,
  remotePath: '/',
  root: localRoot,
});

const silent = () => {};

test('downloads the whole remote tree and keeps bytes and mtimes', async () => {
  const result = await sync(config(), { log: silent });
  assert.deepEqual({ total: result.total, downloaded: result.downloaded, skipped: result.skipped }, {
    total: 3,
    downloaded: 3,
    skipped: 0,
  });
  assert.equal(await readFile(path.join(localRoot, 'ABMCPT01/NP4M-1200.pgs'), 'utf8'), 'IGSS,IGSS1,1\r\n');
  assert.equal(
    await readFile(path.join(localRoot, 'ABMCPT03/backup/older.dcex'), 'utf8'),
    'a,b\r\n',
  );
  await assert.rejects(stat(path.join(localRoot, 'ABMCPT03/.DS_Store')), /ENOENT/);
  const local = await stat(path.join(localRoot, 'ABMCPT03/NP3D-2920.dcex'));
  assert.ok(Math.abs(local.mtimeMs - new Date('2024-03-02T03:04:05Z').getTime()) < 2000);
});

test('skips unchanged files and refetches only what changed remotely', async () => {
  const again = await sync(config(), { log: silent });
  assert.deepEqual([again.downloaded, again.skipped], [0, 3]);

  await put('ABMCPT01/NP4M-1200.pgs', 'IGSS,IGSS1,2\r\n', new Date('2024-04-01T00:00:00Z'));
  const afterEdit = await sync(config(), { log: silent });
  assert.deepEqual([afterEdit.downloaded, afterEdit.skipped], [1, 2]);
  assert.equal(await readFile(path.join(localRoot, 'ABMCPT01/NP4M-1200.pgs'), 'utf8'), 'IGSS,IGSS1,2\r\n');
});

test('dry run reports planned downloads without touching disk', async () => {
  const logs = [];
  const empty = path.join(workspace, 'dry');
  const result = await sync({ ...config(), root: empty }, { dryRun: true, log: (line) => logs.push(line) });
  assert.deepEqual([result.downloaded, result.skipped], [0, 0]);
  assert.equal(logs.filter((line) => line.startsWith('将下载')).length, 3);
  await assert.rejects(stat(empty), /ENOENT/);
});

test('prune removes stale machine files but keeps the tracked README', async () => {
  await writeFile(path.join(localRoot, 'README.md'), '# 机器数据\n');
  await mkdir(path.join(localRoot, 'ABMCPT09'), { recursive: true });
  await writeFile(path.join(localRoot, 'ABMCPT09/stale.pgs'), 'old');
  const result = await sync(config(), { prune: true, log: silent() });
  assert.equal(result.removed, 1);
  assert.equal(await readFile(path.join(localRoot, 'README.md'), 'utf8'), '# 机器数据\n');
  await assert.rejects(stat(path.join(localRoot, 'ABMCPT09/stale.pgs')), /ENOENT/);
  await assert.rejects(stat(path.join(localRoot, 'ABMCPT09')), /ENOENT/);
});

test('rejects paths that escape the data directory', () => {
  assert.equal(resolveTarget('/tmp/data', 'ABMCPT01/a.pgs'), '/tmp/data/ABMCPT01/a.pgs');
  assert.throws(() => resolveTarget('/tmp/data', '../etc/passwd'), /数据目录之外/);
  assert.throws(() => resolveTarget('/tmp/data', 'a/../../b'), /数据目录之外/);
});

test('requires a host and fills documented defaults', () => {
  assert.throws(() => readConfig({}), /FTP_HOST/);
  const config = readConfig({ FTP_HOST: ' disk.example.com ', FTP_PASSWORD: 'secret' });
  assert.equal(config.host, 'disk.example.com');
  assert.equal(config.port, 21);
  assert.equal(config.user, 'anonymous');
  assert.equal(config.remotePath, '/');
  assert.equal(readConfig({ FTP_HOST: 'x', FTP_SECURE: 'true' }).secure, true);
  assert.equal(readConfig({ FTP_HOST: 'x', FTP_USER: 'reader', FTP_PATH: '/cp' }).remotePath, '/cp');
});

test('an empty remote directory finishes without writing files', async () => {
  const emptyRemote = path.join(workspace, 'empty-remote');
  await mkdir(emptyRemote, { recursive: true });
  const started = await startFtp(emptyRemote);
  try {
    const emptyLocal = path.join(workspace, 'empty-local');
    const logs = [];
    const result = await sync({ ...config(), port: started.port, root: emptyLocal }, {
      log: (line) => logs.push(line),
    });
    assert.equal(result.total, 0);
    assert.ok(logs.some((line) => line.includes('没有可同步的文件')));
  } finally {
    await started.server.close();
  }
});
