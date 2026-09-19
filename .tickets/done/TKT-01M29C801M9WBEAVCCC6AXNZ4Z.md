---
schema: 3
id: TKT-01M29C801M9WBEAVCCC6AXNZ4Z
title: Define the shared DTOs and the golden JSON test harness
type: task
status: done
status_reason: All four acceptance criteria are met and tested. Fifteen mutations confirmed each test catches what it claims, including three rerun through -update to prove the structural tests do not regenerate away. AC1's untestable half, that no handler duplicates a shape, is recorded in the note for the first handler's reviewer.
priority: urgent
due_on: null
labels:
  - area:api
  - area:test
  - effort:M
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
updated_at: 2026-09-19T23:33:34Z
created_by:
  id: agent:terva/mieli
  name: Mieli
updated_by:
  id: agent:claude-code/web
  name: ""
extensions: {}
---

## Description

The structs the JSON API serializes are the structs the future HTML templates will render. One definition, two presentation layers, which is the concrete mechanism behind the whole non-JS story.

Golden JSON files per endpoint are the contract. They catch an accidental field rename, and they are also the document a client developer reads instead of asking. docs/09 names Go 1.27 json/v2 strictness as a risk, and goldens from Phase 1 are the mitigation: a duplicate key or a UTF-8 problem shows up as a failing golden rather than as a runtime surprise later.

## Acceptance criteria

- [x] DTOs live in one package that neither store code nor handler code duplicates
- [x] A golden file exists per endpoint and a field change fails the test with a readable diff
- [x] Regenerating goldens is one documented command, so updating them is deliberate rather than reflexive
- [x] Goldens cover the anon, member, and moderator viewer classes where the shape differs

## Implementation plan

`internal/dto`: the structs, and a golden corpus that is the contract.

### Where it lives and why it is not in `internal/web`

docs/07 commits to `internal/service` for the service layer and
`internal/web/html` for the future HTML handlers. The DTOs cannot live in either
without the other importing it, so they get their own leaf package that imports
nothing of ours. `dto` rather than a prettier name because that is the word both
this ticket and docs/07 use, so a reader grepping for "DTO" lands on it.

### The shapes

docs/07 specifies the thread response in full, and describes the others in
prose. Only what a document actually pins gets a struct here; inventing the rest
would produce goldens that assert my guesses:

- Thread page response, from the abridged JSON in docs/07: thread, page, lens,
  viewer, posts, author, attachment.
- Forum tree node and thread list row, from docs/07's table and docs/06's list
  of columns.
- `/me`, from docs/07's one-line description.

`/render`'s response belongs to TKT-01M29C2WWG and is not duplicated here.

### Viewer classes

docs/07 keys the response cache on `viewer_class` in {anon, member, moderator},
and says moderators see tombstone contents. The corpus carries one golden per
class for every endpoint whose shape actually differs, and the harness refuses
a class whose golden is byte-identical to another's -- an identical pair means
either the distinction is fake or the fixture does not exercise it, and both are
worth failing over.

### json/v2

docs/09 names Go 1.27 json/v2 strictness as a risk and this ticket names
goldens as the mitigation. Measured rather than assumed: `encoding/json/v2` is
in go1.27.1 with no GOEXPERIMENT, but it is gated on the module's go directive,
which is 1.26.2, so importing it is a module-wide language bump and out of scope
here.

So the mitigation is built without the bump. The three strictnesses that would
bite were measured against the real toolchain -- duplicate object members,
case-sensitive field matching, invalid UTF-8 -- and each becomes a test over the
corpus and the structs, rather than a promise that a future golden diff will
catch it.

### The test that makes the corpus honest

A golden catches a renamed field only if a fixture populates it. So the harness
reflects over every DTO type, collects every JSON tag, and fails if a name never
appears anywhere in the corpus. Without that, an unpopulated field is a hole
with a passing test over it.

### Regeneration

`go test ./internal/dto -update`, matching `internal/render`, documented in the
package comment and in docs/07 so it is findable from either side.

## Notes

**agent:claude-code/web** at 2026-09-19T23:33:20Z

### json/v2, measured rather than assumed

docs/09 names Go 1.27 json/v2 strictness as a risk and this ticket names
goldens as the mitigation. What is actually true, checked against the toolchain
in this environment:

- `encoding/json/v2` is in go1.27.1 and needs no GOEXPERIMENT. The flag every
  older write-up mentions is gone.
- It is gated on the *module's* go directive, not the toolchain. go.mod says
  `go 1.26.2`, so importing it fails with "json.Marshal requires go1.27 or
  later (file is go1.26)". Adopting it is a module-wide language bump, which is
  a decision with consequences well outside this ticket.

So the mitigation was built without the bump. The three strictnesses were
measured on a scratch module and each became a test here rather than a promise
that some future golden diff would catch it:

| Behaviour | v1 | v2 |
|---|---|---|
| Duplicate object member | keeps the last, silently | `duplicate object member name` |
| Field name case | matched case-insensitively | case-sensitive |
| Invalid UTF-8 | replaced with U+FFFD | `invalid UTF-8 within ...` |

