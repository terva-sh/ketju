import { signal } from '@preact/signals';

// A moderator's selection of posts in one thread, to split or merge
// (docs/06 "Moderator flow for drift", TKT-01M29CYSAM). It lives in the
// MovePosts chunk with the bar and the dialogs, and nothing in the reader's
// own bundle reads it.
//
// It is held here rather than on a page, so it survives moving between the
// thread's pages. The selection bar starts it and ends it: on Cancel, on
// Escape, after a split, and when the thread page leaves the thread.

// maxSelection is the most posts one move takes (threadlog.MaxMoveOrdinals).
export const maxSelection = 10000;

export interface Selection {
  thread: string;
  // ordinals, ascending.
  ordinals: number[];
}

export const selection = signal<Selection | null>(null);

// startSelection begins an empty selection in thread, dropping any other.
export function startSelection(thread: string) {
  selection.value = { thread, ordinals: [] };
}

export function endSelection() {
  selection.value = null;
}

// isSelected is whether ordinal of thread is in the selection.
export function isSelected(thread: string, ordinal: number): boolean {
  const s = selection.value;
  return !!s && s.thread === thread && s.ordinals.includes(ordinal);
}

// toggleSelected adds ordinal to the selection or takes it out. A post is
// not added once the selection holds maxSelection, and nothing is added to
// a selection of another thread.
export function toggleSelected(thread: string, ordinal: number) {
  const s = selection.value;
  if (!s || s.thread !== thread) return;
  if (s.ordinals.includes(ordinal)) {
    selection.value = { thread, ordinals: s.ordinals.filter((o) => o !== ordinal) };
    return;
  }
  if (s.ordinals.length >= maxSelection) return;
  selection.value = { thread, ordinals: [...s.ordinals, ordinal].sort((a, b) => a - b) };
}
