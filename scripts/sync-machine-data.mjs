#!/usr/bin/env node
import { Client } from 'basic-ftp';
import { mkdir, readdir, rename, rm, stat, utimes } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const REPO_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
export const DEFAULT_ROOT = path.join(REPO_ROOT, 'machine-data');
const KEEP_FILES = new Set(['README.md']);
const JUNK = /^(?:\.DS_Store|Thumbs\.db|desktop\.ini|\..*\.swp)$/i;

export function readConfig(env = process.env) {
  const host = (env.FTP_HOST ?? '').trim();
  if (!host) throw new Error('缺少 FTP_HOST；请在 .env 或环境变量中配置云盘地址');
  const secure = /^(1|true|yes)$/i.test(env.FTP_SECURE ?? '');
  return {
    host,
    port: Number(env.FTP_PORT ?? 21),
    user: env.FTP_USER || 'anonymous',
    password: env.FTP_PASSWORD ?? '',
    secure,
    remotePath: env.FTP_PATH || '/',
    root: path.resolve(env.MACHINE_DATA_DIR || DEFAULT_ROOT),
  };
}

/** 解析远端相对路径到本地绝对路径，拒绝越出数据根目录的路径。 */
export function resolveTarget(root, relativePath) {
  const target = path.resolve(root, ...relativePath.split('/').filter(Boolean));
  const base = path.resolve(root);
  if (target !== base && !target.startsWith(base + path.sep)) {
    throw new Error(`拒绝写入数据目录之外的路径: ${relativePath}`);
  }
  return target;
}

async function walk(client, remoteDir, relativeDir, files) {
  await client.cd(remoteDir);
  const entries = await client.list();
  for (const entry of entries) {
    if (JUNK.test(entry.name)) continue;
    const relative = relativeDir ? `${relativeDir}/${entry.name}` : entry.name;
    if (entry.isDirectory) {
      await walk(client, `${remoteDir}/${entry.name}`, relative, files);
      await client.cd(remoteDir);
      continue;
    }
    if (entry.isFile) {
      let modifiedAt = entry.modifiedAt;
      if (!modifiedAt) {
        modifiedAt = await client.lastMod(`${remoteDir}/${entry.name}`).catch(() => undefined);
      }
      files.push({ relative, size: entry.size, modifiedAt });
    }
  }
}

async function localStat(target) {
  try {
    return await stat(target);
  } catch {
    return null;
  }
}

function isFresh(local, remote) {
  if (!local || local.size !== remote.size) return false;
  if (!remote.modifiedAt) return true;
  return Math.abs(local.mtimeMs - remote.modifiedAt.getTime()) < 2000;
}

async function download(client, remoteDir, file, root) {
  const target = resolveTarget(root, file.relative);
  await mkdir(path.dirname(target), { recursive: true });
  const partial = `${target}.part`;
  await client.downloadTo(partial, `${remoteDir}/${file.relative}`);
  await rename(partial, target);
  if (file.modifiedAt) await utimes(target, new Date(), file.modifiedAt);
  return stat(target).then((info) => info.size);
}

async function pruneLocal(root, remoteFiles) {
  const keep = new Set(remoteFiles.map((file) => path.resolve(resolveTarget(root, file.relative))));
  const removed = [];
  async function walkLocal(dir) {
    let entries;
    try {
      entries = await readdir(dir, { withFileTypes: true });
    } catch {
      return;
    }
    for (const entry of entries) {
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) {
        await walkLocal(full);
        const rest = await readdir(full).catch(() => ['x']);
        if (rest.length === 0) await rm(full, { recursive: true, force: true });
      } else if (!keep.has(path.resolve(full)) && !(dir === root && KEEP_FILES.has(entry.name))) {
        await rm(full, { force: true });
        removed.push(path.relative(root, full));
      }
    }
  }
  await walkLocal(root);
  return removed;
}

export async function sync(config, { dryRun = false, prune = false, log = console.log } = {}) {
  const client = new Client(60_000);
  let downloaded = 0;
  let skipped = 0;
  let bytes = 0;
  try {
    await client.access({
      host: config.host,
      port: config.port,
      user: config.user,
      password: config.password,
      secure: config.secure,
    });
    const remoteDir = config.remotePath.replace(/\/+$/, '') || '/';
    const files = [];
    await walk(client, remoteDir, '', files);
    if (files.length === 0) log(`远端 ${remoteDir} 没有可同步的文件`);
    if (!dryRun) await mkdir(config.root, { recursive: true });
    for (const file of files) {
      const target = resolveTarget(config.root, file.relative);
      const local = await localStat(target);
      if (isFresh(local, file)) {
        skipped += 1;
        continue;
      }
      if (dryRun) {
        log(`将下载 ${file.relative}（${file.size} 字节）`);
        continue;
      }
      bytes += await download(client, remoteDir, file, config.root);
      downloaded += 1;
      log(`已下载 ${file.relative}`);
    }
    let removed = [];
    if (prune && !dryRun) removed = await pruneLocal(config.root, files);
    log(
      `${dryRun ? '试运行' : '同步完成'}：下载 ${downloaded}，跳过 ${skipped}，` +
        `清理 ${removed.length}${dryRun ? '' : `，共 ${bytes} 字节`}`,
    );
    return { total: files.length, downloaded, skipped, removed: removed.length, bytes };
  } finally {
    client.close();
  }
}

async function main() {
  const args = new Set(process.argv.slice(2));
  const config = readConfig();
  await sync(config, { dryRun: args.has('--dry-run'), prune: args.has('--prune') });
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().catch((error) => {
    console.error(`同步失败：${error.message}`);
    process.exitCode = 1;
  });
}
