import { afterEach, describe, expect, it, vi } from 'vitest';
import { resetApiCache } from '../api/client';
import { anonymous } from '../bootstrap';
import member from '../../../../internal/dto/testdata/golden/me.member.json';
import { notifications, refreshViewer, viewer } from './viewer';

afterEach(() => {
  vi.unstubAllGlobals();
  resetApiCache();
  viewer.value = anonymous;
});

describe('the viewer', () => {
  it('is replaced by /me, and notifications follow it', async () => {
    const urls: string[] = [];
    vi.stubGlobal('fetch', async (url: string) => (urls.push(url), new Response(JSON.stringify(member), { status: 200 })));
    viewer.value = anonymous;
    expect(notifications.value).toBe(0);
    await refreshViewer();
    expect(urls).toEqual(['/api/v1/me']);
    expect(viewer.value).toEqual(member);
    expect(notifications.value).toBe(member.notifications);
    expect(member.notifications).toBeGreaterThan(0);
  });

  it('stays as it was when /me fails', async () => {
    vi.stubGlobal('fetch', async () => new Response('{}', { status: 503 }));
    viewer.value = member;
    await expect(refreshViewer()).rejects.toBeDefined();
    expect(viewer.value).toEqual(member);
  });

  // Review 413: an older or aborted refresh landing last does not put back
  // a viewer that is no longer current.
  it('takes only the newest refresh', async () => {
    const pending: ((r: Response) => void)[] = [];
    vi.stubGlobal('fetch', () => new Promise<Response>((res) => pending.push(res)));
    const older = refreshViewer();
    const newer = refreshViewer();
    await Promise.resolve();
    pending[1](new Response(JSON.stringify(anonymous), { status: 200 }));
    await newer;
    pending[0](new Response(JSON.stringify(member), { status: 200 }));
    expect(await older).toEqual(member);
    expect(viewer.value).toEqual(anonymous);
  });

  it('does not apply an aborted refresh', async () => {
    vi.stubGlobal('fetch', async () => new Response(JSON.stringify(member), { status: 200 }));
    viewer.value = anonymous;
    const ctl = new AbortController();
    const late = refreshViewer(ctl.signal);
    ctl.abort();
    await late;
    expect(viewer.value).toEqual(anonymous);
  });
});
