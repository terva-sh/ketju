import { batch, signal, type Signal } from '@preact/signals';
import { apiSend } from '../api/client';
import type { CreatedPost, Draft } from '../api/dto';
import { insertBlock, quoteMarkdown } from '../composer/quote';
import { drop, load, resetDrafts, watch, type DraftKey } from './drafts';
import { isPhone } from './layout';
import { viewer } from './viewer';

// The composer's state, one per thread (docs/06 "Composer"). It is here
// rather than in the component because the post actions write to it (quote,
// reply) and the thread's Reply action does, and the phone's composer sheet
// (TKT-01M29CGBVN) shares it. It lives for the session, and is autosaved to
// the viewer's draft for the thread (state/drafts.ts, TKT-01M3NS1931), which
// loadReplyDraft brings back on another visit or device.
export interface Composer {
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
}

const composers = new Map<string, Composer>();

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
    composers.clear();
    resetDrafts();
    owner = who;
  }
}

export function composerFor(thread: string): Composer {
  checkOwner();
  let c = composers.get(thread);
  if (!c) {
    const n: Composer = { source: signal(''), replyTo: signal(null), caret: signal(0), summoned: signal(0), open: signal(false),
      posting: signal(false), failure: signal(null) };
    composers.set(thread, n);
    // Disarmed until loadReplyDraft answers, so a thread the reader may not
    // reply in, which is never loaded, never saves.
    watch(draftKey(thread), () => ({ source: n.source.value, reply_to: n.replyTo.value }));
    c = n;
  }
  return c;
}

// loadReplyDraft brings the viewer's draft for thread into its composer, once
// per thread per session: the thread page asks when the reader may reply,
// so the phone's reply bar shows it before the sheet opens. A draft fills
// only an empty composer with no reply target and no post in flight; what
// the reader typed meanwhile wins, and is saved over it.
export function loadReplyDraft(thread: string): Promise<void> {
  const c = composerFor(thread);
  return load<Draft>(
    draftKey(thread),
    (d) => ({ source: typeof d.source === 'string' ? d.source : '', reply_to: typeof d.reply_to === 'number' ? d.reply_to : null }),
    (s) => {
      if (c.posting.value || c.replyTo.value !== null || c.source.value.trim()) return false;
      // A local copy is read back from storage, so its fields are checked.
      const source = typeof s.source === 'string' ? s.source : '';
      batch(() => {
        c.source.value = source;
        c.replyTo.value = typeof s.reply_to === 'number' ? s.reply_to : null;
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
// chip and records why, for the composer to say.
//
// It lives here rather than in the component because the sheet may close,
// and the composer unmount, while the post is in flight; the answer still
// has to clear the composer it came from.
export async function postReply(thread: string): Promise<CreatedPost | null> {
  const c = composerFor(thread);
  const source = c.source.value;
  if (c.posting.value || !source.trim()) return null;
  const replyTo = c.replyTo.value;
  c.posting.value = true;
  c.failure.value = null;
  try {
    const res = await apiSend<CreatedPost>(
      'POST',
      `/api/v1/threads/${encodeURIComponent(thread)}/posts`,
      replyTo === null ? { source } : { source, reply_to: replyTo },
    );
    // The draft goes before the fields are cleared, so clearing them is not
    // a change to save (state/drafts.ts).
    drop(draftKey(thread));
    c.source.value = '';
    c.replyTo.value = null;
    c.caret.value = 0;
    c.open.value = false;
    return res;
  } catch (err) {
    c.failure.value = err;
    return null;
  } finally {
    c.posting.value = false;
  }
}

// discardReply empties thread's composer: the text, the chip and any
// refusal, and deletes the draft.
export function discardReply(thread: string) {
  const c = composerFor(thread);
  drop(draftKey(thread));
  c.source.value = '';
  c.replyTo.value = null;
  c.caret.value = 0;
  c.failure.value = null;
}

// resetComposers forgets every composer, for tests.
export function resetComposers() {
  composers.clear();
  resetDrafts();
  owner = null;
}
