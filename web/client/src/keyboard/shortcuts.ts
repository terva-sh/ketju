import { signal } from '@preact/signals';
import { useEffect, useRef } from 'preact/hooks';
import { apiGet } from '../api/client';
import type { ThreadList, ThreadPage, ThreadRow } from '../api/dto';
import { pageHref } from '../components/thread/Pager';
import { permalink, type ThreadRef } from '../components/thread/Post';
import { unreadHref } from '../components/ThreadRow';
import { isPhone } from '../state/layout';
import { viewer } from '../state/viewer';

// The desktop keyboard shortcuts on a thread page (docs/06 "Paged, not
// infinite"): j and k between posts, g and a number to an ordinal, u to the
// first unread post, n to the next unread thread in the forum, and ? for the
// list. Each has a visible control that does the same (ThreadFoot, the
// thread's actions, the posts and the pager), so none is the only way to
// anything; on the phone layout there are none at all (docs/06 "Mobile").

// shortcuts is the list the help overlay shows: each key, what it does, and
// the control that does the same.
export const shortcuts: { keys: string[]; does: string; control: string }[] = [
  { keys: ['j'], does: 'Next post', control: 'Scroll, or the pager at the end of a page' },
  { keys: ['k'], does: 'Previous post', control: 'Scroll, or the pager at the start of a page' },
  { keys: ['g', 'number', 'Enter'], does: 'Go to the post with that number', control: 'Go to post, below the posts' },
  { keys: ['u'], does: 'First unread post', control: 'First unread, in the thread’s actions' },
  { keys: ['n'], does: 'Next unread thread in this forum', control: 'Next unread thread, below the posts' },
  { keys: ['?'], does: 'Show this list', control: 'Keyboard shortcuts, below the posts' },
];

// hint is a short message the shortcuts and their controls leave for the
// reader: the "Go to #…" being typed, or why nothing happened. ThreadFoot
// shows it in a status region.
export const hint = signal('');
// helpOpen is whether the shortcut list is showing.
export const helpOpen = signal(false);

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
// the thread's own address, where the server lands them on it.
export function firstUnread(thread: ThreadRef, lastRead: number, postCount: number, route: (url: string) => void) {
  if (lastRead >= postCount) {
    hint.value = 'Nothing unread in this thread.';
    return;
  }
  hint.value = '';
  if (lastRead > 0) {
    const here = document.getElementById(`post-${lastRead + 1}`);
    if (here?.matches(postSelector)) return focusPost(here);
    moveTo(permalink(thread, lastRead + 1), lastRead + 1, route);
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
      if (d) goToOrdinal(Number(d), live.current.page, live.current.route);
    };
    // Focus moving anywhere, by pointer or by Tab, ends g: the number was
    // typed for the page as it was (review 1136).
    const onFocus = () => {
      if (digits === null) return;
      endGoto();
      hint.value = '';
    };

    const onKey = (e: KeyboardEvent) => {
      if (isPhone.value || helpOpen.value || ignored(e)) {
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
          firstUnread(page.thread, page.viewer.last_read_ordinal, page.thread.post_count, route);
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
      clearTimeout(timer);
      if (digits !== null) hint.value = '';
    };
  }, []);
}
