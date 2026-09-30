import { effect, signal, untracked, type Signal } from '@preact/signals';
import { apiGetOptional, apiSend } from '../api/client';
import { isProblem, ProblemError } from '../api/problem';
import { viewer } from './viewer';

// Draft autosave (docs/06 "Composer", docs/07 "Drafts", TKT-01M3NS1931): one
// module for a thread's reply draft and a forum's new-thread draft, so each
// is saved by the same rules.
//
// Saves follow the state, not the textarea: an effect on what the draft
// reads schedules a PUT draftDelay after the last change, so typing, a
// quote, a chip and a discard are saved alike. A key is disarmed until its
// GET answers, so a load slower than the debounce never deletes the draft it
// is about to show (Terva review 1376).
//
// A key has at most one request in flight. The server gives two requests for
// one draft no order, so a later one waits for the answer to the one before
// it, and only the newest waiting one is kept: each PUT and DELETE replaces
// the whole draft.
//
// Leaving the page never waits on a promise (Terva review 1372). On pagehide
// a key with nothing in flight has its save started inside the handler, with
// keepalive. Any draft not yet seen saved is also kept in localStorage with
// the server revision it was based on and the requests that were in flight,
// and the next load in this browser decides by the server's revision, never
// the client's clock, whether the copy or the server's draft wins
// (restoreLocal). An empty copy is kept too while the server may still hold
// a draft, so a post or a blanking that pagehide could not send is deleted
// on the next load (Terva review 1580).

export type DraftKey = { kind: 'reply'; thread: string } | { kind: 'new'; forum: string };

// Snapshot is a draft as its PUT sends it: {source, reply_to} for a reply.
export type Snapshot = Record<string, string | number | null>;

export type DraftStatus = 'idle' | 'saving' | 'saved' | 'failed' | 'off';

// draftDelay is the pause after the last change before a save. The preview
// already spends one write-limiter token per 400 ms pause, and saving on each
// of those would double it.
export const draftDelay = 1500;

// keepaliveLimit is the browser's bound on a keepalive request's body. A
// draft this large or larger is sent without it, and kept locally on
// pagehide instead of sent.
export const keepaliveLimit = 64 * 1024;

const copyPrefix = 'ketju.draft.';

interface Req {
  del: boolean;
  body: Snapshot;
  json: string;
  // prior is whether the server held a draft before this request was queued,
  // which is still so if it is refused.
  prior: boolean | null;
}

interface Entry {
  id: string;
  url: string;
  // owner is the handle the draft is written under, and whose local copy it
  // keeps: fixed when the key is watched, since resetDrafts replaces every
  // entry when the viewer changes.
  owner: string;
  read: () => Snapshot;
  armed: boolean;
  loading: boolean;
  off: boolean;
  // waiting is set from a rate-limited answer until its Retry-After has
  // passed. Nothing is sent meanwhile, whatever changes or the page does:
  // the timer then sends what is waiting (Terva review 1580).
  waiting: boolean;
  // gen moves on drop, so a load answered after a post or discard does not
  // fill the composer, or take the queued DELETE's place as the server state.
  gen: number;
  // sent is the last snapshot queued, which an unchanged snapshot is not
  // sent again for; confirmed is the last one the server answered.
  sent: string | null;
  confirmed: string | null;
  // held is whether the server holds a non-empty draft once what is queued
  // lands, null when that is not known. An empty composer is a DELETE only
  // when it is true.
  held: boolean | null;
  // known is the revision in the newest answer, null until one arrives.
  known: number | null;
  current: Req | null;
  next: Req | null;
  // unsure counts requests that failed without an answer, which the server
  // may still have applied, until the next answer; unsureDeletes those that
  // were DELETEs. A local copy counts them as in flight.
  unsure: number;
  unsureDeletes: number;
  timer?: ReturnType<typeof setTimeout>;
  status: Signal<DraftStatus>;
  // restored is set while the composer shows a copy with no known base
  // (restoreLocal), until the next change.
  restored: Signal<boolean>;
  restoredJson: string | null;
  // copied is the snapshot this page kept locally, removed once the server
  // has answered a save of it.
  copied: string | null;
  stop: () => void;
}

const entries = new Map<string, Entry>();

const idOf = (k: DraftKey) => (k.kind === 'reply' ? `reply:${k.thread}` : `new:${k.forum}`);
const urlOf = (k: DraftKey) =>
  k.kind === 'reply' ? `/api/v1/drafts/${encodeURIComponent(k.thread)}` : `/api/v1/forums/${encodeURIComponent(k.forum)}/draft`;
const copyKey = (e: Entry) => `${copyPrefix}${e.owner}.${e.id}`;

// isEmpty is a snapshot with nothing typed and nothing chosen.
export const isEmpty = (s: Snapshot) => Object.values(s).every((v) => v === null || (typeof v === 'string' && !v.trim()));

