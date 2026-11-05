import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createLogger, loadLogConfig } from './logger.js';

function capture() {
  const lines = [];
  return { lines, sink: { write: (chunk) => lines.push(chunk) } };
}

test('loadLogConfig defaults to info text and validates values', () => {
  assert.deepEqual(loadLogConfig({}), { level: 'info', format: 'text' });
  assert.deepEqual(loadLogConfig({ LOG_LEVEL: 'DEBUG', LOG_FORMAT: 'JSON' }), { level: 'debug', format: 'json' });
  assert.throws(() => loadLogConfig({ LOG_LEVEL: 'verbose' }), /LOG_LEVEL must be/);
  assert.throws(() => loadLogConfig({ LOG_FORMAT: 'xml' }), /LOG_FORMAT must be/);
});

test('createLogger filters entries below the configured level', () => {
  const { lines, sink } = capture();
  const logger = createLogger({ level: 'warn', sink });
  logger.debug('skipped');
  logger.info('skipped too');
  logger.warn('kept');
  logger.error('kept as well');
  assert.equal(lines.length, 2);
  assert.match(lines[0], /level=WARN msg=kept/);
  assert.match(lines[1], /level=ERROR msg=kept as well/);
});

test('text format renders structured fields and quotes unsafe strings', () => {
  const { lines, sink } = capture();
  const logger = createLogger({ level: 'debug', sink, now: () => new Date('2026-01-02T03:04:05.000Z') });
  logger.info('recipe_read', { machine: 'ABMCPT01', bytes: 4801, ok: true, note: 'two words' });
  assert.equal(
    lines[0],
    '2026-01-02T03:04:05.000Z level=INFO msg=recipe_read machine=ABMCPT01 bytes=4801 ok=true note="two words"\n',
  );
});

test('json format emits one parsable record per entry', () => {
  const { lines, sink } = capture();
  const logger = createLogger({ level: 'debug', format: 'json', sink });
  logger.warn('upstream_slow', { duration_ms: 1200 });
  const record = JSON.parse(lines[0]);
  assert.equal(record.level, 'WARN');
  assert.equal(record.msg, 'upstream_slow');
  assert.equal(record.duration_ms, 1200);
  assert.match(record.time, /^\d{4}-\d{2}-\d{2}T/);
});

test('child loggers merge base fields and keep the level threshold', () => {
  const { lines, sink } = capture();
  const logger = createLogger({ level: 'info', sink }).child({ service: 'mock' });
  logger.debug('dropped');
  logger.info('served', { path: '/api/machines' });
  assert.equal(lines.length, 1);
  assert.match(lines[0], /service=mock/);
  assert.match(lines[0], /path=\/api\/machines/);
});

test('createLogger rejects unknown levels', () => {
  assert.throws(() => createLogger({ level: 'trace' }), /unknown level/);
});
