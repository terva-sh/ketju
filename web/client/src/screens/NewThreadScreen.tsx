import { useLocation, useRoute } from 'preact-iso';
import { useEffect, useMemo, useRef, useState } from 'preact/hooks';
import { apiGet, forgetGets, peekGet } from '../api/client';
import type { Forum, ForumTree, ThreadHeader, ThreadList, ThreadPage } from '../api/dto';
import { asProblem, problemMessage, revokesAccess, type ProblemError } from '../api/problem';
import { useResource } from '../api/resource';
import { useVisualViewport } from '../composer/ComposerSheet';
import { attachRefused } from '../composer/Composer';
import { ComposerBody, DraftNote } from '../composer/ComposerBody';
import { continueQuote } from '../composer/quote';
import { threadHref } from '../components/ThreadRow';
import { permalink } from '../components/thread/Post';
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
  prefillContinue,
  refusedHere,
  type ContinueFrom,
  type NewThread,
} from '../state/newThread';
import { threadUrl } from '../state/threads';
import { useShellContext } from '../state/shell';
import { hasTagErrors, tagMessages } from '../state/tagErrors';
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
// Tags are one optional field under the title, separated by spaces or
// commas, and are not autosaved (TKT-01M3YSQPZX). A refused tag is named
// under the field (state/tagErrors.ts).
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
//
// With ?from={thread}&ordinal={n} it is "reply as new thread"
// (TKT-01M29CYSC0): the post's page is read, the composer is pre-filled with
// "@handle wrote:" and a plain blockquote of the post's text once the draft
// has loaded (prefillContinue), and the thread is sent with from, so the
// post gains a "continued in" note. A post that cannot be read leaves the
// composer as it was, says so, and the thread is sent without from.
export function NewThreadScreen() {
  const { params, query } = useRoute();
  const { route, url } = useLocation();
  const slug = params.forum;
  const from = continueFromQuery(query);
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

  // The post continued from, read once per address: its thread's header for
  // the notice, and its quote for the composer. gone is an answer that says
  // the post is not there to continue: not found, not readable, or not shown
  // as itself. Only that drops the continuation. Any other failure (the
  // network, an unavailable server) keeps it, says so and offers a retry,
  // and Post still sends from, so the server decides (Terva review 2059).
  const [origin, setOrigin] = useState<{ key: string; thread?: ThreadHeader; quote?: string; gone?: true; failed?: unknown } | null>(
    null,
  );
  const [tries, setTries] = useState(0);
  const fromKey = from ? `${from.thread}/${from.ordinal}` : '';
  useEffect(() => {
    if (!n) return;
    n.from.value = from;
    if (!from) return;
    const ctl = new AbortController();
    apiGet<ThreadPage>(threadUrl(from.thread, { ordinal: String(from.ordinal) }), { signal: ctl.signal }).then(
      (page) => {
        if (ctl.signal.aborted) return;
        const post = page.posts.find((p) => p.ordinal === from.ordinal);
        // A post not shown as itself is not one to continue: the server
        // would refuse it too.
        if (!post || post.deleted || post.moved) {
          setOrigin({ key: fromKey, gone: true });
          return;
        }
        setOrigin({ key: fromKey, thread: page.thread, quote: continueQuote(post.author.handle, post.html) });
      },
      (err) => !ctl.signal.aborted && setOrigin(revokesAccess(err) ? { key: fromKey, gone: true } : { key: fromKey, failed: err }),
    );
    return () => ctl.abort();
  }, [slug, signedIn, fromKey, tries]);
  const shownOrigin = origin && origin.key === fromKey ? origin : null;
  const loaded = n?.loaded.value ?? false;
  useEffect(() => {
    if (!n || !from || !shownOrigin) return;
    if (shownOrigin.gone) {
      n.from.value = null;
      return;
    }
    if (loaded && shownOrigin.quote !== undefined) prefillContinue(slug, from, shownOrigin.quote);
  }, [shownOrigin, loaded]);

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
            {shownOrigin?.thread && from && (
              <p class="new-thread-notice" data-continues>
                Continuing <a href={permalink(shownOrigin.thread, from.ordinal)}>{shownOrigin.thread.title}</a>, post #
                {from.ordinal}.
              </p>
            )}
            {shownOrigin?.gone && (
              <p class="new-thread-notice" data-continues-gone>
                The post you meant to continue is not there, or you cannot read it. This thread will start on its own.
              </p>
            )}
            {shownOrigin && shownOrigin.failed !== undefined && (
              <p class="new-thread-notice" data-continues-failed>
                The post this thread continues could not be loaded, so it is not quoted. The thread still continues it.{' '}
                <button type="button" onClick={() => setTries((t) => t + 1)}>
                  Retry
                </button>
              </p>
            )}
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
            <TagsField n={n} errors={tagMessages(fields, n.tagsSent)} />
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

// TagsField is the new thread's tags, one line of text. What the server
// refused is named under it, and the field is marked invalid until the next
// attempt.
function TagsField({ n, errors }: { n: NewThread; errors: string[] }) {
  return (
    <div class="new-thread-title new-thread-tags">
      <label for="new-thread-tags">Tags (optional)</label>
      <input
        id="new-thread-tags"
        type="text"
        autoComplete="off"
        autoCapitalize="none"
        spellcheck={false}
        value={n.tags.value}
        readOnly={n.posting.value}
        aria-invalid={errors.length > 0 || undefined}
        aria-describedby={errors.length > 0 ? 'new-thread-tags-hint new-thread-tags-error' : 'new-thread-tags-hint'}
        onInput={(e) => {
          if (!n.posting.value) n.tags.value = e.currentTarget.value;
        }}
      />
      <p class="field-hint" id="new-thread-tags-hint">
        Separate tags with spaces or commas
      </p>
      {errors.length > 0 && (
        <ul class="field-errors" id="new-thread-tags-error" data-tag-errors>
          {errors.map((e) => (
            <li key={e}>{e}</li>
          ))}
        </ul>
      )}
    </div>
  );
}

// threadIdPattern is a thread's base32 id (internal/store/threadid).
const threadIdPattern = /^[0-9a-f][0-9a-hjkmnp-tv-z]{12}$/;

// continueFromQuery reads ?from={thread}&ordinal={n}, or null when the
// address names no post to continue.
export function continueFromQuery(query: Record<string, string>): ContinueFrom | null {
  const thread = query.from ?? '';
  const ordinal = Number(query.ordinal);
  if (!threadIdPattern.test(thread) || !Number.isInteger(ordinal) || ordinal < 1) return null;
  return { thread, ordinal };
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
    case 'not-found':
      return 'The post this thread continues is no longer there, or you can no longer read it.';
    case 'conflict':
      return 'This forum is archived, so your thread was not posted.';
    case 'validation-failed':
      if (p.fieldErrors.attachments) return attachRefused;
      if (hasTagErrors(p.fieldErrors)) return 'Your thread was not posted: a tag was refused. The Tags field says which.';
      return 'The title must be 1 to 200 characters with no control characters, and the post cannot be empty.';
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
