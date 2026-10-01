import { batch, signal, type Signal } from '@preact/signals';
import { apiSend } from '../api/client';
import type { CreatedPost, Draft, Millis } from '../api/dto';
import { asProblem, ProblemError } from '../api/problem';
import { insertBlock, quoteMarkdown } from '../composer/quote';
import { drop, load, resetDrafts, watch, type DraftKey } from './drafts';
import { clearFiles, filesIn, hashes, newFiles, uploading, type Files } from './files';
import { isPhone } from './layout';
import { viewer } from './viewer';

// The composer's state, one per thread (docs/06 "Composer"). It is here
// rather than in the component because the post actions write to it (quote,
// reply) and the thread's Reply action does, and the phone's composer sheet
// (TKT-01M29CGBVN) shares it. It lives for the session, and is autosaved to
// the viewer's draft for the thread (state/drafts.ts, TKT-01M3NS1931), which
// loadReplyDraft brings back on another visit or device.
// Its files (Files) are the attachments that go with the reply.
export interface Composer extends Files {
  source: Signal<string>;
  // replyTo is the post this reply answers, shown as the chip; null for a
  // reply to the thread (docs/06: the bottom composer sets none).
  replyTo: Signal<number | null>;
  // caret is where the textarea's caret last was, where a quote goes.
  caret: Signal<number>;
  // summoned counts the requests to bring the composer into view and put
  // the caret at caret. The composer acts on each change.
  summoned: Signal<number>;
  // open is whether the phone's composer sheet is showing (TKT-01M29CGBVN).
  // Every summons on a phone opens it; closing it keeps source and replyTo,
  // which the reply bar then shows. Desktop ignores it.
  open: Signal<boolean>;
  // posting is true while a post of this composer is in flight, and
  // failure is why the last one was refused, until the next attempt.
  posting: Signal<boolean>;
  failure: Signal<unknown>;
  // slowUntil is when slow mode next lets the viewer post from here, in
  // Unix ms, or null (TKT-01M3R0PM0K); left is the whole seconds until then,
  // 0 once it has passed, which one timer per composer counts down (hold).
  // They are here, not in the component, because the phone's sheet unmounts
  // the composer when it closes and the wait has to outlast that, as the
  // draft does.
  slowUntil: Signal<Millis | null>;
  left: Signal<number>;
  timer?: ReturnType<typeof setTimeout>;
}

// Slow is what the thread page says about slow mode for this viewer
// (docs/07 "Slow mode"): the thread's interval, 0 for off; when the page
// says they may next post; and whether they are exempt, as whoever
// moderates the thread is (can_moderate).
export interface Slow {
  seconds: number;
  nextPostAt: Millis | null;
  exempt: boolean;
}

const composers = new Map<string, Composer>();

function forget() {
  for (const c of composers.values()) clearTimeout(c.timer);
  composers.clear();
  resetDrafts();
}

// owner is whose composers these are. A different viewer, signed in in the
// same tab, starts with none: what one person wrote is not shown to the next
// (review 557), and no save of theirs still waiting goes out under the next
// person's session. Signing in and out load a page today, which empties this
// anyway; this holds without relying on that.
let owner: string | null = null;

const draftKey = (thread: string): DraftKey => ({ kind: 'reply', thread });

// checkOwner forgets every composer and every draft key when the viewer is
// not the one they were made for. The new-thread composers (state/newThread)
// check here first too, so whichever kind is asked for first after a change
// resets the draft keys, and neither resets the other's fresh ones.
export function checkOwner() {
  const who = viewer.value.user?.handle ?? '';
  if (who !== owner) {
    forget();
    owner = who;
  }
}

export function composerFor(thread: string): Composer {
  checkOwner();
  let c = composers.get(thread);
  if (!c) {
    const n: Composer = { source: signal(''), replyTo: signal(null), caret: signal(0), summoned: signal(0), open: signal(false),
      posting: signal(false), failure: signal(null), slowUntil: signal(null), left: signal(0), ...newFiles() };
    composers.set(thread, n);
    // Disarmed until loadReplyDraft answers, so a thread the reader may not
    // reply in, which is never loaded, never saves.
    watch(draftKey(thread), () => ({ source: n.source.value, reply_to: n.replyTo.value, attachments: n.attachments.value }));
    c = n;
  }
  return c;
}

