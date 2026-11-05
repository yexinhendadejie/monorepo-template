import { test } from 'node:test';
import assert from 'node:assert/strict';
import { loadListenConfig } from './config.js';

test('默认只监听回环地址与 3101 端口', () => {
  assert.deepEqual(loadListenConfig({}), { host: '127.0.0.1', port: 3101 });
});

test('容器里可以用 MOCK_HOST 换成 0.0.0.0', () => {
  assert.deepEqual(loadListenConfig({ MOCK_HOST: '0.0.0.0', MOCK_PORT: '8081' }), { host: '0.0.0.0', port: 8081 });
});

test('端口非法时启动即失败', () => {
  for (const value of ['abc', '-1', '65536', '80.5', 'NaN', '12 34']) {
    assert.throws(() => loadListenConfig({ MOCK_PORT: value }), /MOCK_PORT/, value);
  }
});

test('主机为空时启动即失败', () => {
  assert.throws(() => loadListenConfig({ MOCK_HOST: '   ' }), /MOCK_HOST/);
});
