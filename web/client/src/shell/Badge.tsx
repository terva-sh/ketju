import { badgeLabel } from './nav';

// Badge is a navigation count. It shows only when there is one; the words
// beside it are for a screen reader, which would otherwise read "Unread 12"
// with nothing to say what 12 counts.
export function Badge({ n, what }: { n: number | undefined; what: string }) {
  const text = badgeLabel(n);
  if (!text) return null;
  return (
    <span class="badge tnum" data-badge>
      {text}
      <span class="visually-hidden"> {what}</span>
    </span>
  );
}
