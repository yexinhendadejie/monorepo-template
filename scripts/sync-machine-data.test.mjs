import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, mkdir, readFile, rm, stat, symlink, utimes, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { readConfig, resolveTarget, SMB_SHARE, sync } from './sync-machine-data.mjs';
import { createLogger } from '../apps/mockserver/src/logger.js';

async function fixture(t) {
  const workspace = await mkdtemp(path.join(os.tmpdir(), 'recipe-smb-'));
  t.after(() => rm(workspace, { recursive: true, force: true }));
  const source = path.join(workspace, 'share');
  const root = path.join(workspace, 'local');
  await mkdir(source);
  const logs = [];
  const logger = createLogger({ level: 'debug', format: 'json', sink: {
    write: (line) => logs.push(JSON.parse(line)),
  } });
  async function put(relative, content = 'recipe', when = new Date('2024-03-01T02:03:04Z')) {
    const target = path.join(source, relative);
    await mkdir(path.dirname(target), { recursive: true });
    await writeFile(target, content);
    await utimes(target, when, when);
  }
  return { workspace, source, root, logs, logger, put, config: { source, root } };
}

test('Windows defaults to the production UNC share and reuses OS authentication', () => {
  const config = readConfig({}, 'win32');
  assert.equal(config.source, '\\\\10.128.0.21\\d');
  assert.equal(config.source, SMB_SHARE);
  assert.equal('password' in config, false);
  assert.equal(readConfig({ SMB_SOURCE_DIR: '\\\\10.128.0.21\\d\\recipes' }, 'win32').source,
    '\\\\10.128.0.21\\d\\recipes');
});

test('Linux/macOS require an absolute mounted SMB directory', () => {
  for (const platform of ['linux', 'darwin']) {
    assert.throws(() => readConfig({}, platform), /挂载 SMB/);
    assert.throws(() => readConfig({ SMB_SOURCE_DIR: SMB_SHARE }, platform), /挂载目录/);
    assert.throws(() => readConfig({ SMB_SOURCE_DIR: 'relative' }, platform), /绝对路径/);
    assert.equal(readConfig({ SMB_SOURCE_DIR: ' /mnt/recipes ' }, platform).source, '/mnt/recipes');
  }
  assert.equal(readConfig({ SMB_SOURCE_DIR: '/mnt/recipes', MACHINE_DATA_DIR: '/tmp/data' }).root, '/tmp/data');
});

test('old FTP configuration cannot silently connect to the wrong protocol', () => {
  assert.throws(() => readConfig({ FTP_HOST: '10.128.0.21' }, 'linux'), /SMB_SOURCE_DIR/);
});

test('copies a nested share, preserves binary bytes and mtimes, and skips junk', async (t) => {
  const f = await fixture(t);
  const content = Buffer.from([0, 255, 13, 10, 128]);
  await f.put('ABMCPT01/a.pgs', content);
  await f.put('ABMCPT03/backup/b.dcex', 'second');
  for (const junk of ['.DS_Store', 'Thumbs.db', 'desktop.ini', '.file.swp']) await f.put(`ABMCPT01/${junk}`);
  const result = await sync(f.config, { logger: f.logger });
  assert.deepEqual(result, { total: 2, downloaded: 2, skipped: 0, removed: 0, bytes: 11 });
  assert.deepEqual(await readFile(path.join(f.root, 'ABMCPT01/a.pgs')), content);
  assert.equal(await readFile(path.join(f.root, 'ABMCPT03/backup/b.dcex'), 'utf8'), 'second');
  assert.ok(Math.abs((await stat(path.join(f.root, 'ABMCPT01/a.pgs'))).mtimeMs -
    new Date('2024-03-01T02:03:04Z').getTime()) < 2000);
  await assert.rejects(stat(path.join(f.root, 'ABMCPT01/.DS_Store')), { code: 'ENOENT' });
  assert.equal(f.logs.find((entry) => entry.msg === 'sync_completed').downloaded, 2);
  assert.equal(f.logs.find((entry) => entry.msg === 'sync_file_copied').bytes, 5);
  await assert.rejects(stat(path.join(f.root, 'ABMCPT01/a.pgs.part')), { code: 'ENOENT' });
});

test('skips unchanged files and replaces files after size or mtime changes', async (t) => {
  const f = await fixture(t);
  await f.put('M/a.pgs', 'old');
  await f.put('M/b.pgs', 'same');
  await sync(f.config, { logger: f.logger });
  assert.equal((await sync(f.config, { logger: f.logger })).skipped, 2);
  await f.put('M/a.pgs', 'new', new Date('2025-01-01Z'));
  await f.put('M/b.pgs', 'bigger');
  const result = await sync(f.config, { logger: f.logger });
  assert.equal(result.downloaded, 2);
  assert.equal(await readFile(path.join(f.root, 'M/a.pgs'), 'utf8'), 'new');
});

