import { createMockServer } from './server.js';
import { createLogger, loadLogConfig } from './logger.js';
import { loadListenConfig } from './config.js';

const config = loadLogConfig();
const logger = createLogger(config).child({ service: 'mock' });
const { host, port } = loadListenConfig();
createMockServer({ logger }).listen(port, host, () => {
  logger.info('server_started', { address: `http://${host}:${port}`, level: config.level, format: config.format });
});
