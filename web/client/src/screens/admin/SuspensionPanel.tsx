import { useEffect, useId, useRef, useState } from 'preact/hooks';
import type { AdminUserDetail, Me } from '../../api/dto';
import { isProblem } from '../../api/problem';
import { hasStatus } from '../../api/status';
import { viewer } from '../../state/viewer';
import { ConfirmButton } from './common/ConfirmButton';
import { Field } from './common/Field';
import { send, type Sent } from './common/send';

// SuspensionPanel is an account's suspension, on its detail in the users
// screen (TKT-01M3QMRE47, docs/07 "Users, groups and permissions"): the
// status line, and for a viewer who may act on the account, suspend,
// unsuspend, hide their posts and restore hidden posts.
//
// Every route answers 204 with no detail, and a failure can leave the
// account changed: a suspension commits before its hide, so a hide that
// fails leaves the account suspended. So after every answer, success or
// failure, the panel asks for the list again and awaits the refreshed
// detail, and chooses its words from that, never from the detail it
// rendered with (Terva reviews 1516, 1520 and 1523 on PR #181).
//
// The server decides who may act on whom (account.mayActOn, in the
// transaction). This panel only decides what to offer, by the same rule.

// reasonMax is account.ReasonMaxLen: a reason's length in code points, once
// trimmed.
export const reasonMax = 500;

const day = 24 * 60 * 60 * 1000;
type Ends = '1' | '7' | '30' | 'lifted' | 'date';
const presets: { value: Ends; label: string }[] = [
  { value: '1', label: '1 day' },
  { value: '7', label: '7 days' },
  { value: '30', label: '30 days' },
  { value: 'lifted', label: 'Until lifted' },
  { value: 'date', label: 'On a date' },
];

// mayAct mirrors account.mayActOn: never on your own account, an admin on
// anyone else, a moderator on members only. It reads the role alone: a
// member who moderates a forum through a group gets no controls, because
// account actions keep their role gates (docs/08 "Decided 2026-09-30").
// Nobody acts from here on a pending account, which suspension cannot
// start from, or an anonymized one.
export function mayAct(me: Me, d: AdminUserDetail): boolean {
  const v = me.user;
  const t = d.user.user;
  if (!v) return false;
  const handle = t.handle.toLowerCase();
  if (isStatus(d, 'pending') || handle.startsWith('deleted-user-')) return false;
  if (v.handle.toLowerCase() === handle) return false;
  if (v.role === 'admin') return true;
  return v.role === 'moderator' && t.role === 'member';
}

// isStatus is whether the account's status is s. It is an account's
// status, never a problem's: src/api/one-path.test.ts keeps those apart.
function isStatus(d: AdminUserDetail, s: string): boolean {
  const { status } = d.user;
  return status === s;
}

const pad = (n: number) => String(n).padStart(2, '0');

// localInput is ms as a datetime-local input's value, in local time.
function localInput(ms: number): string {
  const t = new Date(ms);
  return `${t.getFullYear()}-${pad(t.getMonth() + 1)}-${pad(t.getDate())}T${pad(t.getHours())}:${pad(t.getMinutes())}`;
}

// whenAt is a moment as the status line says it: the local date and time.
export function whenAt(ms: number): string {
  return localInput(ms).replace('T', ' ');
}

// reasonError is what is wrong with a reason, as the server would refuse
// it, or null.
export function reasonError(reason: string): string | null {
  const r = reason.trim();
  const n = [...r].length;
  if (n === 0) return 'Give a reason. The member sees it.';
  if (n > reasonMax) return `The reason is ${n} characters. Keep it to ${reasonMax}.`;
  if (/\p{Cc}/u.test(r)) return 'The reason cannot hold a tab or other control character.';
  return null;
}

type Message = { text: string; alert: boolean };

