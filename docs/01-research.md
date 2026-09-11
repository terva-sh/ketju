# 01 — Research Findings

Verified against upstream repositories, pkg.go.dev, and the npm registry on
2026-09-10. Each section ends with the decision it drove.

## A. Pure-Go storage engines

| Engine | Latest | License | Character | Fit |
|--------|--------|---------|-----------|-----|
| `go.etcd.io/bbolt` | v1.5.0 (Jun 2026) | MIT | B+tree, single file, single writer, frozen format | good for sessions/caches; poor for relational queries |
| `dgraph-io/badger/v4` | v4.9.6 (Aug 2026) | Apache-2.0 | LSM + value log, hundreds of MB RAM by default, manual value-log GC | overkill |
| `cockroachdb/pebble/v2` | v2.1.7 (Aug 2026) | BSD-3 | RocksDB-class LSM, builds without cgo via klauspost zstd, no transactions, no API stability promise | overkill |
| `modernc.org/sqlite` | v1.58.0 (Sep 2026), SQLite 3.53.4 | BSD-3 | C transpiled to Go; FTS5/JSON1/RTREE built in; ~1.5–3× slower than cgo on writes | solid fallback |
| `ncruces/go-sqlite3` | v0.35.4 (Sep 2026), SQLite 3.53.4 | MIT | SQLite → Wasm → ahead-of-time Go (wasm2go); no runtime interpreter; pure-Go VFS with OFD locks; FTS5 as `ext/fts5` import; Go tokenizers | **recommended** |
| `blevesearch/bleve/v2` | v2.6.1 (Aug 2026) | Apache-2.0 | scorch backend is pure Go (zapx + bbolt); vectors need FAISS (skip) | fallback for search if FTS5 is outgrown |

Benchmarks (cvilsmeier go-sqlite-bench, Mar 2026; lbe read benchmark, Feb
2026) put ncruces 2–3× ahead of modernc on prepared-statement reads and
competitive on concurrent reads; both are within a small constant factor of
cgo `mattn/go-sqlite3`, which is irrelevant at forum scale. Pin ncruces ≥
v0.35.3 (a Windows WAL bug fix). Cap the pool at 4–8 connections and use
`BEGIN IMMEDIATE` for writes to avoid `SQLITE_BUSY` surprises.

Append-only helpers surveyed (`tidwall/wal`, `rosedblabs/wal`,
`hashicorp/raft-wal`, `edsrzf/mmap-go`): none offers the fixed-width
ordinal index with in-place revision pointers we want, and the framing code
is small. We write our own (03).

**Decision.** SQLite via `ncruces/go-sqlite3` in WAL mode for all
authoritative and projection tables; **FTS5 in the same database** for
search (transactionally consistent, one file to back up); no KV store.
`modernc.org/sqlite` is a drop-in substitute if ncruces' pre-1.0 versioning
becomes a problem, since both speak the same SQL and FTS5.

Sources: https://github.com/etcd-io/bbolt/releases ·
https://github.com/dgraph-io/badger/releases ·
https://github.com/cockroachdb/pebble/releases ·
https://gitlab.com/cznic/sqlite/-/tags · https://github.com/ncruces/go-sqlite3/releases ·
https://github.com/ncruces/go-sqlite3/blob/main/vfs/README.md ·
https://github.com/ncruces/go-sqlite3/blob/main/ext/README.md ·
https://github.com/cvilsmeier/go-sqlite-bench · https://github.com/lbe/sqlite-read-benchmark ·
https://github.com/blevesearch/bleve/releases

## B. Flat vs. tree, and what "linear with lenses" borrows

**Arguments for flat.** Atwood's "Web Discussions: Flat by Design" (2012)
lists the tree failure modes: new replies appear anywhere so returning
readers cannot find what is new; deep nesting pushes content off-screen;
indentation creates false intimacy; collapsing is labor pushed onto readers.
Discourse staff state that linear order is load-bearing for their read
tracking. NodeBB: "chose a flat topic structure long ago and this is unlikely
to change; the replies expanding list below a post is nothing more than a
convenience." MetaFilter's community: flat "reads much better because users
aren't composing for threads" — people write to the room. Something Awful is
the decades-long proof that flat + strong moderation works.

