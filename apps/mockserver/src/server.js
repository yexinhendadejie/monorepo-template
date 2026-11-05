import { createServer } from 'node:http';
import { readdir, readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { join, resolve } from 'node:path';

// 机台数据不入库：默认读仓库根目录的 machine-data/（由 make sync 同步），
// 容器里用 MOCK_DATA_DIR 指向挂载进来的目录。
const defaultRoot = process.env.MOCK_DATA_DIR
  ? resolve(process.env.MOCK_DATA_DIR)
  : fileURLToPath(new URL('../../../machine-data/', import.meta.url));

export const formats = ['pgs', 'dcex'];
export const nullLogger = { debug() {}, info() {}, warn() {}, error() {} };

// 目录里只有一种配方格式时才认作机台，避免把日志目录、备份目录当成机台。
function formatOf(names) {
  const found = new Set(
    names.map((name) => name.split('.').pop().toLowerCase()).filter((ext) => formats.includes(ext)),
  );
  return found.size === 1 ? [...found][0] : '';
}

export async function listMachines(root) {
  const entries = await readdir(root, { withFileTypes: true });
  const machines = [];
  for (const entry of entries
    .filter((item) => item.isDirectory() && !item.name.startsWith('.'))
    .sort((a, b) => a.name.localeCompare(b.name))) {
    const format = formatOf(await readdir(join(root, entry.name)));
    if (format) machines.push({ id: entry.name, name: entry.name, format, process: '' });
  }
  return machines;
}

export function createMockServer({ root = defaultRoot, logger = nullLogger } = {}) {
  return createServer(async (req, res) => {
    const started = Date.now();
    const log = logger.child ? logger : nullLogger;
    res.on('finish', () => {
      const level = res.statusCode >= 500 ? 'error' : res.statusCode >= 400 ? 'warn' : 'info';
      log[level]('http_request', {
        method: req.method,
        path: req.url,
        status: res.statusCode,
        duration_ms: Date.now() - started,
      });
    });
    const json = (status, value) => {
      res.writeHead(status, { 'content-type': 'application/json; charset=utf-8' });
      res.end(JSON.stringify(value));
    };
    if (req.method !== 'GET') {
      res.setHeader('allow', 'GET');
      return json(405, { error: '仅支持读取机台数据' });
    }
    try {
      const path = new URL(req.url, 'http://localhost').pathname;
      if (path === '/healthz') return json(200, { status: 'ok' });
      if (path === '/api/machines') return json(200, await listMachines(root));
      const match = path.match(/^\/api\/machines\/([^/]+)\/recipes(?:\/([^/]+)\/content)?$/);
      if (!match) return json(404, { error: '接口不存在' });
      const machineId = decodeURIComponent(match[1]);
      // 目录名不允许包含路径分隔符，否则 join 之后会读到数据目录之外。
      if (!machineId || machineId === '.' || machineId === '..' || /[\\/]/.test(machineId)) {
        return json(404, { error: '机台不存在' });
      }
      let names;
      try {
        names = await readdir(join(root, machineId));
      } catch (error) {
        if (error.code !== 'ENOENT') throw error;
        // 数据目录本身不存在属于部署问题（500），只有机台目录缺失才是 404。
        try {
          await readdir(root);
        } catch {
          throw error;
        }
        return json(404, { error: '机台不存在' });
      }
      const format = formatOf(names);
      if (!format) return json(404, { error: '机台不存在' });
      const files = names.filter((name) => name.toLowerCase().endsWith(`.${format}`)).sort();
      if (!match[2]) return json(200, files.map((name) => ({ id: name, name, format })));
      const filename = decodeURIComponent(match[2]);
      if (!files.includes(filename)) return json(404, { error: '配方不存在' });
      const data = await readFile(join(root, machineId, filename));
      log.debug('recipe_read', { machine: machineId, recipe: filename, bytes: data.length });
      res.writeHead(200, { 'content-type': 'application/octet-stream', 'content-length': data.length });
      res.end(data);
    } catch (error) {
      if (error instanceof URIError) return json(400, { error: '无效的路径编码' });
      log.error('machine_data_read_failed', { path: req.url, err: error.message });
      json(500, { error: '机台数据读取失败' });
    }
  });
}
