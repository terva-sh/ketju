import { useState } from 'preact/hooks';
import type { Author } from '../../api/dto';
import { viewer } from '../../state/viewer';

// avatarUrl is where an avatar is served: /avatars/{hash}, to signed-in
// members only (docs/05 "Display name and bio", TKT-01M3YSQ4CR).
export const avatarUrl = (hash: string) => `/avatars/${encodeURIComponent(hash)}`;

// Avatar is the author's avatar when they have one and the reader is signed
// in, and otherwise their initial on a colour that follows from the handle,
// so the same person is the same colour on every post. A signed-out reader
// and a pre-user see the initial: the server serves avatars to members only.
// An image that fails to load falls back to the initial too.
export function Avatar({ author }: { author: Author }) {
  const [failed, setFailed] = useState('');
  if (author.avatar && viewer.value.user && failed !== author.avatar) {
    return (
      <img
        class="avatar"
        src={avatarUrl(author.avatar)}
        alt=""
        width={36}
        height={36}
        onError={() => setFailed(author.avatar)}
      />
    );
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