**Arguments for trees.** Sub-conversations stay legible; branches can be
pruned. Aragón, Gómez & Kaltenbrunner (ICWSM 2017) measured Menéame's switch
from linear to threaded: a significant increase in *reciprocity*
(reply-to-reply exchanges). No study found shows trees improve
thoughtfulness; educational-board comparisons found no quality difference.
Slack's own design retrospective: multi-level threads were "very
complicated", one level fixed comprehension, in-channel replies caused
double reading.

**Techniques that expose structure without a tree** (all adopted in 06):
reply-to indicator with inline in-place expansion, suppressed when the
parent is directly above (Discourse); quote backlinks (phpBB/SA); per-user
filter and "only OP"; "N new since last visit" divider (MetaFilter,
LessWrong); jump-to-date/timeline scrubber (Discourse 1.6); moderator
split/merge with bidirectional notices; "reply as new thread". Discourse's
score-based summary depends on like volume a small community will not
produce — deferred.

**Read tracking.** phpBB tracks a *time* per (user, topic/forum), which
marks a topic read on open. Discourse tracks a *position*
(`last_read_post_number`) plus per-post screen time, and that single integer
drives unread counts, landing position, and the scrubber marker. Position
tracking is incompatible with trees unless every post is tracked
individually — the mechanical reason Discourse never nested.

**Moderation primitives small forums use.** Flags → one review queue with
auto-hide at a threshold (Discourse default 3); soft delete with recover and
a rare permanent delete; public edit history with a grace window; lock,
pin, archive; slow mode; split/merge (the most used flat-forum tool);
approval of new accounts / first posts. Trust levels and downvotes: not
needed for a community of known people.

**Incumbents' ops burden.** Discourse: Rails + Sidekiq + Postgres + Redis,
2 GB RAM minimum. Flarum: PHP + MySQL + Mithril, extension breakage on major
upgrades. NodeBB: Node + MongoDB/Redis. Lemmy: Rust + Postgres `ltree`
trees, federation queues, orphaned comments on paginated fetch. All need 2–4
external processes to serve fifty people.

**Decision.** Linear thread, paged, position-based read tracking, reply-to
inline expansion with the suppress-directly-below rule, split/merge as a
first-class moderator tool, per-user and reply-chain lenses. No indentation
at any depth. No votes.

Sources: https://blog.codinghorror.com/web-discussions-flat-by-design/ ·
https://meta.discourse.org/t/prevent-reply-to-post-showing-as-a-reply-to-topic/261352 ·
https://community.nodebb.org/post/74071 · https://ask.metafilter.com/312778 ·
https://ojs.aaai.org/index.php/ICWSM/article/view/14880 ·
https://slack.design/articles/threads-in-slack-a-long-design-journey-part-2-of-2/ ·
https://meta.discourse.org/t/change-right-gutter-to-vertical-timeline-topic-controls/44096 ·
https://meta.discourse.org/t/how-does-post-tracking-work-in-discourse/115790 ·
https://www.phpbb.com/community/viewtopic.php?t=1502015 ·
https://meta.discourse.org/t/moving-posts-to-a-new-or-existing-topic/136733 ·
https://meta.discourse.org/t/a-decade-of-forum-advice-from-something-awful/21391 ·
https://github.com/LemmyNet/lemmy/issues/2664 · https://docs.nodebb.org/development/database-structure/

## C. OIDC, sessions, CSRF in Go

| Library | Latest | Notes |
|---------|--------|-------|
| `coreos/go-oidc/v3` | v3.21.0 (Sep 2026), Apache-2.0 | discovery, JWKS caching, ID-token verification, back-channel logout validation; small |
| `golang.org/x/oauth2` | v0.37.0 (Aug 2026), BSD-3 | auth-code flow, PKCE helpers (`GenerateVerifier`, `S256ChallengeOption`), refresh |
| `zitadel/oidc/v3` | v3.49.6 (Sep 2026), Apache-2.0, Go ≥ 1.25 | certified RP+OP, RP-initiated logout; larger surface, v4 in flight |
| `alexedwards/scs/v2` | v2.9.0 (2025), MIT | server-side sessions, SQLite store, `RenewToken`, hashed tokens at rest |
| `gorilla/sessions` | v1.4.0 | marked discontinued/inactive again; avoid |
| `net/http.CrossOriginProtection` | Go 1.25+ | Fetch-Metadata based CSRF; use as outer middleware with `SameSite=Lax` |

