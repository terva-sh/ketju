// UnreadDivider is drawn between the last post the viewer has read and the
// first they have not (docs/06 "Landing on return"): the single most valuable
// piece of UI in a linear forum.
export function UnreadDivider({ unread, lastRead }: { unread: number; lastRead: number }) {
  return (
    <div class="unread-divider" id="unread" role="separator" aria-label={`${unread} new ${unread === 1 ? 'post' : 'posts'} below`}>
      <span>
        {unread} new {unread === 1 ? 'post' : 'posts'} below
      </span>
      <span class="rule" />
      <span class="last-read">last read #{lastRead}</span>
    </div>
  );
}

// dividerBefore is the ordinal the divider goes in front of on this page, or
// null when there is none to draw: nothing read yet, everything read, or the
// line between read and unread is not on this page. It is on the page when a
// read post is on it too, or when the page opens exactly at the line, as the
// landing page does; a later page, all unread, does not repeat it.
export function dividerBefore(ordinals: number[], lastRead: number, postCount: number): number | null {
  if (lastRead <= 0 || lastRead >= postCount) return null;
  const first = ordinals.find((o) => o > lastRead);
  if (first === undefined) return null;
  const readHere = ordinals.some((o) => o <= lastRead);
  return readHere || first === lastRead + 1 ? first : null;
}
