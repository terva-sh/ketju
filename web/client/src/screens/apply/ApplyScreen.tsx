import { useEffect, useState } from 'preact/hooks';
import { useLocation } from 'preact-iso';
import { apiGet, apiSend } from '../../api/client';
import type { ApplicationForm, ApplicationStatus } from '../../api/dto';
import { asProblem, isAbort, isProblem, problemMessage, type ProblemError } from '../../api/problem';
import { ErrorPanel } from '../../components/ErrorPanel';
import { answersOf, FormView, initialValues, type Values } from '../../forms/FormView';
import { useShellContext } from '../../state/shell';
import { viewer } from '../../state/viewer';
import { again } from './when';

// ApplyScreen is /apply: the active registration form, drawn from its schema
// (docs/05 "mode = approval" step 2), and the submission. Every refusal the
// API gives (docs/07 /apply) reads as what the applicant can do next.

const formURL = '/api/v1/apply/form';
const statusURL = '/api/v1/apply/status';

// notAccepting is the one thing a 403 says. A blocked identity and a site
// that is not in approval mode read the same, as the API gives no reason
// either (docs/07).
export const notAccepting = 'This community is not accepting applications right now.';

export function ApplyScreen() {
  const { route } = useLocation();
  const [data, setData] = useState<ApplicationForm | null>(null);
  const [loadError, setLoadError] = useState<ProblemError | null>(null);
  const [attempt, setAttempt] = useState(0);
  const [values, setValues] = useState<Values>({});
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [message, setMessage] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  useShellContext({ title: 'Apply to join', actions: [] }, []);

  // take puts a loaded form on screen. kept are answers from a version the
  // server has since replaced: each is carried over where the new version
  // still has a field of that key that takes an answer.
  const take = (f: ApplicationForm, kept?: Values) => {
    const fresh = initialValues(f.form, f.prefill);
    if (kept) for (const k of Object.keys(fresh)) if (Object.hasOwn(kept, k)) fresh[k] = kept[k];
    setData(f);
    setValues(fresh);
    setErrors({});
  };

  useEffect(() => {
    const ctl = new AbortController();
    setLoadError(null);
    apiGet<ApplicationForm>(formURL, { signal: ctl.signal }).then(
      (f) => take(f),
      (err) => {
        if (ctl.signal.aborted || isAbort(err)) return;
        const p = asProblem(err);
        // An application under review, or an account already made: the
        // status page says which.
        if (isProblem(p, 'conflict')) route('/apply/status', true);
        else setLoadError(p);
      },
    );
    return () => ctl.abort();
  }, [attempt]);

  const submit = async () => {
    if (!data) return;
    setBusy(true);
    setMessage(null);
    try {
      await apiSend<ApplicationStatus>('POST', '/api/v1/apply', {
        version: data.form.version,
        answers: answersOf(data.form, values),
      });
      route('/apply/status', true);
      return;
    } catch (err) {
      if (isAbort(err)) return;
      await refused(asProblem(err));
    } finally {
      setBusy(false);
    }
  };

  const refused = async (p: ProblemError) => {
    if (isProblem(p, 'validation-failed')) {
      setErrors({ ...p.fieldErrors });
      setMessage('Some answers need another look. Each one is marked.');
    } else if (isProblem(p, 'conflict')) {
      await conflicted();
    } else if (isProblem(p, 'forbidden')) {
      setMessage(notAccepting);
    } else if (isProblem(p, 'rate-limited')) {
      setMessage(`Your application was not sent. ${again(p.retryAfter)}`);
    } else {
      setMessage(problemMessage(p).text);
    }
  };

  // A 409 is one of two things (docs/07): an application already pending,
  // perhaps sent from another tab, or a form that changed since it was
  // loaded. The status tells them apart.
  const conflicted = async () => {
    try {
      const s = await apiGet<ApplicationStatus>(statusURL);
      // The one status a submission conflicts with; the status page shows it.
      if (s.can_withdraw) {
        route('/apply/status', true);
        return;
      }
      const f = await apiGet<ApplicationForm>(formURL);
      take(f, values);
      setMessage('The form changed while you were filling it in. Your answers are kept where the questions are the same. Check them and send it again.');
    } catch (err) {
      setMessage(problemMessage(err).text);
    }
  };

  return (
    <main class="apply" data-screen="apply">
      <h1>{data?.form.title || 'Apply to join'}</h1>
      {loadError ? (
        <LoadRefusal error={loadError} onRetry={() => setAttempt((n) => n + 1)} />
      ) : !data ? (
        <p class="apply-loading" aria-live="polite">
          Loading the form…
        </p>
      ) : (
        <FormView
          form={data.form}
          mode="fill"
          prefill={data.prefill}
          values={values}
          errors={errors}
          provider={viewer.value.pre_user?.provider}
          busy={busy}
          submitLabel="Send application"
          message={message}
          onChange={(k, v) => setValues((vs) => ({ ...vs, [k]: v }))}
          onSubmit={submit}
        />
      )}
    </main>
  );
}

// LoadRefusal is why the form is not shown: the refusals an applicant can
// meet before filling it in, and any failure to reach it.
function LoadRefusal({ error, onRetry }: { error: ProblemError; onRetry: () => void }) {
  if (isProblem(error, 'forbidden')) return <p class="apply-lead">{notAccepting}</p>;
  if (isProblem(error, 'rate-limited')) {
    return (
      <div class="apply-panel">
        <p class="apply-lead">You applied recently.</p>
        <p>{again(error.retryAfter)}</p>
        <p>
          <a href="/apply/status">See your application</a>
        </p>
      </div>
    );
  }
  return <ErrorPanel error={error} what="The form" onRetry={isProblem(error, 'unauthenticated') ? undefined : onRetry} />;
}
