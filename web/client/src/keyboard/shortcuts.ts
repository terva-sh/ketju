import { signal } from '@preact/signals';
import { useEffect, useRef } from 'preact/hooks';
import { apiGet } from '../api/client';
import type { ThreadList, ThreadPage, ThreadRow } from '../api/dto';
import { pageHref } from '../components/thread/Pager';
import { permalink, type ThreadRef } from '../components/thread/Post';
import { unreadHref } from '../components/ThreadRow';
import { isPhone } from '../state/layout';

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
// the thread body is a new one for each page. It is the ordinal, or the
// first or last post of the page.
let pending: number | 'first' | 'last' | null = null;

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
  pending = n;
  route(permalink(page.thread, n));
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
    pending = lastRead + 1;
    route(permalink(thread, lastRead + 1));
    return;
  }
  const landing = `/t/${encodeURIComponent(thread.id)}/${encodeURIComponent(thread.slug)}`;
  if (location.pathname === landing && !location.search) {
    // Already where the server landed them.
    const first = posts()[0];
    if (first) focusPost(first);
    return;
  }
  pending = 'first';
  route(landing);
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

// maxListPages bounds how far n reads down a forum's list: the thread it
// wants is almost always on the first page, and a forum of thousands of
// threads should not cost as many requests.
export const maxListPages = 5;

// findNextUnread reads the forum's list, a page at a time, until it holds
// an unread thread after the current one or runs out, and answers the row
// nextUnread picks, or null.
export async function findNextUnread(forum: string, current: string): Promise<ThreadRow | null> {
  const rows: ThreadRow[] = [];
  let cursor = '';
  for (let n = 0; n < maxListPages; n++) {
    const q = cursor ? `?cursor=${encodeURIComponent(cursor)}` : '';
    const list = await apiGet<ThreadList>(`/api/v1/forums/${encodeURIComponent(forum)}/threads${q}`);
    rows.push(...list.threads);
    const at = rows.findIndex((r) => r.id === current);
    if (at >= 0 && rows.some((r, i) => i > at && r.unread > 0)) break;
    cursor = list.next_cursor;
    if (!cursor) break;
  }
  const i = nextUnread(rows, current);
  return i < 0 ? null : rows[i];
}

// openNextUnread opens the next unread thread in the forum where its unread
// badge would (unreadHref), or says there is none.
let seeking = false;
export async function openNextUnread(page: ThreadPage, route: (url: string) => void) {
  if (seeking) return;
  seeking = true;
  hint.value = '';
  try {
    const row = await findNextUnread(page.thread.forum.slug, page.thread.id);
    if (row) {
      pending = 'first';
      route(unreadHref(row));
    } else hint.value = `No other unread threads in ${page.thread.forum.name}.`;
  } catch {
    hint.value = 'Could not load this forum’s threads.';
  } finally {
    seeking = false;
  }
}

// gotoPause is how long g waits after the last digit before going.
export const gotoPause = 1500;

// useThreadShortcuts installs the shortcuts while a thread page's body is
// mounted, and focuses the post a move from another page left pending.
export function useThreadShortcuts(page: ThreadPage, lens: string, route: (url: string) => void) {
  const live = useRef({ page, lens, route });
  live.current = { page, lens, route };

  useEffect(() => {
    hint.value = '';
    const p = pending;
    pending = null;
    if (p === null || isPhone.value) return;
    const all = posts();
    const el = p === 'first' ? all[0] : p === 'last' ? all[all.length - 1] : document.getElementById(`post-${p}`);
    if (el?.matches(postSelector)) {
      el.focus({ preventScroll: true });
      // A permalink's and a landing's scroll are the arrival's (ThreadScreen).
      if (typeof p !== 'number' && !location.hash) el.scrollIntoView?.({ block: 'start' });
    }
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
        e.preventDefault();
        if (/^[0-9]$/.test(e.key) && digits.length < 9) {
          digits += e.key;
          hint.value = `Go to #${digits}`;
          clearTimeout(timer);
          timer = setTimeout(commit, gotoPause);
        } else if (e.key === 'Backspace') {
          digits = digits.slice(0, -1);
          hint.value = `Go to #${digits}`;
        } else if (e.key === 'Enter') commit();
        else {
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
          pending = i < 0 ? 'last' : 'first';
          route(pageHref(page.thread, to, lens));
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
          e.preventDefault();
          firstUnread(page.thread, page.viewer.last_read_ordinal, page.thread.post_count, route);
          return;
        case 'n':
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
    return () => {
      document.removeEventListener('keydown', onKey);
      clearTimeout(timer);
      if (digits !== null) hint.value = '';
    };
  }, []);
}
