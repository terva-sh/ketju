import { batch, signal, type Signal } from '@preact/signals';
import { apiSend } from '../api/client';
import type { CreatedThread, ForumDraft } from '../api/dto';
import { isProblem } from '../api/problem';
import { checkOwner } from './composer';
import { draftNote, draftStatus, drop, load, resetDrafts, watch, type DraftKey } from './drafts';
import { clearFiles, filesIn, hashes, newFiles, uploading, type Files } from './files';
import { viewer } from './viewer';

// The new-thread composer's state, one per forum (docs/06 "Composer",
// TKT-01M3NS16VH). Like a reply's (state/composer.ts) it lives for the
// session, so leaving the screen and coming back keeps what was typed, and
// it is autosaved as the viewer's new-thread draft for the forum, title
// included (state/drafts.ts, docs/07 "Drafts"), which loadNewThreadDraft
// brings back on another visit or device.
// Its files (Files) are the attachments that go with the first post.
export interface NewThread extends Files {
  title: Signal<string>;
  source: Signal<string>;
  caret: Signal<number>;
  // summoned is the composer body's; nothing summons a new thread's, so it
  // stays 0.
  summoned: Signal<number>;
  // posting is true while the thread is being created, and failure is why
  // the last attempt was refused, until the next one or a discard.
  posting: Signal<boolean>;
  failure: Signal<unknown>;
  // loaded is set once the draft's GET has answered, whatever it answered.
  // Until then Discard is off: the server may hold a draft the reader has
  // not been shown, and discarding would delete it unseen (Terva review
  // 1591).
  loaded: Signal<boolean>;
  // loading is whether loadNewThreadDraft has asked, so a second visit
  // while the first GET is still out does not set loaded early.
  loading: boolean;
}

const states = new Map<string, NewThread>();
let owner: string | null = null;
// era counts the owners, as composer.ts's does: a change of viewer moves it
// on, and a rename of the viewer's own account keeps it (Terva review 2023).
let era = 0;

const draftKey = (forum: string): DraftKey => ({ kind: 'new', forum });

// newThreadFor is forum's new-thread composer. A different viewer starts
// with none, as for a reply's (checkOwner).
export function newThreadFor(forum: string): NewThread {
  checkOwner();
  const who = viewer.value.user?.handle ?? '';
  if (who !== owner) {
    states.clear();
    owner = who;
    era++;
  }
  let n = states.get(forum);
  if (!n) {
    const s: NewThread = {
      title: signal(''),
      source: signal(''),
      caret: signal(0),
      summoned: signal(0),
      posting: signal(false),
      failure: signal(null),
      loaded: signal(false),
      loading: false,
      ...newFiles(),
    };
    states.set(forum, s);
    // Disarmed until loadNewThreadDraft answers, so an empty form never
    // deletes a draft before it has loaded, and a failed load never does.
    watch(draftKey(forum), () => ({ title: s.title.value, source: s.source.value, attachments: s.attachments.value }));
    n = s;
  }
  return n;
}

// loadNewThreadDraft brings the viewer's draft for forum into its composer,
// once per forum per session, title and body together. A draft fills only
// an empty form, with no files, no upload and nothing being posted: what the
// reader did meanwhile wins, and is saved over it (Terva review 1782). Whether this browser's own copy wins instead
// is decided by the server's revision (state/drafts.ts restoreLocal).
export async function loadNewThreadDraft(forum: string): Promise<void> {
  const n = newThreadFor(forum);
  if (n.loading) return;
  n.loading = true;
  await load<ForumDraft>(
    draftKey(forum),
    (d) => ({
      title: typeof d.title === 'string' ? d.title : '',
      source: typeof d.source === 'string' ? d.source : '',
      attachments: filesIn(d.attachments),
    }),
    (s) => {
      if (n.posting.value || n.title.value.trim() || n.source.value.trim() || n.attachments.value.length || n.uploads.value.length)
        return false;
      // A local copy is read back from storage, so its fields are checked.
      const source = typeof s.source === 'string' ? s.source : '';
      batch(() => {
        n.title.value = typeof s.title === 'string' ? s.title : '';
        n.source.value = source;
        n.attachments.value = filesIn(s.attachments);
        n.caret.value = source.length;
      });
      return true;
    },
  );
  n.loaded.value = true;
}

