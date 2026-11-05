// 监听参数从环境变量读取，容器里需要 0.0.0.0，本机开发默认只监听回环地址。
export function loadListenConfig(env = process.env) {
  const port = Number(env.MOCK_PORT ?? 3101);
  if (!Number.isInteger(port) || port < 0 || port > 65535) {
    throw new Error('MOCK_PORT must be an integer between 0 and 65535');
  }
  const host = env.MOCK_HOST ?? '127.0.0.1';
  if (!host.trim()) throw new Error('MOCK_HOST must not be empty');
  return { host, port };
}
