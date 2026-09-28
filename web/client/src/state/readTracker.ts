import { apiSend } from '../api/client';
import type { ReadPosition } from '../api/dto';
import { ProblemError } from '../api/problem';
import { forgetLandings } from './threads';

// The read-tracking beacon (docs/06 "Read tracking"): a post counts as read
// once it has been on screen for a second, and the highest such ordinal is
// sent as the viewer's position with PUT /threads/{id}/read (docs/07 "Read
// position"), batched every 30 s and flushed on navigation and when the page
// is hidden or goes away.
//
// The second is what stops a fast scroll to the foot of a page from marking
// everything on it read. The same code runs at every width: read tracking is
// one integer with one meaning everywhere, so nothing here asks which layout
// is showing.
//
// Transport is fetch with keepalive, not navigator.sendBeacon: sendBeacon can
// only POST, and the API takes a PUT with a JSON body (docs/07 "CSRF
// posture" rule 1). keepalive lets the request outlive the page, which is
// all sendBeacon would have added.

// dwellMs is how long a post must stay on screen to count as read.
export const dwellMs = 1000;
// batchMs is how often what has been read goes out while a thread is open.
export const batchMs = 30_000;

// Per thread, for the life of this page:
//
// floor is the highest position sent, answered, or known to be stored.
// Nothing at or below it is sent: the server keeps the larger of the two
// (docs/07), so such a request would change nothing, and batching never sends
// a position behind one already sent. Only markUnread lowers it, because only
// a DELETE lowers the stored position.
const floor = new Map<string, number>();
// pending is the highest ordinal that has been on screen long enough and has
// not gone out yet. It is always above the floor.
const pending = new Map<string, number>();
// inFlight is every PUT outstanding for a thread, so markUnread can wait for
// them before its DELETE.
const inFlight = new Map<string, Set<Promise<void>>>();
// held is the threads with a DELETE outstanding: nothing is sent for them
// until it answers (docs/07 "Mark-unread then a late beacon"). It counts
// them, since two can overlap.
const held = new Map<string, number>();
// marks is the newest markUnread per thread, which the next one waits for.
const marks = new Map<string, Promise<unknown>>();

export const readUrl = (id: string) => `/api/v1/threads/${encodeURIComponent(id)}/read`;

// known raises the floor to a position the server is known to hold: the
// viewer.last_read_ordinal of a page, or a PUT's answer. A lower one, from a
// page answered before a beacon landed, changes nothing.
function known(id: string, stored: number) {
  if (stored > (floor.get(id) ?? 0)) floor.set(id, stored);
  if ((pending.get(id) ?? 0) <= (floor.get(id) ?? 0)) pending.delete(id);
}

// read records that a post has been on screen for the dwell.
function read(id: string, ordinal: number) {
  if (!Number.isSafeInteger(ordinal) || ordinal < 1) return;
  if (ordinal <= (floor.get(id) ?? 0) || ordinal <= (pending.get(id) ?? 0)) return;
  pending.set(id, ordinal);
}

// retryable is a failure that says nothing about the request itself: it did
// not arrive, or the server could not take it just then. Anything else (the
// thread gone, the viewer signed out) would be refused again.
function retryable(err: unknown): boolean {
  if (!(err instanceof ProblemError)) return true;
  return ['network', 'rate-limited', 'internal', 'unavailable'].includes(err.type);
}

// send puts the thread's pending position on the wire, if it has one and is
// not held (unless force, which is markUnread's own flush), and answers when
// every PUT outstanding for it when it was called has settled.
function send(id: string, force = false): Promise<void> {
  const ordinal = pending.get(id);
  if (ordinal !== undefined && (force || !held.has(id))) {
    pending.delete(id);
    const before = floor.get(id) ?? 0;
    floor.set(id, ordinal);
    const p = apiSend<ReadPosition>('PUT', readUrl(id), { ordinal }, { keepalive: true }).then(
      (r) => {
        known(id, r.last_read_ordinal);
        // The server now lands a return elsewhere (threads.ts).
        forgetLandings(id);
      },
      (err) => {
        // Not stored. Put it back for the next flush, unless a later
        // position has gone out since, which covers this one; the retry is
        // the same ordinal, never a lower one.
        if (!retryable(err) || floor.get(id) !== ordinal) return;
        floor.set(id, before);
        read(id, ordinal);
        batching();
      },
    );
    const set = inFlight.get(id) ?? new Set();
    set.add(p);
    inFlight.set(id, set);
    void p.finally(() => {
      set.delete(p);
      if (set.size === 0 && inFlight.get(id) === set) inFlight.delete(id);
    });
  }
  return Promise.all(inFlight.get(id) ?? []).then(() => undefined);
}

// flush sends every thread's pending position now.
export function flush(): Promise<void> {
  return Promise.all([...pending.keys()].map((id) => send(id))).then(() => undefined);
}

