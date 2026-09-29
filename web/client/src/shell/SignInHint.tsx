import { useLocation } from 'preact-iso';
import { meHref } from './nav';

// SignInHint is the signed-out shell's line for an applicant whose
// application was decided (TKT-01M3Q2V25E, docs/05 "mode = approval" step
// 4). Approval retires the pre-user session, so the approved applicant
// arrives here signed out, and learns the decision by signing in as the
// member they now are. The link is the same login page Sign in reaches.
//
// It comes back to the page the reader is on, except from the application
// flow: signed in as a member, /apply and /apply/status refuse them, so it
// comes back to the forum index instead, as the status page's own sign-in
// link does (review 1462).
//
// /me does not say which registration mode the site is in, so every
// signed-out reader sees the line; it reads correctly in any mode.
export function SignInHint() {
  const { path, url } = useLocation();
  const inFlow = path === '/apply' || path.startsWith('/apply/');
  return (
    <p class="signin-hint" data-shell="signin-hint">
      Applied?{' '}
      <a class="hit" href={meHref(inFlow ? '/' : url)}>
        Sign in to see your decision
      </a>
    </p>
  );
}
