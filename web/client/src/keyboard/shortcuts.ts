import { computed, effect, signal } from '@preact/signals';
import { useEffect, useLayoutEffect, useRef } from 'preact/hooks';
import { apiGet } from '../api/client';
import type { ThreadList, ThreadPage, ThreadRow } from '../api/dto';
import { pageHref } from '../components/thread/Pager';
import { lensQuery } from '../components/thread/lens';
import { permalink, type ThreadRef } from '../components/thread/Post';
import { unreadHref } from '../components/ThreadRow';
import { isPhone } from '../state/layout';
import { readPosition } from '../state/readTracker';
import { safeStorage } from '../state/theme';
import { viewer } from '../state/viewer';

// The desktop keyboard shortcuts on a thread page (docs/06 "Paged, not
// infinite"): j and k between posts, g and a number to an ordinal, u to the
// first unread post, n to the next unread thread in the forum, and ? for the
// list. Each has a visible control that does the same (ThreadFoot, the
// thread's actions, the posts and the pager), so none is the only way to
// anything; on the phone layout there are none at all (docs/06 "Mobile").
//
// They can be turned off (WCAG 2.1 SC 2.1.4, TKT-01M3V9DWTN; docs/06
// "Paged, not infinite"): then no character key acts on any page.
//
// One more works on every desktop screen with the top bar: / opens search
// (TKT-01M3V7GE59, useGlobalShortcuts). Its visible control is the top
// bar's Search link.

// shortcuts is the list the help overlay shows: each key, what it does, and
// the control that does the same.
export const shortcuts: { keys: string[]; does: string; control: string }[] = [
  { keys: ['j'], does: 'Next post', control: 'Scroll, or the pager at the end of a page' },
  { keys: ['k'], does: 'Previous post', control: 'Scroll, or the pager at the start of a page' },
  { keys: ['g', 'number', 'Enter'], does: 'Go to the post with that number', control: 'Go to post, below the posts' },
  { keys: ['u'], does: 'First unread post', control: 'First unread, in the thread’s actions' },
  { keys: ['n'], does: 'Next unread thread in this forum', control: 'Next unread thread, below the posts' },
  { keys: ['?'], does: 'Show this list', control: 'Keyboard shortcuts, below the posts' },
  { keys: ['/'], does: 'Search', control: 'Search, in the top bar' },
];

// hint is a short message the shortcuts and their controls leave for the
// reader: the "Go to #…" being typed, or why nothing happened. ThreadFoot
// shows it in a status region.
export const hint = signal('');
// helpOpen is whether the shortcut list is showing.
export const helpOpen = signal(false);

// shortcutsKey is where this browser keeps a signed-out reader's choice:
// "off" when they turned the shortcuts off, and nothing when they are on.
export const shortcutsKey = 'ketju.shortcuts';

// readDeviceShortcuts is this browser's choice: on unless it holds "off",
// and on where storage cannot be read.
export function readDeviceShortcuts(storage: Pick<Storage, 'getItem'> | undefined = safeStorage()): boolean {
  try {
    return storage?.getItem(shortcutsKey) !== 'off';
  } catch {
    return true;
  }
}

// deviceShortcuts is a signed-out reader's choice, this browser's.
export const deviceShortcuts = signal(readDeviceShortcuts());

// setDeviceShortcuts records a signed-out reader's choice and stores it.
// A member's is their account's, changed on /settings, so the settings
// screen's save queue stays the one writer of it.
export function setDeviceShortcuts(on: boolean, storage: Pick<Storage, 'setItem' | 'removeItem'> | undefined = safeStorage()) {
  deviceShortcuts.value = on;
  try {
    if (on) storage?.removeItem(shortcutsKey);
    else storage?.setItem(shortcutsKey, 'off');
  } catch {
    // Not stored; it still holds for this page.
  }
}

// shortcutsOn is whether the single-key shortcuts act: a member's account
// says, as /me's settings carry it, and for anyone else this browser does.
// A settings object with no shortcuts key reads as on, as the server does.
export const shortcutsOn = computed(() =>
  viewer.value.user ? viewer.value.settings.shortcuts !== false : deviceShortcuts.value,
);

// ignored says whether a keydown is not the page's to take: already
// handled, chorded with Ctrl, Meta or Alt (Shift makes ?), or typed into
// something that takes text, the composer, or a dialog.
export function ignored(e: KeyboardEvent): boolean {
  if (e.defaultPrevented || e.ctrlKey || e.metaKey || e.altKey || e.isComposing) return true;
  const t = e.target;
  if (!(t instanceof Element)) return false;
  if (t.closest('input, textarea, select, [contenteditable]:not([contenteditable=false])')) return true;
  return !!t.closest('.composer, .composer-sheet, [role=dialog]');
}