// markUnread sends DELETE /threads/{id}/read, making every post after
// ordinal unread, or the whole thread with none, and answers the position
// stored after it. A beacon sent before the DELETE and landing after it
// would move the position forward again (docs/07). So the thread is held
// from the call, and nothing more is beaconed for it until the DELETE has
// answered; then its pending position goes out, and every PUT for it is
// waited for, before the DELETE is sent.
//
// Marks of one thread run one at a time, in call order: two DELETEs in
// flight together could land in either order, and the one called first
// would then set the position last.
export function markUnread(id: string, ordinal?: number): Promise<ReadPosition> {
  held.set(id, (held.get(id) ?? 0) + 1);
  const before = marks.get(id);
  const run = (async () => {
    try {
      if (before) await before.catch(() => undefined);
      // Held, so no PUT for the thread starts after this one: it waits for
      // every PUT there will be before the DELETE.
      await send(id, true);
      // A read from before the mark, a PUT that failed and was put back
      // included, is what the mark undoes.
      pending.delete(id);
      const r = await apiSend<ReadPosition>('DELETE', readUrl(id), ordinal === undefined ? undefined : { ordinal });
      floor.set(id, r.last_read_ordinal);
      forgetLandings(id);
      return r;
    } finally {
      const n = held.get(id)! - 1;
      if (n > 0) held.set(id, n);
      else held.delete(id);
    }
  })();
  marks.set(id, run);
  void run
    .catch(() => undefined)
    .finally(() => {
      if (marks.get(id) === run) marks.delete(id);
    });
  return run;
}

// A post is on screen when at least half of it is in the viewport, or it
// fills at least half the viewport: a post taller than two screens is never
// half visible, and is still being read.
//
// The observer reports a post crossing one of its thresholds, and for a tall
// post the half-viewport line falls between them: it can drop below the line
// and come back without a report. So the observer says which posts are near,
// and the line is measured for those on every scroll and resize, which is
// how a post moves, and again when its second is up, in case the page's
// layout moved it instead. The second has to be unbroken.
const thresholds = Array.from({ length: 21 }, (_, i) => i / 20);

function onScreen(e: IntersectionObserverEntry): boolean {
  if (!e.isIntersecting) return false;
  const view = e.rootBounds?.height ?? innerHeight;
  return e.intersectionRect.height >= 0.5 * Math.min(e.boundingClientRect.height, view);
}

function onScreenNow(el: Element): boolean {
  const r = el.getBoundingClientRect();
  const shown = Math.min(r.bottom, innerHeight) - Math.max(r.top, 0);
  return shown > 0 && shown >= 0.5 * Math.min(r.height, innerHeight);
}

// tracked counts the lists being tracked. The batch timer runs while one is,
// and after, while a position is still waiting to go out: one whose PUT
// failed as the reader left the thread is retried from wherever they went.
let tracked = 0;
let batch: ReturnType<typeof setInterval> | undefined;

function batching() {
  if (batch === undefined) batch = setInterval(tick, batchMs);
}

function tick() {
  void flush().then(() => {
    if (tracked === 0 && pending.size === 0 && inFlight.size === 0) {
      clearInterval(batch);
      batch = undefined;
    }
  });
}

// leaving flushes when the page is hidden or goes away: a hidden page may
// never be shown again, and on a phone hiding is often the last event a page
// gets (pagehide is the one that fires on unload and into the back/forward
// cache; unload does not reliably).
function leaving() {
  if (document.visibilityState === 'hidden') void flush();
}
const hidden = () => void flush();
let listening = false;

// trackPosts tracks what the viewer reads of one page of thread id: the
// canonical posts and tombstones that are the direct children of list,
// whose ids are post-N and which carry data-ordinal. A reply expander's
// copies sit inside a post and carry no ordinal, so reading one is not
// reading the post it copies (TKT-01M29CGBW6). stored is the viewer's
// position as the page reported it.
//
// It answers the function that stops tracking, which flushes: the thread
// body unmounts on every navigation away from its page.
export function trackPosts(id: string, list: Element, stored: number): () => void {
  known(id, stored);
  const timers = new Map<Element, ReturnType<typeof setTimeout>>();
  // near is the posts the observer has in the viewport at all.
  const near = new Set<Element>();
  const start = (el: Element) => {
    if (timers.has(el) || document.visibilityState === 'hidden') return;
    timers.set(
      el,
      setTimeout(() => {
        timers.delete(el);
        if (onScreenNow(el)) read(id, Number(el.getAttribute('data-ordinal')));
      }, dwellMs),
    );
  };
  const stop = (el: Element) => {
    clearTimeout(timers.get(el));
    timers.delete(el);
  };
  const io = new IntersectionObserver(
    (entries) => {
      for (const e of entries) {
        if (e.isIntersecting) near.add(e.target);
        else near.delete(e.target);
        if (onScreen(e)) start(e.target);
        else stop(e.target);
      }
    },
    { threshold: thresholds },
  );
  for (const el of list.children) {
    if (el.id.startsWith('post-') && el.hasAttribute('data-ordinal')) io.observe(el);
  }
  const measure = () => {
    for (const el of near) {
      if (onScreenNow(el)) start(el);
      else stop(el);
    }
  };
  addEventListener('scroll', measure, { passive: true });
  addEventListener('resize', measure, { passive: true });
  // A hidden page is not being read: the dwell stops, and starts over for
  // what is still on screen when it is shown again.
  const visibility = () => {
    if (document.visibilityState === 'hidden') for (const el of [...timers.keys()]) stop(el);
    else measure();
  };
  document.addEventListener('visibilitychange', visibility);
  tracked++;
  batching();
  if (!listening) {
    // For the life of the page: they only send what is pending.
    listening = true;
    document.addEventListener('visibilitychange', leaving);
    addEventListener('pagehide', hidden);
  }
  return () => {
    io.disconnect();
    for (const el of [...timers.keys()]) stop(el);
    document.removeEventListener('visibilitychange', visibility);
    removeEventListener('scroll', measure);
    removeEventListener('resize', measure);
    tracked--;
    void flush();
  };
}

// resetReadTracker forgets every position, for tests.
export function resetReadTracker() {
  clearInterval(batch);
  batch = undefined;
  tracked = 0;
  floor.clear();
  pending.clear();
  inFlight.clear();
  held.clear();
  marks.clear();
}
