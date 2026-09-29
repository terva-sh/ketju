import { useEffect, useState } from 'preact/hooks';
import { apiGet, apiSend } from '../../api/client';
import type { ApplicationStatus } from '../../api/dto';
import { asProblem, isAbort, isProblem, problemMessage, type ProblemError } from '../../api/problem';
import { ErrorPanel } from '../../components/ErrorPanel';
import { useShellContext } from '../../state/shell';
import { notAccepting } from './ApplyScreen';
import { again, when } from './when';

// StatusScreen is /apply/status, where an applicant lands and comes back to
// (docs/05 "mode = approval" step 4). What it offers comes from the server:
// can_apply, can_apply_at and can_withdraw, so the client keeps no copy of
// the rules. It never shows a moderator's note (TKT-01M29CP2MG).
//
// An approved applicant does not normally see this page: approval retires
// the pre-user session, so the next request is signed out and answers 401
// here (TKT-01M3Q2V25E). The approved state is still drawn, since the API
// keeps the value.

const statusURL = '/api/v1/apply/status';

// signInURL is the server's login page, coming back to the forum index: a
// full page load, since /auth/* is outside the client's routes.
const signInURL = '/auth/signin?return_to=%2F';

export function StatusScreen() {
  const [status, setStatus] = useState<ApplicationStatus | null>(null);
  const [error, setError] = useState<ProblemError | null>(null);
  const [attempt, setAttempt] = useState(0);
  useShellContext({ title: 'Your application', actions: [] }, []);

  useEffect(() => {
    const ctl = new AbortController();
    setError(null);
    apiGet<ApplicationStatus>(statusURL, { signal: ctl.signal }).then(
      (s) => setStatus(s),
      (err) => {
        if (ctl.signal.aborted || isAbort(err)) return;
        setStatus(null);
        setError(asProblem(err));
      },
    );
    return () => ctl.abort();
  }, [attempt]);

  const reload = () => setAttempt((n) => n + 1);

  return (
    <main class="apply" data-screen="apply-status">
      <h1>Your application</h1>
      {error ? (
        <StatusRefusal error={error} onRetry={reload} />
      ) : !status ? (
        <p class="apply-loading" aria-live="polite">
          Loading…
        </p>
      ) : (
        <Status status={status} onChange={setStatus} onStale={reload} />
      )}
    </main>
  );
}

function Status({
  status: s,
  onChange,
  onStale,
}: {
  status: ApplicationStatus;
  onChange: (s: ApplicationStatus) => void;
  onStale: () => void;
}) {
  switch (s.status) {
    case 'none':
      return (
        <div class="apply-panel" data-status="none">
          <p class="apply-lead">You have not applied yet.</p>
          <NextApplication status={s} first />
        </div>
      );
    case 'pending':
      return (
        <div class="apply-panel" data-status="pending">
          <p class="apply-lead">Your application is being reviewed.</p>
          {s.submitted_at !== null && <p>Sent {when(s.submitted_at)}.</p>}
          <p>Come back to this page to see the decision.</p>
          {s.can_withdraw && <Withdraw onDone={onChange} onStale={onStale} />}
        </div>
      );
    case 'approved':
      return (
        <div class="apply-panel" data-status="approved">
          <p class="apply-lead">Your application was approved.</p>
          <p>Sign in again to start reading and posting.</p>
          {s.sign_in && (
            <p>
              <a class="apply-button" href={s.sign_in}>
                Sign in
              </a>
            </p>
          )}
        </div>
      );
    case 'denied':
      return (
        <div class="apply-panel" data-status="denied">
          <p class="apply-lead">Your application was not approved.</p>
          {s.decided_at !== null && <p>Decided {when(s.decided_at)}.</p>}
          <NextApplication status={s} />
        </div>
      );
    case 'withdrawn':
      return (
        <div class="apply-panel" data-status="withdrawn">
          <p class="apply-lead">You withdrew your application.</p>
          {s.decided_at !== null && <p>Withdrawn {when(s.decided_at)}.</p>}
          <NextApplication status={s} />
        </div>
      );
    default:
      // A status this client does not know yet: say so rather than guess.
      return (
        <div class="apply-panel" data-status="unknown">
          <p class="apply-lead">Your application's status could not be shown.</p>
        </div>
      );
  }
}

// NextApplication is what the applicant may do next, as the server says:
// apply now, apply after a moment, or not at all.
function NextApplication({ status: s, first = false }: { status: ApplicationStatus; first?: boolean }) {
  if (s.can_apply) {
    return (
      <p>
        <a class="apply-button" href="/apply">
          {first ? 'Apply to join' : 'Apply again'}
        </a>
      </p>
    );
  }
  if (s.can_apply_at !== null) return <p>You can apply again after {when(s.can_apply_at)}.</p>;
  return <p>{notAccepting}</p>;
}

// Withdraw asks before it acts: the first press says what will happen, and
// only the second sends it.
function Withdraw({ onDone, onStale }: { onDone: (s: ApplicationStatus) => void; onStale: () => void }) {
  const [asking, setAsking] = useState(false);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState<string | null>(null);

  const confirm = async () => {
    setBusy(true);
    setFailed(null);
    try {
      onDone(await apiSend<ApplicationStatus>('POST', '/api/v1/apply/withdraw', {}));
    } catch (err) {
      if (isAbort(err)) return;
      const p = asProblem(err);
      // Nothing pending any more: decided, or withdrawn elsewhere. The
      // status says which.
      if (isProblem(p, 'conflict')) onStale();
      else setFailed(isProblem(p, 'rate-limited') ? again(p.retryAfter) : problemMessage(p).text);
    } finally {
      setBusy(false);
    }
  };

  if (!asking) {
    return (
      <p>
        <button type="button" class="apply-secondary" onClick={() => setAsking(true)}>
          Withdraw application
        </button>
      </p>
    );
  }
  return (
    <div class="apply-confirm" role="group" aria-label="Withdraw application">
      <p>Withdraw your application? The moderators will no longer see it.</p>
      <div class="apply-confirm-actions">
        <button type="button" class="apply-danger" disabled={busy} onClick={confirm}>
          Withdraw
        </button>
        <button
          type="button"
          class="apply-secondary"
          disabled={busy}
          onClick={() => {
            setFailed(null);
            setAsking(false);
          }}
        >
          Keep it
        </button>
      </div>
      {failed && (
        <p class="apply-failed" role="alert">
          {failed}
        </p>
      )}
    </div>
  );
}

// StatusRefusal is why there is no status to show. Signed out is what an
// approved applicant meets, so it points at signing in rather than at an
// error (TKT-01M3Q2V25E).
function StatusRefusal({ error, onRetry }: { error: ProblemError; onRetry: () => void }) {
  if (isProblem(error, 'unauthenticated')) {
    return (
      <div class="apply-panel" data-status="signed-out">
        <p class="apply-lead">You are not signed in.</p>
        <p>If you applied to join, sign in to see your decision.</p>
        <p>
          <a class="apply-button" href={signInURL}>
            Sign in
          </a>
        </p>
      </div>
    );
  }
  if (isProblem(error, 'forbidden')) {
    return (
      <div class="apply-panel">
        <p class="apply-lead">You are already a member, so there is no application to show.</p>
        <p>
          <a href="/">Go to the forums</a>
        </p>
      </div>
    );
  }
  return <ErrorPanel error={error} what="Your application" onRetry={onRetry} />;
}
