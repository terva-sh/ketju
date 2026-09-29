---
schema: 3
id: TKT-01M29CKRE9F0WR2JH8QEMZ7G5B
title: Support multiple providers with domain gates and auto-approve
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:identity
  - effort:M
assignees: []
milestone: phase-2
parent: TKT-01M29BKBKZW26HNHFM1YFFGVZC
origin: null
dependencies:
  - TKT-01M29C5Y342B1ZBX6XCC69MFYM
  - TKT-01M29C7ZZ9KFSNZ8WC8YP1HAYV
  - TKT-01M29CKRG9XYJS4HM1VY1QSJ1V
blocks_on: none
references:
  - ref: design:identity-providers
    path: docs/05-identity-and-accounts.md
claim: null
archive: null
created_at: 2026-09-11T23:24:59Z
updated_at: 2026-09-29T05:06:41Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-groom
  name: ""
extensions: {}
---

## Description

Several providers on the login page at once, `allowed_email_domains` as an optional gate per provider, and `auto_approve` letting an organization's own SSO behave as open registration while everyone else goes through approval.

A domain gate only means something when the provider asserts `email_verified`. Trusting an unverified email claim from a public IdP turns the gate into decoration.

## Acceptance criteria

- [ ] A sign-in whose email domain is outside the provider's allowed_email_domains answers 403 with the 'Sign-in not allowed' page and writes no identity, user or session row
- [ ] With allowed_email_domains set, a token whose email_verified is false, absent or the string 'false' answers 403 with a page saying the email is not verified, even when the domain matches; with the list unset, email_verified is not required (TestGate, TestTheDomainGateNeedsAVerifiedEmail)
- [ ] Under registration.mode approval, a new identity from an auto_approve provider becomes an active member with no application, and one from any other provider gets no users row and no member session (TestAutoApproveUnderApprovalMode)
- [ ] For an auto_approve provider, a valid, unreserved and free normalized preferred_username becomes the handle at the callback with no screen; a missing, invalid, reserved or taken one shows the handle screen, prefilled where possible, with a line naming which of the four it was
- [ ] Register refuses a provider without auto_approve while the mode is approval, even with a valid registration cookie, and writes nothing
- [ ] For each of pending, suspended and deactivated, a user signing in through an auto_approve provider and through an ordinary one gets no session (TestNoProviderLetsAnInactiveUserIn)
- [ ] With three providers configured and one failing discovery, /auth/signin lists exactly the two enabled ones by name in config order
- [ ] ketju config check no longer warns about auto_approve: the branch in warn(), its test expectation, and the docs/02 bullet are removed

## Implementation plan

Groomed 2026-09-29 for Phase 2 (TKT-01M3M9RR26) against main at 7af2c5a.

### Settled in grooming

- **auto_approve.** A provider with `auto_approve = true` skips steps 2–5 of approval mode and behaves as `open` for that provider only (docs/05 "mode = approval").
- **The handle-skip rule** (decided 2026-09-28, docs/10 item 10, TKT-01M29BSPEX; the rule is in docs/05). For an auto_approve provider the handle screen is skipped too. The `preferred_username` claim is normalized as the prefill is. If the result is a valid handle, not reserved, and free in any case, it becomes the handle and the account is created at the callback. Otherwise the normal "choose a handle" screen appears, prefilled with the normalized name when there is one, above a line saying why it could not be used: missing, not a valid handle, reserved, or taken. A handle released by a rename stays reserved for 90 days, so it does not count as free. docs/05 says "for those providers", so the skip applies to an auto_approve provider whether `registration.mode` is `open` or `approval`.
- Only `preferred_username` feeds the skip. The email local part still feeds the fallback screen's prefill, as `identity.Prefill` does today (docs/05 "The prefill …").
- **The domain gate** means something only when the provider asserts `email_verified`. With `allowed_email_domains` set, a sign-in with `email_verified` false or absent is refused even when the domain matches (ticket description and AC2). With the list unset, nothing requires `email_verified`. The gate stays where TKT-01M29C7ZZ9 put it, at every callback before `Login`. It therefore also covers returning members and linking.
- Google's `hd` claim is not used for gating. docs/05 gates on the email domain, and `hd` stays recorded only (`internal/auth/oidc/oidc.go`, `Verify`).
- **The login page** lists the providers that are configured and not disabled, by `name`, in config order, and skips straight to the only one (docs/05 "Provider configuration", decided 2026-09-28, TKT-01M3MFS6KM). `/auth/signin` already does this (`internal/web/auth/signin.go`). This ticket adds the multi-provider test with a disabled provider.
- Email never links identities across providers (docs/05 "Login flow").
- An identity row that already exists with no user is an applicant. Turning auto_approve on later does not convert it: auto_approve changes the registration path, which docs/05 step 3 reaches only for an identity that is not found. A pending application is decided by a moderator, and a blocked identity stays blocked.
- **The warning.** `ketju config check` warns that `auto_approve` has no effect (`warn()` in `internal/config/validate.go`, and docs/02 "Configuration"). This ticket removes both.

