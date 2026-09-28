# 05 — Identity, Sessions, and Accounts

## Principles

- OIDC is the primary and default way in. Local passwords are a feature flag.
- Members are not all human. An **agent** is a member with `kind = 'agent'`
  (04): its own handle, its own standing, its own credential. An admin
  provisions each one, and agents never self-register and never use OIDC.
- A **user** is a local entity with a handle. **Identities** (IdP subjects)
  attach to users. Nothing user-facing is keyed on an IdP subject or email.
- Registration policy is a site setting with three modes:
  `open`, `approval`, `invite` (invite is a later phase; approval is v1).
- Admin bootstrap never requires a password: the first user to complete
  login with an identity listed in `bootstrap.admin_subjects` (each written
  `"provider_key:subject"`, since a bare subject is ambiguous once there are
  two providers) or with an email listed in `bootstrap.admin_emails` becomes
  admin. Alternatively `ketju admin promote <handle>` from the CLI.
  - An email matches only when the provider asserts `email_verified = true`,
    compared on the whole address without regard to case. An unverified
    address never matches, and the sign-in logs a warning saying so.
  - "First login" means any sign-in where the identity matches and the user
    is not already an admin, so a member who existed before the config named
    them is promoted at their next sign-in. The promotion is written to
    `audit_log` as `user.promoted`, and that row makes it happen once: a
    bootstrap admin who is later demoted stays demoted at later sign-ins. A
    matching user who is already an admin with no such row gets one, marked
    `"already": "admin"`, for the same reason.
  - `ketju admin promote [--root DIR] <handle>` needs only the data
    directory, as `ketju fsck` does. It writes the same audit row with no
    actor, and may run while `serve` does.

## Provider configuration

Registration policy is **not** in this file. `mode` and
`allow_local_passwords` are things an admin changes while the server is
running, so they live in the `settings` table (04) and are changed in the
admin UI. Writing them into `config.toml` is refused at startup, with a
message naming the settings table, because a runtime setting that also
exists in a file works right up to the day an admin changes it in the UI
and the next restart silently puts it back.

```toml
[[oidc.providers]]
key = "google"               # stable key stored in identities.provider_key
name = "Google"
issuer = "https://accounts.google.com"
client_id = "…"
client_secret_file = "/run/secrets/google"   # or client_secret = "…"
scopes = ["openid", "email", "profile"]
allowed_email_domains = ["example.org"]      # optional gate
auto_approve = false          # if true, bypass approval mode for this provider

[[oidc.providers]]
key = "authelia"
name = "Club SSO"
issuer = "https://auth.club.example"
client_id = "ketju"
client_secret_file = "/run/secrets/authelia"
groups_claim = "groups"       # optional: map an IdP groups claim …
group_map = { "forum-mods" = "moderators" }   # … onto local groups on each login

[bootstrap]
admin_emails = ["me@example.org"]    # only when the provider asserts email_verified
admin_subjects = ["authelia:8f2c61d4-5b7e-4f0a-9c3e-2d1b0a9e8f7c"]
```

Providers are an allowlist by construction: only configured issuers are ever
contacted, and the login page only shows configured providers. Discovery
documents and JWKS are fetched at startup and refreshed on key-rotation
failures; a provider that fails discovery at startup is disabled with a
loud log line rather than aborting the server. A disabled provider is retried
on each 24-hour refresh, so an IdP that was down during a deploy comes back on
its own rather than needing a restart. If *every* provider fails, startup does
abort: a forum nobody can sign in to should say so rather than serve an empty
login page.

An issuer must be `https`, with no per-provider escape hatch. Discovery over
plaintext is unauthenticated, so whoever can rewrite the response chooses the
key set, and every signature check afterwards passes against keys they minted.
This is deliberately stricter than `insecure_skip_issuer_check`, which only
declines to compare two issuer strings in a document that still arrived over
TLS.

Key-set refreshes provoked by an unrecognised key id are rate limited, because
the key id is a field in a token supplied by whoever called the callback
endpoint. Without a floor, a stream of cheap forged tokens turns this forum into
the thing hammering someone else's identity provider. Key ids that have verified
a token are remembered and are never rate limited, so a real rotation costs one
fetch and nothing afterwards.

## Login flow

1. `GET /auth/{provider}/start` — generate `state`, `nonce`, PKCE verifier;
   store them in a short-lived, encrypted, `SameSite=Lax` cookie; redirect.
2. `GET /auth/{provider}/callback` — verify state, exchange code with PKCE,
   verify ID token (issuer, audience, nonce, signature, expiry). Extract
   `sub`, `email`, `email_verified`, `name`, `picture`, optional groups claim.
3. Look up `identities (provider_key, sub)`.
   - **Found** → load user; if `status = suspended|deactivated` show the
     reason and stop; else create session, apply `group_map`, redirect to
     the `return_to` captured at start (validated as a same-origin path).
   - **Not found, and the user is already signed in** → this is an account
     link: attach the identity to the current user, done.
   - **Not found, signed out** → registration path (below).
