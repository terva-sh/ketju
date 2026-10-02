import { afterEach, describe, expect, it, vi } from 'vitest';
import { resetApiCache } from '../api/client';
import { anonymous } from '../bootstrap';
import member from '../../../../internal/dto/testdata/golden/me.member.json';
import { threadPage } from '../testing';
import { storeThreadPage, threadCache } from './threads';
import { notifications, refreshViewer, setViewerSettings, viewer } from './viewer';

afterEach(() => {
  vi.unstubAllGlobals();
  resetApiCache();
  viewer.value = anonymous;
  document.documentElement.removeAttribute('data-theme');
  localStorage.clear();
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

  // The account's theme wins on each /me (state/theme.ts, TKT-01M29CXK4E).
  it("takes the account's theme", async () => {
    const dark = { ...member, settings: { ...member.settings, theme: 'dark' } };
    vi.stubGlobal('fetch', async () => new Response(JSON.stringify(dark), { status: 200 }));
    localStorage.setItem('ketju.theme', 'light');
    await refreshViewer();
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
    expect(localStorage.getItem('ketju.theme')).toBe('dark');
  });

  // Terva review 1876: a /me asked for before a settings save, answered
  // after it, carries the settings from before; the save's are kept.
  it('keeps settings saved after the refresh was asked for', async () => {
    let answer: (r: Response) => void = () => {};
    vi.stubGlobal('fetch', () => new Promise<Response>((r) => (answer = r)));
    viewer.value = member;
    const refresh = refreshViewer();
    const saved = { ...member.settings, theme: 'dark', timezone: 'Europe/Paris' };
    setViewerSettings(saved);
    answer(new Response(JSON.stringify({ ...member, notifications: 9 }), { status: 200 }));
    await refresh;
    expect(viewer.value.settings).toEqual(saved);
    expect(viewer.value.notifications).toBe(9);
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
  });

  it('takes the settings of a refresh asked for after a save', async () => {
    viewer.value = member;
    setViewerSettings({ ...member.settings, theme: 'dark' });
    vi.stubGlobal('fetch', async () => new Response(JSON.stringify(member), { status: 200 }));
    await refreshViewer();
    expect(viewer.value.settings).toEqual(member.settings);
  });

  // Terva review 1879: a page size learned from /me, set on another device,
  // drops the pages cut at the old size, as a save on this one does.
  it('drops held thread pages when /me brings another page size', async () => {
    viewer.value = member;
    storeThreadPage('t1', threadPage({ id: 't1', slug: 'a', title: 'A' }));
    expect(threadCache.value.size).toBe(1);
    vi.stubGlobal('fetch', async () => new Response(JSON.stringify(member), { status: 200 }));
    await refreshViewer();
    expect(threadCache.value.size).toBe(1);
    const ten = { ...member, settings: { ...member.settings, page_size: 10 } };
    vi.stubGlobal('fetch', async () => new Response(JSON.stringify(ten), { status: 200 }));
    await refreshViewer();
    expect(threadCache.value.size).toBe(0);
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
