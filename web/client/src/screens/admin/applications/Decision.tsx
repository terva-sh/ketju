import { useId, useState } from 'preact/hooks';
import { apiGet } from '../../../api/client';
import type { AdminApplication } from '../../../api/dto';
import { isAbort, isProblem, type ProblemError } from '../../../api/problem';
import { fieldMessage } from '../../../forms/messages';
import { ConfirmButton } from '../common/ConfirmButton';
import { Field } from '../common/Field';
import { send } from '../common/send';
import { hasStatus } from './ApplicationList';

// Decision is the approve and deny controls for one pending application
// (docs/05 "mode = approval" step 5, docs/07 "Applications"). The
// applications screen shows it on an application's detail, and the
// moderation queue (TKT-01M29CRJ4E) imports it for an application item, so
// the two never drift apart.
//
// Approve gives the applicant a handle, the suggested one to start with.
// Deny needs a note, which is for the other moderators: the applicant never
// sees it (maintainer decision 2026-09-29), and the field says so. Either
// answer, and any refusal that means the application changed under the
// moderator, reaches the caller through onChange with the application as it
// now is and a sentence saying what happened.

export const noteMax = 2000;

// The approval's own handle codes (docs/07 "Applications"). They are not
// form field codes, because a handle is no form's field.
const handleWords: Record<string, string> = {
  invalid_handle:
    'A handle is 3 to 30 characters: lowercase letters, digits, _ and -. It starts with a letter or digit and does not end with -.',
  reserved: 'That handle is kept for the site itself. Choose another.',
  taken: 'That handle is in use, or was given up too recently to reuse. Choose another.',
};

export function handleMessage(code: string): string {
  return Object.hasOwn(handleWords, code) ? handleWords[code] : 'That handle was not accepted. Choose another.';
}

// A note's codes are form field codes, said for a note, which may hold line
// breaks and has its own limit.
const noteWords: Record<string, string> = {
  required: 'Write a note.',
  too_long: `Keep the note to ${noteMax} characters or fewer.`,
  invalid_text: 'The note holds a character that is not allowed.',
};

export function noteMessage(code: string): string {
  return Object.hasOwn(noteWords, code) ? noteWords[code] : fieldMessage(code);
}

// decidedSentence says who took the application out of pending, for a
// moderator whose own decision came too late.
export function decidedSentence(a: AdminApplication): string {
  if (hasStatus(a, 'withdrawn')) return 'The applicant withdrew this application.';
  const by = a.decision?.by ? `@${a.decision.by.handle}` : 'another moderator';
  return `Already ${a.status} by ${by}.`;
}

export const moderatorsOnly = 'Moderators only. The applicant never sees this note.';

type Action = 'approve' | 'deny';

