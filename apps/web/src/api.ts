import { useEffect, useState } from 'react';

export async function getJSON<T>(url: string, signal: AbortSignal): Promise<T> {
  const response = await fetch(url, { signal });
  if (!response.ok) {
    let message = `读取失败（HTTP ${response.status}）`;
    try { const body = await response.json(); if (typeof body.error === 'string') message = body.error; } catch { /* Non-JSON upstream errors use the HTTP fallback. */ }
    throw new Error(message);
  }
  return response.json() as Promise<T>;
}

export function useRemote<T>(url: string | undefined, revision: number) {
  const [snapshot, setSnapshot] = useState<{ key: string; data?: T; error?: string }>({ key: '' });
  const key = `${url ?? ''}:${revision}`;
  useEffect(() => {
    if (!url) return;
    const controller = new AbortController();
    getJSON<T>(url, controller.signal).then(
      (data) => { if (!controller.signal.aborted) setSnapshot({ key, data }); },
      (error: unknown) => { if (!controller.signal.aborted) setSnapshot({ key, error: error instanceof Error ? error.message : '读取失败，请重试' }); },
    );
    return () => controller.abort();
  }, [url, key]);
  const current = url && snapshot.key === key ? snapshot : undefined;
  return { data: current?.data, error: current?.error, loading: Boolean(url && !current) };
}
