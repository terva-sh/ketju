import type { ThreadRow as Row } from '../api/dto';
import { Icon } from './Icon';
import { PostTime } from './thread/PostTime';
import './unread.css';
import './threadrow.css';

// threadHref is a thread's page 1: the title's link in a list. It names the
// page, so the reader starts at the top rather than where they left off
// (docs/06 "Thread list views": clicking the title goes to page 1).
export function threadHref(t: Pick<Row, 'id' | 'slug'>): string {
  return `/t/${encodeURIComponent(t.id)}/${encodeURIComponent(t.slug)}?page=1`;
}

// unreadHref is where the unread badge goes: the first unread post. With a
// read position that is the post after it, as a permalink the server
// resolves to its page (dto.ThreadRow). With none, the post after 0 is not
// necessarily the first unread one (mark all read makes older posts read),
// so the badge goes to the thread's own address, where the server lands a
// signed-in reader on their first unread post (docs/06 "Landing on return").
export function unreadHref(t: Pick<Row, 'id' | 'slug' | 'last_read_ordinal'>): string {
  const base = `/t/${encodeURIComponent(t.id)}/${encodeURIComponent(t.slug)}`;
  return t.last_read_ordinal > 0 ? `${base}/${t.last_read_ordinal + 1}` : base;
}

// ThreadRow is one thread in a list (docs/06 "Thread list views"): title,
// tags, author, replies, last poster and time, and for a signed-in reader
// the unread badge. The badge is a control, not an ornament: it and the
// title are separate links to different places.
export function ThreadRow({ thread: t, signedIn }: { thread: Row; signedIn: boolean }) {
  return (
    <li class={`thread-row${t.pinned ? ' row-pinned' : ''}`} data-thread={t.id}>
      <div class="thread-row-main">
        <div class="thread-row-title">
          {t.pinned && (
            <span class="thread-flag pinned">
              <Icon name="pin" size={14} />
              Pinned
            </span>
          )}
          {t.locked && (
            <span class="thread-flag locked" title="Locked">
              <Icon name="lock" size={14} />
              <span class="visually-hidden">Locked</span>
            </span>
          )}
          <a class="thread-title hit" href={threadHref(t)}>
            <span>{t.title}</span>
          </a>
        </div>
        {t.tags.length > 0 && (
          <ul class="thread-tags" aria-label="Tags">
            {t.tags.map((tag) => (
              <li key={tag}>{tag}</li>
            ))}
          </ul>
        )}
        <div class="thread-row-meta">
          <span>by {t.author.handle}</span>
          <span>
            {t.reply_count} repl{t.reply_count === 1 ? 'y' : 'ies'}
          </span>
          <span>
            last {t.last_post_author.handle} <PostTime at={t.last_post_at} />
          </span>
        </div>
      </div>
      {signedIn && t.unread > 0 && (
        <a class="unread-badge thread-unread hit" href={unreadHref(t)} data-unread={t.unread}>
          {t.unread} new
        </a>
      )}
    </li>
  );
}
