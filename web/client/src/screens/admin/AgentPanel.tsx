import { useEffect, useRef, useState } from 'preact/hooks';
import { apiGet } from '../../api/client';
import type { AdminUserDetail, AgentInfo, IssuedCredential } from '../../api/dto';
import { isProblem } from '../../api/problem';
import { when } from '../apply/when';
import { ConfirmButton } from './common/ConfirmButton';
import { Field } from './common/Field';
import { send } from './common/send';

// AgentPanel is an agent account's credential and write limits, on its
// detail in the users screen (TKT-01M3QH3GS2, docs/07 "Agents on the API").
// It is an admin's: the detail's agent is null for a moderator and for a
// human, and UserDetail renders this only when it is not.
//
// It holds the only copy of a token the client ever has. A token arrives in
// the answer to an issue, or in the creation answer, which UsersTab hands to
// this agent's detail once as issued. Either way it is kept here, in this
// component's state, and nowhere else: no signal, no storage, no address, no
// log. It goes when Done is pressed, when the token is revoked, and when the
// detail unmounts, which Close, choosing another account and leaving the
// screen all do (Terva review 1488, finding 1).
export function AgentPanel({
  d,
  url,
  onDetail,
  issued,
  onIssuedTaken,
}: {
  d: AdminUserDetail & { agent: AgentInfo };
  // url is the account's detail: /api/v1/admin/users/{handle}.
  url: string;
  // onDetail shows a newer detail in place of d.
  onDetail: (d: AdminUserDetail) => void;
  // issued is the creation answer's token, handed over once. It is read on
  // mount only, and onIssuedTaken tells the holder to drop its copy.
  issued: IssuedCredential | null;
  onIssuedTaken: () => void;
}) {
  const handle = d.user.user.handle;
  const agent = d.agent;
  const [token, setToken] = useState<IssuedCredential | null>(() => issued);
  const [failed, setFailed] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [issuing, setIssuing] = useState(false);
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    if (issued) onIssuedTaken();
    // Mount only: issued is a handover, not a value to follow.
  }, []);

  // version orders what this panel shows: a refetch answers only if nothing
  // newer was shown while it was in flight.
  const version = useRef(0);
  const show = (next: AdminUserDetail) => {
    version.current++;
    onDetail(next);
  };
  const refresh = async () => {
    const mine = ++version.current;
    try {
      const next = await apiGet<AdminUserDetail>(url);
      if (mine === version.current) onDetail(next);
    } catch {
      // The change was made and is already shown; a failed refetch leaves
      // that in place rather than replacing it with an error.
    }
  };
  const withCredential = (credential: AgentInfo['credential']): AdminUserDetail => ({
    ...d,
    agent: { ...agent, credential },
  });

  const issue = async () => {
    const r = await send<IssuedCredential>('POST', `${url}/credential`);
    if (!r.ok) {
      setFailed(isProblem(r.error, 'conflict') ? 'Only an active agent can be given a token.' : r.message);
      return false;
    }
    setFailed(null);
    setNotice(null);
    setToken(r.value);
    show(withCredential({ created_at: r.value.created_at, last_used_at: null }));
    void refresh();
  };
  const issueFirst = async () => {
    setIssuing(true);
    try {
      await issue();
    } finally {
      setIssuing(false);
    }
  };
  const revoke = async () => {
    const r = await send('DELETE', `${url}/credential`);
    // not-found is another admin having revoked it first: the answer the
    // admin wanted, already true.
    const gone = !r.ok && isProblem(r.error, 'not-found');
    if (!r.ok && !gone) {
      setFailed(r.message);
      return false;
    }
    setFailed(null);
    setToken(null);
    setNotice(gone ? 'This agent has no live token.' : null);
    show(withCredential(null));
    heading.current?.focus();
    void refresh();
  };
  const done = () => {
    setToken(null);
    heading.current?.focus();
  };

  const live = agent.credential;
  return (
    <div class="admin-agent" data-agent-panel>
      <h3 ref={heading} tabIndex={-1}>
        Token
      </h3>
      {live ? (
        <dl class="admin-facts" data-credential>
          <dt>Issued</dt>
          <dd data-issued>{when(live.created_at)}</dd>
          <dt>Last used</dt>
          <dd data-last-used>{live.last_used_at === null ? 'never' : when(live.last_used_at)}</dd>
        </dl>
      ) : (
        <p class="admin-muted" data-no-credential>
          No live token.
        </p>
      )}
      {token && <TokenPanel handle={handle} token={token} onDone={done} />}
      <div class="admin-actions">
        {live ? (
          <>
            <ConfirmButton
              label="Issue new token"
              prompt={`Issue a new token for ${handle}? The current token stops working at once.`}
              onConfirm={issue}
            />
            <ConfirmButton
              label="Revoke"
              danger
              prompt={`Revoke ${handle}'s token? It stops working at once.`}
              onConfirm={revoke}
            />
          </>
        ) : (
          <button type="button" class="admin-button" disabled={issuing} onClick={issueFirst}>
            Issue token
          </button>
        )}
      </div>
      {notice && (
        <p class="admin-muted" role="status">
          {notice}
        </p>
      )}
      {failed && (
        <p class="admin-error" role="alert">
          {failed}
        </p>
      )}
      <Limits d={d} url={url} onSaved={show} />
    </div>
  );
}

