import { act, renderHook, waitFor } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { getJSON, useRemote } from './api';

it('passes cancellation signal and returns successful JSON', async () => {
  const signal = new AbortController().signal;
  const fetch = vi.fn().mockResolvedValue(new Response('{"ok":true}'));
  vi.stubGlobal('fetch', fetch);
  expect(await getJSON('/api/test', signal)).toEqual({ ok: true });
  expect(fetch).toHaveBeenCalledWith('/api/test', { signal });
});

it.each([
  [new Response('{"error":"file broken"}', { status: 422 }), 'file broken'],
  [new Response('upstream HTML', { status: 502 }), '读取失败（HTTP 502）'],
  [new Response('{"error":123}', { status: 500 }), '读取失败（HTTP 500）'],
])('uses readable errors and HTTP fallback', async (response, message) => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response));
  await expect(getJSON('/api/test', new AbortController().signal)).rejects.toThrow(message);
});

it('rejects malformed success JSON and network failures', async () => {
  const fetch = vi.fn().mockResolvedValueOnce(new Response('invalid')).mockRejectedValueOnce(new Error('network offline'));
  vi.stubGlobal('fetch', fetch);
  await expect(getJSON('/api/test', new AbortController().signal)).rejects.toThrow();
  await expect(getJSON('/api/test', new AbortController().signal)).rejects.toThrow('network offline');
});

it('does not request when URL is absent', () => {
  const fetch = vi.fn(); vi.stubGlobal('fetch', fetch);
  const { result } = renderHook(() => useRemote(undefined, 0));
  expect(result.current).toEqual({ data: undefined, error: undefined, loading: false });
  expect(fetch).not.toHaveBeenCalled();
});

it('cancels obsolete requests and ignores late responses even if transport ignores abort', async () => {
  let completeFirst!: (value: Response) => void;
  let firstSignal!: AbortSignal;
  const fetch = vi.fn().mockImplementationOnce((_: string, { signal }: { signal: AbortSignal }) => {
    firstSignal = signal;
    return new Promise((resolve) => { completeFirst = resolve; });
  }).mockResolvedValueOnce(new Response('{"name":"new"}'));
  vi.stubGlobal('fetch', fetch);
  const { result, rerender } = renderHook(({ url }) => useRemote<{ name: string }>(url, 0), { initialProps: { url: '/first' } });
  expect(result.current.loading).toBe(true);
  rerender({ url: '/second' });
  expect(firstSignal.aborted).toBe(true);
  await waitFor(() => expect(result.current.data).toEqual({ name: 'new' }));
  await act(async () => { completeFirst(new Response('{"name":"old"}')); });
  expect(result.current.data).toEqual({ name: 'new' });
});

it('ignores errors from obsolete requests', async () => {
  let rejectFirst!: (reason: Error) => void;
  vi.stubGlobal('fetch', vi.fn().mockImplementationOnce(() => new Promise((_, reject) => { rejectFirst = reject; })).mockResolvedValueOnce(new Response('[]')));
  const { result, rerender } = renderHook(({ url }) => useRemote(url, 0), { initialProps: { url: '/first' } });
  rerender({ url: '/second' });
  await waitFor(() => expect(result.current.data).toEqual([]));
  await act(async () => rejectFirst(new Error('old failure')));
  expect(result.current.error).toBeUndefined();
});

it('exposes unexpected rejection values as a readable generic error', async () => {
  vi.stubGlobal('fetch', vi.fn().mockRejectedValue('bad transport'));
  const { result } = renderHook(() => useRemote('/first', 0));
  await waitFor(() => expect(result.current.error).toBe('读取失败，请重试'));
});

it('aborts pending requests on unmount', () => {
  let signal!: AbortSignal;
  vi.stubGlobal('fetch', vi.fn().mockImplementation((_: string, init: { signal: AbortSignal }) => {
    signal = init.signal; return new Promise(() => {});
  }));
  const { unmount } = renderHook(() => useRemote('/pending', 0));
  unmount(); expect(signal.aborted).toBe(true);
});
