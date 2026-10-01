import type { ComponentChildren } from 'preact';
import { useRef } from 'preact/hooks';
import { Icon } from '../components/Icon';
import { formatSize } from '../components/thread/Attachments';
import { attach, full, type Files } from '../state/files';

// The types POST /blobs takes (docs/07 "Attachments"). The picker offers
// only these; the server sniffs the bytes and decides.
const accept = 'image/png,image/jpeg,image/gif,application/pdf,text/plain';

// AttachButton is the composer toolbar's Attach (TKT-01M3V73J8K): it opens
// the file picker and uploads each file chosen to where query names,
// thread=<id> or forum=<slug>. It is off while a post is in flight and once
// ten files are attached or on their way. The composer offers it only where
// the viewer may upload, and never while editing, since an edit cannot
// change a post's files (docs/03).
export function AttachButton({ files, query, posting }: { files: Files; query: string; posting: boolean }) {
  const input = useRef<HTMLInputElement>(null);
  return (
    <>
      <button
        type="button"
        aria-label="Attach files"
        title="Attach files"
        data-attach
        disabled={posting || full(files)}
        onClick={() => input.current?.click()}
      >
        <Icon name="clip" />
      </button>
      <input
        ref={input}
        type="file"
        multiple
        accept={accept}
        hidden
        data-attach-input
        onChange={(e) => {
          const el = e.currentTarget;
          const chosen = [...(el.files ?? [])];
          // Cleared, so choosing the same file again is a change.
          el.value = '';
          void attach(files, query, chosen);
        }}
      />
    </>
  );
}

// FileChips are the composer's files: each attached one by name and size,
// since the server serves no blob before a post names it (docs/07
// "Attachments"), each upload still on its way, and each refused one with
// the reason. Every chip can be removed, except while a post is in flight.
export function FileChips({ files, posting }: { files: Files; posting: boolean }) {
  const attached = files.attachments.value;
  const ups = files.uploads.value;
  if (!attached.length && !ups.length) return null;
  // data is the chip's data-file (an attached file's hash) or data-upload
  // (uploading or refused).
  const chip = (key: string | number, name: string, detail: ComponentChildren, remove: () => void, data: Record<string, string>) => (
    <li key={key} class="file-chip" {...data} aria-busy={data['data-upload'] === 'uploading' || undefined}>
      <span class="file-name">{name}</span>
      {detail}
      <button type="button" class="file-remove" aria-label={`Remove ${name}`} disabled={posting} onClick={remove}>
        ×
      </button>
    </li>
  );
  return (
    <ul class="composer-files" aria-label="Attached files">
      {attached.map((a) =>
        chip(
          a.hash,
          a.original_name,
          <span class="file-size">{formatSize(a.size)}</span>,
          () => {
            files.attachments.value = files.attachments.value.filter((x) => x.hash !== a.hash);
          },
          { 'data-file': a.hash },
        ),
      )}
      {ups.map((u) =>
        chip(
          u.id,
          u.name,
          u.failure === null ? (
            <span class="file-size">Uploading…</span>
          ) : (
            <span class="file-failure" role="alert">
              {u.failure}
            </span>
          ),
          () => {
            files.uploads.value = files.uploads.value.filter((x) => x.id !== u.id);
          },
          { 'data-upload': u.failure === null ? 'uploading' : 'refused' },
        ),
      )}
    </ul>
  );
}
