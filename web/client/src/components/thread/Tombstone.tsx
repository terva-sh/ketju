// Tombstone stands where a deleted post was, keeping its ordinal and its
// anchor, so the thread's numbering and every link into it still hold
// (docs/06; the moved and moderator variants are TKT-01M29CKRJ's).
export function Tombstone({ ordinal }: { ordinal: number }) {
  return (
    <div class="post tombstone" id={`post-${ordinal}`} data-ordinal={ordinal}>
      <span class="avatar avatar-empty" aria-hidden="true" />
      <span class="ordinal">#{ordinal}</span>
      <span class="tombstone-text">Post removed.</span>
    </div>
  );
}