### Approach

1. **Provider policy in the service.** Add `internal/service/identity/policy.go` with `type Policy map[string]ProviderPolicy` and `ProviderPolicy{AutoApprove bool; GroupsClaim string; GroupMap map[string]string}`, built from `config.OIDC.Providers` by `PolicyFrom(cfg)`. `cmd/ketju/serve.go` sets `identity.Service.Policy`. TKT-01M29CKRF9 uses the same type. Whichever of the two lands first creates it. Alternative: ask `oidc.Registry` at request time. It lost because a provider disabled by a failed refresh drops out of `Lookup`, so its policy would vanish between the callback and `POST /auth/register`. Config does not change while the server runs.
2. **The gate** (`internal/auth/oidc/oidc.go`). Replace `EmailAllowed(email string) bool` with `Gate(id *Identity) GateResult` (`GateOK`, `GateDomain`, `GateUnverified`). With no `AllowedEmailDomains` it returns `GateOK`. Otherwise it returns `GateUnverified` unless `id.EmailVerified`, then `GateDomain` unless the domain matches. The callback in `internal/web/auth/auth.go` logs the result at info and answers 403. `pageNotAllowed` covers the domain case. A new `pageUnverified` says: "This forum accepts sign-ins from this provider only with a verified email address." The IdP already treats both facts as known to the reader, so saying which applies discloses nothing new.
3. **Is registration open for this provider?** `RegistrationOpen(ctx)` becomes `RegistrationOpenFor(ctx, provider string)`: `mode == "open" || s.Policy[provider].AutoApprove`. Inside `Register`'s `IssueUserWith` create func, the in-transaction check `mode != "open"` becomes the same expression. Registration closing between the callback and the POST is therefore still caught for an ordinary provider. An auto_approve provider is never closed while `mode` is `open` or `approval`. The `Login` interface in `internal/web/auth/auth.go` changes to match.
4. **Skipping the handle screen.** In `internal/service/identity/handle.go`, split `suggest` into `normalize(s) string`, which returns the normalized string even when it is not a valid handle, and the existing validity filter. Add `AutoHandle(preferred string) (h string, why SkipReason)`, returning `SkipMissing`, `SkipInvalid` or `SkipReserved`, or `h` with no reason. Extract the "free in any case" read from `Register` into `handleFree(ctx, q, h) error`, returning nil or `ErrHandleTaken`, so the 90-day reservation of TKT-01M3NRP3W9 "Implement handle changes with cooldown, reservation, and redirect" hooks in at one place. Add `(*Service).AutoRegister(ctx, c Claims, meta, replaces) (Result, SkipReason, error)`. It calls `AutoHandle`, and when that yields a handle it calls `Register`, which maps `ErrHandleTaken` to `SkipTaken`. `Register` keeps `IssueUserWith` as its only way to a session.
5. **The callback** (`beginRegistration` in `internal/web/auth/register.go`). If `RegistrationOpenFor` is true and the provider auto-approves, call `AutoRegister`. On success, set the cookie and answer 303 to `return_to`. On a skip reason, seal the registration cookie as today, with a new `pending.Why` field (sealed, so it cannot be forged), and 303 to `/auth/register`. `registerForm` renders `Why` as one line above the field: "Your sign-in's username could not be used as a handle: it is missing / not a valid handle / reserved / taken." The prefill is `normalize(preferred_username)` when that is non-empty, and `Prefill` otherwise. If `RegistrationOpenFor` is true and the provider does not auto-approve, behaviour is unchanged. If it is false, approval mode takes the applicant path of TKT-01M29CKRG9.
6. **The warning.** Delete the `pr.AutoApprove` branch from `warn()` in `internal/config/validate.go`, with its comment. Delete its expectation in `internal/config/config_test.go`. Delete the docs/02 "Configuration" bullet "auto_approve has no effect until approval-mode registration exists". Update the `RegistrationOpen` doc comment in `register.go`.
7. **Issuer callers.** `AutoRegister` reaches a session only through `Register` → `IssueUserWith`. `issuerCallers` in `internal/auth/session/chokepoint_test.go`, added by TKT-01M29CKRG9, does not change, because `Register` is still the caller. If this ticket lands first, no list exists yet, and `doorCallers` and `insertCallers` stay as they are.
8. **Docs.** docs/05 "Provider configuration" gets one sentence on `email_verified` under the gate. docs/07 "URL scheme" `/auth/register` names the reason line. Both are written in the implementing PR.