// blank is s with everything cleared, what the composer holds after a post.
const blank = (s: Snapshot): Snapshot => Object.fromEntries(Object.entries(s).map(([k, v]) => [k, typeof v === 'string' ? '' : null]));

// revisionOf reads Draft-Revision, which every draft 204 carries.
function revisionOf(h: Headers): number | null {
  const v = h.get('Draft-Revision');
  return v !== null && /^[0-9]+$/.test(v) ? Number(v) : null;
}

// refused is an answer that saving again would get: the reader may not keep
// this draft. Autosave turns off for the key.
const refused = (err: unknown) =>
  isProblem(err, 'forbidden') || isProblem(err, 'conflict') || isProblem(err, 'not-found') || isProblem(err, 'unauthenticated');

let listening = false;

// watch starts autosave for key, disarmed until load arms it. read is the
// draft as it stands, read inside an effect so every change to it schedules a
// save.
export function watch(key: DraftKey, read: () => Snapshot) {
  const id = idOf(key);
  if (entries.has(id)) return;
  const e: Entry = {
    id,
    url: urlOf(key),
    owner: viewer.peek().user?.handle ?? '',
    read,
    armed: false,
    loading: false,
    off: false,
    waiting: false,
    gen: 0,
    sent: null,
    confirmed: null,
    held: null,
    known: null,
    current: null,
    next: null,
    unsure: 0,
    unsureDeletes: 0,
    status: signal<DraftStatus>('idle'),
    restored: signal(false),
    restoredJson: null,
    copied: null,
    stop: () => {},
  };
  entries.set(id, e);
  e.stop = effect(() => {
    const j = JSON.stringify(read());
    untracked(() => changed(e, j));
  });
  if (!listening) {
    listening = true;
    addEventListener('pagehide', unload);
    addEventListener('pageshow', resume);
    document.addEventListener('visibilitychange', hidden);
  }
}

function changed(e: Entry, j: string) {
  if (!e.armed || e.off) return;
  if (j !== e.restoredJson) e.restored.value = false;
  if (e.waiting) return;
  e.status.value = 'saving';
  schedule(e, draftDelay);
}

function schedule(e: Entry, ms: number) {
  clearTimeout(e.timer);
  e.timer = setTimeout(() => flush(e), ms);
}

// settled is the status once nothing is waiting to go out.
function settled(e: Entry) {
  e.status.value = e.current || e.timer ? 'saving' : e.held ? 'saved' : 'idle';
}

// flush sends the draft as it stands, if it differs from what was last sent:
// a PUT, or a DELETE for an empty one the server is known to hold. An empty
// draft the server is not known to hold sends nothing.
function flush(e: Entry) {
  if (e.waiting) return;
  clearTimeout(e.timer);
  e.timer = undefined;
  if (!e.armed || e.off) return;
  const snap = e.read();
  const j = JSON.stringify(snap);
  if (j !== e.sent) {
    if (!isEmpty(snap)) enqueue(e, { del: false, body: snap, json: j, prior: e.held });
    else if (e.held) enqueue(e, { del: true, body: snap, json: j, prior: e.held });
    else e.sent = j;
  }
  settled(e);
}

function enqueue(e: Entry, r: Req) {
  e.sent = r.json;
  e.held = !r.del;
  if (e.current || e.waiting) e.next = r;
  else start(e, r);
}

// start puts r on the wire. apiSend calls fetch before it awaits anything, so
// from a pagehide handler the request starts inside the handler.
function start(e: Entry, r: Req) {
  e.current = r;
  e.status.value = 'saving';
  let rev: number | null = null;
  const keepalive = new TextEncoder().encode(r.json).length < keepaliveLimit;
  apiSend<{ revision?: unknown } | undefined>(r.del ? 'DELETE' : 'PUT', e.url, r.del ? undefined : r.body, {
    keepalive,
    onHeaders: (h) => (rev = revisionOf(h)),
  }).then(
    (d) => answered(e, r, typeof d?.revision === 'number' ? d.revision : rev),
    (err) => failed(e, r, err),
  );
}

function answered(e: Entry, r: Req, rev: number | null) {
  e.current = null;
  if (e.off) return;
  if (rev !== null) e.known = Math.max(e.known ?? rev, rev);
  e.unsure = e.unsureDeletes = 0;
  e.confirmed = r.json;
  if (e.copied === r.json) removeCopy(e);
  if (!next(e)) settled(e);
}