Whoever raises the go directive should find these already passing. If one does
not, it fails in this package rather than at a client.

### The tag-collision finding

Writing the duplicate-key test surfaced something worse than the case it was
written for. Given two fields carrying the same JSON tag at the same depth,
`encoding/json` drops *both* and returns no error: the struct marshals to `{}`
with the pair simply gone.

That is caught by the golden diff exactly once -- and the natural response to a
golden failure is to rerun with `-update`, which makes it pass with two fields
missing from the API. So `TestNoTwoFieldsShareATag` checks the tags directly,
and the same reasoning is why the other structural tests were written to
survive a regeneration: each was mutation-tested by mutating, running `-update`,
and confirming it still failed.

### What is deliberately not here

`POST /render`'s response belongs to TKT-01M29C2WWG "Return structured
mentions, quotes, and reply_to from the render call", and duplicating it here
would give it two definitions before it has one.

Everything else absent is absent because no document pins its shape. docs/07
specifies the thread page response in full and describes the forum tree, the
thread list row and `/me` in prose; docs/06 supplies the thread list columns.
That is the whole of what has a struct. Inventing the notifications, search or
admin shapes would produce goldens asserting my guesses, and a committed golden
reads to the next person as a decision somebody made.

`Settings` is the same restraint at field level: timezone and locale from the
users row in docs/04, page size from docs/06. `prefs_json` holds more, but what
is in it is not decided. Note that the page size fixture uses 25 because docs/06
gives that as the current default -- TKT-01M29BW9N is still deciding between 20,
25 and 30, and that decision changes a fixture here, not a type.

### AC1's untestable half

The criterion asks for one package that neither store code nor handler code
duplicates. No handler exists yet, so nothing can be checked for a duplicate
shape. What is enforced instead is the layering that makes the sharing possible:
`TestTheDTOPackageImportsNothingOfOurs` fails if any non-test file in this
package imports anything under the module path. An import of `internal/store`
would be the first step of the failure the criterion describes -- a DTO shaped
around a row rather than around what a client needs.

The other half arrives with the first handler, and the reviewer of that ticket
is the one who can check it.

### Mutation testing

Fifteen mutations, each confirmed to fail the test that claims to catch it, and
the package restored byte-identical afterwards (`diff -rq`): a renamed tag, an
unpopulated new field, a field never given a value, a pointer never nil, a
pointer never set, an unreachable declared type, two viewer classes made
identical, a golden with no fixture, two fields sharing a tag, two tags
differing only by case, invalid UTF-8 in a committed golden, a duplicate member
hand-edited into one, an import of another internal package, and three of the
above rerun through `-update` to confirm the structural tests do not regenerate
away.

**agent:claude-code/web** at 2026-09-19T23:33:34Z

in-progress to done: All four acceptance criteria are met and tested. Fifteen mutations confirmed each test catches what it claims, including three rerun through -update to prove the structural tests do not regenerate away. AC1's untestable half, that no handler duplicates a shape, is recorded in the note for the first handler's reviewer.

## Summary

`internal/dto` holds the structs the JSON API serializes and the future HTML
templates render, and `internal/dto/testdata/golden` holds twelve committed JSON
files that are the contract. The package imports nothing under the module path,
enforced by a test, so it cannot acquire the database's vocabulary or a cycle
with the handlers.

The shapes are the ones the documents pin and no others: the thread page
response from docs/07's abridged JSON, the forum tree, the thread list row from
docs/06's columns, and `/me`. Absent endpoints are absent because nothing
specifies them, and a golden over a guess reads as a decision.

Goldens are named `<endpoint>.<viewer class>` and regenerated with
`go test ./internal/dto -update`, documented in the package comment and in
docs/07. A field change fails with a diff naming the line that moved.

The corpus is held honest by four structural tests, all of which survive a
regeneration -- which matters, because the tempting response to a golden failure
is to rerun with `-update`. Every field of every served type must be given a
value by some fixture, or its rename would change a key nobody is looking at.
Every nullable field must appear both null and set, since the null is the half a
client gets wrong. No two goldens for one endpoint may be identical, which would
mean the viewer class distinction is fake or unexercised. And no two fields may
share a JSON tag: `encoding/json` resolves that collision by dropping both
fields and returning no error, so the pair vanishes from the API in silence.

Three further tests hold down the json/v2 risk docs/09 names. `encoding/json/v2`
turns out to be in go1.27.1 with no GOEXPERIMENT but gated on the module's go
directive, which is 1.26.2 -- so adopting it is a language bump this ticket has
no business making. Instead the three strictnesses were measured against the
real toolchain and asserted directly: no duplicate object members, valid UTF-8
with no U+FFFD, and no two fields differing only by case. Whoever raises the
directive should find them already passing.

AC1's other half is untestable until a handler exists to check for a duplicate
shape; the note says so and names what was enforced in its place.