### Tests

- `internal/auth/oidc` `TestGate`, a table test: list unset; list set with a matching verified email; a matching domain with `email_verified` false, missing, or the string "false"; a wrong domain that is verified; no email; case in the domain. Each row gives exactly one `GateResult`.
- `internal/web/auth` `TestTheDomainGateNeedsAVerifiedEmail`. Each refusal is 403 with its own page, and afterwards no identity, user or session row exists (`identity_schema` row counts).
- `internal/web/auth` `TestAutoApproveUnderApprovalMode`. Two providers, `club` (auto_approve) and `public`, with mode `approval`. A new identity from `club` with `preferred_username` "Ada.L" gets user `ada_l`, active, a member session, and no application. A new identity from `public` gets no `users` row and no member session. Until TKT-01M29CKRG9 lands that is today's 501; afterwards it is a pre-user session.
- `TestAutoApproveFallsBackToTheHandleScreen`, one subtest per reason (missing, invalid, reserved `admin`, taken `Ada_L` in another case). Each shows the screen with the reason line, the prefill where it applies, and no user row. Submitting a free handle then registers under approval mode, which proves the POST honours the provider's policy.
- `TestRegisterUnderApprovalModeOnlyForAutoApprove` (service). `Register` with claims from a provider that does not auto-approve, while mode is `approval`, is `ErrRegistrationClosed` and writes nothing, even with a correctly sealed cookie.
- **Chokepoint and status.** `TestNoProviderLetsAnInactiveUserIn` extends `TestTheCallbackLetsNoInactiveUserIn` (`pending_test.go`) across providers. For each of `pending`, `suspended` and `deactivated`, a user holding identities on both an auto_approve and an ordinary provider signs in through each. The page is "Account unavailable", no session row is written, and `/me` stays anonymous. A new `club` identity whose preferred handle belongs to a suspended account gets `SkipTaken`, never that account.
- `internal/web/auth` `TestTheLoginPageListsOnlyHealthyProviders`. Three configured providers, one of them failing discovery (the fake IdP's discovery returns 500). `/auth/signin` lists the two others by name, in config order.
- `internal/config` `TestAutoApproveIsNotAWarning`. A config with `auto_approve = true` checks clean, with no warning.

### Out of scope

- The applicant path for providers that do not auto-approve: TKT-01M29CKRG9. This ticket depends on it, so the "others still require it" half of AC3 can be shown as a pre-user session rather than a 501.
- Mapping group claims: TKT-01M29CKRF9. It shares `Policy`.
- Linking: TKT-01M29CKRES.
- The 90-day reservation of released handles: TKT-01M3NRP3W9. `handleFree` is its hook.
- Gating on `hd`: not planned. docs/05 gates on the email domain.