function failed(e: Entry, r: Req, err: unknown) {
  e.current = null;
  if (e.off) return;
  const unanswered = !(err instanceof ProblemError) || isProblem(err, 'network');
  if (unanswered) {
    e.unsure++;
    if (r.del) e.unsureDeletes++;
  }
  if (refused(err)) return turnOff(e);
  if (next(e)) return;
  // Not saved: the next change, or pagehide, sends the draft again.
  e.held = unanswered ? null : r.prior;
  e.sent = null;
  e.status.value = 'failed';
  if (isProblem(err, 'rate-limited')) {
    e.waiting = true;
    clearTimeout(e.timer);
    e.timer = setTimeout(() => {
      e.timer = undefined;
      e.waiting = false;
      if (!next(e)) flush(e);
    }, ((err as ProblemError).retryAfter ?? draftDelay / 1000) * 1000);
  }
}

function next(e: Entry): boolean {
  const n = e.next;
  if (!n) return false;
  e.next = null;
  start(e, n);
  return true;
}

function turnOff(e: Entry) {
  e.off = true;
  clearTimeout(e.timer);
  e.timer = undefined;
  e.next = null;
  e.status.value = 'off';
}

// load fetches key's draft once per session, restores a local copy if it
// wins, offers the result to fill, and arms the key. fill answers whether it
// took the draft: it takes one only into an empty composer with no post in
// flight, so loading never overwrites what the reader typed. Loading sends
// nothing back, unless the reader typed while it was in flight or a local
// copy won.
export async function load<T extends { revision: number }>(
  key: DraftKey,
  toSnapshot: (body: T) => Snapshot,
  fill: (s: Snapshot) => boolean,
): Promise<void> {
  const e = entries.get(idOf(key));
  if (!e || e.loading || e.off) return;
  e.loading = true;
  const gen = e.gen;
  let found: Snapshot | null;
  let rev: number | null;
  try {
    const r = await apiGetOptional<T>(e.url);
    found = r.body && toSnapshot(r.body);
    rev = r.body ? (typeof r.body.revision === 'number' ? r.body.revision : null) : revisionOf(r.headers);
  } catch (err) {
    if (entries.get(e.id) !== e || e.off) return;
    if (refused(err)) return turnOff(e);
    // Armed with the server's state unknown: a non-empty draft may be saved,
    // since a PUT replaces the whole draft, but nothing is deleted.
    e.armed = true;
    return changed(e, '');
  }
  if (entries.get(e.id) !== e || e.off) return;
  e.armed = true;
  if (rev !== null) e.known = Math.max(e.known ?? rev, rev);
  // A post or discard during the load has queued a DELETE, which is the
  // server's state now; the load's answer is not.
  if (gen !== e.gen) return settled(e);
  const local = restoreLocal(e, found, rev);
  e.sent = e.confirmed = JSON.stringify(found ?? blank(e.read()));
  e.held = found ? !isEmpty(found) : false;
  const shown = local?.snapshot ?? found;
  const filled = shown !== null && fill(shown);
  const j = JSON.stringify(e.read());
  if (filled && local?.notice) {
    e.restored.value = true;
    e.restoredJson = j;
  }
  if (filled && local) flush(e);
  else if (j !== e.sent) changed(e, j);
  else settled(e);
}

interface Copy {
  snapshot: Snapshot;
  base: number | null;
  inflight: number;
  deletes: number;
}

// restoreLocal decides, when key's load answers, whether this browser's copy
// of it wins, and removes the copy either way. found is the server's draft,
// null for a 204, and rev the revision the answer carried.
//
// base is the revision this tab last heard before the copy was made, and
// inflight and deletes the requests it had sent and not heard back from. A
// 200 at no more than base + inflight, or a 204 at no more than base +
// deletes, means nothing but this tab's own requests reached the draft
// since, so the copy wins. Anything more is another device's save, post or
// discard, and the server wins: text a delete removed never comes back.
// A copy with no base, whose load failed and none of whose saves were
// answered, loses to a 200 and is restored on a 204, with a notice: it is
// then the only record of what was written. So is one whose answer carried
// no revision. An empty copy that wins clears the composer's draft, which
// the load then deletes; one that cannot be ordered is dropped.
function restoreLocal(e: Entry, found: Snapshot | null, rev: number | null): { snapshot: Snapshot; notice: boolean } | null {
  const raw = storage(() => localStorage.getItem(copyKey(e)));
  if (!raw) return null;
  removeCopy(e);
  let c: Copy;
  try {
    c = JSON.parse(raw) as Copy;
  } catch {
    return null;
  }
  if (!c || typeof c.snapshot !== 'object' || !c.snapshot) return null;
  if (c.base === null || rev === null) return found || isEmpty(c.snapshot) ? null : { snapshot: c.snapshot, notice: true };
  return rev <= c.base + (found ? c.inflight : c.deletes) ? { snapshot: c.snapshot, notice: false } : null;
}

