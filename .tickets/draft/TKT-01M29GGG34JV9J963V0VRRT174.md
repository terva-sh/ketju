---
schema: 3
id: TKT-01M29GGG34JV9J963V0VRRT174
title: Decide the project name, org, and Go module path
type: chore
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:ops
  - effort:S
  - decision
assignees: []
milestone: phase-0
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: design:architecture-layout
    path: docs/02-architecture.md
claim: null
archive: null
created_at: 2026-09-12T00:33:06Z
updated_at: 2026-09-12T00:33:06Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:terva/mieli
  name: Mieli
extensions: {}
---

## Description

The repository is called neoforum, there is no git remote, and the first production Go code needs a module path. The current thinking is a rebrand under the `terva-sh` org (https://github.com/terva-sh), with the leaf name unsettled.

The cost of getting this wrong is not flat, and that is the part worth acting on. Today the module path appears in exactly one line of `go.mod`, because `internal/store/threadlog` and its tests are a single package that imports nothing of ours. The moment a second package imports the first, the path is in every import block in the tree, and it stays there.

So a provisional path is nearly free right now and gets steadily less free. TKT-01M29BW9PT proceeds on `github.com/terva-sh/neoforum` as a placeholder, and this ticket is what makes the real choice before the second package lands.

Renaming also reaches past `go.mod`: the README, the docs set, the `neoforum` binary name and its subcommands, the `NEOFORUM_*` environment prefix, the `__Host-nf_session` cookie name, the `NEOFTHRD` and `NEOFIDX1` file magics, and the data directory layout in docs/03. The two magics are the ones that matter, because they land in every log file ever written and changing them after data exists is a format version bump.

## Acceptance criteria

- [ ] The name and the org are decided and recorded in the README
- [ ] The module path is set in go.mod and every import agrees with it
- [ ] The binary name, the env var prefix, and the cookie name are settled with the project name
- [ ] The NEOFTHRD and NEOFIDX1 magics are either confirmed as-is or changed before TKT-01M29C5Y1E freezes the format
- [ ] docs are swept for the old name in prose, not only in code
