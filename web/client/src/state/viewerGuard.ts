import { apiGet } from '../api/client';
import type { Me } from '../api/dto';
import { viewer } from './viewer';

// The viewer guard (TKT-01M3QVWKAG). Signing out is a form post, and the tab
// that posts it loads the next page, which drops everything the client held
// for the person who left. Two pages it does not reach could still show that
// person's forum, composers and counts: another tab of the same browser, and
// a page the back/forward cache restores. Each reloads here once it learns
// the browser is no longer signed in as its viewer, so what it rebuilds is
// the next viewer's, from a fresh bootstrap. A page load and not a reset in
// place, for the reason the sign-out is a form: a load drops every signal,
// held GET and composer at once, where a reset could miss one.
//
// Only a page with a viewer to hide acts. A signed-out page shows what
// anyone may see, and is not reloaded when someone signs in elsewhere.
//
// The guard never changes the viewer signal. A change the page learns of
// itself, from /me, is the screens' to handle in place: the thread page asks
// again for a page fetched for someone else (TKT-01M3QHKD21). The guard is
// for the changes a page cannot learn of, and a reload also drops what
// module memory holds, such as the last thread list (state/lists.ts).

// who names a viewer: a member by handle, an applicant by provider and the
// session's tag, which differs between two applicants of one provider (Terva
// review 1627), and '' for nobody.
const who = (m: Me) => (m.user ? `u:${m.user.handle}` : m.pre_user ? `p:${m.pre_user.provider}:${m.pre_user.tag}` : '');

const channelName = 'ketju.viewer';

// guardViewer starts the guard, and answers a function that stops it.
// reload is the page's, replaced in tests.
export function guardViewer(reload: () => void = () => location.reload()): () => void {
  const mine = () => who(viewer.peek());
  const stale = (seen: string) => !!mine() && seen !== mine();

  // Every tab shares the cookie, so the newest page load says who the
  // browser is signed in as. Each page says it once, as it starts; the
  // channel never delivers to the page that sent.
  let channel: BroadcastChannel | undefined;
  try {
    channel = new BroadcastChannel(channelName);
    channel.onmessage = (e: MessageEvent) => {
      if (typeof e.data === 'string' && stale(e.data)) reload();
    };
    channel.postMessage(mine());
  } catch {
    // No channel: only the back/forward cache is guarded.
  }

  // A restored page missed whatever happened while it was away, sign-out
  // included, so it asks. One that cannot find out reloads too: showing the
  // wrong person's page is worse than a load that may fail.
  const shown = (e: PageTransitionEvent) => {
    if (!e.persisted || !mine()) return;
    apiGet<Me>('/api/v1/me').then(
      (m) => stale(who(m)) && reload(),
      () => reload(),
    );
  };
  addEventListener('pageshow', shown);
  return () => {
    channel?.close();
    removeEventListener('pageshow', shown);
  };
}
