---
schema: 3
id: TKT-01M3RBMVMJFVT3TSW49X8Z4AX7
title: Stop the seal round-trip test failing when ciphertext contains abc
type: bug
status: done
status_reason: null
priority: low
due_on: null
labels: []
assignees: []
milestone: phase-2
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:212
    path: null
claim: null
archive: null
created_at: 2026-09-30T05:12:24Z
updated_at: 2026-09-30T10:04:42Z
created_by:
  id: agent:claude-code/phase2-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase2-run-rbmvmj
  name: ""
extensions: {}
---

## Description

`TestRoundTrip` in `internal/auth/seal/seal_test.go` fails whenever the sealed value happens to contain the substring `abc`, which is the plaintext it checks for (line 37: `strings.Contains(v, "abc")`). The sealed value is random base64url, so any three-character window matches `abc` with probability 64^-3. Over a value about 90 characters long, that is roughly 3 failures in 10,000 runs.

It happened on CI run 1325, for PR #206 at head 2a5ab87, a ticket-only change. The value printed was `...5c6abckU...`.

### Fix

Check for a plaintext that cannot be mistaken for chance. Either seal a long, distinctive value and check for that, or decode the sealed value and check that the raw bytes of the encoded plaintext are absent. Do not weaken the check to nothing: its point is that a seal hides its contents.

Reported by the Phase 2 run orchestrator on 2026-09-30.

## Acceptance criteria

- [x] TestRoundTrip seals a marker of at least 24 characters and fails when reveals finds it in the sealed text or in its base64url-decoded bytes (internal/auth/seal/seal_test.go)
- [x] TestRevealsCatchesAnUnsealedValue: reveals answers true for the base64url encoding of the unencrypted envelope and false for a real seal of the same value
- [x] go test -race -count=10000 -run 'TestRoundTrip|TestRevealsCatchesAnUnsealedValue' ./internal/auth/seal/ passes, and a note records the command and its time
- [x] internal/auth/seal/seal.go is unchanged

## Implementation plan

Groomed 2026-09-30 for Phase 2 (TKT-01M3M9RR26), against main at 5124209.

What exists there:
- `TestRoundTrip` in `internal/auth/seal/seal_test.go:30-49` seals `flow{"abc", 7}` and fails when the sealed text contains `abc` (`:37`). The sealed text is `base64.RawURLEncoding` of nonce, ciphertext and tag (`Seal` in `seal.go`), so any three-character window is `abc` with probability 64^-3. That is the flake CI run 1325 hit.
- The check is also weak in the other direction. If `Seal` stopped encrypting and only base64-encoded its JSON, the text would hold the plaintext's base64, which contains the literal `abc` only when the encoding happens to align with it. So the check that fails by chance also passes by chance.
- `TestTheWholeSignIn` in `internal/web/auth/auth_test.go:243` makes the same kind of check on the flow cookie with `a-thread`, 8 characters. Its chance of a false failure is about 200 × 64^-8, near 7 × 10^-13 per run.

### Settled in grooming

- **The check stays, and gets stronger.** Its point is that a seal hides its contents (the package comment in `seal.go`: "the browser can neither read it nor change it"). The ticket's description forbids weakening it to nothing.
- **Check a long marker in both the text and the decoded bytes** (engineering choice). The test seals a marker of at least 24 characters. It fails when the marker appears in the sealed text or in the text's base64url-decoded bytes. A false failure then needs 24 given bytes by chance, about 64^-24 in the text and 256^-24 in the bytes. The decoded check is the one that catches a seal that does not encrypt. Three alternatives lost:
  - A long marker checked in the text only. It removes the flake but still passes an unencrypted seal by chance.
  - Retrying the seal when the text matches. It hides the flake instead of removing it.
  - A fixed nonce, to make the output deterministic. `Seal` reads `crypto/rand` directly, and injecting a reader changes `seal.go` for a test.
- **The check proves it can fail.** A second test hands the same check an unencrypted envelope and expects it to report the marker. Without that, a check that never fires looks the same as one that works.
- **`seal.go` does not change.** This is a test defect.

### Approach

