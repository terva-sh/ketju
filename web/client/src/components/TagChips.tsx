import './threadrow.css';

// tagHref is a forum's list filtered to one tag (docs/06 "Filtering by
// tag", TKT-01M3YSQPZX).
export function tagHref(forum: string, tag: string): string {
  return `/f/${encodeURIComponent(forum)}?tag=${encodeURIComponent(tag)}`;
}

// TagChips is a thread's tags as chips, each a link to forum's list
// filtered to it: in a thread list and on the thread head. A thread with no
// tags draws nothing. Each chip takes .hit, so on a phone its target is 44
// px about its centre while it keeps its size (docs/06 "Mobile").
export function TagChips({ tags, forum }: { tags: string[]; forum: string }) {
  if (tags.length === 0) return null;
  return (
    <ul class="thread-tags" aria-label="Tags">
      {tags.map((tag) => (
        <li key={tag}>
          <a class="hit" href={tagHref(forum, tag)} data-tag={tag}>
            {tag}
          </a>
        </li>
      ))}
    </ul>
  );
}