4. Session cookie: `__Host-ketju_session`, `HttpOnly`, `Secure`, `SameSite=Lax`,
   value is the random session id; the server stores its hash.

The pre-login state in step 1 stays in the cookie, sealed with authenticated
encryption so it is both secret and tamper-evident, and it expires with the
login attempt. An anonymous server-side session row was the alternative, and
it lost: every abandoned login would cost a database write, and anyone could
fill a table by requesting `/start` in a loop. (Decided 2026-09-24, 10 Q8,
TKT-01M29BSPEF.)

The cookie is `__Host-ketju_login`. It lasts ten minutes and is cleared by the
callback whatever the outcome, so it serves one sign-in. It is sealed with
AES-256-GCM under a 32-byte key at `secrets/cookie.key` in the data directory,
or at `cookie_key_file` in `config.toml`. The server creates the key, mode
0600, the first time it starts without one. The key must then persist: a new
one ends every sign-in in progress, though it signs nobody out. A stateless
cookie cannot make the nonce strictly one-time. Replay is bounded instead by
the ten minutes, by the cookie being cleared on first use, and by the IdP's
single-use codes.

A suspended, deactivated or pending account that signs in gets an "account
unavailable" page and no session. The reason and any end date come with the
Phase 2 moderation columns. Sign-out is `POST /auth/logout`, never a GET, so a
page elsewhere cannot sign a reader out with an image tag. (TKT-01M29C7ZZ9.)

Email is never used to auto-link identities across providers. Two providers
asserting the same email are two identities until the user links them while
signed in. This closes the classic "attacker controls an IdP that asserts
victim's email" hole.

## Registration path

### `mode = "open"`
Show a one-field "choose a handle" screen (prefilled from `preferred_username`
or the email local part, checked for uniqueness), create user with
`status = active`, create identity, create session.

### `mode = "approval"`
1. Create the identity row with `user_id = NULL` and a **pending** user is
   *not* created yet. Instead the visitor is placed in a "pre-user" session
   bound to the identity id, which may only reach the application form,
   the application status page, and logout.
2. Render the active version of the `registration` form (see Forms below).
   Fields the form marks `from_claims` are prefilled (name, email) and may be
   locked.
3. Submitting creates an `applications` row (`status = pending`) and notifies
   everyone in `moderators`+`admins` (notification kind `new_application`,
   optionally email later).
4. Status page: "Your application is being reviewed." Re-visiting the site
   with that identity shows the same page; a pending applicant can withdraw
   and re-apply once per 24 h to avoid spam.
5. Moderator reviews at `/admin/applications`: sees answers, the identity's
   provider/email/verified flag, prior applications from the same identity,
   and free-text notes from other moderators. Actions: **approve** (choose or
   confirm handle; creates the `users` row `status = active`, links the
   identity, notifies applicant), **deny** (with note; optional "block this
   subject" which keeps the identity row with `user_id = NULL` and a
   `blocked` marker so re-applications are refused), **request changes**
   (later phase).
6. Approval is recorded in `audit_log`.

