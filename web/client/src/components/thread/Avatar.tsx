import type { Author } from '../../api/dto';

// Avatar is the author's uploaded image when there is one, and otherwise
// their initial on a colour that follows from the handle, so the same person
// is the same colour on every post.
export function Avatar({ author }: { author: Author }) {
  if (author.avatar) {
    return <img class="avatar" src={`/blobs/${encodeURIComponent(author.avatar)}`} alt="" width={36} height={36} />;
  }
  const initial = [...(author.display_name || author.handle || '?')][0].toUpperCase();
  return (
    <span class="avatar avatar-fallback" data-hue={hue(author.handle)} aria-hidden="true">
      {initial}
    </span>
  );
}

// hue picks one of eight fallback colours from the handle.
export function hue(handle: string): number {
  let h = 0;
  for (const ch of handle) h = (h * 31 + ch.codePointAt(0)!) >>> 0;
  return h % 8;
}
