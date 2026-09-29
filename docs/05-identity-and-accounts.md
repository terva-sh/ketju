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
admin UI. Until that screen exists (Phase 2), and on a fresh install where
nobody can register to reach it, the operator changes them on a stopped
instance with `ketju admin setting set [--root DIR] KEY VALUE`, which
validates the value through the settings registry and writes the same
`setting.changed` audit row, with no actor and `"via": "cli"`. It refuses
while `serve` holds the data directory. `get KEY` and `list` read the
settings, and may run beside `serve`. A fresh install opens registration
with `ketju admin setting set registration.mode open`; the default stays
`approval`, so a public install is closed until an operator opens it
(TKT-01M3MAS0A3). Writing them into `config.toml` is refused at startup, with a
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

The login page is `GET /auth/signin?return_to=…` (07 "URL scheme"), and it
is where the client's "Sign in" goes. It lists the providers that are
configured and not disabled, each by its `name`, and skips straight to a
provider's start when that provider is the only one. With none configured it
says, as a page rather than an error, that sign-in is not available. It carries `return_to` on to
start, validated the same way, and sends a reader who is already signed in
straight back to it. (Decided 2026-09-28, TKT-01M3MFS6KM: a server page,
skipped when there is one provider.)

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
     link only when the sign-in began from the explicit "link a sign-in"
     action in settings, which marks the login cookie as a link attempt.
     Then attach the identity to the current user, done. Otherwise it is
     refused and nothing is attached. Decided 2026-09-29
     (TKT-01M29CKRES). Linking on any sign-in while signed in lost, for two
     reasons: a member switching accounts would link them silently, and a
     forged sign-in at a weak provider could attach an attacker's identity
     to someone's account.
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

The callback seals the verified claims (provider, issuer, subject, email,
`email_verified`, name, `preferred_username`) and the `return_to` into the
`__Host-ketju_reg` cookie, with the same key and attributes as the login
cookie and a purpose of its own, and redirects to `GET /auth/register`. The
cookie lasts fifteen minutes, so an abandoned registration writes nothing.
A browser drops a cookie over 4 KB. So if the sealed value is over 3800
bytes, the registration sheds claims until it fits: first the name and
`preferred_username`, so the display name becomes the handle and the field
starts empty; then the `return_to`, which becomes `/`. If the claims the
account needs still do not fit, the sign-in fails at the callback.
The screen is served by the server, as the other sign-in pages are, so that
signing up does not depend on the client loading. `POST /auth/register` takes
the form field `handle` and, in one transaction, checks that the identity has
no account and nobody has the handle in any case, then writes the user, the
identity and the session. Two people submitting one handle at once get one
account and one "that handle is taken". A config-named admin is promoted
straight after, as at a sign-in. The cookie is cleared on success. An
invalid, reserved or taken handle shows the form again with the reason.
(TKT-01M29C8005.)

The prefill lowercases ASCII letters, turns each run of other characters into
one `_`, cuts to 30, and trims a leading or trailing `_` or `-`; if what is
left is not a handle registration would accept, the field starts empty. It is
a suggestion and reserves nothing.

### `mode = "approval"`
1. Create the identity row with `user_id = NULL` and a **pending** user is
   *not* created yet. Instead the visitor is placed in a "pre-user" session
   bound to the identity id, which may only reach the application form,
   the application status page, and logout.

   The identity and the session are written in one transaction
   (`session.IssuePreUserWith`), and the callback answers 303 to `/apply`;
   an applicant who signs in again gets a new pre-user session and 303 to
   `/apply/status`. What the session may reach is a positive list,
   `web.PreUserRoutes`: `GET /api/v1/me`, the sign-in routes
   (`/auth/signin`, `/auth/{provider}/start`, `/auth/{provider}/callback`),
   `POST /auth/logout`, `/assets/`, and the `/apply` and `/apply/status`
   pages. The application API joins it with the form (TKT-01M29CP2KZ).
   `web.PreUserGate` enforces it in the middleware chain, after session
   resolution and before CSRF and every handler: an API path answers 403
   `forbidden`, any other page 303 to `/apply/status`, and any other
   request 403. A route added later is closed to a pre-user until the list
   names it. The list covers public reads too, so an applicant who wants to
   read as a guest signs out. A pre-user session stops resolving the moment
   its identity has a user, so it never becomes a member session in place:
   after approval the applicant signs in again, through the status check of
   `session.IssueUser` (TKT-01M29CKRG9).
