import { signal, type Signal } from '@preact/signals';
import { apiUpload } from '../api/client';
import type { Attachment } from '../api/dto';
import { asProblem, problemMessage } from '../api/problem';

// A composer's files (docs/07 "Attachments", TKT-01M3V73J8K): the ones
// uploaded and waiting to go out with the post, and the uploads still on
// their way or refused. A reply's composer and a new thread's each have one,
// and their drafts keep the attached ones (docs/07 "Drafts").

// maxFiles is how many files one post takes.
export const maxFiles = 10;

// Upload is a chosen file the server has not taken: failure is null while
// it is on its way, and otherwise why it was refused, which its chip says
// until the writer removes it.
export interface Upload {
  id: number;
  name: string;
  failure: string | null;
}

export interface Files {
  attachments: Signal<Attachment[]>;
  uploads: Signal<Upload[]>;
}

export const newFiles = (): Files => ({ attachments: signal([]), uploads: signal([]) });

// uploading counts the uploads still on their way. Post waits for them, so a
// post never leaves without a file the writer chose.
export const uploading = (f: Files) => f.uploads.value.filter((u) => u.failure === null).length;

// full is whether f takes no more files: the attached and those on their way.
export const full = (f: Files) => f.attachments.value.length + uploading(f) >= maxFiles;

let ids = 0;

const tooMany = `A post takes at most ${maxFiles} files.`;

// attach uploads each chosen file in turn to where query says it is going,
// thread=<id> or forum=<slug>, and attaches each one the server takes, once
// per hash. A file past the tenth is refused here, on its own chip. A chip
// removed while its file is on the way attaches nothing when the answer
// comes.
//
// The room is checked again when each answer comes, since the files can
// change while it is on the way: an answer that would make an eleventh is
// refused on its chip rather than attached (Terva review 1782).
export async function attach(f: Files, query: string, chosen: File[]) {
  const room = Math.max(maxFiles - f.attachments.value.length - uploading(f), 0);
  const ups = chosen.map((file, i): Upload => ({ id: ++ids, name: file.name, failure: i < room ? null : tooMany }));
  f.uploads.value = [...f.uploads.value, ...ups];
  const refuse = (id: number, why: string) => (f.uploads.value = f.uploads.value.map((u) => (u.id === id ? { ...u, failure: why } : u)));
  for (let i = 0; i < Math.min(room, chosen.length); i++) {
    const id = ups[i].id;
    const here = () => f.uploads.value.some((u) => u.id === id);
    if (!here()) continue;
    try {
      const a = await apiUpload<Attachment>(`/api/v1/blobs?${query}`, chosen[i]);
      if (!here()) continue;
      const have = f.attachments.value.some((x) => x.hash === a.hash);
      if (!have && f.attachments.value.length >= maxFiles) {
        refuse(id, tooMany);
        continue;
      }
      f.uploads.value = f.uploads.value.filter((u) => u.id !== id);
      if (!have) f.attachments.value = [...f.attachments.value, a];
    } catch (err) {
      refuse(id, uploadFailure(err));
    }
  }
}

// clearFiles empties f: after a post, or a discard.
export function clearFiles(f: Files) {
  f.attachments.value = [];
  f.uploads.value = [];
}

// hashes is what a post or a draft sends for f's files, absent when there
// are none.
export const hashes = (list: Attachment[]) => (list.length ? { attachments: list.map((a) => a.hash) } : {});

// filesIn reads the attachments of a draft, from the server or from this
// browser's copy, keeping only entries a chip can show.
export function filesIn(v: unknown): Attachment[] {
  return Array.isArray(v)
    ? v.filter((a) => a && typeof a.hash === 'string' && typeof a.original_name === 'string' && typeof a.size === 'number')
    : [];
}

// uploadFailure says why an upload was refused, in the words of attaching
// (docs/07 "Attachments"): the file's type, a blocked file, the size cap, no
// upload permission, and the write limiter.
export function uploadFailure(err: unknown): string {
  const p = asProblem(err);
  switch (p.fieldErrors.file ?? p.type) {
    case 'type_not_allowed':
      return 'Only PNG, JPEG and GIF images, PDFs and plain text can be attached.';
    case 'blocked':
      return "This file can't be attached.";
    case 'payload-too-large':
      return 'This file is larger than this forum accepts.';
    case 'forbidden':
      return "You can't attach files here.";
    case 'rate-limited':
      if (p.retryAfter) return `You are uploading too quickly. You can try again in ${p.retryAfter} second${p.retryAfter === 1 ? '' : 's'}.`;
  }
  return problemMessage(err).text;
}
