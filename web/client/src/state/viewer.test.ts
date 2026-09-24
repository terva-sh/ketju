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
});