export function Decision({
  app,
  url,
  onChange,
}: {
  app: AdminApplication;
  // The application's address, /api/v1/admin/applications/{id}.
  url: string;
  onChange: (a: AdminApplication, notice: string) => void;
}) {
  // The ids are this instance's, since the queue shows several at once.
  const id = useId();
  const [handle, setHandle] = useState(app.suggested_handle);
  const [approveNote, setApproveNote] = useState('');
  const [denyNote, setDenyNote] = useState('');
  const [block, setBlock] = useState(false);
  const [errors, setErrors] = useState<Partial<Record<'handle' | 'approveNote' | 'denyNote', string>>>({});
  const [failed, setFailed] = useState<{ action: Action; message: string } | null>(null);

  // refused handles a refusal: a field error goes beside its field, a
  // conflict re-fetches the application and says why, and anything else is
  // said beside the action. It answers false, which keeps the confirmation
  // open, unless the application has moved on.
  const refused = async (action: Action, error: ProblemError, message: string): Promise<boolean> => {
    const fields = error.fieldErrors;
    if (isProblem(error, 'validation-failed') && (fields.handle || fields.note)) {
      setErrors(
        action === 'approve'
          ? { handle: fields.handle && handleMessage(fields.handle), approveNote: fields.note && noteMessage(fields.note) }
          : { denyNote: fields.note && noteMessage(fields.note) },
      );
      return false;
    }
    if (isProblem(error, 'forbidden')) {
      setFailed({ action, message: 'Your account may no longer review applications.' });
      return false;
    }
    if (!isProblem(error, 'conflict')) {
      setFailed({ action, message });
      return false;
    }
    let now: AdminApplication;
    try {
      now = await apiGet<AdminApplication>(url);
    } catch (err) {
      if (isAbort(err)) throw err;
      setFailed({ action, message: 'This application changed, and it could not be loaded again. Reload the page.' });
      return false;
    }
    if (!hasStatus(now, 'pending')) {
      onChange(now, decidedSentence(now));
      return true;
    }
    setFailed({
      action,
      message:
        action === 'approve'
          ? 'This identity can no longer be approved: it was removed, already has an account, or is blocked. Deny it instead.'
          : 'This identity can no longer be blocked. Deny without blocking.',
    });
    onChange(now, '');
    return false;
  };

  const approve = async () => {
    setErrors({});
    setFailed(null);
    const body: { handle: string; note?: string } = { handle: handle.trim() };
    if (approveNote.trim()) body.note = approveNote;
    const r = await send<AdminApplication>('POST', `${url}/approve`, body);
    if (!r.ok) return refused('approve', r.error, r.message);
    const who = r.value.decision?.user?.handle ?? body.handle;
    onChange(r.value, `Approved as @${who}. They can sign in now.`);
  };

  const deny = async () => {
    setErrors({});
    setFailed(null);
    const r = await send<AdminApplication>('POST', `${url}/deny`, { note: denyNote, block });
    if (!r.ok) return refused('deny', r.error, r.message);
    onChange(r.value, block ? 'Denied, and the identity is blocked from applying again.' : 'Denied.');
  };

  const failure = (action: Action) =>
    failed?.action === action && (
      <p class="admin-error" role="alert">
        {failed.message}
      </p>
    );

  return (
    <div class="app-decision" data-decision>
      <section class="app-decide" aria-labelledby={`${id}-approve`}>
        <h3 id={`${id}-approve`}>Approve</h3>
        <Field
          label="Handle"
          hint="Suggested from their sign-in. You can change it before approving."
          error={errors.handle}
        >
          {(c) => (
            <input
              {...c}
              type="text"
              autocomplete="off"
              autocapitalize="none"
              spellcheck={false}
              maxLength={30}
              value={handle}
              onInput={(e) => setHandle((e.target as HTMLInputElement).value)}
              data-handle
            />
          )}
        </Field>
        <Field label="Note (optional)" hint={moderatorsOnly} error={errors.approveNote}>
          {(c) => (
            <textarea
              {...c}
              rows={2}
              maxLength={noteMax}
              value={approveNote}
              onInput={(e) => setApproveNote((e.target as HTMLTextAreaElement).value)}
            />
          )}
        </Field>
        <ConfirmButton
          label="Approve"
          prompt={`Approve as @${handle.trim()}? This makes them a member.`}
          confirmLabel="Approve"
          disabled={handle.trim() === ''}
          onConfirm={approve}
        />
        {failure('approve')}
      </section>

      <section class="app-decide" aria-labelledby={`${id}-deny`}>
        <h3 id={`${id}-deny`}>Deny</h3>
        <Field label="Note (required)" hint={moderatorsOnly} error={errors.denyNote}>
          {(c) => (
            <textarea
              {...c}
              rows={3}
              maxLength={noteMax}
              value={denyNote}
              onInput={(e) => setDenyNote((e.target as HTMLTextAreaElement).value)}
              data-deny-note
            />
          )}
        </Field>
        <div class="app-check">
          <input
            id={`${id}-block`}
            type="checkbox"
            checked={block}
            onChange={(e) => setBlock((e.target as HTMLInputElement).checked)}
            aria-describedby={`${id}-block-hint`}
          />
          <label for={`${id}-block`}>Block this subject</label>
          <p id={`${id}-block-hint`} class="admin-hint">
            They cannot apply again with this sign-in until a moderator lifts the block.
          </p>
        </div>
        <ConfirmButton
          label="Deny"
          prompt={block ? 'Deny, and block them from applying again?' : 'Deny this application?'}
          confirmLabel="Deny"
          danger
          disabled={denyNote.trim() === ''}
          onConfirm={deny}
        />
        {denyNote.trim() === '' && <p class="admin-hint">Write a note to deny.</p>}
        {failure('deny')}
      </section>
    </div>
  );
}
