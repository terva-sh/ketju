import { useEffect } from 'preact/hooks';
import { useLocation } from 'preact-iso';
import { apiGet } from '../../api/client';
import type { Profile } from '../../api/dto';
import { useResource } from '../../api/resource';
import { Avatar } from '../../components/thread/Avatar';
import { RoleBadge } from '../../components/thread/RoleBadge';
import { LinkSignIn } from '../../shell/LinkSignIn';
import { profileHref, sameHandle } from '../../shell/nav';
import { SignOutForm } from '../../shell/SignOut';
import { viewer } from '../../state/viewer';

// What the account screens share: a member's profile, fetched once per
// handle, its heading, and the viewer's own Account block.

export const profileUrl = (handle: string) => `/api/v1/users/${encodeURIComponent(handle)}`;

// useProfile is handle's profile, as GET /users/{handle} answers it for this
// viewer (docs/07). The server decides everything about who sees what
// (docs/05 "Whose profile is served"); the screens draw what it sends.
//
// A handle a rename released answers with a 302, which the fetch follows, so
// the profile arrives under the account's handle now. The address is then
// replaced with that handle, rest (what follows the handle, /posts) and the
// query kept (docs/05 "Changing a handle"). Case alone is not a rename: the
// server matches handles without it.
export function useProfile(handle: string, rest = '') {
  const key = profileUrl(handle);
  const res = useResource(key, (signal) => apiGet<Profile>(key, { signal }));
  const { route } = useLocation();
  const now = res.data?.user.handle;
  useEffect(() => {
    if (now && !sameHandle(now, handle)) route(profileHref(now) + rest + location.search, true);
  }, [now, handle]);
  return res;
}

// displayName is how a profile names its account: the display name, or the
// handle without one, as a post header does. An anonymized account has none.
export const displayName = (p: Profile | undefined, handle: string) => (p ? p.user.display_name || p.user.handle : handle);

const statusWords: Record<string, string> = {
  pending: 'Pending',
  suspended: 'Suspended',
  deactivated: 'Deactivated',
};

// ProfileHead is a profile's heading: the avatar, the name, the handle, the
// role badge, and the account's status when the server sends one. It sends
// one only to moderators and admins; a status it leaves null is never
// guessed at, and an active account carries no badge. Until the profile
// arrives the heading is the handle in the address.
export function ProfileHead({ handle, profile }: { handle: string; profile?: Profile }) {
  const u = profile?.user;
  const status = profile?.status;
  return (
    <header class="profile-head">
      {u && <Avatar author={u} />}
      <div>
        <h1>{displayName(profile, handle)}</h1>
        {u && (
          <p class="profile-handle">
            <span>@{u.handle}</span>
            <RoleBadge author={u} />
            {status && status !== 'active' && (
              <span class="role-badge profile-status" data-status={status}>
                {statusWords[status] ?? status}
              </span>
            )}
          </p>
        )}
      </div>
    </header>
  );
}

// Joined is the day an account joined, in the viewer's timezone and locale.
export function Joined({ at }: { at: number }) {
  const { timezone, locale } = viewer.value.settings;
  const d = new Date(at);
  return (
    <time dateTime={d.toISOString()}>
      {d.toLocaleDateString(locale || undefined, { year: 'numeric', month: 'long', day: 'numeric', timeZone: timezone || undefined })}
    </time>
  );
}

// AccountBlock is on the viewer's own profile: the account's actions, one
// press from every screen, since the top bar's handle and the phone's Me tab
// both come to the profile (decided 2026-09-29, TKT-01M3QS0TMV): Settings
// (TKT-01M29CXK4E), "Link a sign-in", and Sign out (TKT-01M3QVWKAG, for
// this browser only).
export function AccountBlock() {
  return (
    <section class="account-block" data-account aria-labelledby="account-heading">
      <h2 id="account-heading">Account</h2>
      <p>
        Timezone, page size, theme, and where you are signed in.{' '}
        <a class="hit" href="/settings" data-nav="settings">
          Settings
        </a>
      </p>
      <LinkSignIn />
      <SignOutForm class="sign-out" />
    </section>
  );
}
