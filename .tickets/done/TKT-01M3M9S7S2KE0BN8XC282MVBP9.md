---
schema: 3
id: TKT-01M3M9S7S2KE0BN8XC282MVBP9
title: Deploy a local dogfooding instance of ketju on this machine
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:ops
  - effort:M
assignees: []
milestone: phase-1
parent: null
origin: null
dependencies:
  - TKT-01M3MAS08RJDT1RRJZVA6TB423
  - TKT-01M3MAS0A3K53DRHK0R04G16AF
  - TKT-01M3MFS6KM13JGSDBXGQEZW7QA
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T15:22:53Z
updated_at: 2026-09-28T17:56:32Z
created_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
updated_by:
  id: agent:claude-code/phase1-run-438e26c1
  name: ""
extensions: {}
---

## Description

Phase 1's exit criterion (docs/09) is "a friend group can hold a conversation on it". Passing tests do not prove that, and nothing yet serves ketju anywhere a phone or a second person can reach. This ticket stands up a long-running local instance on this machine, as the lampi and git-ticket-canvas projects already run theirs, so that ketju can be used for real.

The same instance is what the real-device check TKT-01M38WTKSQ (Verify the Phase 1 client on real iOS and Android devices) needs. A phone has to reach a served build, over the network, through the TLS-terminating reverse proxy that docs/02 assumes. So that ticket depends on this one.

### Before planning
The hostnames, ports, service manager, reverse proxy, TLS setup, data locations and deploy steps come from how lampi and git-ticket-canvas are deployed on this machine, and from their documentation. Read those first and follow the same pattern. Do not guess a path, port or hostname (the machine-wide agent rules forbid it). Check that any path or port is free before using it.