// TokenPanel shows a token the once it can be shown. The token is the value
// of a read-only input, so it can be selected by hand when the clipboard is
// refused, and it is not written into the page's markup.
function TokenPanel({ handle, token, onDone }: { handle: string; token: IssuedCredential; onDone: () => void }) {
  const input = useRef<HTMLInputElement>(null);
  const [copied, setCopied] = useState<'no' | 'yes' | 'refused'>('no');
  useEffect(() => {
    setCopied('no');
    input.current?.focus();
    input.current?.select();
  }, [token]);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(token.token);
      setCopied('yes');
    } catch {
      setCopied('refused');
      input.current?.focus();
      input.current?.select();
    }
  };
  return (
    <div class="admin-token" role="status" data-token-panel>
      <p class="admin-token-warning">This token is shown once. Store it now.</p>
      <Field label={`New token for ${handle}`}>
        {(c) => (
          <input
            {...c}
            ref={input}
            class="admin-token-value"
            type="text"
            readOnly
            value={token.token}
            autocomplete="off"
            autoCapitalize="off"
            spellcheck={false}
          />
        )}
      </Field>
      <div class="admin-actions">
        <button type="button" class="admin-button" onClick={copy}>
          Copy
        </button>
        <button type="button" class="admin-button" onClick={onDone}>
          Done
        </button>
        {copied === 'yes' && <span data-copied>Copied</span>}
        {copied === 'refused' && <span data-copied>Select the token and copy it</span>}
      </div>
    </div>
  );
}

// Limits is the agent's write bucket: how many writes at once, and how many
// seconds earn one back (docs/08 "Rate limits"). An empty field is the site
// default, which the placeholder shows, and is sent as null.
//
// The fields are text with a numeric keyboard, not type=number, and whole
// holds them to the server's bounds before anything is sent. A number input
// reports what it cannot parse, "1e" or "12-", as an empty value, which this
// form would send as null and so reset the limit to the default unasked.
const refillMax = 604800; // settings.AgentRefillMaxSeconds

function Limits({ d, url, onSaved }: { d: AdminUserDetail & { agent: AgentInfo }; url: string; onSaved: (d: AdminUserDetail) => void }) {
  const a = d.agent;
  const [burst, setBurst] = useState(a.burst === null ? '' : String(a.burst));
  const [refill, setRefill] = useState(a.refill_seconds === null ? '' : String(a.refill_seconds));
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  const save = async (e: Event) => {
    e.preventDefault();
    const b = whole(burst, 1, Number.MAX_SAFE_INTEGER);
    const r = whole(refill, 1, refillMax);
    if (b === undefined || r === undefined) {
      setSaved(false);
      setFailed(`Enter whole numbers: a burst of at least 1, and a refill from 1 to ${refillMax} seconds. Or leave a field empty.`);
      return;
    }
    setBusy(true);
    setSaved(false);
    try {
      const res = await send<AdminUserDetail>('PUT', `${url}/agent-limits`, { burst: b, refill_seconds: r });
      if (!res.ok) {
        setFailed(
          isProblem(res.error, 'validation-failed')
            ? `The server refused those limits: a burst of at least 1, and a refill from 1 to ${refillMax} seconds.`
            : res.message,
        );
        return;
      }
      setFailed(null);
      setSaved(true);
      onSaved(res.value);
    } finally {
      setBusy(false);
    }
  };

  return (
    <form class="admin-limits" onSubmit={save} aria-label="Write limits">
      <h3>Write limits</h3>
      <div class="admin-inline">
        <Field label="Burst" hint={`Writes allowed at once. Empty uses the site default, ${a.default_burst}.`}>
          {(c) => (
            <input
              {...c}
              type="text"
              inputMode="numeric"
              autocomplete="off"
              placeholder={String(a.default_burst)}
              value={burst}
              onInput={(e) => setBurst((e.target as HTMLInputElement).value)}
              data-burst
            />
          )}
        </Field>
        <Field
          label="Refill seconds"
          hint={`Seconds to earn one write back. Empty uses the site default, ${a.default_refill_seconds}.`}
        >
          {(c) => (
            <input
              {...c}
              type="text"
              inputMode="numeric"
              autocomplete="off"
              placeholder={String(a.default_refill_seconds)}
              value={refill}
              onInput={(e) => setRefill((e.target as HTMLInputElement).value)}
              data-refill
            />
          )}
        </Field>
        <button type="submit" class="admin-button" disabled={busy}>
          Save limits
        </button>
      </div>
      {saved && (
        <p class="admin-muted" role="status">
          Saved.
        </p>
      )}
      {failed && (
        <p class="admin-error" role="alert">
          {failed}
        </p>
      )}
    </form>
  );
}

// whole reads a limit field: null when empty, the number when it is a whole
// number from min to max, and undefined when it is neither.
function whole(s: string, min: number, max: number): number | null | undefined {
  const t = s.trim();
  if (t === '') return null;
  if (!/^[0-9]+$/.test(t)) return undefined;
  const n = Number(t);
  return n >= min && n <= max ? n : undefined;
}
