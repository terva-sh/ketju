import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { FakeEventSource } from '../testing';
import { hiddenMs, pollMs, readyMs, subscribe } from './live';

// The live-update subscription (TKT-01M29CTYA0, docs/06 "Live updates"),
// against FakeEventSource and fake timers. refresh is what a hint or a poll
// asks for: the page on screen again.

let refreshed: number;
let held: number;
let close: () => void;
const refresh = () => void refreshed++;
const flush = () => vi.advanceTimersByTimeAsync(0);
const open = () => FakeEventSource.open();
let visibility: DocumentVisibilityState;
let spy: { mockRestore(): void };

beforeEach(() => {
  vi.useFakeTimers();
  FakeEventSource.all = [];
  FakeEventSource.ready = false;
  vi.stubGlobal('EventSource', FakeEventSource);
  visibility = 'visible';
  spy = vi.spyOn(document, 'visibilityState', 'get').mockImplementation(() => visibility);
  refreshed = 0;
  held = 100;
});
afterEach(() => {
  close?.();
  FakeEventSource.ready = true;
  vi.useRealTimers();
  vi.unstubAllGlobals();
  spy.mockRestore();
});

const start = (id = 't1', r: () => Promise<unknown> | void = refresh) => (close = subscribe(id, () => held, r).close);
const setVisibility = (v: DocumentVisibilityState) => {
  visibility = v;
  document.dispatchEvent(new Event('visibilitychange'));
};

