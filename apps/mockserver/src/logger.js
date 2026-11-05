const LEVELS = { debug: 10, info: 20, warn: 30, error: 40 };

export function loadLogConfig(env = process.env) {
  const level = (env.LOG_LEVEL ?? 'info').toLowerCase();
  if (!(level in LEVELS)) throw new Error('LOG_LEVEL must be debug, info, warn or error');
  const format = (env.LOG_FORMAT ?? 'text').toLowerCase();
  if (format !== 'text' && format !== 'json') throw new Error('LOG_FORMAT must be text or json');
  return { level, format };
}

export function createLogger({ level = 'info', format = 'text', sink = process.stdout, now = () => new Date() } = {}) {
  const threshold = LEVELS[level];
  if (threshold === undefined) throw new Error(`unknown level: ${level}`);
  const write = (entryLevel, msg, fields = {}) => {
    if (LEVELS[entryLevel] < threshold) return;
    const time = now().toISOString();
    const line =
      format === 'json'
        ? JSON.stringify({ time, level: entryLevel.toUpperCase(), msg, ...fields })
        : `${time} level=${entryLevel.toUpperCase()} msg=${msg} ${Object.entries(fields)
            .map(([key, value]) => `${key}=${formatValue(value)}`)
            .join(' ')}`.trim();
    sink.write(line + '\n');
  };
  return {
    level,
    debug: (msg, fields) => write('debug', msg, fields),
    info: (msg, fields) => write('info', msg, fields),
    warn: (msg, fields) => write('warn', msg, fields),
    error: (msg, fields) => write('error', msg, fields),
    child: (base) => {
      const inner = createLogger({ level, format, sink, now });
      return {
        level,
        debug: (msg, fields) => inner.debug(msg, { ...base, ...fields }),
        info: (msg, fields) => inner.info(msg, { ...base, ...fields }),
        warn: (msg, fields) => inner.warn(msg, { ...base, ...fields }),
        error: (msg, fields) => inner.error(msg, { ...base, ...fields }),
      };
    },
  };
}

function formatValue(value) {
  if (typeof value === 'string' && /^[\w./:-]+$/.test(value)) return value;
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  return JSON.stringify(value);
}