2. Render the active version of the `registration` form (see Forms below).
   Fields the form marks `from_claim` are prefilled (name, email) and may be
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

For those providers the handle screen is skipped too (decided 2026-09-28,
docs/10 item 10). The `preferred_username` claim is normalized as the prefill
is. If the result is a valid handle, not reserved, and free in any case, it
becomes the handle, and the account is created at the callback. Otherwise
the normal "choose a handle" screen appears, prefilled with the normalized
name when it has one, above a line saying why it could not be used: missing,
not a valid handle, reserved, or taken. A handle released by a rename stays
reserved for 90 days (below), so it does not count as free.

## Changing a handle

Decided 2026-09-28 (docs/10 item 9, TKT-01M29BSPEN):

- **Members.** A member may change their own handle once every 30 days. The
  new handle passes the same checks as at registration: valid, not reserved,
  free in any case.
- **The old handle.** It is reserved for 90 days, so nobody can take it to
  impersonate. During that time `/u/{old}` and `GET /users/{old}` answer a
  302 to the new handle, with `Cache-Control: no-store`. The redirect is
  temporary on purpose: a cached 301 would keep sending visitors to the former
  owner after the handle is free and belongs to someone else. Afterwards the
  old handle answers `not-found` and is free again.
- **History.** Mentions, posts and quotes name the member by user id, so
  nothing in history changes; posts show the current handle.
- **Admins** may rename any member without the cooldown, for example to fix
  a grandfathered handle ending in `-` (TKT-01M3ANX1NF). The old handle is
  reserved and redirected just the same.
- **Audit.** Every change is audited, with the old and new handle.
- **Anonymized accounts** (`deleted-user-<id>`) cannot be renamed.

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

#### The schema rules (TKT-01M29CP2KE)