// loadReplyDraft brings the viewer's draft for thread into its composer, once
// per thread per session: the thread page asks when the reader may reply,
// so the phone's reply bar shows it before the sheet opens. A draft fills
// only an empty composer with no reply target, no files, no upload and no
// post in flight; what the reader did meanwhile wins, and is saved over it
// (Terva review 1782: a draft's files filled in under an upload could make
// an eleventh).
export function loadReplyDraft(thread: string): Promise<void> {
  const c = composerFor(thread);
  return load<Draft>(
    draftKey(thread),
    (d) => ({
      source: typeof d.source === 'string' ? d.source : '',
      reply_to: typeof d.reply_to === 'number' ? d.reply_to : null,
      attachments: filesIn(d.attachments),
    }),
    (s) => {
      if (c.posting.value || c.replyTo.value !== null || c.source.value.trim() || c.attachments.value.length || c.uploads.value.length)
        return false;
      // A local copy is read back from storage, so its fields are checked.
      const source = typeof s.source === 'string' ? s.source : '';
      batch(() => {
        c.source.value = source;
        c.replyTo.value = typeof s.reply_to === 'number' ? s.reply_to : null;
        c.attachments.value = filesIn(s.attachments);
        c.caret.value = source.length;
      });
      return true;
    },
  );
}

// summon brings c up: into view at the foot of the thread, or on a phone in
// the composer sheet.
//
// While a post is in flight the composer is frozen: quote and reply still
// bring it up, showing what is being posted, but change nothing, because a
// successful post clears what it sent and must not clear a quote or a chip
// chosen after it was sent (review on PR #81). The textarea is read-only
// and the chip's × is off for the same reason (Composer).
function summon(c: Composer) {
  c.summoned.value++;
  if (isPhone.value) c.open.value = true;
}

// hold sets c's wait to until and counts it down. The count ticks on the
// second boundaries of until, with one timer a second rather than one for
// the whole wait: a browser fires a delay over 2^31 - 1 ms at once, and a
// huge site default gives a next_post_at weeks away (TKT-01M3R33SR5). Each
// tick reads the clock again, so a clock that jumps is followed.
//
// When a countdown ends, a rate-limited refusal goes with it: its words name
// a wait that has passed (Terva review 1593, finding 2).
function hold(c: Composer, until: Millis, ticking = false) {
  clearTimeout(c.timer);
  c.slowUntil.value = until;
  const ms = until - Date.now();
  c.left.value = ms > 0 ? Math.ceil(ms / 1000) : 0;
  if (ms > 0) c.timer = setTimeout(() => hold(c, until, true), ms % 1000 || 1000);
  else if (ticking && asProblem(c.failure.value).type === 'rate-limited') c.failure.value = null;
}

// extend keeps the later of c's wait and until. Nothing shortens a wait:
// not a page fetched before the viewer's own post, which still says null
// (review 1540 on PR #203), nor a post's answer or a Retry-After shorter
// than a later next_post_at a page brought while the post was in flight
// (Terva review 1593, finding 1).
function extend(c: Composer, until: Millis | null) {
  const now = c.slowUntil.value;
  if (until !== null && (now === null || until > now)) hold(c, until);
}

// holdAtLeast keeps the later of thread's wait and at, the page's
// next_post_at.
export function holdAtLeast(thread: string, at: Millis | null) {
  extend(composerFor(thread), at);
}

// quotePost inserts a quotation of post ordinal at the caret and brings the
// composer up with the caret after it.
export function quotePost(thread: string, ordinal: number, text: string) {
  const c = composerFor(thread);
  if (!c.posting.value) {
    const next = insertBlock(c.source.value, c.caret.value, quoteMarkdown(ordinal, text));
    c.source.value = next.source;
    c.caret.value = next.caret;
  }
  summon(c);
}

// replyToPost makes the reply answer post ordinal and brings the composer up.
export function replyToPost(thread: string, ordinal: number) {
  const c = composerFor(thread);
  if (!c.posting.value) c.replyTo.value = ordinal;
  summon(c);
}

