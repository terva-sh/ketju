// SignOutForm is the one Sign out control, for the applicant's bar and the
// member's own profile (TKT-01M3QVWKAG). It is a plain form post, not a
// fetch: the server's cross-origin check (internal/web/csrf.go) holds it to
// this site, and POST /auth/logout revokes this browser's session, clears the
// cookie and answers 303 to /. The browser loads that page, which drops
// everything the client held for the viewer who left. class is the button's.
export function SignOutForm({ class: cls }: { class?: string }) {
  return (
    <form method="post" action="/auth/logout">
      <button type="submit" class={cls}>
        Sign out
      </button>
    </form>
  );
}