// drop is a post or a discard: whatever is pending for key is cancelled,
// this browser's copy removed, and a DELETE queued behind any request in
// flight, so a posted reply is never saved again or restored.
export function drop(key: DraftKey) {
  const e = entries.get(idOf(key));
  if (!e) return;
  removeCopy(e);
  if (e.off) return;
  // A Retry-After still running sends the DELETE when it has passed.
  if (!e.waiting) {
    clearTimeout(e.timer);
    e.timer = undefined;
  }
  e.gen++;
  e.next = null;
  e.restored.value = false;
  const empty = blank(e.read());
  enqueue(e, { del: true, body: empty, json: JSON.stringify(empty), prior: e.held });
}

// unload is pagehide. It awaits nothing and starts at most one request per
// key: with a request for the key in flight a second could overtake it, and
// the older text would win. Nothing starts while a Retry-After runs. A
// request queued behind one in flight is dropped, since it would start only
// if the page lived; the copy stands in for it, counting what is in flight.
// Every non-empty draft not seen saved is kept, and an empty one while the
// server may hold a draft: a post's DELETE queued behind a save in flight is
// then sent by the next load, if nothing but that save reached the draft.
function unload() {
  for (const e of entries.values()) {
    if (!e.waiting) {
      clearTimeout(e.timer);
      e.timer = undefined;
    }
    if (!e.armed || e.off) continue;
    if (e.next) {
      e.held = e.next.prior;
      e.next = null;
    }
    const snap = e.read();
    const j = JSON.stringify(snap);
    if (!e.current && j === e.confirmed) continue;
    e.sent = e.current ? e.current.json : e.confirmed;
    const empty = isEmpty(snap);
    if (!e.current && (empty || new TextEncoder().encode(j).length < keepaliveLimit)) flush(e);
    if (!e.owner || (empty && !e.current && !e.held)) continue;
    const c: Copy = {
      snapshot: snap,
      base: e.known,
      inflight: (e.current ? 1 : 0) + e.unsure,
      deletes: (e.current?.del ? 1 : 0) + e.unsureDeletes,
    };
    storage(() => localStorage.setItem(copyKey(e), JSON.stringify(c)));
    e.copied = j;
  }
}

// resume is the page coming back from the back/forward cache after
// pagehide: what unload held back is saved as usual.
function resume(ev: PageTransitionEvent) {
  if (!ev.persisted) return;
  for (const e of entries.values()) if (e.armed && !e.off && !e.waiting && JSON.stringify(e.read()) !== e.sent) schedule(e, 0);
}

// hidden saves what is waiting at once: on a phone a hidden page is often
// never shown again, and pagehide may never come.
function hidden() {
  if (document.visibilityState !== 'hidden') return;
  for (const e of entries.values()) if (e.timer !== undefined) flush(e);
}

function removeCopy(e: Entry) {
  e.copied = null;
  if (e.owner) storage(() => localStorage.removeItem(copyKey(e)));
}

// storage runs f, answering undefined when storage is refused: a private
// window, a full quota. A copy is a safeguard, and a missing one costs no
// more than having none.
function storage<T>(f: () => T): T | undefined {
  try {
    return f();
  } catch {
    return undefined;
  }
}

// clearForeignCopies removes every local copy not under handle, and every one
// when nobody is signed in. Signing out is a plain form post that runs no
// client code, so the page after it, which loads with no viewer, is where
// the last person's copies are removed.
export function clearForeignCopies(handle: string | null) {
  storage(() => {
    const mine = handle ? `${copyPrefix}${handle}.` : null;
    for (let i = localStorage.length - 1; i >= 0; i--) {
      const k = localStorage.key(i);
      if (k?.startsWith(copyPrefix) && !(mine && k.startsWith(mine))) localStorage.removeItem(k);
    }
  });
}

// startDrafts clears other people's copies now and whenever the viewer
// changes, and drops the pending saves of a viewer who has left, so none goes
// out under the next person's session.
export function startDrafts() {
  let last: string | undefined;
  return effect(() => {
    const handle = viewer.value.user?.handle ?? '';
    untracked(() => {
      clearForeignCopies(handle || null);
      if (last !== undefined && last !== handle) resetDrafts();
      last = handle;
    });
  });
}

// draftNote is what the composer says about key's draft.
export function draftNote(key: DraftKey): string {
  const e = entries.get(idOf(key));
  if (!e) return '';
  const s = e.status.value;
  if (s === 'failed') return 'Draft not saved';
  if (e.restored.value) return 'Restored from this browser';
  return s === 'saving' ? 'Saving…' : s === 'saved' ? 'Draft saved' : '';
}

// resetDrafts forgets every key: an owner change, and tests. A request in
// flight is answered into an entry nobody holds, and nothing follows it.
export function resetDrafts() {
  for (const e of entries.values()) {
    e.stop();
    turnOff(e);
  }
  entries.clear();
}
