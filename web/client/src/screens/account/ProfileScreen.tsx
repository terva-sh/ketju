import { useRoute } from 'preact-iso';
import { isProblem } from '../../api/problem';
import { ErrorPanel } from '../../components/ErrorPanel';
import { profileHref, sameHandle } from '../../shell/nav';
import { useShellContext } from '../../state/shell';
import { viewer } from '../../state/viewer';
import { NotFound } from '../Placeholder';
import { AccountBlock, displayName, Joined, ProfileHead, useProfile } from './profile';

// ProfileScreen is /u/{handle} (TKT-01M3V3CFMA): a member as GET
// /users/{handle} answers for this viewer. The handle, display name, role
// badge, bio, join date and post count, and the status when the server sends
// it, which it does only to moderators and admins. The server decides who
// sees which profile (docs/05 "Whose profile is served"): an unknown handle,
// or a pending account to a member, is not-found, and a signed-out reader is
// unauthenticated and told to sign in (ErrorPanel).
//
// The bio is rendered and sanitized by the server as a post body is
// (internal/render), and drawn as one.
//
// The viewer's own profile carries the Account block, drawn from the viewer
// rather than the answer, so it is there while the profile loads.
export function ProfileScreen() {
  const { params } = useRoute();
  const handle = params.handle;
  const res = useProfile(handle);
  const p = res.data;
  const me = viewer.value.user;
  const own = !!me && sameHandle(handle, me.handle);
  const name = displayName(p, handle);
  useShellContext({ title: name, actions: [] }, [name]);

  if (isProblem(res.error, 'not-found')) return <NotFound />;

  return (
    <main class="account" data-screen="profile">
      <div class="account-card">
        <ProfileHead handle={handle} profile={p} />
        {res.error && !p && <ErrorPanel error={res.error} what="This profile" onRetry={res.retry} />}
        {p && p.bio_html && <div class="post-body profile-bio" dangerouslySetInnerHTML={{ __html: p.bio_html }} />}
        {p && (
          <dl class="profile-facts">
            <div>
              <dt>Joined</dt>
              <dd>
                <Joined at={p.joined_at} />
              </dd>
            </div>
            <div>
              <dt>Posts</dt>
              <dd data-post-count>{p.post_count}</dd>
            </div>
          </dl>
        )}
        {p && (
          <a class="profile-posts" href={`${profileHref(p.user.handle)}/posts`} data-profile-posts>
            View posts
          </a>
        )}
      </div>
      {own && <AccountBlock />}
    </main>
  );
}
