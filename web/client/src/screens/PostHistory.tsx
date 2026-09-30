import { useMemo, useState } from 'preact/hooks';
import { apiGet } from '../api/client';
import type { PostHistory as PostHistoryDTO, PostRevision } from '../api/dto';
import { useResource } from '../api/resource';
import { ErrorPanel } from '../components/ErrorPanel';
import { Avatar } from '../components/thread/Avatar';
import { permalink, type ThreadRef } from '../components/thread/Post';
import { PostTime } from '../components/thread/PostTime';
import { removedText } from '../components/thread/removal';
import { RoleBadge } from '../components/thread/RoleBadge';
import { wordDiff } from '../history/wordDiff';

// PostHistory is a post's history (docs/08 "Post history"): every revision
// with who wrote it, when and why, and a word-level diff of any two, taken
// from their markdown sources here rather than on the server
// (TKT-01M29CP2Q3).
//
// It renders what the endpoint serves and decides nothing about who may see
// it. The server refuses an anonymous reader, and a member when
// public_edit_history is off; it withholds a removed post's revisions from
// all but those who may see the post; and it blanks a hard-redacted post's.
// Those refusals render through ErrorPanel, and the withheld and redacted
// cases say so.
export function PostHistory({ thread, ordinal }: { thread: ThreadRef; ordinal: string }) {
  const url = `/api/v1/threads/${encodeURIComponent(thread.id)}/posts/${encodeURIComponent(ordinal)}/history`;
  const res = useResource(url, (signal) => apiGet<PostHistoryDTO>(url, { signal }));
  return (
    <section class="post-history" data-history-for={ordinal}>
      <h2>History of post #{ordinal}</h2>
      {res.error && <ErrorPanel error={res.error} what="This post's history" onRetry={res.retry} />}
      {!res.data && !res.error && <p class="loading">Loading…</p>}
      {res.data && <Revisions key={url} history={res.data} />}
      <p>
        <a href={permalink(thread, Number(ordinal))}>Back to post #{ordinal}</a>
      </p>
    </section>
  );
}

// The words a redacted post reads with wherever it appears (removal.tsx).
const permanently = removedText({ removal: null, redacted: true });

function Revisions({ history }: { history: PostHistoryDTO }) {
  const revs = history.revisions;
  if (revs.length === 0) {
    // A removed post's history, withheld: the tombstone's own words when it
    // is gone for good, as every other place a redacted post appears says.
    return (
      <p class="history-notice" data-history-withheld>
        {history.redacted ? permanently : 'Post removed. Its history is not shown.'}
      </p>
    );
  }
  if (history.redacted) {
    // A moderator's view of a hard-redacted post: its revisions, each served
    // with no source or reason, so there is nothing to compare.
    return (
      <>
        <p class="history-notice" data-history-redacted>
          {permanently}. Its revisions are listed without their content.
        </p>
        <ol class="revisions">
          {[...revs].reverse().map((r) => (
            <li key={r.revision}>
              <RevisionLine rev={r} />
            </li>
          ))}
        </ol>
      </>
    );
  }
  return <Compare history={history} />;
}

// Compare lists the revisions newest first, each with a From and a To
// choice, and shows the diff of the two chosen: at first the newest against
// the one before it.
function Compare({ history }: { history: PostHistoryDTO }) {
  const revs = history.revisions;
  const newest = revs[revs.length - 1].revision;
  const [from, setFrom] = useState(revs.length > 1 ? revs[revs.length - 2].revision : newest);
  const [to, setTo] = useState(newest);
  const source = (n: number) => revs.find((r) => r.revision === n)?.source ?? '';
  const runs = useMemo(() => (from === to ? [] : wordDiff(source(from), source(to))), [history, from, to]);
  const several = revs.length > 1;

  return (
    <>
      {history.deleted && (
        <p class="history-notice" data-history-removed>
          This post is removed. Its history is shown to you because you may see it.
        </p>
      )}
      <ol class="revisions">
        {[...revs].reverse().map((r) => (
          <li key={r.revision} class={r.revision === from || r.revision === to ? 'chosen' : undefined}>
            {several && (
              <span class="compare-picks">
                <label class="pick">
                  <input
                    type="radio"
                    name="history-from"
                    checked={r.revision === from}
                    onChange={() => setFrom(r.revision)}
                    aria-label={`Compare from revision ${r.revision}`}
                  />
                </label>
                <label class="pick">
                  <input
                    type="radio"
                    name="history-to"
                    checked={r.revision === to}
                    onChange={() => setTo(r.revision)}
                    aria-label={`Compare to revision ${r.revision}`}
                  />
                </label>
              </span>
            )}
            <RevisionLine rev={r} />
          </li>
        ))}
      </ol>
      {several ? (
        <div class="history-diff" data-from={from} data-to={to}>
          <h3>
            Revision {from} to revision {to}
          </h3>
          {from === to ? (
            <p class="history-notice">Choose two different revisions to compare.</p>
          ) : (
            <>
              <p class="diff-key">Deleted words are struck through; inserted words are underlined.</p>
              <div class="diff">
                {runs.map((r, i) =>
                  r.op === 'del' ? <del key={i}>{r.text}</del> : r.op === 'ins' ? <ins key={i}>{r.text}</ins> : r.text,
                )}
              </div>
            </>
          )}
        </div>
      ) : (
        <p class="history-notice">This post has not been edited.</p>
      )}
    </>
  );
}

// RevisionLine is one revision's who, when and why. A grace edit, made by
// its author within the grace window, is stored and listed like any other,
// and is marked because the post shows no edited marker for it.
function RevisionLine({ rev }: { rev: PostRevision }) {
  return (
    <div class="revision" data-revision={rev.revision}>
      <Avatar author={rev.actor} />
      <div class="revision-main">
        <div class="revision-head">
          <span class="revision-number">Revision {rev.revision}{rev.revision === 1 && ' (original)'}</span>
          <span class="author">{rev.actor.display_name || rev.actor.handle}</span>
          <RoleBadge author={rev.actor} />
          <PostTime at={rev.at} />
        </div>
        {rev.reason && <p class="revision-reason">Reason: {rev.reason}</p>}
        {rev.grace && (
          <p class="revision-grace" data-grace>
            Grace edit: made soon after posting, so the post is not marked edited for it.
          </p>
        )}
      </div>
    </div>
  );
}