// postSelector is every canonical post on the page, a tombstone included. A
// reply expander's copies carry data-reply and no id, so they are not in it.
export const postSelector = '.posts > [id^="post-"]';

const posts = () => [...document.querySelectorAll<HTMLElement>(postSelector)];

// readingLine is the distance from the top of the viewport at which a post
// counts as the one being read: a post scrolled to stops at its
// scroll-margin-top (thread.css), 72 px, and a few px either way still
// counts.
const readingLine = 64;

// step is the index of the post j (+1) or k (-1) goes to, among posts whose
// tops (viewport offsets) are tops, from the post at current, or from the
// reading line when no post is current. It answers -1 or tops.length when
// the step leaves the page.
export function step(tops: number[], current: number, dir: 1 | -1): number {
  if (current >= 0) return current + dir;
  if (dir === 1) {
    const i = tops.findIndex((t) => t >= readingLine);
    return i < 0 ? tops.length : i;
  }
  let i = -1;
  tops.forEach((t, n) => {
    if (t < readingLine) i = n;
  });
  return i;
}

// A move to another page leaves a post to focus once it has rendered, since
// the thread body is a new one for each page: an ordinal; the first or last
// post of the page; or on a landing, where the server chose the page, the
// first unread post on it.
// It names the address it is for, and only a thread body mounted at that
// address takes it: a move that fails leaves it for no other page (review
// 1130).
type Target = number | 'first' | 'last' | 'landing';
let pending: { target: Target; url: string } | null = null;

// moveTo routes to url, leaving target to focus when it has rendered.
function moveTo(url: string, target: Target, route: (url: string) => void) {
  pending = { target, url };
  route(url);
}

// focusPost moves focus to a post and puts it at the top of the screen.
export function focusPost(el: HTMLElement) {
  el.focus({ preventScroll: true });
  el.scrollIntoView?.({ block: 'start' });
}

// goToOrdinal goes to post n of the thread: here when it is on the page,
// otherwise its permalink.
export function goToOrdinal(n: number, page: ThreadPage, route: (url: string) => void) {
  if (!Number.isSafeInteger(n) || n < 1 || n > page.thread.post_count) {
    hint.value = `There is no post #${n}.`;
    return;
  }
  hint.value = '';
  const here = document.getElementById(`post-${n}`);
  if (here?.matches(postSelector)) return focusPost(here);
  moveTo(permalink(page.thread, n), n, route);
}

// firstUnread goes to the first post the reader has not read: the post
// after their read position, here or by its permalink, or with no position,
// the thread's own address, where the server lands them on it. lastRead is
// the position as the read tracker knows it now (readPosition), not the
// one the page was drawn with, which reading since has passed.
//
// Under a held unread lens, lens unread:B, the post goes by its permalink
// under the lens, which serves the lens page holding it, and with no
// position that is the thread's first post. It is never B + 1: the reader
// may have read past it since the lens opened (Terva review 1730). A lens
// whose B is above the position, a shared or typed one, leaves that post
// out, so it goes by the canonical permalink instead (Terva review 1792).
export function firstUnread(
  thread: ThreadRef,
  lastRead: number,
  postCount: number,
  route: (url: string) => void,
  lens = '',
) {
  if (lastRead >= postCount) {
    hint.value = 'Nothing unread in this thread.';
    return;
  }
  hint.value = '';
  const held = /^unread:[0-9]+$/.test(lens) && Number(lens.slice(7)) <= lastRead;
  if (lastRead > 0 || held) {
    const here = document.getElementById(`post-${lastRead + 1}`);
    if (here?.matches(postSelector)) return focusPost(here);
    moveTo(permalink(thread, lastRead + 1) + (held ? `?lens=${lensQuery(lens)}` : ''), lastRead + 1, route);
    return;
  }
  const landing = `/t/${encodeURIComponent(thread.id)}/${encodeURIComponent(thread.slug)}`;
  if (location.pathname === landing && !location.search) {
    // Already where the server landed them.
    const first = posts()[0];
    if (first) focusPost(first);
    return;
  }
  moveTo(landing, 'landing', route);
}

// nextUnread picks the thread n goes to from a forum's rows in list order:
// the first unread one after the current thread, or, when none is after it,
// the first unread one before it. With the current thread not among the
// rows, it is the first unread row.
export function nextUnread(rows: Pick<ThreadRow, 'id' | 'unread'>[], current: string): number {
  const at = rows.findIndex((r) => r.id === current);
  const after = rows.findIndex((r, i) => i > at && r.unread > 0 && r.id !== current);
  if (after >= 0) return after;
  return rows.findIndex((r, i) => i < at && r.unread > 0);
}