// replyToThread brings the composer up as a reply to the thread, with no
// reply_to.
export function replyToThread(thread: string) {
  const c = composerFor(thread);
  if (!c.posting.value) c.replyTo.value = null;
  summon(c);
}

// postReply posts thread's composer (POST /threads/{id}/posts, docs/07) and
// answers the created post, or null when nothing was posted.
//
// One post at a time: while one is in flight another asks for nothing, so a
// double click posts once. The textarea is read-only meanwhile (Composer),
// so what was sent is what is cleared. A refusal keeps the source and the
// chip and records why, for the composer to say. Nothing is posted while a
// file is still uploading, so a post never leaves without one the writer
// chose.
//
// It lives here rather than in the component because the sheet may close,
// and the composer unmount, while the post is in flight; the answer still
// has to clear the composer it came from.
//
// In slow mode a post starts the viewer's wait, unless they are exempt: the
// page is not fetched again after a post, and would otherwise leave Send on
// into a certain refusal. The wait runs from when the post was made, the
// earlier of its created_at and this clock's now, so a slow answer does
// not lengthen it, and a server clock ahead of this one does not either
// (Terva review 1594, finding 1). A rate-limited refusal starts the wait its
// Retry-After names, and says the wait in force, which a later next_post_at
// may make longer than its own (finding 2).
//
// An answer that arrives after the viewer has changed belongs to nobody on
// screen: it drops no draft, since the key would now be the next person's,
// clears nothing, starts no wait, and answers null, so the screen goes
// nowhere (TKT-01M3SAKPK9, as createThread does after Terva review 1591).
export async function postReply(thread: string, slow?: Slow): Promise<CreatedPost | null> {
  const c = composerFor(thread);
  const who = owner;
  const source = c.source.value;
  if (c.posting.value || !source.trim() || uploading(c)) return null;
  const replyTo = c.replyTo.value;
  c.posting.value = true;
  c.failure.value = null;
  try {
    const res = await apiSend<CreatedPost>('POST', `/api/v1/threads/${encodeURIComponent(thread)}/posts`, {
      source,
      ...(replyTo === null ? {} : { reply_to: replyTo }),
      ...hashes(c.attachments.value),
    });
    if (!current(thread, c, who)) return null;
    // The draft goes before the fields are cleared, so clearing them is not
    // a change to save (state/drafts.ts).
    drop(draftKey(thread));
    c.source.value = '';
    c.replyTo.value = null;
    clearFiles(c);
    c.caret.value = 0;
    c.open.value = false;
    if (slow && slow.seconds > 0 && !slow.exempt) extend(c, Math.min(res.post.created_at, Date.now()) + slow.seconds * 1000);
    return res;
  } catch (err) {
    if (!current(thread, c, who)) return null;
    const p = asProblem(err);
    if (p.type === 'rate-limited' && p.retryAfter) {
      extend(c, Date.now() + p.retryAfter * 1000);
      if (c.left.value > p.retryAfter) err = new ProblemError('rate-limited', p.status, c.left.value);
    }
    c.failure.value = err;
    return null;
  } finally {
    c.posting.value = false;
  }
}

// current is whether c is still thread's composer for the viewer who was
// who, read without starting a new one. The viewer check catches a change no
// lookup has followed yet, the owner check one that a lookup has, and the
// identity check a change and back with a lookup while the other viewer was
// current. A change and back with no lookup between leaves the composer, its
// draft and the post all the one viewer's, and is not guarded (review 1609).
function current(thread: string, c: Composer, who: string | null): boolean {
  return (viewer.peek().user?.handle ?? '') === who && owner === who && composers.get(thread) === c;
}

// discardReply empties thread's composer: the text, the chip, the files and
// any refusal, and deletes the draft. The wait stays: it is the viewer's, not
// the draft's.
export function discardReply(thread: string) {
  const c = composerFor(thread);
  drop(draftKey(thread));
  c.source.value = '';
  c.replyTo.value = null;
  clearFiles(c);
  c.caret.value = 0;
  c.failure.value = null;
}

// resetComposers forgets every composer, for tests.
export function resetComposers() {
  forget();
  owner = null;
}