### What the design already fixes
- One process per data directory, with the data-directory lock (docs/02, docs/10).
- A reverse proxy terminates TLS; ketju serves plain HTTP behind it (docs/02).
- `ketju config check` validates the config before first start (PR #106).
- `/healthz` answers 503 when the database or data dir check fails, and 200 `degraded` on quarantine (PR #107, docs/02 "Observability").
- `ketju admin promote --root DIR <handle>` and `bootstrap.admin_subjects` / `bootstrap.admin_emails` make the first admin (PR #97).
- `registration.mode = "open"` works; approval mode is Phase 2 (PR #102).
- The README's "Recovering a lost or damaged database" and `ketju reindex` cover recovery (PR #100). There is no `ketju backup` yet (TKT-01M29CXK35, phase-4), so the instance needs a documented interim backup of the data directory.

### Decisions for a person
- An OIDC provider for the dogfood instance, and its client credentials. Only secret references go in the repository or a ticket, never the values.
- Who is invited, and whether registration is open.
- How upgrades are deployed: which build, from which commit, and how often.

## Acceptance criteria

- [x] ketju serves from a documented data directory under a service manager, restarts on failure, and passes ketju config check
- [ ] A reverse proxy terminates TLS and a phone on the local network reaches the client at a documented URL
- [x] Sign-in works through the chosen OIDC provider, and the first admin is bootstrapped
- [x] /healthz is monitored or at least documented as the check to run
- [x] The deploy, upgrade, and interim backup steps are written down where the other local deployments keep theirs
- [x] No secret value is committed or written into a ticket

## Notes

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T15:40:24Z

Promoted 2026-09-28 at the maintainer's direction, to deploy now at https://ketju.brokkr.local.sothr.com. Pattern chosen by the maintainer: a user unit like git-ticket-canvas. Authentik provider pk 32 'Provider for Brokkr Ketju' and application 'brokkr-ketju' were created through the credbroker. Redirect https://ketju.brokkr.local.sothr.com/auth/authentik/callback, grant_types [authorization_code], flows, signing key and scope mappings copied from lampi's provider 31. The maintainer creates the 'Brokkr Ketju User' group and writes the client secret file. The first admin comes from bootstrap.admin_emails. Deploying found two blocking bugs: the issuer trailing slash (TKT-01M3MAS08R) and no way to open registration (TKT-01M3MAS0A3).

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T17:48:12Z

### Deployment record, 2026-09-28

The instance is served at https://ketju.brokkr.local.sothr.com, and it runs main at c40b414. It follows the git-ticket-canvas pattern, as the maintainer chose: a user unit, no root, with lingering on so the unit survives logout.

| Thing | Location |
|---|---|
| Binary | `~/.local/bin/ketju` |
| Unit | `~/.config/systemd/user/ketju.service`, enabled. `ExecStartPre` runs `ketju config check`; `Restart=on-failure`, `RestartSec=5`, `UMask=0077` |
| Config | `~/.config/ketju/config.toml`, mode 0600. It holds no secret |
| OIDC client secret | `~/.config/ketju/oidc-client-secret`, mode 0600. The maintainer wrote it; no agent has read it |
| Data | `~/.local/state/ketju/data`, mode 0700. The cookie key is created at `secrets/cookie.key` in it |
| Listen | 127.0.0.1:8788, loopback only |
| Proxy | Traefik `~/.config/traefik/dynamic/ketju.yml`: entrypoints websecure and websecure-ts; no `tls` key, so the `*.brokkr.local.sothr.com` wildcard certificate is used; access logs off, because callbacks carry codes |
| Logs | `journalctl --user -u ketju.service`, JSON |

**Authentik.** OAuth2 provider pk 32, "Provider for Brokkr Ketju", and application `brokkr-ketju`, both created through the credbroker. The settings are copied from lampi's provider 31, with grant_types `[authorization_code]` and a strict redirect to `https://ketju.brokkr.local.sothr.com/auth/authentik/callback`. The issuer `https://authentik.local.sothr.com/application/o/brokkr-ketju/` ends in a slash, which PR #119 made acceptable. Two policy bindings, mode "any": the groups "Brokkr Ketju User" and "Brokkr Ketju Admin", which the maintainer created and manages.

**First run.**
1. Start the service once to create the database, then stop it.
2. `ketju admin setting set --root ~/.local/state/ketju/data registration.mode open` (PR #120).
3. `systemctl --user enable --now ketju.service`.

**First admin.** The maintainer signed in as `warrick`. `bootstrap.admin_emails` did not apply: Authentik's default email scope mapping does not assert `email_verified`, and ketju refused, logging a WARN that says so. With serve stopped for a few seconds, they were promoted by `ketju admin promote --root ~/.local/state/ketju/data warrick`; the audit_log row is `user.promoted {"role":"admin","via":"cli"}`. `admin_emails` can only apply if Authentik's mapping asserts email_verified. Until the key is removed or the mapping changes, it logs a WARN at each of the maintainer's sign-ins.

**Health.** `curl -s https://ketju.brokkr.local.sothr.com/healthz` answers 200 `{"status":"ok",...}`, 200 `degraded` when a thread is quarantined, and 503 on a database or data-directory failure (docs/02 "Observability"). Nothing polls it yet; it is the documented check to run after every start.

**Upgrade.** From a checkout of main at a named commit:
1. `export GOROOT=$(go env GOROOT)`.
2. `go build -trimpath -o /tmp/ketju-build/ketju ./cmd/ketju`.
3. `/tmp/ketju-build/ketju config check --config ~/.config/ketju/config.toml`.
4. Keep the old binary for rollback.
5. `install -m 0755 /tmp/ketju-build/ketju ~/.local/bin/ketju`.
6. `systemctl --user restart ketju.service`.
7. Curl `/healthz`.

Migrations run at startup. Roll back to the previous binary only if no new migration ran.

**Interim backup.** `ketju backup` is TKT-01M29CXK35 in Phase 4. Until then:
1. Stop the service.
2. Copy `~/.local/state/ketju/data` whole: thread logs, `db/` and `secrets/`.
3. Start the service.

The thread logs are the source of truth. `ketju reindex` rebuilds the database from them, as README "Recovering a lost or damaged database" describes. A database restored older than the logs can reuse member ids: TKT-01M3KB7FXC.

**Bugs found and fixed while deploying.** All four are now done:
- TKT-01M3MAS08R, the issuer slash (PR #119);
- TKT-01M3MAS0A3, `ketju admin setting` (PR #120);
- TKT-01M3MAS0BQ, TestHealthzP99 under race (PR #118);
- TKT-01M3MFS6KM, a signed-out reader had no way to sign in: the client linked to the unbuilt /settings; now /auth/signin redirects to the only provider (PR #121).

The same record, which is a handoff and not the record, is at `~/.local/state/agent-handoffs/ketju/2026-09-28-dogfood-deployment.md`, where the lampi deployment keeps its procedure.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T17:48:12Z

Criteria, with evidence:
1. **Ticked.**
   - The unit is enabled and lingering is on.
   - `ExecStartPre` runs `ketju config check`, and the check exits 0.
   - `Restart=on-failure` is set.
   - The data directory is documented above.
2. **Left unticked.**
   - TLS is terminated by Traefik with the wildcard certificate. `curl` verified the chain (ssl_verify_result 0), and DNS resolves the name to 100.93.117.77.
   - The maintainer confirmed the sign-in redirect works in their browser on 2026-09-28.
   - No phone has reached it yet. That check belongs to TKT-01M38WTKSQ (Verify the Phase 1 client on real iOS and Android devices), which depends on this ticket and can now run.
3. **Ticked.**
   - The maintainer signed in through Authentik and reported it on 2026-09-28.
   - The first admin was made by `ketju admin promote`, one of the two bootstrap paths docs/05 names. The email path could not apply, for the reason recorded above.
4. **Ticked.** /healthz is documented as the check to run. Nothing monitors it.
5. **Ticked.** The steps are in the note above, and in the handoff directory where the lampi deployment keeps its procedure.
6. **Ticked.**
   - No secret value is in any commit or ticket. The client secret lives only in a 0600 file the maintainer wrote.
   - The client_id and the provider pk are identifiers, not secrets.

**agent:claude-code/phase1-run-438e26c1** at 2026-09-28T17:56:32Z

This note supersedes the **Upgrade** step list in the deployment-record note above. Review 1202 found that it never created the build directory. It also kept the rollback binary in /tmp, which does not survive a reboot. The corrected steps, from a checkout of main at a named commit:

1. `export GOROOT=$(go env GOROOT)`.
2. `install -d -m 0700 /tmp/ketju-build`.
3. `go build -trimpath -o /tmp/ketju-build/ketju ./cmd/ketju`.
4. `/tmp/ketju-build/ketju config check --config ~/.config/ketju/config.toml`.
5. Keep the running binary for rollback, outside /tmp: `cp ~/.local/bin/ketju ~/.local/bin/ketju.prev`.
6. `install -m 0755 /tmp/ketju-build/ketju ~/.local/bin/ketju`.
7. `systemctl --user restart ketju.service`.
8. Curl `/healthz`.

To roll back:
1. `install -m 0755 ~/.local/bin/ketju.prev ~/.local/bin/ketju`.
2. Restart the service.

Roll back only if no new migration ran.

The c40b414 deploy on 2026-09-28 kept its rollback binary at /tmp/ketju-build/ketju.prev. It has been copied to `~/.local/bin/ketju.prev`.

## Summary

The dogfood instance is live at https://ketju.brokkr.local.sothr.com, running main c40b414. It is a user unit on 127.0.0.1:8788 behind Traefik with the wildcard certificate, and signs in through Authentik application brokkr-ketju, gated by the Brokkr Ketju groups. Registration is open. The maintainer (warrick) is admin through ketju admin promote, because Authentik does not assert email_verified. Deploying found four Phase 1 bugs, all fixed (PRs #118-#121). A phone has not yet reached it (criterion 2); that moves to the real-device check TKT-01M38WTKSQ.
