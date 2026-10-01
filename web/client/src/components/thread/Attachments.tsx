import { useState } from 'preact/hooks';
import type { Attachment, Post } from '../../api/dto';
import { isPhone } from '../../state/layout';
import { Icon } from '../Icon';

// Attachments lists a post's files as the mockups' cards (docs/06 "Post
// anatomy", docs/mockups/generate.py attachment; TKT-01M3V73J8K), on the
// thread page, in the reply expander's copies and in the moderation queue.
// A tombstone has none: the API sends it none.
//
// Each card is one link to the blob, named by the file's name and size. An
// image shows its thumbnail, or a file icon if there is none to load (too
// large to thumbnail, or broken), and on a desktop its dimensions. Any other
// file downloads under its own name, because the server names none
// (docs/07 "Attachments").
export function Attachments({ post }: { post: Pick<Post, 'attachments'> }) {
  if (post.attachments.length === 0) return null;
  return (
    <ul class="attachments">
      {post.attachments.map((a) => (
        <li key={a.hash}>
          <Card a={a} />
        </li>
      ))}
    </ul>
  );
}

function Card({ a }: { a: Attachment }) {
  const href = `/blobs/${encodeURIComponent(a.hash)}`;
  const image = a.width !== null && a.height !== null;
  const [thumb, setThumb] = useState(image);
  return (
    <a class="attachment" href={href} download={image ? undefined : a.original_name} data-attachment={a.hash}>
      {thumb ? (
        <img src={`${href}/thumb`} alt="" width={48} height={36} loading="lazy" onError={() => setThumb(false)} />
      ) : (
        <span class="attachment-icon">
          <Icon name="file" size={20} />
        </span>
      )}
      <span class="attachment-text">
        <span class="attachment-name">{a.original_name}</span>{' '}
        <span class="attachment-meta">
          {formatSize(a.size)}
          {image && !isPhone.value && ` · ${a.width}×${a.height}`}
        </span>
      </span>
    </a>
  );
}

export function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}
