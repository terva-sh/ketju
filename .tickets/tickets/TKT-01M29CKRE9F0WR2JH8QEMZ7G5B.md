---
schema: 3
id: TKT-01M29CKRE9F0WR2JH8QEMZ7G5B
title: Support multiple providers with domain gates and auto-approve
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/phase2-run-ckre9
  branch: run/ckre9-multi-provider
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/ketju/.claude/worktrees/agent-ae5086623333f99f6
  commit: fe5cdcb7fbda81cadab35546674c9b2a26e732d2
  session: null
  claimed_at: 2026-09-29T21:51:00Z
  expires_at: null
archive: null
created_at: 2026-09-11T23:24:59Z
updated_at: 2026-09-29T21:52:00Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/phase2-run-ckre9
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

Groomed 2026-09-29 for Phase 2 (TKT-01M3M9RR26) against main at 7af2c5a. Revised at claim, 2026-09-29, against main at fe5cdcb.

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

### Approach (revised against fe5cdcb)

What changed since grooming: `identity.Policy`/`ProviderPolicy{GroupMap}` exists (TKT-01M29CKRF9), `identity.HandleFree(ctx, q, h, now)` exists and `Register` already calls it in its transaction (TKT-01M29CKRFS, TestEveryHandleWriteChecksHandleFree), and the applicant path and `issuerCallers` exist (TKT-01M29CKRG9). No schema change and no migration are needed.

1. **Policy.** Add `AutoApprove bool` to `ProviderPolicy`, set by `PolicyFrom`. `(*Service).AutoApproves(provider) bool` reads it. The callback asks the service, not `oidc.Provider.AutoApprove()`, so the check at the callback and the one in `Register`'s transaction read one source. `Provider.AutoApprove()` is removed, since nothing else calls it.
2. **The gate** (`internal/auth/oidc/oidc.go`). Replace `EmailAllowed(email) bool` with `Gate(id *Identity) GateResult` (`GateOK`, `GateDomain`, `GateUnverified`). With no list it returns `GateOK`. Otherwise it returns `GateUnverified` unless `id.EmailVerified`, then `GateDomain` unless the domain matches. The callback logs the result at info and answers 403 with `pageNotAllowed` or a new `pageUnverified`. Both run before `Login`, so they cover returning members too.
3. **Open for this provider.** `RegistrationOpen(ctx)` becomes `RegistrationOpenFor(ctx, provider)`: mode `open`, or mode `approval` and the provider auto-approves. `Register`'s in-transaction check uses the same function of mode and policy, so a provider that does not auto-approve is refused under approval mode with `ErrRegistrationClosed`, even with a valid sealed cookie, and nothing is written.
4. **Skipping the handle screen** (`handle.go`). Split `suggest` into `normalize(s)`, the prefill's transform without the validity filter, and the filter. `AutoHandle(preferred) (string, SkipReason)` returns `SkipMissing` for an empty claim, `SkipInvalid` when it normalizes to nothing or to an invalid handle, and `SkipReserved` for `ErrHandleReserved`. `(*Service).AutoRegister(ctx, c, meta, replaces) (Result, SkipReason, error)` refuses a provider that does not auto-approve, calls `AutoHandle`, then `Register`, and maps `ErrHandleTaken` to `SkipTaken`. `HandleFree` answers that for a handle anyone holds, in any case, and for one a release still reserves. `Register` stays the only way to a session, through `IssueUserWith`.
5. **The callback** (`beginRegistration`). When registration is not open for this provider, approval mode's applicant path runs, unchanged. When it is open and the provider auto-approves, the callback calls `AutoRegister`. Success sets the session cookie and answers 303 to `return_to`. A skip reason seals the registration cookie with a new `pending.Why` and answers 303 to `/auth/register`. `registerForm` prefills `normalize(preferred_username)` when `Why` is set and that is non-empty, and `Prefill` otherwise, and shows one line naming the reason. When it is open and the provider does not auto-approve, nothing changes.
6. **The warning.** Delete the `pr.AutoApprove` branch from `warn()`, its case in `TestWarningsNameTheirKeyAndDoNotFail`, and the docs/02 bullet. Remove the 501 branch and the auto_approve subtest of `TestTheBranchesStillToCome`, and reword `pageRegistration`.
7. **Issuer callers.** `AutoRegister` reaches a session only through `Register` → `IssueUserWith`. `issuerCallers` does not change, and the diff shows it.
8. **Docs.** docs/05 "Provider configuration" gets one sentence on `email_verified` under the gate. docs/07 `/auth/register` names the reason line, and says an auto_approve provider may register under approval mode.

### Tests

- `oidc` `TestGate` (table): list unset; matching and verified; matching with `email_verified` false, absent, or the string "false"; wrong domain, verified; no email; domain case; a subdomain.
- `web/auth` `TestTheDomainGateNeedsAVerifiedEmail`: each refusal is 403 with its page, and no identity, user or session row exists afterwards. With the list unset, `email_verified` is not needed.
- `web/auth` `TestAutoApproveUnderApprovalMode`: `club` (auto_approve) and `public`, mode approval. `club` with `preferred_username` "Ada.L" gets active user `ada_l`, a member session, and no application. `public` gets no users row and a pre-user session, not a member.
- `web/auth` `TestAutoApproveFallsBackToTheHandleScreen`: missing, invalid, reserved `admin`, taken (`Ada_L` held as `ada_l`, and by a suspended account). Each shows the screen with the reason line and the prefill, and writes no user row. Submitting a free handle then registers under approval mode.
- `identity` `TestRegisterUnderApprovalModeOnlyForAutoApprove`, and the same in `web/auth` with a sealed cookie from a provider that does not auto-approve: `ErrRegistrationClosed` or 403, nothing written.
- `web/auth` `TestNoProviderLetsAnInactiveUserIn`: pending, suspended and deactivated users holding identities on `club` and `public` sign in through each, under both modes. Each gets "Account unavailable", no session row, and an anonymous `/me`.
- `web/auth` `TestTheLoginPageListsOnlyHealthyProviders`: three providers, one failing discovery. The page lists the other two by name, in config order.
- `config`: `auto_approve = true` checks clean, with no warning.

### Out of scope

- The applicant path for providers that do not auto-approve: TKT-01M29CKRG9 (done).
- Mapping group claims: TKT-01M29CKRF9 (done). It shares `Policy`.
- Linking: TKT-01M29CKRES.
- Gating on `hd`: not planned. docs/05 gates on the email domain.
