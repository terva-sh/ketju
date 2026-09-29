import { useEffect, useRef, useState } from 'preact/hooks';
import { useLocation } from 'preact-iso';
import { apiGet, apiSend } from '../../api/client';
import type { ApplicationForm, ApplicationStatus, Form, FormField } from '../../api/dto';
import { asProblem, isAbort, isProblem, problemMessage, type ProblemError } from '../../api/problem';
import { ErrorPanel } from '../../components/ErrorPanel';
import { kindOf, type Value } from '../../forms/fields';
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

// changedSince is a refusal of answers the applicant has already changed.
export const changedSince = 'Your application was not sent, and you have changed the answers it was refused over. Send it again.';

export function ApplyScreen() {
  const { route } = useLocation();
  const [data, setData] = useState<ApplicationForm | null>(null);
  const [loadError, setLoadError] = useState<ProblemError | null>(null);
  const [attempt, setAttempt] = useState(0);
  const [values, setValues] = useState<Values>({});
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [refusals, setRefusals] = useState(0);
  const [message, setMessage] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  useShellContext({ title: 'Apply to join', actions: [] }, []);

  // take puts a loaded form on screen. previous is the version the server
  // has since replaced, with the answers given to it: each answer is carried
  // over only where it still fits (carried, below).
  const take = (f: ApplicationForm, previous?: { form: Form; values: Values }) => {
    const fresh = initialValues(f.form, f.prefill);
    if (previous) {
      for (const field of f.form.fields) {
        const v = carried(previous, field);
        if (v !== undefined && Object.hasOwn(fresh, field.key)) fresh[field.key] = v;
      }
    }
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

  // latest is the answers as they are now. The form stays editable while a
  // submission is in flight, so what a refusal describes, the answers sent,
  // can differ from these by the time it arrives.
  const latest = useRef(values);
  latest.current = values;

  const submit = async () => {
    if (!data) return;
    setBusy(true);
    setMessage(null);
    const sent = answersOf(data.form, values);
    try {
      await apiSend<ApplicationStatus>('POST', '/api/v1/apply', { version: data.form.version, answers: sent });
      route('/apply/status', true);
      return;
    } catch (err) {
      if (isAbort(err)) return;
      await refused(asProblem(err), sent);
    } finally {
      setBusy(false);
    }
  };

  // refused says what a refused submission means. Field errors describe
  // only the submission that drew them, so any other answer clears them
  // (review 1456), and so does an answer changed while it was in flight
  // (review 1460).
  const refused = async (p: ProblemError, sent: Values) => {
    if (isProblem(p, 'validation-failed')) {
      const still = stillSent(p.fieldErrors, sent, latest.current);
      setErrors(still);
      setRefusals((n) => n + 1);
      // With a field to mark, the line saying so is drawn from the errors
      // and goes with the last. With none, the refusal must still be said
      // (review 1460): the body named no field, or every field it named
      // has changed since.
      if (Object.keys(still).length > 0) setMessage(null);
      else if (Object.keys(p.fieldErrors).length > 0) setMessage(changedSince);
      else setMessage(problemMessage(p).text);
      return;
    }
    setErrors({});
    if (isProblem(p, 'conflict')) {
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
  // loaded. The status tells them apart (review 1453). The server checks
  // every other refusal before the version, so a form change leaves the
  // applicant free to apply; anything else is the status page's to explain.
  const conflicted = async () => {
    try {
      const s = await apiGet<ApplicationStatus>(statusURL);
      if (isPending(s) || !s.can_apply) {
        route('/apply/status', true);
        return;
      }
      const f = await apiGet<ApplicationForm>(formURL);
      take(f, data ? { form: data.form, values: latest.current } : undefined);
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
          refusals={refusals}
          provider={viewer.value.pre_user?.provider}
          busy={busy}
          submitLabel="Send application"
          message={message ?? (Object.keys(errors).length > 0 ? 'Some answers need another look. Each one is marked.' : null)}
          onChange={(k, v) => {
            setValues((vs) => ({ ...vs, [k]: v }));
            // A changed answer is no longer the one refused.
            setErrors((es) => (Object.hasOwn(es, k) ? withoutKey(es, k) : es));
          }}
          onSubmit={submit}
        />
      )}
    </main>
  );
}

// carried is the answer given to the previous version's field of the same
// key, if it still fits the new field, or undefined (review 1453). It fits
// when the field kept its kind in fields.tsx's map and whether it takes
// several choices, and, for a field with options, when every choice made is
// still one of them. Otherwise the new field starts empty: a line of text
// kept in a box would be sent as text while the box showed unticked.
export function carried(previous: { form: Form; values: Values }, field: FormField): Value | undefined {
  const was = previous.form.fields.find((f) => f.key === field.key);
  if (!was || !Object.hasOwn(previous.values, field.key)) return undefined;
  const kind = kindOf(field);
  if (!kind || kindOf(was) !== kind || was.multiple !== field.multiple) return undefined;
  const v = previous.values[field.key];
  if (field.options.length > 0) {
    const chosen = Array.isArray(v) ? v : [v];
    if (!chosen.every((c) => c === '' || field.options.includes(c as string))) return undefined;
  }
  return v;
}

// stillSent is the field errors that still describe the form: those for a
// field whose answer is the one that was sent. An error for a key that was
// not sent, a locked field or the schema as a whole, stands.
export function stillSent(errors: Record<string, string>, sent: Values, now: Values): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [k, code] of Object.entries(errors)) {
    if (!Object.hasOwn(sent, k) || JSON.stringify(sent[k]) === JSON.stringify(now[k])) out[k] = code;
  }
  return out;
}

// withoutKey is o less its entry for key.
function withoutKey(o: Record<string, string>, key: string): Record<string, string> {
  const out = { ...o };
  delete out[key];
  return out;
}

// isPending is an application under review.
const isPending = ({ status }: ApplicationStatus) => status === 'pending';

// LoadRefusal is why the form is not shown: the refusals an applicant can
// meet before filling it in, and any failure to reach it.
function LoadRefusal({ error, onRetry }: { error: ProblemError; onRetry: () => void }) {
  // A member is refused the form too (docs/07), and is not an applicant a
  // closed registration turned away.
  if (isProblem(error, 'forbidden') && viewer.value.user) {
    return (
      <div class="apply-panel">
        <p class="apply-lead">You are already a member, so there is nothing to apply for.</p>
        <p>
          <a href="/">Go to the forums</a>
        </p>
      </div>
    );
  }
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
