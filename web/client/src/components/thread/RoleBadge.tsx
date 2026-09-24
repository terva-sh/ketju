import type { Author } from '../../api/dto';

// RoleBadge carries the account's role and kind (docs/06 "Post anatomy"): an
// agent is marked on every post, and so is a moderator or admin. A human
// member, the common case, carries none, as in the mockups.
export function RoleBadge({ author }: { author: Author }) {
  const labels: string[] = [];
  if (author.role === 'moderator' || author.role === 'admin') labels.push(author.role);
  if (author.kind === 'agent') labels.push('agent');
  if (labels.length === 0) return null;
  return <span class="role-badge">{labels.join(' · ')}</span>;
}
