import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { resetApiCache } from '../api/client';
import type { Me } from '../api/dto';
import anon from '../../../../internal/dto/testdata/golden/me.anon.json';
import member from '../../../../internal/dto/testdata/golden/me.member.json';
import preUser from '../../../../internal/dto/testdata/golden/me.pre_user.json';
import { viewer } from './viewer';
import { guardViewer } from './viewerGuard';

// The viewer guard (TKT-01M3QVWKAG): after a sign-out, another tab and a
// page the back/forward cache restores do not go on showing the viewer who
// left. The member golden's handle is alice.

// Channel stands in for BroadcastChannel: every open channel of a name hears
// what another sends, as tabs of one browser do, and never its own.
class Channel {
  static open: Channel[] = [];
  onmessage: ((e: MessageEvent) => void) | null = null;
  sent: unknown[] = [];
  constructor(readonly name: string) {
    Channel.open.push(this);
  }
  postMessage(data: unknown) {
    this.sent.push(data);
    for (const c of Channel.open) if (c !== this && c.name === this.name) c.onmessage?.(new MessageEvent('message', { data }));
  }
  close() {
    Channel.open = Channel.open.filter((c) => c !== this);
  }
}

const as = (m: unknown) => structuredClone(m) as Me;
const withHandle = (handle: string) => {
  const m = as(member);
  m.user!.handle = handle;
  return m;
};

let saved: Me;
let stop: (() => void) | undefined;
let reload: ReturnType<typeof vi.fn>;
beforeEach(() => {
  saved = viewer.value;
  Channel.open = [];
  vi.stubGlobal('BroadcastChannel', Channel);
  reload = vi.fn();
});
afterEach(() => {
  stop?.();
  stop = undefined;
  viewer.value = saved;
  vi.unstubAllGlobals();
  resetApiCache();
});

// tab starts the guard for viewer me, as a page load does.
function tab(me: Me) {
  viewer.value = me;
  stop = guardViewer(reload as unknown as () => void);
  return Channel.open.at(-1)!;
}

// another is a second tab's channel, which says who it loaded as.
const another = () => new Channel('ketju.viewer');

describe('another tab', () => {
  it('says who this page loaded as', () => {
    const peer = another();
    const heard: unknown[] = [];
    peer.onmessage = (e) => heard.push(e.data);
    tab(as(member));
    expect(heard).toEqual(['u:alice']);
  });

  it.each([
    ['signed out', ''],
    ['someone else', 'u:carol'],
    ['an applicant', 'p:github'],
  ])('reloads a member’s page when a tab loads %s', (_, seen) => {
    tab(as(member));
    another().postMessage(seen);
    expect(reload).toHaveBeenCalledTimes(1);
  });

  it('leaves a member’s page alone when a tab loads the same member', () => {
    tab(as(member));
    another().postMessage('u:alice');
    expect(reload).not.toHaveBeenCalled();
  });

  it('leaves a signed-out page alone when someone signs in elsewhere', () => {
    tab(as(anon));
    another().postMessage('u:alice');
    expect(reload).not.toHaveBeenCalled();
  });

  it('reloads an applicant’s page once they are signed out', () => {
    tab(as(preUser));
    another().postMessage('');
    expect(reload).toHaveBeenCalledTimes(1);
  });

  it('compares with the viewer as it is now, not as it loaded', () => {
    tab(as(anon));
    viewer.value = as(member);
    another().postMessage('');
    expect(reload).toHaveBeenCalledTimes(1);
  });

  it('ignores anything that is not a viewer', () => {
    tab(as(member));
    another().postMessage({ user: null });
    expect(reload).not.toHaveBeenCalled();
  });

  it('stops listening once stopped', () => {
    tab(as(member));
    stop!();
    another().postMessage('');
    expect(reload).not.toHaveBeenCalled();
  });

  it('still starts where there is no channel', () => {
    vi.stubGlobal('BroadcastChannel', undefined);
    viewer.value = as(member);
    expect(() => (stop = guardViewer(reload as unknown as () => void))).not.toThrow();
  });
});

describe('a restored page', () => {
  // happy-dom's PageTransitionEvent is a plain Event, which drops persisted
  // from its init, so the event carries it by hand.
  const show = (persisted: boolean) => dispatchEvent(Object.assign(new Event('pageshow'), { persisted }));
  const settle = () => new Promise((r) => setTimeout(r, 0));

  function answering(me: Me | null) {
    const urls: string[] = [];
    vi.stubGlobal('fetch', async (url: string) => {
      urls.push(url);
      return me ? new Response(JSON.stringify(me), { status: 200 }) : new Response('{}', { status: 503 });
    });
    return urls;
  }

  it.each([
    ['signed out', as(anon)],
    ['someone else', withHandle('carol')],
  ])('reloads when /me says the browser is now %s', async (_, me) => {
    const urls = answering(me);
    tab(as(member));
    show(true);
    await settle();
    expect(urls).toEqual(['/api/v1/me']);
    expect(reload).toHaveBeenCalledTimes(1);
  });

  it('stays when /me says the same member', async () => {
    const urls = answering(as(member));
    tab(as(member));
    show(true);
    await settle();
    expect(urls).toEqual(['/api/v1/me']);
    expect(reload).not.toHaveBeenCalled();
  });

  it('reloads when it cannot find out', async () => {
    answering(null);
    tab(as(member));
    show(true);
    await settle();
    expect(reload).toHaveBeenCalledTimes(1);
  });

  it('asks nothing on a first load', async () => {
    const urls = answering(as(anon));
    tab(as(member));
    show(false);
    await settle();
    expect(urls).toEqual([]);
    expect(reload).not.toHaveBeenCalled();
  });

  it('asks nothing for a signed-out page', async () => {
    const urls = answering(as(member));
    tab(as(anon));
    show(true);
    await settle();
    expect(urls).toEqual([]);
    expect(reload).not.toHaveBeenCalled();
  });
});