// findNextUnread reads the forum's list, a page at a time, until it holds
// an unread thread after the current one or the list ends, and answers the
// row nextUnread picks, or null. It reads to the end rather than stopping
// at a bound: only the whole list can say that nothing below is unread, and
// so that wrapping to one above is right (review 1129). A cursor answered
// twice ends it, rather than looping.
export async function findNextUnread(forum: string, current: string, signal?: AbortSignal): Promise<ThreadRow | null> {
  const rows: ThreadRow[] = [];
  const seen = new Set<string>();
  let cursor = '';
  for (;;) {
    const q = cursor ? `?cursor=${encodeURIComponent(cursor)}` : '';
    const list = await apiGet<ThreadList>(`/api/v1/forums/${encodeURIComponent(forum)}/threads${q}`, { signal });
    rows.push(...list.threads);
    const at = rows.findIndex((r) => r.id === current);
    if (at >= 0 && rows.some((r, i) => i > at && r.unread > 0)) break;
    seen.add(cursor);
    cursor = list.next_cursor;
    if (!cursor || seen.has(cursor)) break;
  }
  const i = nextUnread(rows, current);
  return i < 0 ? null : rows[i];
}

// The search n started, which leaving the thread page abandons, so a reader
// who has moved on is not taken somewhere when it finishes (review 1129). A
// second n starts it again.
let search: AbortController | null = null;

// openNextUnread opens the next unread thread in the forum where its unread
// badge would (unreadHref), or says there is none. It lands on the post the
// badge names: the one after the read position, or with none, the first
// unread post on the page the server lands the reader on.
export async function openNextUnread(page: ThreadPage, route: (url: string) => void) {
  search?.abort();
  const mine = new AbortController();
  search = mine;
  hint.value = '';
  try {
    const row = await findNextUnread(page.thread.forum.slug, page.thread.id, mine.signal);
    if (mine.signal.aborted) return;
    if (row) {
      moveTo(unreadHref(row), row.last_read_ordinal > 0 ? row.last_read_ordinal + 1 : 'landing', route);
    } else hint.value = `No other unread threads in ${page.thread.forum.name}.`;
  } catch {
    if (!mine.signal.aborted) hint.value = 'Could not load this forum’s threads.';
  } finally {
    if (search === mine) search = null;
  }
}

// abandonSearch stops a search n started, when its thread page goes.
function abandonSearch() {
  search?.abort();
  search = null;
}

// tracksUnread is whether the reader has read positions to go by: they are
// signed in. u and n, and the controls that do the same, are theirs only.
export const tracksUnread = () => !!viewer.value.user;

// gotoPause is how long g waits after the last digit before going.
export const gotoPause = 1500;

// openSearch is what / does (docs/06 "Paged, not infinite", TKT-01M3V7GE59).
// On /search it focuses the query field and selects its text, so typing
// replaces the search. Anywhere else it goes to /search, whose field takes
// focus when it has no query.
//
// The search screen is known by its field being on the page, not by the
// address: the router also draws it at /search/, and a test of the path
// would send that reader to an empty search (Terva review 1988).
export function openSearch(route: (url: string) => void) {
  const q = document.getElementById('search-q');
  if (q instanceof HTMLInputElement) {
    q.focus();
    q.select();
    return;
  }
  route('/search');
}

// useGlobalShortcuts installs / on every desktop screen with the top bar;
// the shell mounts it once. The listener goes on in a layout effect, so the
// key is the page's from the frame the shell is drawn in (TKT-01M3TV4HBF).
// Every condition is read at the key press: not on a phone, the shortcuts
// on, the list closed, and not typed into text, the composer or a dialog.
// preventDefault keeps / from opening Firefox's quick find.
export function useGlobalShortcuts(route: (url: string) => void) {
  const live = useRef(route);
  live.current = route;
  useLayoutEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== '/' || isPhone.value || !shortcutsOn.value || helpOpen.value || ignored(e)) return;
      e.preventDefault();
      openSearch(live.current);
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, []);
}