export function SuspensionPanel({
  d,
  url,
  refetch,
  onListChanged,
}: {
  d: AdminUserDetail;
  // url is the account's detail: /api/v1/admin/users/{handle}.
  url: string;
  // refetch fetches the detail again, shows it, and resolves to it, or to
  // null when the fetch failed.
  refetch: () => Promise<AdminUserDetail | null>;
  // onListChanged asks for the users list again.
  onListChanged: () => void;
}) {
  const u = d.user;
  const handle = u.user.handle;
  const status = u.status;
  const may = mayAct(viewer.value, d);
  const [message, setMessage] = useState<Message | null>(null);
  const [restore, setRestore] = useState(false);
  // One change at a time: the words for an answer are chosen from the
  // detail fetched after it, and a second change in flight would make that
  // detail say something about both.
  const [pending, setPending] = useState(false);
  const inFlight = useRef(false);
  const heading = useRef<HTMLHeadingElement>(null);

  // run sends one change and, whatever the answer, asks for the list again
  // and awaits the refreshed detail, then says what happened from both.
  // Resolving to false keeps the confirmation open.
  const run =
    (make: () => Promise<Sent<unknown>>, words: (r: Sent<unknown>, fresh: AdminUserDetail) => Message) =>
    async (): Promise<boolean> => {
      if (inFlight.current) {
        setMessage({ text: 'Another change to this account is still being made. Try again when it finishes.', alert: true });
        return false;
      }
      inFlight.current = true;
      setPending(true);
      setMessage(null);
      try {
        const r = await make();
        onListChanged();
        const fresh = await refetch();
        if (fresh === null) {
          const said = r.ok ? 'Done.' : r.message;
          setMessage({ text: `${said} Reload to see the account as it is now.`, alert: !r.ok });
        } else {
          setMessage(words(r, fresh));
        }
        if (r.ok) heading.current?.focus();
        return r.ok;
      } finally {
        inFlight.current = false;
        setPending(false);
      }
    };

  const fail = (text: string): Message => ({ text, alert: true });
  const done = (text: string): Message => ({ text, alert: false });
  // unfinished is an answer the step after the commit can give: the
  // server's own 503 (a thread it could not reach) or a 500. A proxy's 502 or
  // 504 is unavailable too, but says nothing about which step failed
  // (Terva review 1586, finding 2).
  const unfinished = (r: Sent<unknown>) => !r.ok && hasStatus(r.error, 500, 503);
  // unreached is the server's own 503 to a hide or a restore: some threads
  // could not be reached, after the rest were done.
  const unreached = (r: Sent<unknown>) => !r.ok && hasStatus(r.error, 503);

  const suspend = (body: { reason: string; until: number | null; hide_posts: boolean }) =>
    run(
      () => send('POST', `${url}/suspend`, body),
      (r, fresh) => {
        const now = isStatus(fresh, 'suspended');
        // A success is said as the refreshed account stands, which another
        // moderator may have changed since (Terva review 1586, finding 1).
        if (r.ok) {
          return now
            ? done(`@${handle} is suspended.`)
            : fail(`@${handle} was suspended, but is not suspended now. Someone else may have lifted it. What is shown now is current.`);
        }
        // The finish words need both signs: an answer the hide step can give,
        // and an account that went from not suspended to suspended. Neither
        // code is unique to the hide, and a conflict committed nothing.
        if (body.hide_posts && unfinished(r) && status !== 'suspended' && now) {
          return fail(
            `@${handle} is now suspended, but the request did not finish, so some posts may not be hidden. Press Hide their posts to finish.`,
          );
        }
        if (isProblem(r.error, 'conflict')) {
          return fail(
            now
              ? `@${handle} is already suspended, perhaps by someone else. What is shown now is current.`
              : 'This account cannot be suspended. It may be pending, anonymized, or the last active admin.',
          );
        }
        return fail(r.message);
      },
    )();

  const unsuspend = run(
    () => send('POST', `${url}/unsuspend`, { restore_posts: restore }),
    (r, fresh) => {
      if (r.ok) {
        setRestore(false);
        if (isStatus(fresh, 'suspended')) {
          return fail(`@${handle} was unsuspended, but is suspended again now, perhaps by someone else. What is shown now is current.`);
        }
        return done(
          restore
            ? `@${handle} is no longer suspended, and the posts this suspension hid are restored where you moderate.`
            : `@${handle} is no longer suspended.`,
        );
      }
      // The restore runs after the unsuspension commits, as the hide runs
      // after a suspension does, and can stop part-way the same way.
      if (restore && unfinished(r) && status === 'suspended' && !isStatus(fresh, 'suspended')) {
        return fail(
          `@${handle} is no longer suspended, but the request did not finish, so some posts may still be hidden. Press Restore hidden posts to finish.`,
        );
      }
      if (isProblem(r.error, 'conflict')) return fail('This account is no longer suspended.');
      return fail(r.message);
    },
  );

  const hide = run(
    () => send('POST', `${url}/posts/hide`),
    (r) => {
      if (r.ok) return done(`The visible posts of @${handle} are hidden, where you moderate.`);
      if (unreached(r)) {
        return fail('Some threads could not be reached. Press Hide their posts again to finish them.');
      }
      return fail(r.message);
    },
  );

  const restorePosts = run(
    () => send('POST', `${url}/posts/restore`),
    (r) => {
      if (r.ok) return done('Posts a suspension hid are restored, where you moderate.');
      if (unreached(r)) {
        return fail('Some threads could not be reached. Press Restore hidden posts again to finish them.');
      }
      return fail(r.message);
    },
  );

  // A viewer who may not act on an account that is not suspended has
  // nothing to see here.
  if (!may && !d.suspension) return null;
  const agent = u.user.kind === 'agent';
  return (
    <div class="admin-suspension" data-suspension-panel>
      <h3 ref={heading} tabIndex={-1}>
        Suspension
      </h3>
      {d.suspension && (
        <div data-suspension>
          <p>{d.suspension.until === null ? 'Suspended until lifted' : `Suspended until ${whenAt(d.suspension.until)}`}</p>
          <p class="admin-suspension-reason">Reason: {d.suspension.reason}</p>
        </div>
      )}
      {may && (
        <>
          {(status === 'active' || status === 'deactivated') && (
            <SuspendForm handle={handle} agent={agent} disabled={pending} onSuspend={suspend} />
          )}
          {status === 'suspended' && (
            <div class="admin-actions">
              <label class="admin-check">
                <input
                  type="checkbox"
                  checked={restore}
                  onChange={(e) => setRestore((e.target as HTMLInputElement).checked)}
                  data-restore-posts
                />
                Restore posts hidden with this suspension
              </label>
              <ConfirmButton
                label="Unsuspend"
                prompt={
                  `Unsuspend @${handle}?` +
                  (restore ? ' The posts this suspension hid are restored.' : '') +
                  (agent ? ' Its token stays revoked.' : '')
                }
                disabled={pending}
                onConfirm={unsuspend}
              />
            </div>
          )}
          <div class="admin-actions">
            {status === 'suspended' && (
              <ConfirmButton
                label="Hide their posts"
                danger
                prompt={`Hide every visible post by @${handle} in forums you moderate?`}
                disabled={pending}
                onConfirm={hide}
              />
            )}
            <ConfirmButton
              label="Restore hidden posts"
              prompt={`Restore the posts a suspension of @${handle} hid, in forums you moderate?`}
              disabled={pending}
              onConfirm={restorePosts}
            />
          </div>
        </>
      )}
      {message && (
        <p class={message.alert ? 'admin-error' : 'admin-muted'} role={message.alert ? 'alert' : 'status'} data-suspension-message>
          {message.text}
        </p>
      )}
    </div>
  );
}

