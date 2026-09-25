import { useRef, useState } from 'preact/hooks';
import { apiGet, peekGet } from '../api/client';
import type { Forum, ForumTree } from '../api/dto';
import { useResource } from '../api/resource';
import { ErrorPanel } from '../components/ErrorPanel';
import { Icon } from '../components/Icon';
import { PostTime } from '../components/thread/PostTime';
import { viewer } from '../state/viewer';
import { ForumPicker } from './ForumPicker';
import './forums.css';

export const forumsUrl = '/api/v1/forums';

// ForumIndex is the front page (docs/mockups Main.dc.html on desktop,
// MobileFront*.dc.html on phones): the forums the reader may read, in the
// server's order, each with its activity, the reader's unread count and its
// latest post. In a small community this is the screen that answers "is
// anything happening", so the counts are the content.
//
// The mockup groups forums under labels. The tree has no labels, only root
// forums with children, one level deep (docs/04), so a root with children is
// a group headed by its name, its own row first; roots without children
// share an unlabelled group, in order.
//
// One markup serves both widths: the rows are a grid on desktop and cards on
// a phone (forums.css), as docs/07 "Responsive" keeps layout in CSS outside
// the shell. The floating new-thread button is the phone's; desktop has the
// header's button. Both open the forum picker, because a thread needs a
// forum (settled in grooming).
export function ForumIndex() {
  const res = useResource(forumsUrl, (signal) => apiGet<ForumTree>(forumsUrl, { signal }), () =>
    peekGet<ForumTree>(forumsUrl),
  );
  const me = viewer.value;
  const signedIn = !!me.user;
  const [picking, setPicking] = useState(false);
  const opener = useRef<HTMLElement | null>(null);
  const pick = (e: Event) => {
    opener.current = e.currentTarget as HTMLElement;
    setPicking(true);
  };
  const forums = res.data?.forums;

  return (
    <main class="forum-index" data-screen="forum-index">
      <header class="forum-index-head">
        <div>
          <h1>Forums</h1>
          {signedIn && <p class="forum-index-summary">{summary(me.unread.threads)}</p>}
        </div>
        {signedIn && forums && forums.length > 0 && (
          <button type="button" class="new-thread" onClick={pick} aria-haspopup="dialog">
            New thread
          </button>
        )}
      </header>
      {res.error && !forums && <ErrorPanel error={res.error} what="The forums" onRetry={res.retry} />}
      {forums && forums.length === 0 && (
        <p class="forum-index-empty" data-empty-index>
          There are no forums here yet.
        </p>
      )}
      {forums && forums.length > 0 && (
        <>
          <div class={`forum-cols${signedIn ? ' signed-in' : ''}`} aria-hidden="true">
            <span>Forum</span>
            <span class="col-activity">Activity</span>
            {signedIn && <span class="col-unread">Unread</span>}
            <span>Last post</span>
          </div>
          {groups(forums).map((g, i) => (
            <section class="forum-group" key={g.label ?? `plain-${i}`} aria-label={g.label}>
              {g.label && <h2 class="forum-group-label">{g.label}</h2>}
              <ul class="forum-rows">
                {g.rows.map((f) => (
                  <ForumRow key={f.slug} forum={f} signedIn={signedIn} />
                ))}
              </ul>
            </section>
          ))}
        </>
      )}
      {signedIn && forums && forums.length > 0 && (
        <button type="button" class="fab" aria-label="New thread" aria-haspopup="dialog" onClick={pick} data-fab>
          <Icon name="pen" size={22} />
        </button>
      )}
      {picking && forums && (
        <ForumPicker
          forums={forums}
          onClose={() => {
            setPicking(false);
            opener.current?.focus();
          }}
        />
      )}
    </main>
  );
}

function summary(threads: number): string {
  if (threads === 0) return 'Nothing unread.';
  return `${threads} unread thread${threads === 1 ? '' : 's'}.`;
}

interface Group {
  label?: string;
  rows: Forum[];
}

// groups lays the tree out as the mockup's groups: a root with children is
// its own group, headed by its name, with itself first; roots without
// children, taken in order, share an unlabelled one.
export function groups(forums: Forum[]): Group[] {
  const out: Group[] = [];
  for (const f of forums) {
    if (f.children.length > 0) {
      out.push({ label: f.name, rows: [f, ...f.children] });
      continue;
    }
    const last = out[out.length - 1];
    if (last && last.label === undefined) last.rows.push(f);
    else out.push({ rows: [f] });
  }
  return out;
}

const privacy: Record<string, string> = { members: 'Members only', restricted: 'Restricted' };

function ForumRow({ forum: f, signedIn }: { forum: Forum; signedIn: boolean }) {
  const href = `/f/${encodeURIComponent(f.slug)}`;
  const last = f.last_post;
  return (
    <li class={`forum-row${signedIn ? ' signed-in' : ''}`} data-forum={f.slug}>
      <div class="forum-main">
        <div class="forum-title">
          <a class="forum-name" href={href}>
            {f.name}
          </a>
          {privacy[f.visibility] && (
            <span class="forum-lock" title={privacy[f.visibility]}>
              <Icon name="lock" size={14} />
              <span class="visually-hidden">{privacy[f.visibility]}</span>
            </span>
          )}
          {f.archived && <span class="forum-archived">Archived</span>}
        </div>
        {f.description && <p class="forum-desc">{f.description}</p>}
      </div>
      <div class="forum-activity">
        {f.thread_count} thread{f.thread_count === 1 ? '' : 's'}
      </div>
      {signedIn && (
        <div class="forum-unread">
          {f.unread > 0 ? (
            <a class="unread-badge" href={href} data-unread={f.unread}>
              {f.unread} new
            </a>
          ) : (
            <span class="unread-none" aria-label="Nothing unread">
              —
            </span>
          )}
        </div>
      )}
      <div class="forum-last">
        {last ? (
          <>
            <a class="forum-last-title" href={`/t/${encodeURIComponent(last.thread.id)}/${encodeURIComponent(last.thread.slug)}`}>
              {last.thread.title}
            </a>
            <span class="forum-last-meta">
              <span class="forum-last-by">{last.author.handle} · </span>
              <PostTime at={last.at} />
            </span>
          </>
        ) : (
          <span class="forum-none" data-no-threads>
            No threads yet.
            {signedIn && !f.archived && (
              <>
                {' '}
                <a href={`${href}/new`}>Start one</a>
              </>
            )}
          </span>
        )}
      </div>
    </li>
  );
}
