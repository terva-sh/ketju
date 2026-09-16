---
schema: 3
id: TKT-01M29C8018NBCR238GXMMBH73J
title: Map errors to RFC 9457 problem details
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area:api
  - effort:S
assignees: []
milestone: phase-1
parent: TKT-01M29BKBMCNDSYHYF9WXAPDNRN
origin: null
dependencies:
  - TKT-01M29C5Y3SAD25GYYEMHYPDF24
blocks_on: none
references:
  - ref: design:api-principles
    path: docs/07-api-and-clients.md
claim: null
archive: null
created_at: 2026-09-11T23:18:33Z
updated_at: 2026-09-16T20:41:35Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/web
  name: ""
extensions: {}
---

## Description

One error mapping for the whole API: `application/problem+json` with a stable `type` code the client can switch on, plus a human-readable title and detail.

Stability of the code is the part that matters. The client branches on it, the future HTML client turns it into a flash message, and changing one is a breaking change. A catalogue of codes in one file, rather than strings scattered through handlers, is what keeps that true.

Internal detail must not leak. A database constraint name in a problem detail tells an attacker the schema and tells a member nothing.

## Acceptance criteria

- [x] Every error response carries application/problem+json with a type from a single catalogue
- [x] A domain error maps to its status code in one place rather than at each call site
- [x] No internal error text, SQL, or stack detail reaches a response body
- [x] The code catalogue is documented and a golden test covers each one

## Notes

**agent:claude-code/web** at 2026-09-16T20:41:23Z

Built as `internal/problem`: `problem.go` (codes, catalogue, writers),
`domain.go` (the error mapping), tests, and `testdata/catalogue.json`.
`internal/web` was migrated onto it and its placeholder `Problem` /
`WriteProblem` removed -- that placeholder's own doc comment named this ticket
as the owner of the full mapping.

### Where it lives, and why not in internal/web

A separate package, not an addition to `internal/web`. Handler packages need
to write errors; if the catalogue lived in `web` they would import the router
and the middleware chain to do it. The dependency also runs the right way:
`problem` imports the domain packages, and none of them import `problem`.
Storage does not know what an HTTP status is and should not.

### The no-leak guarantee is structural, not a rule

AC3 is the one worth getting right, because the failure mode is a one-line
change that looks like an improvement: putting `err.Error()` in the body to
help a client debug. The domain errors say things like
`threadlog: checksum mismatch`, and once the database lands they will say
`UNIQUE constraint failed: identities.sub` -- which describes the schema to
whoever provoked it and means nothing to a member.

So `WriteError` takes the error *and* the logger together. The error goes to
the log, where it is useful; the only text that can reach the body is a
constant from the catalogue. There is deliberately no variant taking an error
without a logger, because that variant's obvious implementation is the leak.

`TestNoInternalDetailReachesTheBody` wraps real sentinels in the kind of text
a handler would actually produce (a query, a file path, a constraint name) and
asserts none of it appears. `TestTheErrorStillReachesTheLog` is its other
half: not leaking is only correct if the error is not simply discarded.

### Decisions

**`Code` is a defined type, not a string.** A handler cannot invent one at a
call site. `Write` falls back to `CodeInternal` for a code that is not in the
catalogue rather than panicking or emitting a blank `type` -- a blank type is
worse than a wrong one, because the client's switch has no arm for it.

**`type` is a short relative URI reference, not an absolute URL.** RFC 9457
types the member as a URI reference and a relative one conforms. docs/07 asks
for "a stable type code the client can switch on", and clients compare it as
an opaque string either way.

**One code for "no such route" and "no such object".** Telling an unauthorized
reader which thread ids exist is a disclosure; one answer is the cheapest way
not to.

**`not-implemented` stays distinct from `not-found`,** carrying forward the
distinction the router already made: during Phase 1 most handler groups do not
exist, and "not built yet" is a different fact from "no such endpoint".

**Errors can be tagged rather than only mapped.** `Coded(code, err)` lets a
handler say what kind of failure it hit without inventing a package-level
sentinel for every validation rule, and an explicit tag beats a sentinel the
error happens to wrap. The error chain survives, so `errors.Is` still finds
the cause.

**Corruption maps to `internal`, listed rather than left to fall through,** so
the record shows those errors were considered and deliberately given the bug
bucket. An unmapped error is also `internal`, and is logged at error level
precisely because it means the table has a gap -- if it logged at the same
level as a client's bad id, the gap would never be noticed.

**`Retry-After` is here rather than in the rate limiter.** docs/07 documents
429-with-Retry-After as part of the API's error contract, and the catalogue
owns the contract. Whole seconds per RFC 9110, which needs no clock agreement.
Provided for TKT-01M29C800X.

**`group()` lost its name parameter.** The 501 body used to interpolate the
group name. The catalogue's titles are fixed text, and the request path
already says which group was asked for, so the name bought nothing and would
have been the one title assembled at a call site.

### Verification

Six mutations, each caught by a named test:

1. Put the cause in the body -> `the response body leaked "UNIQUE constraint
   failed: identities.sub"`.
2. Rename a code -> golden diff, plus `documents code "not-found", which does
   not exist`.
3. Explicit `Coded` tag no longer wins -> `FromError = not-found, want the
   explicit tag forbidden to win`.
4. Unknown code yields a blank type -> `TestAnUnknownCodeStillProducesASwitchableType`.
5. Declare a code with no catalogue entry -> `code "teapot" is declared but has
   no catalogue entry, so Write would answer it with a 500`.
6. Change a status in the docs table only -> `conflict: documented as 400, code
   says 409`.

Mutation 5 is why `TestEveryDeclaredCodeIsInTheCatalogue` parses this
package's own source with go/ast rather than checking a hand-written list: a
list would not catch the failure it exists for, since whoever forgot the entry
would forget the list too.

Mutation 6 is why the docs table is parsed and checked. AC4 asks for the
catalogue to be documented, and documentation asserted once is documentation
that is wrong within a release.

`gofmt`, `go vet` and `go test -race -count=1 ./...` are clean across the
module.

## Summary

internal/problem is the API's error vocabulary: fourteen stable codes, one table mapping each to a status and a fixed title, and one table mapping domain errors to codes. It is its own package so handler packages can write errors without importing the router, and so the dependency runs from problem to the domain rather than back. The no-leak rule is structural rather than remembered: WriteError takes the error and the logger together, sending the error to the log and putting only a catalogue constant in the body, and there is no variant taking an error without a logger because that variant's obvious implementation is the leak. Coded() lets a handler tag an error with a code instead of inventing a sentinel per validation rule, and the tag beats any sentinel the error wraps. type is a short relative URI reference, which conforms to RFC 9457 and is what docs/07 asks clients to switch on. The catalogue is documented in docs/07 and both halves are tested: a golden file locks every code's rendered response, and a test parses the docs table and fails on drift. A third test parses the package's own source with go/ast so a code declared without a catalogue entry cannot ship. internal/web was migrated onto it and its placeholder removed.
