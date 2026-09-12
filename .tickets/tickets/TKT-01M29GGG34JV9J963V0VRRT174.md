---
schema: 3
id: TKT-01M29GGG34JV9J963V0VRRT174
title: Decide the project name, org, and Go module path
type: chore
status: review
status_reason: "All five criteria met and verified mechanically: the old name greps to zero outside three deliberately kept records, go vet is clean, the threadlog tests pass under the new module path, and the docs/02 box re-measures at 94 runes. Held at review rather than done because the magic-byte change touches the on-disk format ahead of TKT-01M29C5Y1E, and the README now states an infrastructure fact (that go.mod and origin are meant to disagree) that the owner should confirm reads correctly."
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
updated_at: 2026-09-12T20:33:48Z
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

- [x] The name and the org are decided and recorded in the README
- [x] The module path is set in go.mod and every import agrees with it
- [x] The binary name, the env var prefix, and the cookie name are settled with the project name
- [x] The NEOFTHRD and NEOFIDX1 magics are either confirmed as-is or changed before TKT-01M29C5Y1E freezes the format
- [x] docs are swept for the old name in prose, not only in code

## Notes

**agent:terva/mieli** at 2026-09-12T20:33:40Z

in-progress to review: All five criteria met and verified mechanically: the old name greps to zero outside three deliberately kept records, go vet is clean, the threadlog tests pass under the new module path, and the docs/02 box re-measures at 94 runes. Held at review rather than done because the magic-byte change touches the on-disk format ahead of TKT-01M29C5Y1E, and the README now states an infrastructure fact (that go.mod and origin are meant to disagree) that the owner should confirm reads correctly.

## Summary

The project is **Ketju**, in the `terva-sh` org. All five criteria met.

*Ketju* is Finnish for "chain" and is the ordinary Finnish word for a forum thread. Both readings were the reason for choosing it: a thread is a chain of posts, and a thread log on disk is an append-only chain of framed records. It fits the org's existing naming, where terva is pine tar and mieli is mind.

### The decision that mattered more than the name

The magic bytes now name the format rather than the product: `NEOFTHRD` became `THRDLOG1` and `NEOFIDX1` became `THRDIDX1`, in docs/03 and in `internal/store/threadlog/format.go`.

This ticket originally framed the magics as the reason the name was urgent, since they land in every log ever written and changing them after data exists is a version bump. That framing was half right. The magics are the expensive part, but the fix is not to choose the name earlier. It is to stop putting the product name in a file signature at all. A thread log is a thread log whatever the binary above it is called. Branding there buys nothing and costs a format version bump on the day the name changes.

With that done, the name is off TKT-01M29C5Y1E's critical path entirely, and a future rename touches prose and one line of `go.mod`. Coupling would also have capped the name at five characters, to fit `XXXXXLOG` in eight bytes.

### Repositories, and a trap worth knowing about

Development happens on the internal forge, which is now `origin`:

```
ssh://git@git.local.sothr.com:2222/terva-sh/ketju.git
```

Release commits are pushed to a public mirror at `git@github.com:terva-sh/ketju.git`. The mirror is not wired up as a remote yet, by the owner's call.

The Go module path is `github.com/terva-sh/ketju`, naming the mirror rather than `origin`, because that is the path an importer resolves. **The module path and `origin` are supposed to disagree.** That is recorded in bold in the README, because a future session will otherwise see the mismatch and "fix" one of them.

### The sweep

55 occurrences to zero, outside three records left on purpose. Nine documents, the README, `go.mod`, the spike module, `format.go`, and `docs/mockups/generate.py`.

Two spots needed hand work rather than substitution, because `neoforum` is three characters longer than `ketju`: the deployment box in docs/02, re-verified at 94 runes on every line, and the column-aligned module layout and data tree.

Seventeen ticket files were swept. A draft that says "Ship `neoforum reindex --search`" is an instruction somebody will follow, not prose. Left alone: TKT-01M29BSPD and TKT-01M29BW9PT, both in review, whose summaries record what was true when that work was done, and this ticket's own description, which has to keep the old names in order to say what it changed.

`go vet ./...` is clean and the threadlog tests pass as `github.com/terva-sh/ketju/internal/store/threadlog`. No import block needed touching, because threadlog is still the only package and imports nothing of ours. That was the window this ticket existed to use, and it cost one line instead of every import in the tree.