Providers with `auto_approve = true` (e.g. the organization's own SSO) skip
steps 2–5 and behave as `open` for that provider only.

### Forms

A minimal declarative form system, used first for registration and reusable
later for reports, polls-lite, or event RSVPs. Schema is JSON stored in
`form_versions.schema_json`:

```json
{
  "title": "Apply to join",
  "intro_markdown": "Tell us a bit about yourself…",
  "fields": [
    {"key": "name",  "type": "text", "label": "Name", "from_claim": "name", "required": true},
    {"key": "email", "type": "email", "label": "Email", "from_claim": "email", "locked": true},
    {"key": "who",   "type": "textarea", "label": "Who invited you or how did you find us?", "required": true, "max": 1000},
    {"key": "rules", "type": "checkbox", "label": "I have read the guidelines", "required": true},
    {"key": "interests", "type": "select", "label": "Interests", "options": ["Games", "Books", "Hiking"], "multiple": true}
  ]
}
```

Field types in v1: `text`, `textarea`, `email`, `checkbox`, `select`,
`markdown_notice` (display only). Validation runs server-side from the schema;
the Preact client renders the same schema. A non-JS client renders it with
`html/template` trivially — this is one reason the form is data, not code.
Admins edit the form in a small UI that writes a new version; the old version
stays for old applications.

## Sessions and CSRF

The session store is hand-rolled, about 200 lines, rather than
`alexedwards/scs/v2`. Three reasons. It gives exact control over how the id is
hashed at rest. Revocation is immediate, because deleting a row is the whole
operation. And it models a session with no user yet, the pre-user session
approval mode needs (below). `scs` assumes a session belongs to a user, and a
library that cannot express that is one we would fight. Its middleware
ergonomics were the case for it, and the chain in `internal/web` already
provides them. (Decided 2026-09-24, 10 Q7, TKT-01M29BSPEF.)

- Server-side sessions (table `sessions`), 30-day idle expiry, 90-day hard
  expiry, sliding `last_used_at` updated at most once per 5 minutes.
- Session list in user settings with "sign out this device / everywhere".
- Suspending a user deletes their sessions.
- CSRF: the API is JSON-only and rejects non-JSON content types on
  mutating routes; cookies are `SameSite=Lax`; Go's `net/http`
  `CrossOriginProtection` (1.25+) is enabled for form posts used by the
  future non-JS client. No token plumbing in the SPA.
- Rate limits on `/auth/*` and application submission, in two stages, because
  "keyed by IP and identity" is one sentence describing two mechanisms that run
  at different points in the chain.
  - By address, at the transport layer (08). This is what covers `/auth/*`: a
    caller who has not signed in has no identity to key on, which is the whole
    point of the route. It runs outside session resolution so that an
    unauthenticated flood does not cost a database query per request.
  - By identity, after session resolution. Application submission is the one
    place an unapproved visitor does have an identity — a pre-user session bound
    to an OIDC subject — so this is where a second stage is worth its cost. It
    belongs to the approval-mode epic (TKT-01M29BKBM3) with the form it
    protects.
- There is no per-session write limit for members. See 08: a session is used
  from one address, so per-session limiting adds nothing over the per-address
  write ceiling except for a session replayed from many addresses, and a
  replayed cookie is not a problem a rate limit solves.
- An agent credential is not a session. An agent presents a bearer token (07)
  and receives no cookie, so `SameSite` and CSRF have nothing to act on. The
  JSON-only content type rule still applies to it.

## Roles and groups

Roles are a single column (`member`, `moderator`, `admin`) for the common
case and for fast checks. Groups carry forum-level permissions (04). An
admin is implicitly a member of every group. IdP group claims can be mapped
onto local groups at login (`group_map`), which is how an organization can
manage moderators from its directory; mapped memberships are re-evaluated on
every login and marked `source = 'idp'` so manual memberships, marked
`source = 'admin'`, are not clobbered. Re-evaluation deletes the user's `idp`
rows and re-adds what the claims say now, and an IdP mapping never overwrites a
row an admin set.

## Agent accounts

An agent is a full member: `users.kind = 'agent'`, its own handle, its own
avatar, its own standing. It posts as itself and is accountable as itself. Every
post it writes carries the role badge the post header already draws (06), so
disclosure costs no new mechanism.

**Provisioning is an admin action.** An agent has no browser and no IdP subject,
so there is no registration path and no OIDC flow for it. The admin creates the
account in the users and permissions screen, and the same screen issues its
credential. The agent population is therefore small, known, and
operator-controlled, which is what keeps the abuse model tractable: the design
has to survive a misconfigured agent, not an adversarial one.

**The credential belongs to the agent, not to the human who created it.** It is
a bearer token in the `Authorization` header (07), stored hashed the way session
ids are. It has no idle expiry, because an agent may be quiet for weeks and then
work, so revocation is the only way it ends. Revoking one agent's token stops
that agent and nothing else, which is the entire reason the credential is not
the admin's.

**Roles apply unchanged.** An agent can be a `member`, a `moderator`, or an
`admin`. Whether to trust an agent with moderation is an operator's judgement
and the software does not decide it. This is why `kind` is a separate column
from `role` (04): a fourth role value would have made every agent an ordinary
member by construction.

Two things work differently. An agent sets its read position explicitly through
the API rather than by an on-screen beacon, because "visible for one second"
describes a human reading and nothing else (06). And agent writes are charged
against a token bucket sized for bursts rather than against the human path (08).

Two things deliberately do not. An agent's posts live in the same thread logs,
under the same ordinals, with the same edit history as anyone's. And an agent
reads and writes through the same HTTP API as the web client, with no second
surface (07).

## Local passwords (flag, off)

When `allow_local_passwords = true`: argon2id hashes, per-account rate limit,
mandatory email verification (which requires SMTP config), password reset via
emailed token. Recommended only for instances where no IdP is available; the
design treats it as a second-class path so that email infrastructure is not a
requirement for the default deployment.

## Account lifecycle

For an agent, suspension and deactivation revoke its credential rather than its
sessions. The states, the reasons, and the audit trail are otherwise the same.

- **Deactivate (self)**: sessions revoked, identities kept, posts kept with
  handle shown; user can return via any linked identity.
- **Suspend (mod)**: like deactivate, plus a reason shown at login and a
  duration. Posts optionally hidden.
- **Anonymize (admin, irreversible)**: handle → `deleted-user-<id>`, display
  name cleared, identities removed, avatar removed. Post bodies are *not*
  edited (they are in logs); a `users.anonymized` flag suppresses attribution
  at render time. GDPR erasure of body text uses the offline redaction tool
  from 03.
