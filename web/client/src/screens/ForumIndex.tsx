import { useEffect, useRef, useState } from 'preact/hooks';
import { apiGet, peekGet } from '../api/client';
import type { Forum, ForumTree } from '../api/dto';
import { useResource } from '../api/resource';
import { ErrorPanel } from '../components/ErrorPanel';
import { Icon } from '../components/Icon';
import { MarkAllRead } from '../components/MarkAllRead';
import { PostTime } from '../components/thread/PostTime';
import { useAfterMark } from '../state/lists';
import { HelpOverlay } from '../keyboard/HelpOverlay';
import { helpOpen } from '../keyboard/shortcuts';
import { isPhone } from '../state/layout';
import { siteName, viewer } from '../state/viewer';
import { ForumPicker, pickable } from './ForumPicker';
import '../components/unread.css';
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
//
// Beside it on desktop is Mark all read for the whole site (PUT /me/read),
// as the mockup's header draws it (TKT-01M39WBN4Q divergence F3,
// TKT-01M3V6Y4JP). The phone mockup has none; a phone marks the site read
// from /unread, as the desktop can too.
export function ForumIndex() {
  const res = useResource(forumsUrl, (signal) => apiGet<ForumTree>(forumsUrl, { signal }), () =>
    peekGet<ForumTree>(forumsUrl),
  );
  // A mark all read changes every count here (state/lists.ts).
  useAfterMark(res.retry);
  const me = viewer.value;
  const signedIn = !!me.user;
  const [picking, setPicking] = useState(false);
  const opener = useRef<HTMLElement | null>(null);
  const pick = (e: Event) => {
    opener.current = e.currentTarget as HTMLElement;
    setPicking(true);
  };
  const forums = res.data?.forums;
  // The new-thread controls need somewhere to start a thread: with every
  // forum archived, or none, there is nothing to pick.
  const canStart = signedIn && !!forums && pickable(forums).length > 0;

  return (
    <main class="forum-index" data-screen="forum-index">
      <header class="forum-index-head">
        <div>
          <h1>{siteName}</h1>
          {signedIn && <p class="forum-index-summary">{summary(me.unread.threads)}</p>}
        </div>
        {signedIn && forums && (
          <div class="list-actions">
            {forums.length > 0 && <MarkAllRead url="/api/v1/me/read" />}
            {canStart && (
              <button type="button" class="new-thread" onClick={pick} aria-haspopup="dialog">
                New thread
              </button>
            )}
          </div>
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
      {forums && forums.length > 0 && <IndexFoot forums={forums} members={res.data?.members ?? null} />}
      {canStart && (
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

// IndexFoot ends the front page (docs/06 "Thread list views (forum index)",
// TKT-01M3S3ZRYD): the member count, which the server sends a member alone,
// and the posts in the forums listed here, so only forums the reader may
// read are counted. From 720 px up it offers the shortcut list by a button;
// a phone has no shortcuts (docs/06 "Mobile"). No key is bound here: a
// single-character shortcut needs a way to turn it off first (WCAG 2.1.4,
// TKT-01M3V9DWTN; Terva review 1887), so the button does not say "Press ?".
function IndexFoot({ forums, members }: { forums: Forum[]; members: number | null }) {
  const phone = isPhone.value;
  const locale = viewer.value.settings.locale || undefined;
  const count = (n: number, what: string) => `${n.toLocaleString(locale)} ${what}${n === 1 ? '' : 's'}`;
  let posts = 0;
  for (const f of forums) {
    posts += f.post_count;
    for (const c of f.children) posts += c.post_count;
  }
  useEffect(
    () => () => {
      helpOpen.value = false;
    },
    [],
  );
  return (
    <footer class="forum-index-foot">
      <span>
        {members !== null && <span data-members={members}>{count(members, 'member')} · </span>}
        <span data-posts-total={posts}>{count(posts, 'post')}</span>
      </span>
      {!phone && (
        <button type="button" class="keys-hint" aria-haspopup="dialog" onClick={() => (helpOpen.value = true)}>
          Keyboard shortcuts
        </button>
      )}
      {helpOpen.value && !phone && <HelpOverlay lead="These keys work on a thread page, where ? opens this list." />}
    </footer>
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
          <a class="forum-name hit" href={href}>
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
        <span>
          {f.thread_count} thread{f.thread_count === 1 ? '' : 's'}
        </span>
        <span class="forum-posts" data-posts={f.post_count}>
          {f.post_count} post{f.post_count === 1 ? '' : 's'}
        </span>
      </div>
      {signedIn && (
        <div class="forum-unread">
          {f.unread > 0 ? (
            <a class="unread-badge hit" href={href} data-unread={f.unread}>
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
            <a
              class="forum-last-title hit"
              href={`/t/${encodeURIComponent(last.thread.id)}/${encodeURIComponent(last.thread.slug)}`}
            >
              <span>{last.thread.title}</span>
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
                <a class="hit" href={`${href}/new`}>
                  Start one
                </a>
              </>
            )}
          </span>
        )}
      </div>
    </li>
  );
}