describe('the live subscription', () => {
  it('opens one stream for the thread and closes it on close()', () => {
    start('01k4 z');
    expect(open().map((s) => s.url)).toEqual(['/events?threads=01k4%20z']);
    close();
    expect(open()).toEqual([]);
    vi.advanceTimersByTime(10 * pollMs);
    expect(FakeEventSource.all).toHaveLength(1);
    expect(refreshed).toBe(0);
  });

  it('a hint longer than the held length refreshes, and one no longer does nothing', () => {
    start();
    const s = open()[0];
    s.emit('ready', {});
    s.emit('thread', { thread_id: 't1', log_length: 100 });
    s.emit('thread', { thread_id: 't1', log_length: 90 });
    s.emit('thread', { thread_id: 'other', log_length: 500 });
    expect(refreshed).toBe(0);
    s.emit('thread', { thread_id: 't1', log_length: 120 });
    expect(refreshed).toBe(1);
  });

  it('falls back to polling when ready does not arrive in 10 s', () => {
    start();
    vi.advanceTimersByTime(readyMs - 1);
    expect(open()).toHaveLength(1);
    vi.advanceTimersByTime(1);
    expect(open()).toEqual([]);
    vi.advanceTimersByTime(pollMs);
    expect(refreshed).toBe(1);
  });

  it('falls back to polling on a stream error', () => {
    start();
    open()[0].fail();
    expect(open()).toEqual([]);
    vi.advanceTimersByTime(pollMs - 1);
    expect(refreshed).toBe(0);
    vi.advanceTimersByTime(1);
    expect(refreshed).toBe(1);
    vi.advanceTimersByTime(pollMs);
    expect(refreshed).toBe(2);
  });

  it('polls with no EventSource at all', () => {
    vi.stubGlobal('EventSource', undefined);
    start();
    vi.advanceTimersByTime(2 * pollMs);
    expect(refreshed).toBe(2);
  });

  it('retries the stream every fourth poll', () => {
    start();
    open()[0].fail();
    vi.advanceTimersByTime(3 * pollMs);
    expect(refreshed).toBe(3);
    expect(FakeEventSource.all).toHaveLength(1);
    vi.advanceTimersByTime(pollMs);
    expect(refreshed).toBe(4);
    expect(FakeEventSource.all).toHaveLength(2);
    expect(open()).toHaveLength(1);
    // It fails too: polling goes on, on the same schedule, and the next
    // retry is four polls later.
    open()[0].fail();
    vi.advanceTimersByTime(4 * pollMs);
    expect(refreshed).toBe(8);
    expect(FakeEventSource.all).toHaveLength(3);
  });

  it('polls every 30 s and stops when a stream says ready', async () => {
    start();
    open()[0].fail();
    vi.advanceTimersByTime(4 * pollMs);
    expect(refreshed).toBe(4);
    open()[0].emit('ready', {});
    await flush();
    vi.advanceTimersByTime(10 * pollMs);
    expect(refreshed).toBe(4);
    expect(open()).toHaveLength(1);
    // Live again: a hint refreshes, and a later error starts polling over.
    open()[0].emit('thread', { thread_id: 't1', log_length: 101 });
    expect(refreshed).toBe(5);
    open()[0].fail();
    vi.advanceTimersByTime(pollMs);
    expect(refreshed).toBe(6);
  });

  it('a stream that says ready is not dropped at 10 s', () => {
    start();
    open()[0].emit('ready', {});
    vi.advanceTimersByTime(5 * readyMs);
    expect(open()).toHaveLength(1);
    expect(refreshed).toBe(0);
  });

  it('a hidden tab closes its stream after 60 s and reopens when shown', () => {
    start();
    open()[0].emit('ready', {});
    setVisibility('hidden');
    vi.advanceTimersByTime(hiddenMs - 1);
    expect(open()).toHaveLength(1);
    vi.advanceTimersByTime(1);
    expect(open()).toEqual([]);
    vi.advanceTimersByTime(10 * pollMs);
    expect(refreshed).toBe(0);
    setVisibility('visible');
    expect(open()).toHaveLength(1);
    expect(FakeEventSource.all).toHaveLength(2);
  });

  it('a tab shown again within 60 s keeps its stream', () => {
    start();
    open()[0].emit('ready', {});
    setVisibility('hidden');
    vi.advanceTimersByTime(hiddenMs - 1);
    setVisibility('visible');
    vi.advanceTimersByTime(hiddenMs);
    expect(open()).toHaveLength(1);
    expect(FakeEventSource.all).toHaveLength(1);
  });

  it('a hidden tab stops polling too', () => {
    start();
    open()[0].fail();
    setVisibility('hidden');
    vi.advanceTimersByTime(hiddenMs);
    const n = refreshed;
    vi.advanceTimersByTime(10 * pollMs);
    expect(refreshed).toBe(n);
    expect(open()).toEqual([]);
  });

  // Terva reviews 1779 and 1780.
  it('a failed refresh on a live stream is tried again every 30 s until one answers', async () => {
    let fails = 2;
    start('t1', () => (refreshed++, fails-- > 0 ? Promise.reject(new Error('503')) : Promise.resolve()));
    open()[0].emit('ready', {});
    open()[0].emit('thread', { thread_id: 't1', log_length: 101 });
    await flush();
    expect(refreshed).toBe(1);
    await vi.advanceTimersByTimeAsync(pollMs);
    expect(refreshed).toBe(2);
    await vi.advanceTimersByTimeAsync(pollMs);
    expect(refreshed).toBe(3);
    await vi.advanceTimersByTimeAsync(10 * pollMs);
    expect(refreshed).toBe(3);
  });

  it('a hidden tab stops trying a failed refresh, and its new stream catches up when shown', async () => {
    start('t1', () => (refreshed++, Promise.reject(new Error('503'))));
    open()[0].emit('ready', {});
    open()[0].emit('thread', { thread_id: 't1', log_length: 101 });
    await flush();
    // Hidden between retries, so one is due after the stream stops.
    await vi.advanceTimersByTimeAsync(10_000);
    setVisibility('hidden');
    await vi.advanceTimersByTimeAsync(hiddenMs);
    const n = refreshed;
    expect(open()).toEqual([]);
    await vi.advanceTimersByTimeAsync(20 * pollMs);
    expect(refreshed).toBe(n);
    setVisibility('visible');
    open()[0].emit('ready', {});
    open()[0].emit('thread', { thread_id: 't1', log_length: 101 });
    expect(refreshed).toBe(n + 1);
  });

  it('a failed refresh while polling waits for the next poll', async () => {
    start('t1', () => (refreshed++, Promise.reject(new Error('503'))));
    open()[0].fail();
    await vi.advanceTimersByTimeAsync(pollMs);
    expect(refreshed).toBe(1);
    await vi.advanceTimersByTimeAsync(pollMs - 1);
    expect(refreshed).toBe(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(refreshed).toBe(2);
  });

  it('closing leaves no stream and no timer behind', () => {
    start();
    open()[0].fail();
    setVisibility('hidden');
    close();
    expect(vi.getTimerCount()).toBe(0);
    setVisibility('visible');
    expect(open()).toEqual([]);
  });
});