Patterns from Gitea/Forgejo, Immich, Miniflux, Discourse: providers are
admin-configured (allowlist); identity key is `(issuer, sub)`, never email;
email auto-linking is opt-in and warned against; approval is a `status`
column checked at session creation *including on the OIDC callback* (Gitea
shipped a bypass, fixed in PR #24035); bootstrap is first-user-is-admin, an
env/CLI-created admin, or an IdP claim. Discourse invite paths once bypassed
approval (GHSA-x7jh-mx5q-6f9q) — any future invite mode must go through the
same gate.

Go standard library as of 1.27 (Aug 2026): `ServeMux` method+wildcard
patterns (1.22), `CrossOriginProtection` (1.25), `slog.NewMultiHandler`
(1.26), generic methods and `encoding/json/v2` as the default JSON
implementation (1.27 — rejects duplicate keys and invalid UTF-8; test
fixtures accordingly), a new `uuid` package (1.27). A third-party router is
unnecessary.

**Decision.** `go-oidc/v3` + `x/oauth2` for the relying party, one
`*oidc.Provider` per configured issuer. Server-side sessions in SQLite
(either `scs/v2` or ~200 lines of our own — decide at implementation time;
05 specifies the behavior either must have). `CrossOriginProtection` +
`SameSite=Lax`, no CSRF tokens. Target Go 1.26 minimum, 1.27 preferred.

Sources: https://pkg.go.dev/github.com/coreos/go-oidc/v3/oidc ·
https://pkg.go.dev/golang.org/x/oauth2 · https://github.com/zitadel/oidc/releases ·
https://pkg.go.dev/github.com/alexedwards/scs/v2 · https://endoflife.date/gorilla ·
https://pkg.go.dev/net/http#CrossOriginProtection · https://go.dev/doc/go1.27 ·
https://docs.gitea.com/administration/config-cheat-sheet · https://github.com/go-gitea/gitea/pull/24035 ·
https://docs.immich.app/administration/oauth/ · https://miniflux.app/docs/configuration.html ·
https://meta.discourse.org/t/how-to-configure-approval-based-sign-ups/112128

## D. Frontend tooling

| Piece | Version | Notes |
|-------|---------|-------|
| Preact | 10.29.8 stable; 11.0.0-rc.2 | start on 10.x, migration documented as straightforward |
| `@preact/signals` | 2.11.2 | |
| `preact-iso` | 2.12.2 | recommended router: `LocationProvider`, `Router`, `lazy`, `prerender`, `hydrate` |
| Vite | 8.3.0 (Rolldown + Oxc since 8.0, Mar 2026) | Node ≥ 20.19 / 22.12 |
| `a-h/templ` | v0.3.1020 (May 2026) | type-checked Go components; candidate for the non-JS client |
| htmx | 2.0.10 `latest`; 4.0.0 released Aug 2026 | for progressive enhancement of the non-JS client |
| `yuin/goldmark/v2` | v2.0.2 (Sep 2026) | GFM, footnotes, typographer; raw HTML off by default |
| `microcosm-cc/bluemonday` | v1.0.27 | `UGCPolicy` after goldmark, always |

Embedding pattern: `//go:embed all:dist` → `fs.Sub` → file server with an
SPA fallback for non-`/api` paths, immutable cache headers on hashed
`/assets/*`; Vite `server.proxy` to the Go server in dev. Discourse's no-JS
"crawler view" is user-agent sniffed and humans with JS off get a broken
shell — a cautionary example; we do real progressive enhancement by keeping
all logic in a service layer both handler stacks call.

Markdown: the client-side JS parsers (markdown-it, micromark, marked) all
drift from goldmark. Compiling goldmark to Wasm costs 2–3 MB. **Decision:**
server renders only; preview via a debounced `POST /api/render`.

Sources: https://registry.npmjs.org/preact/latest · https://preactjs.com/guide/v10/preact-iso/ ·
https://vite.dev/blog/announcing-vite8 · https://github.com/a-h/templ/releases ·
https://four.htmx.org/announcements/2026-08-28-htmx-4.0.0-is-released ·
https://pkg.go.dev/github.com/yuin/goldmark/v2 · https://pkg.go.dev/github.com/microcosm-cc/bluemonday ·
https://meta.discourse.org/t/broken-crawler-view-when-you-disable-javascript/226365
