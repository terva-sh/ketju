import { useLocation, useRoute } from 'preact-iso';
import { useEffect, useMemo, useRef } from 'preact/hooks';
import { apiGet, forgetGets, peekGet } from '../api/client';
import type { Forum, ForumTree, ThreadList } from '../api/dto';
import { asProblem, problemMessage, type ProblemError } from '../api/problem';
import { useResource } from '../api/resource';
import { useVisualViewport } from '../composer/ComposerSheet';
import { attachRefused } from '../composer/Composer';
import { ComposerBody, DraftNote } from '../composer/ComposerBody';
import { threadHref } from '../components/ThreadRow';
import { meHref } from '../shell/nav';
import { uploading } from '../state/files';
import { isPhone } from '../state/layout';
import {
  canDiscard,
  createThread,
  discardNewThread,
  loadNewThreadDraft,
  newThreadFor,
  newThreadNote,
  refusedHere,
  type NewThread,
} from '../state/newThread';
import { useShellContext } from '../state/shell';
import { viewer } from '../state/viewer';
import { forumsUrl } from './ForumIndex';
import { NotFound } from './Placeholder';
import { threadListUrl } from './ThreadListScreen';
import './newthread.css';

// MaxTitle is the server's bound on a title, in characters
// (internal/service/threads MaxTitle).
const maxTitle = 200;

// NewThreadScreen is /f/{forum}/new, the new-thread composer (docs/07 URL
// scheme, docs/06 "Composer"; TKT-01M3NS16VH): a title and the shared
// composer body, with Post thread in its footer on a desktop. On a phone the
// screen is the composer sheet itself, a column that fills the screen with
// the toolbar last, and Post is in the top bar (ShellContext.primary), since
// the footer would sit under the keyboard. The tab bar gives way to it
// (ShellContext.composing), and Back leaves the route.
//
// The title and the body are autosaved as the viewer's draft for the forum
// (state/newThread.ts over state/drafts.ts), loaded when the screen opens,
// and deleted once the thread is created, which then opens.
//
// Attach is offered where the forum tree says the viewer may upload to a new
// thread there (can_upload), and the files go with the first post
// (TKT-01M3V73J8K).
//
// The API decides who may start a thread here; the screen only warns. An
// archived forum, when the forum tree says so, gets a notice before anything
// is typed, and Post stays on, because an admin may start a thread there
// (docs/04, docs/08). Only the create route's own forbidden or conflict
// turns Post off. A refused draft save turns autosave off and says so, and
// leaves Post alone: the draft route is a separate answer.
export function NewThreadScreen() {
  const { params } = useRoute();
  const { route, url } = useLocation();
  const slug = params.forum;
  const signedIn = !!viewer.value.user;
  const phone = isPhone.value;
  const tree = useResource(signedIn ? forumsUrl : null, (signal) => apiGet<ForumTree>(forumsUrl, { signal }), () =>
    peekGet<ForumTree>(forumsUrl),
  );
  const forum = tree.data ? findForum(tree.data.forums, slug) : undefined;
  // The tree holds every forum the viewer may see, so one absent from it is
  // one they cannot start a thread in either.
  const missing = !!tree.data && !forum;
  const name = forum?.name ?? peekGet<ThreadList>(threadListUrl(slug))?.forum?.name ?? '';
  const n = signedIn ? newThreadFor(slug) : null;
  const main = useRef<HTMLElement>(null);
  useVisualViewport(main);

  useEffect(() => {
    if (!n) return;
    // A refusal from an earlier visit is not this one's.
    if (!n.posting.value) n.failure.value = null;
    void loadNewThreadDraft(slug);
  }, [slug, signedIn]);

  const posting = n?.posting.value ?? false;
  const blocked = !!n && refusedHere(n);
  const ready = !!n && !!n.title.value.trim() && !!n.source.value.trim() && !uploading(n);
  const canPost = ready && !posting && !blocked;
  const post = () => {
    void createThread(slug).then((res) => {
      if (!res) return;
      // The forum's lists now hold a thread their held copies do not.
      const lists = `/api/v1/forums/${encodeURIComponent(slug)}/threads`;
      forgetGets((u) => u === forumsUrl || u === lists || u.startsWith(lists + '?'));
      route(threadHref(res.thread));
    });
  };
  // Discard waits for the draft to load and for something to discard, on
  // both layouts, so it never deletes a draft the reader has not seen
  // (Terva review 1591).
  const discardable = !!n && canDiscard(n);
  const discard = () => {
    if (!n || !canDiscard(n)) return;
    if ((n.title.value.trim() || n.source.value.trim()) && !confirm('Discard this thread? The title and text will be deleted.')) return;
    discardNewThread(slug);
  };

  const back = { href: `/f/${encodeURIComponent(slug)}`, label: name || 'Threads' };
  const ctx = useMemo(
    () =>
      missing
        ? null
        : {
            title: 'New thread',
            back,
            actions: discardable ? [{ id: 'discard-thread', label: 'Discard draft', run: discard }] : [],
            composing: !!n,
            primary: n ? { id: 'post-thread', label: posting ? 'Posting…' : 'Post', run: post, disabled: !canPost } : undefined,
          },
    [missing, slug, name, !!n, posting, canPost, discardable],
  );
  useShellContext(ctx, [ctx]);

  if (missing) return <NotFound />;

  const head = (
    <header class="new-thread-head">
      <a class="new-thread-up" href={back.href}>
        {name || 'Threads'}
      </a>
      <h1 class={phone ? 'visually-hidden' : undefined}>New thread</h1>
    </header>
  );

  if (!n) {
    return (
      <main class="new-thread-page" data-screen="new-thread" ref={main}>
        {head}
        <p class="new-thread-signin">
          You need to sign in to start a thread. <a href={meHref(url)}>Sign in</a>
        </p>
      </main>
    );
  }

  const failure = n.failure.value;
  const fields = failure ? asProblem(failure).fieldErrors : {};
  const note = newThreadNote(slug);
  const draft = { kind: 'new', forum: slug } as const;
  return (
    <main class={`new-thread-page${phone ? ' in-sheet' : ''}`} data-screen="new-thread" ref={main}>
      {head}
      <ComposerBody
        state={n}
        name="New thread"
        label="Thread text"
        placeholder="Write the first post. Markdown works."
        sheet={phone}
        invalid={'source' in fields}
        upload={forum?.can_upload ? `forum=${encodeURIComponent(slug)}` : undefined}
        head={phone && note ? <DraftNote draft={draft} note={note} /> : undefined}
        notice={
          <>
            {forum?.archived && (
              <p class="new-thread-notice" data-archived>
                This forum is archived. Only admins can start threads here.
              </p>
            )}
            {failure !== null && (
              <p class="post-failure" role="alert" id="new-thread-failure">
                {createFailure(failure)}
              </p>
            )}
            <TitleField n={n} invalid={'title' in fields} />
          </>
        }
        foot={
          <footer class="composer-foot">
            <span>Markdown supported</span>
            {!phone && <DraftNote draft={draft} note={note} />}
            <button type="button" class="discard" disabled={!discardable} onClick={discard}>
              Discard
            </button>
            <button type="button" class="post-reply" data-post-thread disabled={!canPost} aria-busy={posting} onClick={post}>
              {posting ? 'Posting…' : 'Post thread'}
            </button>
          </footer>
        }
      />
    </main>
  );
}