// useThreadShortcuts installs the shortcuts while a thread page's body is
// mounted, and focuses the post a move from another page left pending.
export function useThreadShortcuts(page: ThreadPage, lens: string, route: (url: string) => void) {
  const live = useRef({ page, lens, route });
  live.current = { page, lens, route };

  useEffect(() => {
    hint.value = '';
    const p = pending && pending.url === location.pathname + location.search ? pending.target : null;
    pending = null;
    if (p !== null && !isPhone.value) {
      const all = posts();
      const lastRead = live.current.page.viewer.last_read_ordinal;
      const el =
        p === 'first'
          ? all[0]
          : p === 'last'
            ? all[all.length - 1]
            : p === 'landing'
              ? (all.find((e) => Number(e.dataset.ordinal) > lastRead) ?? all[0])
              : document.getElementById(`post-${p}`);
      if (el?.matches(postSelector)) {
        el.focus({ preventScroll: true });
        // A permalink's and a landing's scroll are the arrival's
        // (ThreadScreen); a page's is this.
        if ((p === 'first' || p === 'last') && !location.hash) el.scrollIntoView?.({ block: 'start' });
      }
    }
    return abandonSearch;
  }, []);

  useEffect(() => {
    // The digits typed after g, or null when g is not in progress.
    let digits: string | null = null;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const endGoto = () => {
      clearTimeout(timer);
      digits = null;
    };
    const commit = () => {
      const d = digits;
      endGoto();
      hint.value = '';
      // The pause's timer goes here with no key pressed, so the switch is
      // read again (Terva review 1967).
      if (d && shortcutsOn.value) goToOrdinal(Number(d), live.current.page, live.current.route);
    };
    // Focus moving anywhere, by pointer or by Tab, ends g: the number was
    // typed for the page as it was (review 1136).
    const onFocus = () => {
      if (digits === null) return;
      endGoto();
      hint.value = '';
    };
    // Turning the shortcuts off ends a g in progress at once, timer and
    // all, rather than at the next key (Terva review 1967).
    const stopWatching = effect(() => {
      if (shortcutsOn.value || digits === null) return;
      endGoto();
      hint.value = '';
    });

    const onKey = (e: KeyboardEvent) => {
      if (isPhone.value || helpOpen.value || !shortcutsOn.value || ignored(e)) {
        if (digits !== null) {
          endGoto();
          hint.value = '';
        }
        return;
      }
      const { page, lens, route } = live.current;
      if (digits !== null) {
        // Only the keys g takes are kept from the page. Any other ends it
        // and does what it would have: Tab still moves focus (review 1130).
        if (/^[0-9]$/.test(e.key) && digits.length < 9) {
          e.preventDefault();
          digits += e.key;
          hint.value = `Go to #${digits}`;
          clearTimeout(timer);
          timer = setTimeout(commit, gotoPause);
        } else if (e.key === 'Backspace') {
          // An edit restarts the pause, as a digit does (review 1140).
          e.preventDefault();
          digits = digits.slice(0, -1);
          hint.value = `Go to #${digits}`;
          clearTimeout(timer);
          timer = setTimeout(commit, gotoPause);
        } else if (e.key === 'Enter') {
          e.preventDefault();
          commit();
        } else {
          endGoto();
          hint.value = '';
        }
        return;
      }
      switch (e.key) {
        case 'j':
        case 'k': {
          e.preventDefault();
          const all = posts();
          const current = all.findIndex((el) => el === document.activeElement || el.contains(document.activeElement));
          const i = step(
            all.map((el) => el.getBoundingClientRect().top),
            current,
            e.key === 'j' ? 1 : -1,
          );
          if (i >= 0 && i < all.length) return focusPost(all[i]);
          const to = i < 0 ? page.page.number - 1 : page.page.number + 1;
          if (to < 1 || to > page.page.total_pages) return;
          moveTo(pageHref(page.thread, to, lens), i < 0 ? 'last' : 'first', route);
          return;
        }
        case 'g':
          e.preventDefault();
          digits = '';
          hint.value = 'Go to #';
          timer = setTimeout(() => {
            if (digits === '') {
              endGoto();
              hint.value = '';
            }
          }, gotoPause * 2);
          return;
        case 'u':
          if (!tracksUnread()) return;
          e.preventDefault();
          firstUnread(page.thread, readPosition(page.thread.id, page), page.thread.post_count, route, lens);
          return;
        case 'n':
          if (!tracksUnread()) return;
          e.preventDefault();
          void openNextUnread(page, route);
          return;
        case '?':
          e.preventDefault();
          helpOpen.value = true;
          return;
      }
    };
    document.addEventListener('keydown', onKey);
    document.addEventListener('focusin', onFocus);
    return () => {
      document.removeEventListener('keydown', onKey);
      document.removeEventListener('focusin', onFocus);
      stopWatching();
      clearTimeout(timer);
      if (digits !== null) hint.value = '';
    };
  }, []);
}