`internal/forms` parses a schema and validates answers against it, through
one table of field types. No code names a field key. A schema is refused
with `validation-failed` and a list of `{field, code}` errors (07 "Error
catalogue") when it breaks any of these rules:

- The schema is one JSON object with only the properties `title`,
  `intro_markdown` and `fields`. An unknown property is refused rather than
  ignored, so a schema written by a newer build fails loudly in an older one.
- The schema is at most 64 KiB both as sent and in the canonical form it is
  stored in, which can be longer because JSON escapes some characters. So
  every version a save accepts can be loaded again.
- `title` is 1 to 200 characters. `intro_markdown` is at most 10000.
- `fields` holds 1 to 50 fields. Each has a `key` matching
  `^[a-z][a-z0-9_]{0,39}$`, unique in the form, and a `type` from the table
  below. A `label` of 1 to 500 characters is required except on a
  `markdown_notice`. A title, label or option may not hold a control
  character or a Unicode line or paragraph separator (U+2028, U+2029).
- Each type allows only the optional properties the table lists. Any other
  is refused as `not_allowed`.

<!-- form-types:begin -->
| Type | Optional properties | `max` cap | The answer |
|---|---|---|---|
| `text` | `required`, `max`, `from_claim`, `locked` | 200 | a string on one line |
| `email` | `required`, `max`, `from_claim`, `locked` | 254 | a bare address, as `net/mail` reads it |
| `textarea` | `required`, `max` | 5000 | a string; `\n` and tab allowed, `\r\n` stored as `\n` |
| `checkbox` | `required` | none | `true` or `false`; required means ticked |
| `select` | `required`, `options`, `multiple` | none | one of `options`, or with `multiple` a list of them without repeats |
| `markdown_notice` | `markdown` | none | none: it is display only, and a value sent for it is refused |
<!-- form-types:end -->

- `max` is 1 to the type's cap and counts characters. Without it the cap
  applies.
- `options` is required on a `select`: 1 to 100 unique options of 1 to 200
  characters each.
- `from_claim` is one of `name`, `email` or `preferred_username`. `locked`
  requires `from_claim`.
- `markdown` is a notice's body, at most 10000 characters. It and
  `intro_markdown` are rendered through `internal/render` like a post.

Answers are strings trimmed of surrounding space. Control characters are
checked before the trim, so a one-line answer with a line break at either
end is refused rather than trimmed. A one-line answer (`text`, `email`)
refuses U+2028 and U+2029 as it refuses `\n`. A `textarea` accepts them. A `null` is no answer, except for a
`markdown_notice`, which is sent nothing at all. An answer for a key the
form has no field for is `bad-request`, because the client drew its inputs
from the schema. A `locked` field ignores whatever was submitted and stores
the claim. When the IdP sent no such claim it stores an empty value and is
not held to `required`, because the applicant cannot supply a value the IdP
withheld.

A saved schema is stored in canonical form, so the same schema always
stores the same bytes. Each version is parsed when it is loaded. A stored
version that no longer parses is a 500 and an error-level log naming the
form and version, never a form served without the field it could not read.
The server logs the same at startup for the registration form, and keeps
serving.

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

**Every sign-in re-evaluates, through whichever provider.** A sign-in makes
the user's `idp` memberships exactly the groups that this sign-in's provider
maps from this token. A provider with no `group_map` asserts no groups, and so
does a token that leaves the claim out, so either removes every `idp`
membership. A member cannot keep a group their directory revoked by signing
in through a second provider. With two providers that both map groups, each
sign-in leaves what that provider's directory asserts now. A claim value the
map does not name grants nothing, and a map target that names no group grants
nothing and is logged at warn, at startup and at each sign-in that meets it.
The implicit `everyone` and `members` groups cannot be targets; `config check`
refuses them. Registration is the first sign-in, and maps the same way.
Mapping changes groups and never `users.role`. Only an active user who has
just been given a session is mapped, and a mapping that fails fails the
sign-in and ends that session. (TKT-01M29CKRF9, after review 1342.)

**Memberships are re-evaluated only at sign-in.** Nothing re-reads the claims
during a session, so a group revoked at the IdP stays with the member until
their next sign-in through any provider. That can be as long as the 90-day
hard expiry of a session they already hold. An admin who needs it sooner
removes the membership, or suspends the account, which ends its sessions.

Each membership a sign-in adds or removes writes one `audit_log` row in the
transaction that changes it: `group.member_added` or `group.member_removed`,
targeting the user, with no actor, `"via": "group_map"`, and the group, its
id, `"source": "idp"` and the provider in `details_json`. A sign-in that
changes nothing writes nothing.

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
  handle shown. The member returns by signing in with any linked identity and
  pressing Reactivate on the "account unavailable" page; signing in alone
  does not reactivate (decided 2026-09-29, TKT-01M29CKRFS). Silent
  reactivation lost because a stolen identity would undo a deliberate
  deactivation unseen. Admin-only reactivation lost because a person should
  not need an admin to come back.
- **Suspend (mod)**: like deactivate, plus a reason shown at login and a
  duration. Posts optionally hidden.
- **Who may act on whom** (decided 2026-09-29, TKT-01M29CKRFS and
  TKT-01M29CP2QK): moderators suspend members, and admins suspend anyone
  except themselves. Nobody can suspend, deactivate, anonymize or demote the
  last active admin.
- **Anonymize (admin, irreversible)**: handle → `deleted-user-<id>`, display
  name cleared, identities removed, avatar removed. The old handle is
  reserved for 90 days with no redirect, then freed (decided 2026-09-29).
  Audit rows are append-only and keep the old handle, including rename rows.
  Anonymization does not reach them, and that is accepted: the audit log is
  admin-only, and redacting it would weaken the one record of what happened. Post bodies are *not*
  edited (they are in logs); a `users.anonymized` flag suppresses attribution
  at render time. GDPR erasure of body text uses the offline redaction tool
  from 03.

Whose profile is served (`GET /users/{handle}`, 07) follows from the
states above. A pending account has not joined, so its profile and history
are `not-found` except to moderators and admins. A suspended or deactivated
account's posts are kept with the handle shown, so its profile and history
are served to members as any other account's are; its status is shown only
to moderators and admins. Decided by the maintainer on 2026-09-28
(TKT-01M3KJJNDC).