// SuspendForm is the suspend form: the reason, when it ends, and whether to
// hide the posts, checked here as the server checks them, and sent only
// after the confirmation.
function SuspendForm({
  handle,
  agent,
  disabled,
  onSuspend,
}: {
  handle: string;
  agent: boolean;
  disabled: boolean;
  onSuspend: (body: { reason: string; until: number | null; hide_posts: boolean }) => Promise<boolean>;
}) {
  const [open, setOpen] = useState(false);
  const [reason, setReason] = useState('');
  const [ends, setEnds] = useState<Ends | null>(null);
  const [date, setDate] = useState('');
  const [hidePosts, setHidePosts] = useState(false);
  const [errors, setErrors] = useState<{ reason?: string; ends?: string; date?: string }>({});
  const id = useId();
  const opener = useRef<HTMLButtonElement>(null);
  const reasonInput = useRef<HTMLInputElement>(null);
  const wasOpen = useRef(false);
  useEffect(() => {
    if (open) reasonInput.current?.focus();
    else if (wasOpen.current) opener.current?.focus();
    wasOpen.current = open;
  }, [open]);

  // until is the chosen end in Unix ms, null for until lifted, or
  // undefined when there is none yet. A preset counts from now, the moment
  // it is asked for.
  const until = (): number | null | undefined => {
    if (ends === null) return undefined;
    if (ends === 'lifted') return null;
    if (ends === 'date') {
      const t = date ? new Date(date).getTime() : NaN;
      return Number.isNaN(t) ? undefined : t;
    }
    return Date.now() + Number(ends) * day;
  };

  // check says beside each field what the server would refuse, and
  // answers whether the form may be sent.
  const check = (): boolean => {
    const e: typeof errors = {};
    const r = reasonError(reason);
    if (r) e.reason = r;
    if (ends === null) e.ends = 'Choose when the suspension ends.';
    else if (ends === 'date') {
      const t = until();
      if (t === undefined) e.date = 'Choose the date and time it ends.';
      else if (t !== null && t <= Date.now()) e.date = 'Choose a moment in the future.';
    }
    setErrors(e);
    return Object.keys(e).length === 0;
  };

  const confirm = async () => {
    if (!check()) return false;
    const ok = await onSuspend({ reason: reason.trim(), until: until() ?? null, hide_posts: hidePosts });
    if (ok) {
      setReason('');
      setEnds(null);
      setDate('');
      setHidePosts(false);
      setOpen(false);
    }
    return ok;
  };

  if (!open) {
    return (
      <div class="admin-actions">
        <button type="button" class="admin-button danger" ref={opener} disabled={disabled} onClick={() => setOpen(true)}>
          Suspend
        </button>
      </div>
    );
  }

  const length = [...reason.trim()].length;
  const how =
    ends === 'lifted'
      ? 'until it is lifted'
      : ends === 'date'
        ? `until ${date.replace('T', ' ')}`
        : ends === '1'
          ? 'for 1 day'
          : `for ${ends} days`;
  const prompt =
    `Suspend @${handle} ${how}?` +
    (hidePosts ? ' Their posts are hidden too.' : '') +
    (agent ? ' Its token is revoked, and unsuspending does not restore it.' : '');
  return (
    <form class="admin-form admin-suspend" aria-label={`Suspend @${handle}`} onSubmit={(e) => e.preventDefault()} noValidate>
      <Field label="Reason" hint="The member sees this when they try to sign in." error={errors.reason}>
        {(c) => (
          <input
            {...c}
            ref={reasonInput}
            type="text"
            autocomplete="off"
            value={reason}
            onInput={(e) => setReason((e.target as HTMLInputElement).value)}
            data-reason
          />
        )}
      </Field>
      <p class="admin-hint" data-reason-count>
        {length} of {reasonMax} characters
      </p>
      <fieldset class="admin-ends" aria-describedby={errors.ends ? `${id}-ends` : undefined}>
        <legend>Ends</legend>
        {presets.map((p) => (
          <label key={p.value} class="admin-check">
            <input
              type="radio"
              name="ends"
              value={p.value}
              checked={ends === p.value}
              onChange={() => setEnds(p.value)}
              data-ends={p.value}
            />
            {p.label}
          </label>
        ))}
        {errors.ends && (
          <p class="admin-error" role="alert" id={`${id}-ends`}>
            {errors.ends}
          </p>
        )}
      </fieldset>
      {ends === 'date' && (
        <Field label="Ends at" error={errors.date}>
          {(c) => (
            <input
              {...c}
              type="datetime-local"
              min={localInput(Date.now())}
              value={date}
              onInput={(e) => setDate((e.target as HTMLInputElement).value)}
              data-until
            />
          )}
        </Field>
      )}
      <label class="admin-check">
        <input
          type="checkbox"
          checked={hidePosts}
          onChange={(e) => setHidePosts((e.target as HTMLInputElement).checked)}
          aria-describedby={`${id}-hide`}
          data-hide-posts
        />
        Hide their posts
      </label>
      <p class="admin-hint" id={`${id}-hide`}>
        Members see 'removed by a moderator'; you can restore them later.
      </p>
      <div class="admin-actions">
        <ConfirmButton label="Suspend" danger prompt={prompt} disabled={disabled} beforeAsk={check} onConfirm={confirm} />
        <button type="button" class="admin-button" onClick={() => setOpen(false)}>
          Cancel
        </button>
      </div>
    </form>
  );
}