// createThread creates forum's thread from its composer (POST
// /forums/{slug}/threads, docs/07) and answers it, or null when nothing was
// created.
//
// One at a time: while one is in flight another asks for nothing, so a
// double click creates one thread, and nothing is sent while a file is still
// uploading. The fields are read-only meanwhile, so
// what was sent is what is cleared. The draft is dropped before the fields
// are cleared, so clearing them is not a change to save, and drop removes
// this browser's copy with the server's draft. A refusal keeps the text and
// records why, for the screen to say.
//
// An answer that arrives after the viewer has changed belongs to nobody on
// screen: it drops no draft, since the key would now be the next person's,
// and answers null, so the screen goes nowhere (Terva review 1591).
export async function createThread(forum: string): Promise<CreatedThread | null> {
  const n = newThreadFor(forum);
  const at = era;
  const title = n.title.value;
  const source = n.source.value;
  if (n.posting.value || !title.trim() || !source.trim() || uploading(n)) return null;
  n.posting.value = true;
  n.failure.value = null;
  try {
    const res = await apiSend<CreatedThread>('POST', `/api/v1/forums/${encodeURIComponent(forum)}/threads`, {
      title,
      source,
      ...hashes(n.attachments.value),
    });
    if (!current(forum, n, at)) return null;
    drop(draftKey(forum));
    batch(() => {
      n.title.value = '';
      n.source.value = '';
      clearFiles(n);
      n.caret.value = 0;
    });
    return res;
  } catch (err) {
    n.failure.value = err;
    return null;
  } finally {
    n.posting.value = false;
  }
}

// current is whether n is still forum's composer for the owner of era at,
// read without starting a new one. A rename of the viewer's own account
// moves owner with the viewer and keeps the era, so it passes.
function current(forum: string, n: NewThread, at: number): boolean {
  return (viewer.peek().user?.handle ?? '') === owner && era === at && states.get(forum) === n;
}

// canDiscard is whether Discard may act: the draft has loaded, nothing is
// being posted, and there is something to discard.
export function canDiscard(n: NewThread): boolean {
  return n.loaded.value && !n.posting.value && !!(n.title.value || n.source.value || n.attachments.value.length);
}

// discardNewThread empties forum's composer, and any refusal, and deletes
// the draft. It does nothing until the draft has loaded, and nothing to the
// server while the form is empty: either way the only draft it could delete
// is one the reader has not been shown.
export function discardNewThread(forum: string) {
  const n = newThreadFor(forum);
  n.failure.value = null;
  if (!canDiscard(n)) return;
  drop(draftKey(forum));
  batch(() => {
    n.title.value = '';
    n.source.value = '';
    clearFiles(n);
    n.caret.value = 0;
    n.failure.value = null;
  });
}

// refusedHere is a refusal from the create route that trying again would
// get too: the reader may not start a thread in this forum, or it is
// archived to them. Only this disables Post. A refused draft save never
// does, since the draft route and the create route are separate answers
// (TKT-01M3NS16VH, Terva reviews 1372 and 1375).
export function refusedHere(n: NewThread): boolean {
  const f = n.failure.value;
  return isProblem(f, 'forbidden') || isProblem(f, 'conflict');
}

// newThreadNote is what the screen says about forum's draft: draftNote's
// words, and "Draft not saved" once a refused save or load has turned
// autosave off, which draftNote leaves unsaid.
export function newThreadNote(forum: string): string {
  const key = draftKey(forum);
  return draftNote(key) || (draftStatus(key) === 'off' ? 'Draft not saved' : '');
}

// renameNewThreadOwner keeps the new-thread composers across a rename of
// the viewer's own account (state/rename.ts).
export function renameNewThreadOwner(from: string, to: string) {
  if (owner === from) owner = to;
}

// resetNewThreads forgets every new-thread composer and draft key, for
// tests.
export function resetNewThreads() {
  states.clear();
  resetDrafts();
  owner = null;
}