function TitleField({ n, invalid }: { n: NewThread; invalid: boolean }) {
  return (
    <div class="new-thread-title">
      <label for="new-thread-title">Title</label>
      <input
        id="new-thread-title"
        type="text"
        maxLength={maxTitle}
        autoComplete="off"
        value={n.title.value}
        readOnly={n.posting.value}
        aria-invalid={invalid || undefined}
        aria-describedby={invalid ? 'new-thread-failure' : undefined}
        onInput={(e) => {
          if (!n.posting.value) n.title.value = e.currentTarget.value;
        }}
      />
    </div>
  );
}

// findForum is slug's forum in the tree, a root or a child.
function findForum(forums: Forum[], slug: string): Forum | undefined {
  for (const f of forums) {
    if (f.slug === slug) return f;
    const c = findForum(f.children ?? [], slug);
    if (c) return c;
  }
  return undefined;
}

// createFailure says why a thread was not created, in the words of starting
// one: the refusals POST /forums/{slug}/threads names in docs/07. The text
// stays, so it can be copied or tried again.
export function createFailure(err: unknown): string {
  const p: ProblemError = asProblem(err);
  switch (p.type) {
    case 'forbidden':
      return 'You may not start threads in this forum.';
    case 'conflict':
      return 'This forum is archived, so your thread was not posted.';
    case 'validation-failed':
      return p.fieldErrors.attachments
        ? attachRefused
        : 'The title must be 1 to 200 characters with no control characters, and the post cannot be empty.';
    case 'payload-too-large':
      return 'The post is longer than the limit allows.';
    case 'rate-limited':
      return p.retryAfter
        ? `You are posting too quickly. You can try again in ${p.retryAfter} second${p.retryAfter === 1 ? '' : 's'}.`
        : 'You are posting too quickly. Wait a moment and try again.';
    default:
      return problemMessage(err).text;
  }
}
