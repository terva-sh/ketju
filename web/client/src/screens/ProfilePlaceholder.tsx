import { useRoute } from 'preact-iso';
import { LinkSignIn } from '../shell/LinkSignIn';
import { sameHandle } from '../shell/nav';
import { viewer } from '../state/viewer';
import { Placeholder } from './Placeholder';

// ProfilePlaceholder is /u/{handle} until a ticket builds the profile. On
// the viewer's own profile it adds an Account block, where the account's
// actions are until the settings screens (TKT-01M29CXK4) exist: the top
// bar's handle and the phone's Me tab both come here, so the block is one
// press from every screen (decided 2026-09-29, TKT-01M3QS0TMV).
// TKT-01M3QVWKAG puts Sign out in the same block.
export function ProfilePlaceholder() {
  const { params } = useRoute();
  const me = viewer.value.user;
  const own = !!me && !!params.handle && sameHandle(params.handle, me.handle);
  return (
    <Placeholder screen="profile" title="Profile">
      {own && (
        <section data-account aria-labelledby="account-heading">
          <h2 id="account-heading">Account</h2>
          <LinkSignIn />
        </section>
      )}
    </Placeholder>
  );
}
