import { focusMainHeading } from '../components/focus';
import { routes } from '../routes';
import './shell.css';

// ApplicantShell is everything an approval-mode applicant sees: /me says
// pre_user, and their session reaches the application form, the status
// page and sign-out, and nothing else (docs/05 "mode = approval" step 1,
// TKT-01M29CKRG9). The server refuses every other route, so there is no
// forum navigation to offer: the bar has the wordmark and "Sign out", the
// same at every width, and every address but /apply is the status page.
//
// The two screens are the route table's own entries, so the ticket that
// builds them (TKT-01M29CP2NG) changes routes.tsx and nothing here.
export function ApplicantShell({ path }: { path: string }) {
  const Screen = screenFor(path);
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
        <Screen />
      </div>
    </div>
  );
}

// screenFor is the application form at /apply and the status page anywhere
// else.
function screenFor(path: string) {
  const want = path === '/apply' ? '/apply' : '/apply/status';
  return routes.find((r) => r.path === want)!.component;
}
