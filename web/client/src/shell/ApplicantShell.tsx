import { useEffect, useMemo } from 'preact/hooks';
import { Route, Router, useLocation } from 'preact-iso';
import { focusMainHeading, routeFocuser } from '../components/focus';
import { routes } from '../routes';
import './shell.css';

// ApplicantShell is everything an approval-mode applicant sees: /me says
// pre_user, and their session reaches the application form, the status
// page and sign-out, and nothing else (docs/05 "mode = approval" step 1,
// TKT-01M29CKRG9). The server refuses every other route, so there is no
// forum navigation to offer: the bar has the wordmark and "Sign out", the
// same at every width.
//
// Any address but the two is replaced with /apply/status (TKT-01M29CP2NG),
// so the address says what the page is. The server's allowlist is the
// enforcement; this only spares the applicant a screen whose every request
// would be refused.

// home is the address an applicant at path belongs at.
const home = (path: string) => (path === '/apply' ? '/apply' : '/apply/status');

// The two screens are the route table's own entries.
const screen = (path: string) => routes.find((r) => r.path === path)!.component;

export function ApplicantShell() {
  const { path, route } = useLocation();
  // Focus follows the applicant from the form to the status page, as it
  // does between the member shell's screens. It starts from where the
  // address is going, so the replacement below is not a move.
  const onRouteChange = useMemo(() => routeFocuser(home(location.pathname)), []);
  useEffect(() => {
    if (path !== home(path)) route(home(path), true);
  }, [path]);
  return (
    <div class="shell applicant" data-layout="applicant">
      <a
        class="skip-link"
        href="#main"
        onClick={(e) => {
          e.preventDefault();
          focusMainHeading();
        }}
      >
        Skip to content
      </a>
      <header class="topbar applicant" data-shell="applicant-bar">
        <span class="wordmark">Ketju</span>
        {/* A plain form post: the server's cross-origin check holds it to
            this site, and sign-out answers with a redirect home. */}
        <form method="post" action="/auth/logout">
          <button type="submit" class="sign-out">
            Sign out
          </button>
        </form>
      </header>
      <div class="shell-content" id="main">
        <Router onRouteChange={onRouteChange}>
          <Route path="/apply" component={screen('/apply')} />
          <Route default component={screen('/apply/status')} />
        </Router>
      </div>
    </div>
  );
}
