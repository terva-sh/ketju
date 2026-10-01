import { useRef, useState } from 'preact/hooks';
import { apiGet, apiSend } from '../../api/client';
import type { SessionInfo, SessionList } from '../../api/dto';
import { isProblem, problemMessage } from '../../api/problem';
import { useResource } from '../../api/resource';
import { ErrorPanel } from '../../components/ErrorPanel';
import { PostTime } from '../../components/thread/PostTime';
import { Confirm, leaveHome, SettingsHead } from './settings';

export const sessionsUrl = '/api/v1/settings/sessions';

// SessionsScreen is /settings/sessions (TKT-01M29CXK4E): every browser the
// member is signed in on, as GET /settings/sessions answers, most recently
// used first, this one marked. Signing another out removes its row in place.
// Signing this one out, or every one, asks first, and then loads / in full,
// because this browser is signed out too (docs/05 "Sessions and CSRF",
// decided by the run 2026-10-01, pending maintainer review). Neither asks
// for a recent sign-in (docs/05, decided by the maintainer 2026-10-01).
export function SessionsScreen() {
  const res = useResource(sessionsUrl, (signal) => apiGet<SessionList>(sessionsUrl, { signal }));
  // ended are the sessions this screen has signed out since the list came.
  const [ended, setEnded] = useState<string[]>([]);
  const [ask, setAsk] = useState<SessionInfo | 'all' | null>(null);
  const [refused, setRefused] = useState<{ id: string; text: string } | null>(null);
  const busy = useRef(new Set<string>());

  const end = (s: SessionInfo) => apiSend('DELETE', `${sessionsUrl}/${encodeURIComponent(s.id)}`, undefined, { status: 204 });

  // signOutOther ends another browser's session and drops its row. One
  // already gone, signed out elsewhere meanwhile, refreshes the list.
  const signOutOther = async (s: SessionInfo) => {
    if (busy.current.has(s.id)) return;
    busy.current.add(s.id);
    setRefused(null);
    try {
      await end(s);
      setEnded((e) => [...e, s.id]);
    } catch (err) {
      if (isProblem(err, 'not-found')) res.retry();
      else setRefused({ id: s.id, text: problemMessage(err).text });
    } finally {
      busy.current.delete(s.id);
    }
  };

  const list = res.data?.sessions.filter((s) => !ended.includes(s.id));
  return (
    <main class="account" data-screen="settings-sessions">
      <SettingsHead title="Sessions" />
      {res.error && !res.data && <ErrorPanel error={res.error} what="Your sessions" onRetry={res.retry} />}
      {list && (
        <section class="account-card" aria-labelledby="sessions-heading">
          <h2 id="sessions-heading" class="settings-title">
            Where you are signed in
          </h2>
          <ul class="settings-rows">
            {list.map((s) => (
              <li key={s.id} class="settings-row" data-session={s.id} data-current={s.current || undefined}>
                <div class="settings-what">
                  <p class="settings-name">
                    {s.device}
                    {s.current && <span class="role-badge">This device</span>}
                  </p>
                  <p class="settings-meta">
                    {s.ip}
                    {s.provider && ` · signed in with ${s.provider}`}
                  </p>
                  <p class="settings-meta">
                    Signed in <PostTime at={s.created_at} /> · Last used <PostTime at={s.last_used_at} />
                  </p>
                  {refused?.id === s.id && (
                    <p class="settings-error" role="alert">
                      {refused.text}
                    </p>
                  )}
                </div>
                <button
                  type="button"
                  class="sign-out"
                  aria-label={s.current ? 'Sign out this device' : `Sign out ${s.device}`}
                  onClick={() => (s.current ? setAsk(s) : void signOutOther(s))}
                >
                  Sign out
                </button>
              </li>
            ))}
          </ul>
          <button type="button" class="sign-out settings-all" onClick={() => setAsk('all')} data-everywhere>
            Sign out everywhere
          </button>
        </section>
      )}
      {ask === 'all' && (
        <Confirm
          title="Sign out everywhere?"
          action="Sign out everywhere"
          onClose={() => setAsk(null)}
          run={async () => {
            await apiSend('DELETE', sessionsUrl, undefined, { status: 204 });
            leaveHome();
          }}
        >
          Every browser and device signed in to this account is signed out, this browser too. You will need to sign in
          again.
        </Confirm>
      )}
      {ask && ask !== 'all' && (
        <Confirm
          title="Sign out this device?"
          action="Sign out"
          onClose={() => setAsk(null)}
          run={async () => {
            await end(ask);
            leaveHome();
          }}
        >
          This browser is signed out. Your other sessions stay signed in.
        </Confirm>
      )}
    </main>
  );
}
