import { useState } from 'preact/hooks';
import { apiGet, apiSend } from '../../api/client';
import type { IdentityInfo, IdentityList, IdentityRemoved } from '../../api/dto';
import { isProblem, ProblemError } from '../../api/problem';
import { useResource } from '../../api/resource';
import { ErrorPanel } from '../../components/ErrorPanel';
import { PostTime } from '../../components/thread/PostTime';
import { LinkSignIn } from '../../shell/LinkSignIn';
import { Confirm, leaveHome, SettingsHead } from './settings';

export const identitiesUrl = '/api/v1/settings/identities';

// onlyWay is why an identity cannot be removed: the server refuses to take
// the last one on a provider configured now (docs/05 "Sessions and CSRF",
// decided by the run 2026-10-01, pending maintainer review).
export const onlyWay = 'This is your only way to sign in.';

const nameOf = (i: IdentityInfo) => (i.configured ? i.provider_name : `No longer configured (${i.provider})`);

// IdentitiesScreen is /settings/identities (TKT-01M29CXK4E): the sign-ins
// linked to the account, as GET /settings/identities answers, oldest first.
// Each has Remove, disabled and explained where the answer says it is not
// removable. Removing asks first. Removing the one this session came
// through ends this session too, and the answer's signed_out then loads /
// in full; otherwise the list is asked for again. A conflict, the last
// usable sign-in, says why and refreshes. Adding one is Link a sign-in, the
// server's page, which comes back here. Nothing asks for a recent sign-in
// (docs/05, decided by the maintainer 2026-10-01).
export function IdentitiesScreen() {
  const res = useResource(identitiesUrl, (signal) => apiGet<IdentityList>(identitiesUrl, { signal }));
  const [ask, setAsk] = useState<IdentityInfo | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const remove = async (i: IdentityInfo) => {
    try {
      const r = await apiSend<IdentityRemoved>('DELETE', `${identitiesUrl}/${i.id}`, undefined, { status: 200 });
      if (r?.signed_out === true) return leaveHome();
      if (r?.signed_out !== false) throw new ProblemError('internal', 200);
    } catch (err) {
      if (isProblem(err, 'conflict')) setNotice(onlyWay);
      else if (!isProblem(err, 'not-found')) throw err;
    }
    setAsk(null);
    res.retry();
  };

  const list = res.data?.identities;
  return (
    <main class="account" data-screen="settings-identities">
      <SettingsHead title="Sign-in identities" />
      {res.error && !res.data && <ErrorPanel error={res.error} what="Your sign-in identities" onRetry={res.retry} />}
      {notice && (
        <p class="settings-error" role="alert">
          {notice}
        </p>
      )}
      {list && (
        <section class="account-card" aria-labelledby="identities-heading">
          <h2 id="identities-heading" class="settings-title">
            Ways you sign in
          </h2>
          <ul class="settings-rows">
            {list.map((i) => (
              <li key={i.id} class="settings-row" data-identity={i.id}>
                <div class="settings-what">
                  <p class="settings-name">
                    {nameOf(i)}
                    {i.current && <span class="role-badge">Used for this session</span>}
                  </p>
                  {i.email && <p class="settings-meta">{i.email}</p>}
                  <p class="settings-meta">
                    Linked <PostTime at={i.created_at} /> · Last sign-in{' '}
                    {i.last_login_at === null ? 'never' : <PostTime at={i.last_login_at} />}
                  </p>
                  {!i.removable && (
                    <p class="settings-hint" id={`identity-${i.id}-why`} data-not-removable>
                      {onlyWay}
                    </p>
                  )}
                </div>
                <button
                  type="button"
                  class="sign-out"
                  disabled={!i.removable}
                  aria-label={`Remove ${nameOf(i)}`}
                  aria-describedby={i.removable ? undefined : `identity-${i.id}-why`}
                  onClick={() => {
                    setNotice(null);
                    setAsk(i);
                  }}
                >
                  Remove
                </button>
              </li>
            ))}
          </ul>
          <LinkSignIn />
        </section>
      )}
      {ask && (
        <Confirm title="Remove this sign-in?" action="Remove" onClose={() => setAsk(null)} run={() => remove(ask)}>
          {nameOf(ask)} will no longer sign you in to this account.
          {ask.current && ' You signed in with it this time, so this browser will be signed out too.'}
        </Confirm>
      )}
    </main>
  );
}
