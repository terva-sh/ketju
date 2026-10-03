import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks';
import { apiGet, apiSend } from '../../api/client';
import type { HandleChange, HandleState } from '../../api/dto';
import { asProblem, isProblem } from '../../api/problem';
import { useResource } from '../../api/resource';
import { ErrorPanel } from '../../components/ErrorPanel';
import { PostTime } from '../../components/thread/PostTime';
import { handleMessage } from '../../handles';
import { renameViewer } from '../../state/rename';
import { refreshViewer } from '../../state/viewer';
import { Confirm, leaveHome, SettingsHead } from './settings';

export const handleUrl = '/api/v1/settings/handle';
export const deactivateUrl = '/api/v1/settings/deactivate';

const lastAdmin = 'You are the only active admin. Make someone else an admin first.';
const tooSoon = 'You can change your handle once every 30 days.';

// maxDelay is the longest setTimeout waits, 2^31 - 1 ms (about 24.8 days);
// a longer delay fires at once.
export const maxDelay = 2 ** 31 - 1;

// AccountScreen is /settings/account (TKT-01M3V6XTYG; docs/05 "Changing a
// handle", decided by the run 2026-10-02): the member changes their handle,
// and deactivates their account. GET /settings/handle says whether the 30
// days since their last change are over, so the cooldown shows before they
// type. Each action asks first. Neither asks for a recent sign-in (docs/05
// "Sessions and CSRF", decided by the maintainer 2026-10-02).
//
// A change takes the new handle into the viewer at once, so the top bar and
// Back link show it, and asks /me again. This tab's viewer guard does not
// reload it: the guard says who it is once, as the page starts, and a /me
// it asks for itself is the page's to take (state/viewerGuard.ts).
// Deactivating ends every session, this one included, so it loads / in full,
// as signing out does.
export function AccountScreen() {
  const res = useResource(handleUrl, (signal) => apiGet<HandleState>(handleUrl, { signal }));
  // changed is what a change, or a refusal inside the cooldown, said since
  // the answer came: newer than it.
  const [changed, setChanged] = useState<HandleState | null>(null);
  const [value, setValue] = useState('');
  const [error, setError] = useState('');
  const [done, setDone] = useState('');
  const [ask, setAsk] = useState<{ handle: string } | 'deactivate' | null>(null);
  // settled moves focus once the dialog has closed on an outcome and given
  // focus back to its opener: to the field, or, while the cooldown disables
  // it and the button, to what the change or the refusal said.
  const [settled, setSettled] = useState(0);
  const field = useRef<HTMLInputElement>(null);
  const said = useRef<HTMLParagraphElement>(null);
  const announced = useRef<HTMLParagraphElement>(null);
  useLayoutEffect(() => {
    if (!settled) return;
    if (field.current && !field.current.disabled) field.current.focus();
    else (error ? said : announced).current?.focus();
  }, [settled]);

  const state = changed ?? res.data;
  const next = state?.next_change_at ?? null;
  const until = next !== null && next > Date.now() ? next : null;
  // The screen draws again when the cooldown ends, so a page left open
  // across the date opens the field without a reload (Terva review 2009).
  // A timer cannot wait more than maxDelay, so a longer wait draws again
  // then and sets another.
  const [woken, wake] = useState(0);
  useEffect(() => {
    if (until === null) return;
    const t = setTimeout(() => wake((n) => n + 1), Math.min(until - Date.now(), maxDelay));
    return () => clearTimeout(t);
  }, [until, woken]);

  // refuse closes the dialog and shows why beside the field.
  const refuse = (why: string) => {
    setAsk(null);
    setError(why);
    setSettled((n) => n + 1);
  };

  const open = (e: Event) => {
    e.preventDefault();
    const handle = value.trim().toLowerCase();
    setDone('');
    if (!handle) return refuse(handleMessage('invalid_handle', true));
    setError('');
    setAsk({ handle });
  };

  const change = async (to: string) => {
    try {
      const r = await apiSend<HandleChange>('PUT', handleUrl, { handle: to }, { status: 200 });
      renameViewer(r.handle);
      refreshViewer().catch(() => {});
      setChanged({ handle: r.handle, next_change_at: r.next_change_at });
      setValue('');
      setError('');
      setAsk(null);
      setDone(`Your handle is now @${r.handle}.`);
      setSettled((n) => n + 1);
    } catch (err) {
      const p = asProblem(err);
      const code = p.fieldErrors.handle;
      if (isProblem(p, 'validation-failed') && code) return refuse(handleMessage(code, true));
      if (isProblem(p, 'conflict') && p.availableAt !== null) {
        if (state) setChanged({ handle: state.handle, next_change_at: p.availableAt });
        return refuse(tooSoon);
      }
      throw err;
    }
  };

  return (
    <main class="account" data-screen="settings-account">
      <SettingsHead title="Account" />
      {res.error && !state && <ErrorPanel error={res.error} what="Your handle" onRetry={res.retry} />}
      {state && (
        <form class="account-card settings-form" aria-labelledby="handle-heading" onSubmit={open} data-handle-card>
          <h2 id="handle-heading" class="settings-title">
            Handle
          </h2>
          <p class="settings-name" data-current-handle>
            @{state.handle}
          </p>
          <div class="settings-field">
            <label for="account-handle">New handle</label>
            <input
              id="account-handle"
              ref={field}
              type="text"
              value={value}
              disabled={until !== null}
              autocomplete="off"
              autocapitalize="none"
              spellcheck={false}
              aria-describedby="account-handle-hint account-handle-error"
              aria-invalid={!!error}
              onInput={(e) => setValue(e.currentTarget.value)}
            />
            <p id="account-handle-hint" class="settings-hint" data-cooldown={until !== null || undefined}>
              {until !== null ? (
                <>
                  You can change your handle again from <PostTime at={until} />.
                </>
              ) : (
                'Once every 30 days. Your old handle sends people to your new one for 90 days, and nobody, you included, can take it in that time.'
              )}
            </p>
            <p id="account-handle-error" ref={said} tabIndex={-1} class="settings-error" data-field-error="handle">
              {error}
            </p>
          </div>
          <button type="submit" class="sign-out settings-all" disabled={until !== null}>
            Change handle
          </button>
          <p ref={announced} tabIndex={-1} class="settings-saved" role="status">
            {done}
          </p>
        </form>
      )}
      {state && (
        <section class="account-card" aria-labelledby="deactivate-heading" data-deactivate-card>
          <h2 id="deactivate-heading" class="settings-title">
            Deactivate your account
          </h2>
          <p class="settings-meta">
            You are signed out everywhere. Your posts stay, under your handle. To come back, sign in and press Reactivate.
          </p>
          <button type="button" class="sign-out settings-all" onClick={() => setAsk('deactivate')} data-deactivate>
            Deactivate account
          </button>
        </section>
      )}
      {ask && ask !== 'deactivate' && (
        <Confirm
          title={`Change your handle to @${ask.handle}?`}
          action="Change handle"
          onClose={() => setAsk(null)}
          run={() => change(ask.handle)}
        >
          You can change it again in 30 days. For 90 days your old handle, @{state?.handle}, sends people to your new
          one, and nobody, you included, can take it.
        </Confirm>
      )}
      {ask === 'deactivate' && (
        <Confirm
          title="Deactivate your account?"
          action="Deactivate account"
          onClose={() => setAsk(null)}
          explain={(err) => (isProblem(err, 'conflict') ? lastAdmin : undefined)}
          run={async () => {
            await apiSend('POST', deactivateUrl, undefined, { status: 204 });
            leaveHome();
          }}
        >
          Every browser and device signed in to this account is signed out, this one too. Your posts stay, under your
          handle. To come back, sign in and press Reactivate.
        </Confirm>
      )}
    </main>
  );
}