test('dry run reports plans without creating a destination or deleting stale data', async (t) => {
  const f = await fixture(t);
  await f.put('M/a.pgs');
  assert.equal((await sync(f.config, { dryRun: true, prune: true, logger: f.logger })).downloaded, 0);
  assert.equal(f.logs.find((entry) => entry.msg === 'sync_file_planned').path, 'M/a.pgs');
  await assert.rejects(stat(f.root), { code: 'ENOENT' });
  await mkdir(f.root);
  await writeFile(path.join(f.root, 'stale.pgs'), 'keep');
  await sync(f.config, { dryRun: true, prune: true, logger: f.logger });
  assert.equal(await readFile(path.join(f.root, 'stale.pgs'), 'utf8'), 'keep');
});

test('prune removes stale data and empty directories while preserving README', async (t) => {
  const f = await fixture(t);
  await f.put('M/a.pgs');
  await mkdir(path.join(f.root, 'old'), { recursive: true });
  await writeFile(path.join(f.root, 'old/stale.pgs'), 'old');
  await writeFile(path.join(f.root, 'README.md'), 'tracked');
  const result = await sync(f.config, { prune: true, logger: f.logger });
  assert.equal(result.removed, 1);
  assert.equal(await readFile(path.join(f.root, 'README.md'), 'utf8'), 'tracked');
  await assert.rejects(stat(path.join(f.root, 'old')), { code: 'ENOENT' });
});

test('unavailable shares and empty shares never prune existing local data', async (t) => {
  const f = await fixture(t);
  await mkdir(f.root);
  await writeFile(path.join(f.root, 'keep.pgs'), 'keep');
  await assert.rejects(sync({ ...f.config, source: path.join(f.workspace, 'missing') },
    { prune: true, logger: f.logger }), /SMB 共享目录不可访问/);
  await assert.rejects(sync(f.config, { prune: true, logger: f.logger }), /拒绝清理/);
  assert.equal(await readFile(path.join(f.root, 'keep.pgs'), 'utf8'), 'keep');
  const result = await sync(f.config, { logger: f.logger });
  assert.equal(result.total, 0);
});

test('rejects a file as source before touching local files', async (t) => {
  const f = await fixture(t);
  await f.put('a.pgs');
  await assert.rejects(sync({ ...f.config, source: path.join(f.source, 'a.pgs') },
    { logger: f.logger }), /SMB 共享目录不可访问/);
  await assert.rejects(stat(f.root), { code: 'ENOENT' });
});

test('rejects equal and overlapping source/destination directories', async (t) => {
  const f = await fixture(t);
  for (const root of [f.source, path.join(f.source, 'nested'), f.workspace]) {
    await assert.rejects(sync({ source: f.source, root }, { logger: f.logger }), /互相包含/);
  }
});

test('does not follow source symlinks or destination directory symlinks', async (t) => {
  const f = await fixture(t);
  await f.put('M/a.pgs');
  const outside = path.join(f.workspace, 'outside');
  await mkdir(outside);
  await writeFile(path.join(outside, 'private.pgs'), 'private');
  await symlink(outside, path.join(f.source, 'linked'));
  assert.equal((await sync(f.config, { logger: f.logger })).total, 1);
  await rm(path.join(f.root, 'M'), { recursive: true });
  await symlink(outside, path.join(f.root, 'M'));
  await assert.rejects(sync(f.config, { logger: f.logger }), /不允许符号链接/);
  await assert.rejects(stat(path.join(outside, 'a.pgs')), { code: 'ENOENT' });
});

test('rejects temporary file symlinks and preserves the existing recipe', async (t) => {
  const f = await fixture(t);
  await f.put('a.pgs', 'new');
  await mkdir(f.root);
  const outside = path.join(f.workspace, 'outside.pgs');
  await writeFile(outside, 'private');
  await writeFile(path.join(f.root, 'a.pgs'), 'old');
  await symlink(outside, path.join(f.root, 'a.pgs.part'));
  await assert.rejects(sync(f.config, { logger: f.logger }), /临时文件不允许符号链接/);
  assert.equal(await readFile(outside, 'utf8'), 'private');
  assert.equal(await readFile(path.join(f.root, 'a.pgs'), 'utf8'), 'old');
});

test('rejects POSIX and Windows traversal and absolute paths', () => {
  assert.equal(resolveTarget('/tmp/data', 'M/a.pgs'), '/tmp/data/M/a.pgs');
  for (const name of ['../etc/passwd', 'a/../../b', '..\\private', '/etc/passwd', 'C:\\private']) {
    assert.throws(() => resolveTarget('/tmp/data', name), /数据目录之外/);
  }
});

test('JSON logging filters debug at info level and omits credentials and file content', async (t) => {
  const f = await fixture(t);
  await f.put('a.pgs', 'sensitive recipe');
  const lines = [];
  const logger = createLogger({ level: 'info', format: 'json', sink: { write: (line) => lines.push(line) } });
  await sync(f.config, { logger });
  assert.equal(lines.length, 1);
  assert.equal(JSON.parse(lines[0]).msg, 'sync_completed');
  assert.equal(lines.join('').includes('sensitive recipe'), false);
  assert.equal(lines.join('').includes('password'), false);
});