1. In `internal/auth/seal/seal_test.go`, add `const marker = "seal-must-hide-this-state-value"` (31 characters) and a helper `reveals(sealed, marker string) bool`. It answers true when `sealed` contains `marker`, or when `base64.RawURLEncoding.DecodeString(sealed)` succeeds and the bytes contain `marker`.
2. `TestRoundTrip` seals `flow{marker, 7}` in place of `flow{"abc", 7}`, and replaces `strings.Contains(v, "abc")` with `reveals(v, marker)`. The `Open` round trip and the fresh-nonce check stay as they are, with `marker` in place of `"abc"`.
3. Add `TestRevealsCatchesAnUnsealedValue`. It builds the plaintext `Seal` would encrypt, `json.Marshal(envelope{At: …, V: …})` of `flow{marker, 7}`, and base64url-encodes it. `reveals` must answer true for that value and false for a real seal of the same flow.
4. The other tests in the file keep `"abc"`, because none of them checks concealment.

### Tests

- `TestRoundTrip` (changed) and `TestRevealsCatchesAnUnsealedValue` (new).
- `go test -race -count=10000 -run 'TestRoundTrip|TestRevealsCatchesAnUnsealedValue' ./internal/auth/seal/` passes. Record the command and its time in a note. At the old rate of about 3 failures in 10,000, this run would have failed with a probability near 95 %.

### Out of scope

- `auth_test.go:243`'s `a-thread` check. Its false-failure rate is about 7 × 10^-13 per run, so it is left alone.
- Any change to `seal.go`.

## Notes

**agent:claude-code/phase2-groom** at 2026-09-30T09:07:55Z

Plan refreshed against main at 5124209 on 2026-09-30. The ticket had no plan: this writes the first one and its four criteria, on top of the phase-2 milestone PR #208 set. Settled as engineering choices: the test seals a 31-character marker and checks both the sealed text and its base64url-decoded bytes, and a second test proves the check fires on an unencrypted envelope. The old three-character check failed by chance and also passed an unencrypted seal by chance. Retrying on a match and a fixed nonce lost; seal.go does not change. auth_test.go:243's 8-character check (about 7e-13 per run) is left alone. Re-applied onto main at a660ae5, after PR #208 merged.

**agent:claude-code/phase2-run-rbmvmj** at 2026-09-30T09:59:34Z

Claimed 2026-09-30 by the extended Phase 2 run. The plan was checked against main at 5362311: seal_test.go and seal.go are unchanged since 885bcac, so the plan stands as groomed and is not rewritten.

Built as planned in internal/auth/seal/seal_test.go: const marker = "seal-must-hide-this-state-value" (31 characters); reveals(sealed, marker) checks the sealed text and its base64url-decoded bytes; TestRoundTrip seals flow{marker, 7}; TestRevealsCatchesAnUnsealedValue proves the check fires on base64url(json(envelope)) and stays quiet on a real seal. The other tests keep "abc". seal.go is unchanged.

Mutations tried, reverted after each:
- reveals without the decoded-bytes check: caught by TestRevealsCatchesAnUnsealedValue (the marker is not in the base64 text).
- Seal returning base64url(plain) with no encryption: caught by TestRoundTrip.

Repeat run, 2026-09-30 09:58:52-09:59:09 UTC, local amd64, Go 1.27.1:
  go test -race -count=10000 -run 'TestRoundTrip|TestRevealsCatchesAnUnsealedValue' ./internal/auth/seal/
  ok  github.com/terva-sh/ketju/internal/auth/seal  16.615s (17 s wall)

## Summary

TestRoundTrip in internal/auth/seal/seal_test.go now seals a 31-character marker and checks, through a helper named reveals, that neither the sealed text nor its base64url-decoded bytes contain it. The old three-character check failed by chance about 3 runs in 10,000 and could pass an unencrypted seal by chance; the new one does neither. TestRevealsCatchesAnUnsealedValue proves the check fires on an unencrypted envelope. seal.go is unchanged. 10,000 race runs of both tests passed in 17 s; both mutations tried (reveals without the decoded check, Seal without encryption) were caught.
