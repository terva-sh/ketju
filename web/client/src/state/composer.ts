import { signal, type Signal } from '@preact/signals';
import { insertBlock, quoteMarkdown } from '../composer/quote';

// The composer's state, one per thread (docs/06 "Composer"). It is here
// rather than in the component because the post actions write to it (quote,
// reply) and the thread's Reply action does, and the phone's composer sheet
// (TKT-01M29CGBVN) is to share it. It lives for the session: saving it to
// the server is the drafts endpoint's (TKT-01M29CA9CE).
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
}

const composers = new Map<string, Composer>();

export function composerFor(thread: string): Composer {
  let c = composers.get(thread);
  if (!c) {
    c = { source: signal(''), replyTo: signal(null), caret: signal(0), summoned: signal(0) };
    composers.set(thread, c);
  }
  return c;
}

// quotePost inserts a quotation of post ordinal at the caret and brings the
// composer up with the caret after it.
export function quotePost(thread: string, ordinal: number, text: string) {
  const c = composerFor(thread);
  const next = insertBlock(c.source.value, c.caret.value, quoteMarkdown(ordinal, text));
  c.source.value = next.source;
  c.caret.value = next.caret;
  c.summoned.value++;
}

// replyToPost makes the reply answer post ordinal and brings the composer up.
export function replyToPost(thread: string, ordinal: number) {
  const c = composerFor(thread);
  c.replyTo.value = ordinal;
  c.summoned.value++;
}

// replyToThread brings the composer up as a reply to the thread, with no
// reply_to.
export function replyToThread(thread: string) {
  const c = composerFor(thread);
  c.replyTo.value = null;
  c.summoned.value++;
}

// resetComposers forgets every composer, for tests.
export function resetComposers() {
  composers.clear();
}
