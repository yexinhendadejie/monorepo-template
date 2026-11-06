#!/usr/bin/env node
import { copyFile, lstat, mkdir, readdir, realpath, rename, rm, stat, utimes } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { createLogger, loadLogConfig } from '../apps/mockserver/src/logger.js';

const REPO_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
export const DEFAULT_ROOT = path.join(REPO_ROOT, 'machine-data');
export const SMB_SHARE = '\\\\10.128.0.21\\d';
const KEEP_FILES = new Set(['README.md']);
const JUNK = /^(?:\.DS_Store|Thumbs\.db|desktop\.ini|\..*\.swp)$/i;

export function readConfig(env = process.env, platform = process.platform) {
  const source = (env.SMB_SOURCE_DIR ?? '').trim() || (platform === 'win32' ? SMB_SHARE : '');
  if (!source) throw new Error('请先挂载 SMB 共享 \\\\10.128.0.21\\d，并设置 SMB_SOURCE_DIR 为挂载后的机台数据目录');
  if (platform !== 'win32' && source.startsWith('\\\\')) {
    throw new Error('Linux/macOS 不能直接读取 UNC 路径，请将 SMB_SOURCE_DIR 设置为 SMB 挂载目录');
  }
  if (!path.isAbsolute(source) && !(platform === 'win32' && path.win32.isAbsolute(source))) {
    throw new Error('SMB_SOURCE_DIR 必须是绝对路径');
  }
  return { source, root: path.resolve(env.MACHINE_DATA_DIR || DEFAULT_ROOT) };
}

/** 解析共享目录的相对路径到本地绝对路径，拒绝越出数据根目录。 */
export function resolveTarget(root, relativePath) {
  if (relativePath.includes('\\') || path.posix.isAbsolute(relativePath) || /^[a-z]:/i.test(relativePath)) {
    throw new Error(`拒绝写入数据目录之外的路径: ${relativePath}`);
  }
  const target = path.resolve(root, ...relativePath.split('/').filter(Boolean));
  const base = path.resolve(root);
  if (target !== base && !target.startsWith(base + path.sep)) {
    throw new Error(`拒绝写入数据目录之外的路径: ${relativePath}`);
  }
  return target;
}

async function walk(source, relativeDir, files) {
  const entries = await readdir(resolveTarget(source, relativeDir), { withFileTypes: true });
  for (const entry of entries) {
    if (JUNK.test(entry.name)) continue;
    const relative = relativeDir ? `${relativeDir}/${entry.name}` : entry.name;
    const full = resolveTarget(source, relative);
    // 不跟随共享目录中的符号链接，避免读到共享范围之外。
    if (entry.isDirectory()) await walk(source, relative, files);
    else if (entry.isFile()) {
      const info = await stat(full);
      files.push({ relative, size: info.size, modifiedAt: info.mtime });
    }
  }
}

async function localStat(target) {
  try {
    return await lstat(target);
  } catch (error) {
    if (error.code === 'ENOENT') return null;
    throw error;
  }
}

function isFresh(local, remote) {
  return local?.isFile() && local.size === remote.size &&
    Math.abs(local.mtimeMs - remote.modifiedAt.getTime()) < 2000;
}

async function safeTarget(root, relative) {
  const target = resolveTarget(root, relative);
  let current = root;
  for (const part of path.relative(root, target).split(path.sep)) {
    current = path.join(current, part);
    if ((await localStat(current))?.isSymbolicLink()) throw new Error('本地同步路径不允许符号链接');
  }
  return target;
}

async function download(source, file, root) {
  const target = await safeTarget(root, file.relative);
  await mkdir(path.dirname(target), { recursive: true });
  const partial = `${target}.part`;
  if ((await localStat(partial))?.isSymbolicLink()) throw new Error('临时文件不允许符号链接');
  try {
    await copyFile(resolveTarget(source, file.relative), partial);
    await utimes(partial, new Date(), file.modifiedAt);
    await rename(partial, target);
    return file.size;
  } finally {
    await rm(partial, { force: true });
  }
}

async function pruneLocal(root, remoteFiles) {
  const keep = new Set(remoteFiles.map((file) => resolveTarget(root, file.relative)));
  const removed = [];
  async function walkLocal(dir) {
    for (const entry of await readdir(dir, { withFileTypes: true })) {
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) {
        await walkLocal(full);
        if ((await readdir(full)).length === 0) await rm(full, { recursive: true });
      } else if (!keep.has(full) && !(dir === root && KEEP_FILES.has(entry.name))) {
        await rm(full, { force: true });
        removed.push(path.relative(root, full));
      }
    }
  }
  await walkLocal(root);
  return removed;
}

export async function sync(config, { dryRun = false, prune = false, logger = createLogger(loadLogConfig()) } = {}) {
  const start = Date.now();
  let downloaded = 0;
  let skipped = 0;
  let bytes = 0;
  // 必须先成功访问共享目录，失败时不修改本地数据。
  let source;
  try {
    source = await realpath(config.source);
    if (!(await stat(source)).isDirectory()) throw new Error('源路径不是目录');
  } catch (error) {
    throw new Error(`SMB 共享目录不可访问，请检查挂载、网络及系统登录权限（${error.code || '非目录'}）`);
  }
  const root = await realpath(config.root).catch(async (error) => {
    if (error.code !== 'ENOENT') throw error;
    // 解析已存在的父目录，识别通过父目录链接指向共享源的情况。
    const parent = path.dirname(config.root);
    if (parent === config.root) throw error;
    return path.join(await existingPath(parent), path.basename(config.root));
  });
  if (root === source || root.startsWith(source + path.sep) || source.startsWith(root + path.sep)) {
    throw new Error('SMB 源目录与本地数据目录不能相同或互相包含');
  }
  logger.debug('sync_started', { source, dry_run: dryRun, prune });
  const files = [];
  await walk(source, '', files);
  if (prune && files.length === 0) throw new Error('共享目录没有文件，拒绝清理本地数据；请检查 SMB 挂载和源目录');
  if (!dryRun) await mkdir(root, { recursive: true });
  for (const file of files) {
    const target = await safeTarget(root, file.relative);
    if (isFresh(await localStat(target), file)) {
      skipped += 1;
      continue;
    }
    if (dryRun) {
      logger.info('sync_file_planned', { path: file.relative, bytes: file.size });
      continue;
    }
    bytes += await download(source, file, root);
    downloaded += 1;
    logger.debug('sync_file_copied', { path: file.relative, bytes: file.size });
  }
  const removed = prune && !dryRun ? await pruneLocal(root, files) : [];
  const result = { total: files.length, downloaded, skipped, removed: removed.length, bytes };
  logger.info('sync_completed', { ...result, dry_run: dryRun, duration_ms: Date.now() - start });
  return result;
}

async function existingPath(target) {
  try {
    return await realpath(target);
  } catch (error) {
    if (error.code !== 'ENOENT' || path.dirname(target) === target) throw error;
    return path.join(await existingPath(path.dirname(target)), path.basename(target));
  }
}

async function main() {
  const logger = createLogger(loadLogConfig());
  try {
    const args = new Set(process.argv.slice(2));
    await sync(readConfig(), { dryRun: args.has('--dry-run'), prune: args.has('--prune'), logger });
  } catch (error) {
    logger.error('sync_failed', { err: error.message });
    process.exitCode = 1;
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) await main();
