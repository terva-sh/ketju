import { useLocation } from 'preact-iso';
import { viewer } from '../state/viewer';
import { linkSignInHref } from './nav';

// LinkSignIn is the way to the server's "link a sign-in" page, for an
// active member only. /me gives a user only for a member's session: a
// signed-out reader and a pre-user both have none, and a pre-user is drawn
// by ApplicantShell, not here. The server's page checks again and sends
// anyone else to sign in. A plain anchor, so the browser leaves the client
// for the server's page.
export function LinkSignIn() {
  const { url } = useLocation();
  if (!viewer.value.user) return null;
  return (
    <p>
      Add another way to sign in to this account.{' '}
      <a class="hit" href={linkSignInHref(url)} data-nav="link-sign-in">
        Link a sign-in
      </a>
    </p>
  );
}
