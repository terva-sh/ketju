import { useLocation } from 'preact-iso';
import { useEffect, useRef, useState } from 'preact/hooks';
import { apiGet } from '../../../api/client';
import type { AdminApplication, AdminApplicationList, ApplicationNote } from '../../../api/dto';
import { isAbort, isProblem } from '../../../api/problem';
import { useResource } from '../../../api/resource';
import { ErrorPanel } from '../../../components/ErrorPanel';
import { FormView } from '../../../forms/FormView';
import { when } from '../../apply/when';
import { ConfirmButton } from '../common/ConfirmButton';
import { Field } from '../common/Field';
import { send } from '../common/send';
import { Decision, noteMax, noteMessage } from './Decision';
import { ApplicantCell, applicationHref, hasStatus, StatusMark } from './ApplicationList';

// ApplicationDetail is one application, at /admin/applications?id=N: who
// applied and through which sign-in, the answers against the form version
// they answered, the identity's earlier applications, the moderators' notes,
// and the decision, or the controls to take one while it is pending.
//
// Everything here is staff's: the applicant sees their status only, never a
// note (TKT-01M29CP2MG).
export function ApplicationDetail({ id }: { id: number }) {
  const url = `/api/v1/admin/applications/${id}`;
  const res = useResource(url, (signal) => apiGet<AdminApplication>(url, { signal }));
  // fresh is the application as the last change on this screen answered it.
  const [fresh, setFresh] = useState<AdminApplication | null>(null);
  const [notice, setNotice] = useState('');
  const noticeRef = useRef<HTMLParagraphElement>(null);
  const a = fresh ?? res.data;

  // A notice takes focus, so a keyboard user whose control has gone with
  // the decision hears what happened and continues from there.
  useEffect(() => {
    if (notice) noticeRef.current?.focus();
  }, [notice]);

  if (!a) {
    if (res.error) {
      return isProblem(res.error, 'not-found') ? (
        <p>
          There is no application #{id}. <a href="/admin/applications">Back to applications</a>
        </p>
      ) : (
        <ErrorPanel error={res.error} what="The application" onRetry={res.retry} />
      );
    }
    return (
      <p class="admin-muted" aria-live="polite">
        Loading…
      </p>
    );
  }

  const changed = (next: AdminApplication, said: string) => {
    setFresh(next);
    setNotice(said);
  };

  return (
    <section class="admin-detail app-detail" aria-labelledby="app-title" data-application={a.id}>
      <p class="app-back">
        <a href="/admin/applications">All pending applications</a>
      </p>
      <h2 id="app-title">
        Application #{a.id} <StatusMark status={a.status} />
      </h2>
      {notice && (
        <p class="app-notice" role="status" tabIndex={-1} ref={noticeRef} data-notice>
          {notice}
        </p>
      )}

      <dl class="admin-facts">
        <dt>Submitted</dt>
        <dd>{when(a.created_at)}</dd>
        <dt>Applicant</dt>
        <dd>
          {a.identity ? (
            <ApplicantCell provider={a.identity.provider} email={a.identity.email} verified={a.identity.email_verified} />
          ) : (
            <span class="admin-muted">The sign-in this came from has been removed.</span>
          )}
        </dd>
        {a.identity?.blocked && (
          <>
            <dt>Blocked</dt>
            <dd class="app-blocked" data-blocked>
              <span class="admin-mark">Blocked</span> This sign-in may not apply again.{' '}
              <Unblock url={url} onChange={changed} />
            </dd>
          </>
        )}
      </dl>

      <h3>Answers</h3>
      <p class="admin-muted" data-form-version>
        {a.form.title}, version {a.form.version} of the form, as they answered it.
      </p>
      <FormView form={a.form} mode="read" answers={a.answers} />

      <h3>Decision</h3>
      {hasStatus(a, 'pending') ? (
        <Decision app={a} url={url} onChange={changed} />
      ) : (
        <Decided a={a} />
      )}

      <h3>Earlier applications from this sign-in</h3>
      {a.prior.length === 0 ? (
        <p class="admin-muted">None.</p>
      ) : (
        <ul class="app-prior" data-prior>
          {a.prior.map((p) => (
            <li key={p.id}>
              <a href={applicationHref(p.id)}>Application #{p.id}</a>, {when(p.created_at)} <StatusMark status={p.status} />
            </li>
          ))}
        </ul>
      )}

      <Notes url={url} notes={a.notes} onAdded={(n) => setFresh({ ...a, notes: [...a.notes, n] })} />
    </section>
  );
}

