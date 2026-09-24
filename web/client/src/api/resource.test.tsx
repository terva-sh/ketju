import { render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, describe, expect, it } from 'vitest';
import { ProblemError } from './problem';
import { useResource, type Resource } from './resource';

// A deferred request the test settles by hand.
function deferred<T>() {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => ((resolve = res), (reject = rej)));
  return { promise, resolve, reject };
}

// mount renders useResource with the given key, recording every render's
// answer.
function mount<T>(fetcher: (key: string, signal: AbortSignal) => Promise<T>, peek?: (key: string) => T | undefined) {
  const seen: { key: string | null; res: Resource<T> }[] = [];
  const root = document.createElement('div');
  function Probe({ k }: { k: string | null }) {
    const res = useResource(k, (s) => fetcher(k!, s), peek && (() => peek(k!)));
    seen.push({ key: k, res });
    return null;
  }
  return {
    seen,
    last: () => seen[seen.length - 1].res,
    show: (k: string | null) => act(() => render(<Probe k={k} />, root)),
    unmount: () => act(() => render(null, root)),
  };
}

const flush = () => act(() => new Promise((r) => setTimeout(r, 0)));

let cleanup: (() => unknown) | undefined;
afterEach(async () => {
  await cleanup?.();
  cleanup = undefined;
});

describe('useResource', () => {
  // AC4: an error ends loading, and retry reaches data.
  it('ends loading on an error and recovers on retry', async () => {
    let calls = 0;
    const m = mount(async () => {
      if (calls++ === 0) throw new TypeError('network down');
      return 'data';
    });
    cleanup = m.unmount;
    await m.show('a');
    await flush();
    expect(m.last().loading).toBe(false);
    expect(m.last().error?.type).toBe('network');
    expect(m.last().data).toBeUndefined();

    await act(() => m.last().retry());
    expect(m.last().loading).toBe(true);
    expect(m.last().error).toBeUndefined();
    await flush();
    expect(m.last()).toMatchObject({ data: 'data', error: undefined, loading: false });
    expect(calls).toBe(2);
  });

  it('keeps what this key had when a retry fails', async () => {
    let calls = 0;
    const m = mount(async () => {
      if (calls++ === 0) return 'first';
      throw new ProblemError('unavailable', 503);
    });
    cleanup = m.unmount;
    await m.show('a');
    await flush();
    await act(() => m.last().retry());
    await flush();
    expect(m.last()).toMatchObject({ data: 'first', loading: false });
    expect(m.last().error?.type).toBe('unavailable');
  });

  // Review 411: an error that withdraws access withdraws the data with it.
  it.each([
    ['not-found', 404],
    ['gone', 410],
    ['forbidden', 403],
    ['unauthenticated', 401],
  ] as const)('drops what this key had when a retry answers %s', async (type, status) => {
    let calls = 0;
    const m = mount(async () => {
      if (calls++ === 0) return 'first';
      throw new ProblemError(type, status);
    });
    cleanup = m.unmount;
    await m.show('a');
    await flush();
    await act(() => m.last().retry());
    await flush();
    expect(m.last()).toMatchObject({ data: undefined, loading: false });
    expect(m.last().error?.type).toBe(type);
  });

  it("never answers a key with another key's data or error, even for one render", async () => {
    const pending = deferred<string>();
    const m = mount(async (k) => {
      if (k === 'a') return 'for a';
      if (k === 'b') throw new ProblemError('not-found', 404);
      return pending.promise;
    });
    cleanup = m.unmount;
    await m.show('a');
    await flush();
    await m.show('b');
    await flush();
    await m.show('c');
    await flush();
    for (const { key, res } of m.seen) {
      if (key !== 'a') expect(res.data).not.toBe('for a');
      if (key !== 'b') expect(res.error).toBeUndefined();
    }
    expect(m.last().loading).toBe(true);
  });

  it('ignores a superseded request whatever it settles with, and aborts it', async () => {
    const first = deferred<string>();
    let aborted = false;
    const m = mount(async (k, signal) => {
      if (k === 'a') {
        signal.addEventListener('abort', () => (aborted = true));
        return first.promise;
      }
      return 'for b';
    });
    cleanup = m.unmount;
    await m.show('a');
    await m.show('b');
    await flush();
    expect(aborted).toBe(true);
    await act(() => first.reject(new TypeError('late')));
    await flush();
    expect(m.last()).toMatchObject({ data: 'for b', error: undefined, loading: false });
  });

  // A superseded request that succeeds late is as old as one that fails
  // late. Taking its answer would overwrite the current key's, and the screen
  // would wait for a request that already finished.
  it('ignores a superseded request that succeeds late', async () => {
    const first = deferred<string>();
    const m = mount(async (k) => (k === 'a' ? first.promise : 'for b'));
    cleanup = m.unmount;
    await m.show('a');
    await m.show('b');
    await flush();
    await act(() => first.resolve('for a'));
    await flush();
    expect(m.last()).toMatchObject({ data: 'for b', error: undefined, loading: false });
  });

  it('shows the held value at once and replaces it with the answer', async () => {
    const answer = deferred<string>();
    const m = mount(
      () => answer.promise,
      (k) => (k === 'a' ? 'held' : undefined),
    );
    cleanup = m.unmount;
    await m.show('a');
    expect(m.seen[0].res).toMatchObject({ data: 'held', loading: true });
    await act(() => answer.resolve('fresh'));
    await flush();
    expect(m.last()).toMatchObject({ data: 'fresh', loading: false });
  });

  it('asks for nothing with a null key', async () => {
    let calls = 0;
    const m = mount(async () => (calls++, 'x'));
    cleanup = m.unmount;
    await m.show(null);
    await flush();
    expect(calls).toBe(0);
    expect(m.last()).toMatchObject({ data: undefined, error: undefined, loading: false });
  });

  // An abort the hook did not cause is a failure like any other: only its
  // own abort, of a superseded request, is silent. Ignoring this one would
  // leave the screen loading for good.
  it('ends an abort it did not cause in an error', async () => {
    const m = mount(async () => {
      throw new DOMException('aborted', 'AbortError');
    });
    cleanup = m.unmount;
    await m.show('a');
    await flush();
    expect(m.last().loading).toBe(false);
    expect(m.last().error).toBeDefined();
  });
});