// Decided is how the application left pending: who, when, and the note.
function Decided({ a }: { a: AdminApplication }) {
  const d = a.decision;
  if (!d) return <p class="admin-muted">Not decided.</p>;
  return (
    <div data-decided>
      <p>
        {hasStatus(a, 'withdrawn') ? (
          <>Withdrawn by the applicant on {when(d.at)}.</>
        ) : (
          <>
            {hasStatus(a, 'approved') ? 'Approved' : 'Denied'} by{' '}
            {d.by ? `@${d.by.handle}` : 'a moderator'} on {when(d.at)}.
          </>
        )}
        {d.user && (
          <>
            {' '}
            They joined as <a href={`/u/${encodeURIComponent(d.user.handle)}`}>@{d.user.handle}</a>.
          </>
        )}
      </p>
      {d.note && (
        <div class="app-note">
          <p class="admin-muted">The moderator's note. The applicant never sees it.</p>
          <p class="app-note-body">{d.note}</p>
        </div>
      )}
      <NextPending />
    </div>
  );
}

// NextPending opens the newest application still waiting, so a moderator
// can go on from one they have just decided without the list between.
function NextPending() {
  const { route } = useLocation();
  const [said, setSaid] = useState('');
  const [busy, setBusy] = useState(false);
  const next = async () => {
    setBusy(true);
    setSaid('');
    try {
      const list = await apiGet<AdminApplicationList>('/api/v1/admin/applications?status=pending');
      const first = list.applications[0];
      if (first) route(applicationHref(first.id));
      else setSaid('No other application is waiting.');
    } catch (err) {
      if (!isAbort(err)) setSaid('The waiting applications could not be loaded. Try again.');
    } finally {
      setBusy(false);
    }
  };
  return (
    <p class="app-next">
      <button type="button" class="admin-button" onClick={next} disabled={busy}>
        Next pending
      </button>{' '}
      <span role="status">{said}</span>
    </p>
  );
}

// Unblock lifts the block the identity's denial set (maintainer decision
// 2026-09-29). Only the denial that set it can lift it (review 1470), so a
// refusal re-fetches and says so.
function Unblock({ url, onChange }: { url: string; onChange: (a: AdminApplication, notice: string) => void }) {
  const unblock = async () => {
    const r = await send<AdminApplication>('POST', `${url}/unblock`, {});
    if (r.ok) {
      onChange(r.value, 'Unblocked. They may apply again, one day after their last application.');
      return;
    }
    if (!isProblem(r.error, 'conflict')) throw r.error;
    const now = await apiGet<AdminApplication>(url);
    onChange(
      now,
      now.identity?.blocked
        ? 'This block was set when another of their applications was denied. Open the newest denied one to lift it.'
        : 'This sign-in is no longer blocked.',
    );
  };
  return <ConfirmButton label="Unblock" prompt="Let this sign-in apply again?" confirmLabel="Unblock" onConfirm={unblock} />;
}

// Notes are the moderators' notes, oldest first, and a form to add one.
function Notes({ url, notes, onAdded }: { url: string; notes: ApplicationNote[]; onAdded: (n: ApplicationNote) => void }) {
  const [body, setBody] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);
  const add = async (e: Event) => {
    e.preventDefault();
    if (busy || body.trim() === '') return;
    setBusy(true);
    setError(undefined);
    const r = await send<ApplicationNote>('POST', `${url}/notes`, { body });
    setBusy(false);
    if (!r.ok) {
      const code = r.error.fieldErrors.body;
      setError(code ? noteMessage(code) : r.message);
      return;
    }
    setBody('');
    onAdded(r.value);
  };
  return (
    <>
      <h3>Moderators' notes</h3>
      {notes.length === 0 ? (
        <p class="admin-muted">No notes yet.</p>
      ) : (
        <ol class="app-notes" data-notes>
          {notes.map((n) => (
            <li key={n.id}>
              <p class="admin-muted">
                @{n.author.handle}, {when(n.created_at)}
              </p>
              <p class="app-note-body">{n.body}</p>
            </li>
          ))}
        </ol>
      )}
      <form class="app-add-note" onSubmit={add} aria-label="Add a note">
        <Field label="Add a note" hint="For the other moderators. The applicant never sees notes." error={error}>
          {(c) => (
            <textarea
              {...c}
              rows={3}
              maxLength={noteMax}
              value={body}
              onInput={(e) => setBody((e.target as HTMLTextAreaElement).value)}
            />
          )}
        </Field>
        <button type="submit" class="admin-button" disabled={busy || body.trim() === ''}>
          Add note
        </button>
      </form>
    </>
  );
}
