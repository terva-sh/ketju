---
schema: 3
id: TKT-01M3V3BVPYQ3AKK9ETPNA9ZHMC
title: Drive Phase 3 to done autonomously
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area:process
assignees: []
milestone: phase-3
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:264
    path: null
  - ref: pr:271
    path: null
  - ref: pr:270
    path: null
  - ref: pr:275
    path: null
  - ref: pr:286
    path: null
claim: null
archive: null
created_at: 2026-10-01T06:45:24Z
updated_at: 2026-10-01T15:16:52Z
created_by:
  id: agent:claude-code/phase3-run-orchestrator
  name: ""
updated_by:
  id: agent:claude-code/phase3-run-orchestrator
  name: ""
extensions: {}
---

## Description

The maintainer, on 2026-10-01, after the extended Phase 2 run closed (TKT-01M3RQP0VZ): "Let's plan, file tickets, groom and promote, them and continue autonomously through phase 3 or until we hit an issue I must absolutely resolve".

docs/14 carries the grant, the scope (every `phase-3` ticket, the settings pair pulled forward from Phase 4, the profile screen, the exit check, and the drafts the run placed in Phase 3), and the stop rule. Under that rule the run settles member-visible product questions itself and records them for review. It stops only on the six classes of question docs/14 lists.

This ticket tracks the run. The orchestrator logs merges here in batches, and its summary is the report: every run decision pending review, every recorded disagreement, every criterion merged unticked, and every question the maintainer must answer.

## Acceptance criteria

- [ ] Every phase-3 ticket other than the epics and this one is done or archived, or blocked on a question docs/14 says the maintainer must answer
- [ ] Every run decision, recorded disagreement, unticked criterion and open question is listed in the summary for the maintainer
- [ ] docs/11 counts are refreshed when the run ends

## Notes

**agent:claude-code/phase3-run-orchestrator** at 2026-10-01T15:16:52Z

Orchestrator log, 2026-10-01 (covers merges #264 to #321).

- #264 merged a599b48: docs/14 grant, scope, stop rule; review 1675 fixed ("exceed"); clean on a599b48.
- #271 (live + attachments grooming) merged after a sixth review round, granted as an exception to the five-round cap because round 5's findings were narrow; round 6 clean on 6dfd01d (review 1716 medium on own-reply rendering resolved before clean comment 21517).
- #272 merged clean a15d1b2 (CI 1807).
- #270 -> #275 (search grooming): round 6 found two highs; carried onto -2 with at most two more rounds. Merged e3a018d. Review 1731's two mediums carried as binding notes on TKT-01M29CRJ3B and TKT-01M29CTY9D ("Requirement carried from review 1731"). GAP: no review result exists for head e3a018d; the orchestrator verified 64a8983..e3a018d touch only two .tickets files (the carry notes). Lanes told the carry rule (carry=true review on ticket-only heads); it is in groom-lane-tail.md.
- #278 (TKT-01M3VBWMA8 stall) merged 1b4b0df: runner b3cc8959 freezes all jobs ~10 s; ticket blocked class 3 on TKT-01M3VDYENN (runner host, outside the repo). Maintainer told in text; push notification was not delivered (Remote Control inactive).
- #280 -> #284 TKT-01M29CXK0K blob store merged d253ac0/4990dea; reviews 1732/1735/1736 fixed; clean b1542ebf on 4990dea/3890b38; CI 1889; migration 0020. Run decision pending maintainer review: the collector keeps a file with no row if a thread log names it.
- Conflict cause: the grooming brief told every lane to link its PR on this tracking ticket, so every grooming PR conflicted after each merge (#267->#274->#286, #268->#276->#285, #269->#273->#279->#282). Fixed 2026-10-01: lanes link on their own tickets; the orchestrator logs here.

- #269 -> #273 -> #279 -> #282 polish/exit grooming merged e539a3e + ccb234d. #273 review 1725 (round 6) carried onto TKT-01M3S3Z9E1; #279 clean 9d2e8885 but main moved; #282 review 1737 (medium, non-security a11y: plan contradicted the carried requirement) fixed in ticket-only commit ccb234d and carried; merged on green CI 1882 with review status failure on b534383. Orchestrator read ccb234d and confirmed it resolves 1737. TKT-01M3S3Z9GT merged into Z9E1 and archived. Filed no-milestone: TKT-01M3V7CFJ5, TKT-01M3V9DWTN. Seven run decisions in docs/06 (see lane report). Contradiction noted: site_name shown twice on the front page (wordmark + heading) -- run decision 4, pending maintainer review.
- #267 -> #274 -> #286 phone/settings/profile grooming merged (head 81632ff, clean 9ea42f36, CI 1901). Reviews 1691/1704/1734 fixed. Filed no-milestone: TKT-01M3V6XSW2 (should link/unlink/sign-out-everywhere need a recent sign-in? security question for the maintainer, blocks nothing), TKT-01M3V6XTYG, TKT-01M3V6XVVG. Six run decisions (docs/05 x2, docs/06 x3, docs/07 theme).
- MAINTAINER QUESTION (class 5, anticipated): web/client/scripts/bundle.mjs counts every lazy reader chunk except src/screens/admin/ toward the 70000 B budget, so lazy-loading saves nothing. Reader is ~60465 B with ~9.5 KB free; Phase 3 client work (SSE pill, lenses, unread, notifications, search, attachments, settings, profile, sheets, polish) is estimated well past that. Options: raise the number, or exclude non-thread-view lazy screens (settings, profile, search, notifications) from the count the way admin is. Either changes the budget, so the run does not choose. Raised with the maintainer 2026-10-01; client lanes proceed and stop only if their PR would exceed the budget.
- #266 -> #277 -> #283 -> #293 lenses grooming merged d7e3521 (clean full review on d7e3521, CI 1939). Reviews 1692/1702/1714 fixed; 1730 and 1741 (three mediums, non-security, read beacon under lenses) carried onto TKT-01M29CTY71 as binding requirements; TY71's PR must amend docs/06's read-tracking decision accordingly. Seven run decisions (docs/06, docs/07 lens grammar, docs/10 Q15). TODO in the log PR: link TKT-01M29CGBZD --depends-on TKT-01M29CTY71 (lane avoided editing the other lane's file). Note for TKT-01M29CTY7K: nothing sets users.mark_all_read_at yet; mark-all-read must update the landing page, unread divider, First unread and the unread lens together.
- #292 TKT-01M3TV4HBF merged 3f1884b (code cb1f100); clean 3447c4e5 on 1c29e88, CI 1935 (2060 vitest, 193 playwright); reader 60465->60464 B; nine race cases failed on main first; no drafts.
- #281 -> #287 TKT-01M29CRJ3X SSE endpoint merged 0da0411 (747112d, c28f98a, 0da0411); reviews 1733/1742/1744/1745/1746/1747 fixed (1746, 1747 lows); clean 1748 (b7637b18) on 7f71507 / base 6d10fe0; CI 1948. Client wiring is TKT-01M29CTYA0 (queued for the dist). Hub.Stats ready for TKT-01M29CEE7S.
- #291 TKT-01M3S3ZZQD e2e seed merged (code 90d2ff2, tip 55e3550): clean 873a4f61 on 8a0f96b; screenshots follow-up 1752 clean; carried to aa6bd74 (orchestrator checked 8a0f96b..aa6bd74 is one .tickets file); CI 1957. Unblocks TKT-01M3S3Z9E1.

- #295 TKT-01M29CCC0X settings endpoints merged (code 7d0cdf2, tip f2a4cfd); clean 679f49cd on 2195a02 / base 3f1884b, first round; CI 1962. Main moved (SSE) after review; lane merged after running race/tsc/contract/dist on a local merge with new main. Push run 1997 on f2a4cfd reads "failure" but was cancelled by the a6a37a9 push (run 1998 covers it). Type-only client change (dto.ts, dto-contract.ts), dist byte-identical. session.Info.Key replaced by Session.ID.
- #268 -> #276 -> #285 -> #294 unread/notifications grooming merged a6a37a9 (head d8e6d77) on green CI 1980 + carried failing result (past the cap). Carried as binding requirements: review 1743 (CRJ2T catch-up after dropped events; V6Y4JP e2e must not touch alice's shared session) and review 1751 (CTY7K: New badge excludes threads with no post; rewrite the docs/06 forum mark-all-read bullet). No security findings (orchestrator read 1751). Filed phase-3: TKT-01M3V6Y4JP (four list screens), TKT-01M3V6Y87D (watch/mute control). Run decisions: docs/04 notifications ranking/serving/edits, watches precedence; docs/06 read tracking and list definitions. TODO in log PR: add V6Y4JP and V6Y87D to the exit check's dependencies.
- #289 TKT-01M3V73DP6 drafts keep attachments merged 39ec336 (code 8b808d5); reviews 1749 (high+medium) and 1754 fixed; round 3 clean feb366ac on 8827424 / base 3f1884b; CI 1964; migration 0021. AC2 left unticked: a repeated hash is kept once rather than refused (run decision, docs/07 "Drafts", pending maintainer review). threads.Attachments gained Recheck.
- #299 TKT-01M29CRJ1P lens framework merged dbb697d; clean e71cf086 on 7c47e2a / base 39ec336 (first run d6f2d24d superseded); CI 1990. Unknown lens serves the full page; cache key gains resolved lens + page ordinal digest; dto.Lens gains gaps (type-only client change, dist unchanged, approved by orchestrator). Four lenses queued in one sequential lane. gate.py fixed to let a newer review row beat a superseded error.
- #290 -> #296 -> #298 -> #301 TKT-01M29CRJ3B search index merged (code c4bae05, link d80c233); reviews 1750/1753/1755 (highs fixed); clean on 2a908a5 / base 39ec336 (#301), CI 2006. Migration 0022. Criterion 9 "moved" unticked (PostsSplitOut reserved, no body yet). Run decisions (docs/04): attributed quotations and autolinks stay out of the indexed text. CTY8T must filter at serve time.
- TKT-01M3VHSYFR (Extended replay leaves stale .idx entries; a deleted/hidden/redacted post is served visible again after a crash; reproduced on main 3890b38) filed by the search lane, PLACED IN PHASE 3 by the orchestrator 2026-10-01: it breaks the docs/14 "never widen access" invariant, which is part of the exit. Not a maintainer question (.idx is a rebuildable projection). Lane grooms then fixes; exit check depends on it. Dogfood deployment runs affected code: maintainer told.
- #288 -> #302 TKT-01M29CYS9W thumbnails merged (code 7a2a1c7, link 54580e5); review 1748 (area-average resize) fixed; clean 89a7ca9c on f8c9615 / base 39ec336; CI 2010. GET /blobs/{hash}/thumb behind the blob's Authorize, shared access fixture. Run decision (docs/07 Attachments): stricter decode estimate for interlaced PNG (2x) and CMYK JPEG (1.5x).
- #297 -> #300 -> #303 TKT-01M3S3ZF6J thread head merged 73e8208 (code 701b7fe); clean on 9367d59 (#303), fcff8ef3 on #300; CI 2017; reader 60464 -> 60602 B (+138).
- #304 TKT-01M29CTY4J unread lens merged 2e937eb (code c91ebe7); clean 47bc4fc7 on b2c038e / base d80c233; CI 2022. Framework: View resolves the lens after the viewer block; resolvers take lensViewer (access + lastRead).
- #306 grooming TKT-01M3VHSYFR merged eaf54f6; clean 837a4dfa on 21c4759 / base 73e8208; CI 2033. Placed phase-3 under TKT-01M29BKBKP; exit check depends on it. Run decision (docs/03 Durability and recovery): serve discards every .idx once per data directory (marker <data>/index-writeback) so poisoned indexes rebuild -- the dogfood instance rebuilds its indexes once on the upgrade that ships the fix. Filed no-milestone TKT-01M3VRFRFW (new-post entry vs header ordering; orchestrator read it: a zeroed entry reveals nothing hidden, so it stays out of phase 3).
- #309 TKT-01M3S3ZZYA forum index heading + draft-saved note merged 0d5a4d0; review 1762 (medium: stale GET overwrote saved time) fixed with revision guard; clean 0f256cd6 on d9fd624; CI 2050; reader 60602 -> 60809 B (+207). Phone reply sheet: the draft note sits in the sheet head (footer hidden there).
- #310 TKT-01M29CTY58 author lens merged 4e2ef49 (code 14f4599); clean 632d7982 on 5117c2b / base 2e937eb; CI 2054.
- Main push CI: no push run completed between 1872 and 2080; each merge cancels the previous push run (concurrency group). PR CI is green on each head and lanes test merged trees locally; the orchestrator watches for the next completed push run.
- #311 TKT-01M29CTY8T search API merged 49d3713 (code 2dd70d5); review 1765 (medium: no-store on errors) fixed; clean 52b1c62b on df4fcfd / base eaf54f6; CI 2072. Every hit re-judged under its thread at serve time; index never trusted alone. Run decision (docs/07 Field errors): q failures use required/too_long/too_many. Offset paging may repeat one result (docs/07).
- #307 -> #313 TKT-01M29CTY9D ketju reindex --search merged ca5e50c; review 1766 (high: read errors classed as log damage) fixed; clean 83f8a4f4 on 4ca74ad / base 0d5a4d0; CI 2077. Rebuild reads logs only, never the .idx.
- #312 TKT-01M3VHSYFR fix merged 10c057d; clean 0e5e370d on 26a7c0b / base eaf54f6, security follow-up fe6a0a57 clean, carried to ticket-only c18c00f; CI 2066, 2081. Rebuilt opens were also affected (confirmed on main). Crash test failed on main in 21 cases. serve discards pre-fix .idx once (marker). AC9 unticked (CI race log names packages only). Dogfood deployment still runs the pre-fix build: maintainer told; redeploy is theirs (class 3).
- #314 TKT-01M3S3ZS0H last-read time merged c403e67; clean on 09bcd71; CI 2084; reader 60809 -> 60823 B (+14). last_read_at per-viewer; TestCachedPayloadHoldsNoViewer refuses it in cached bytes. Polish lane released the dist.
- #316 TKT-01M29CTY5T reply chain lens merged 902c30e (code 14a497d, fix a4ea728); review 1771 (medium: uint32->int bounds on 32-bit) fixed; clean 5b221e6c on 275aa00 / base 49d3713 (diff truncated; follow-up 1772 on lens_test.go clean); CI 2097. Filed no-milestone TKT-01M3VVZE4G (Index.Entry panics under GOARCH=386 on ordinal 4294967295).
- Main push run 2116 on 902c30e (first completed push run since 1872) FAILED one test: [desktop] e2e/read-tracking.spec.ts:66 (dwelt post 25 not in sent [1..5]; 6.1 s) on runner b3cc8959. Orchestrator ran the spec on 902c30e locally --repeat-each=5, both projects: 54/54 passed, 2.4-3.3 s each. Classed as the runner freeze, not a regression; add as evidence on TKT-01M3VDYENN.
- #305 -> #308 -> #315 -> #318 TKT-01M29CRJ29 watches and mutes merged (code 5288a5e, link 04224e4); all reviews clean (00e212c2, 699f0b83, 5298f40a, 2f33b69c on 989285f / base c403e67); CI 2111. Merged head's review recorded only as a PR comment (lane skipped the ticket-only commit). domain.EffectiveWatch; SQL coalesce(tw.level, fw.level, ''); ThreadList.watch optional in dto.ts until TKT-01M3V6Y87D. Codes: unknown level not_an_option, non-string wrong_type (groomed 'invalid' is not in docs/07's list).
- #317 TKT-01M3NS122F redact clears search merged 40b9c2d; review 1775 (high: Prepare emptied another version's index) fixed; clean eb95d153 on 9479285 / base 902c30e; CI 2123. Proof scans every search table incl. FTS5 shadow tables and raw ketju.db + WAL bytes; four mutations each caught. Merged head's review recorded only in the lane report.
- #319 -> #321 TKT-01M29CTY6E since/mentions/mod lenses merged ba98d3a (code 395b9cb); clean bb2c4dfc + access follow-up 1777 on #319, clean 0f41fd90 on 468c9bf / base 04224e4 (#321); CI 2131. All four lenses done.
- Orchestrator change 2026-10-01: client lanes may prepare (implement and test, no dist commit, no PR) while another lane holds the dist; the next holder rebases its unpushed branch and regenerates the dist when handed it. Queue reordered: V73J8K before Z9E1.

### Questions only the maintainer can answer (open)
- Reader bundle budget (class 5): lazy chunks count toward 70000 B (web/client/scripts/bundle.mjs); reader at 60823 B with most Phase 3 client work ahead. Raise the number, or count only the thread view.
- CI runner b3cc8959 freezes ~10 s (TKT-01M3VDYENN, class 3). The exit check depends on TKT-01M3VBWMA8, which is blocked on it, so Phase 3 cannot exit until it is fixed.
- Redeploy the dogfood instance with the TKT-01M3VHSYFR fix (class 3).
- TKT-01M3V6XSW2 (no milestone): should linking, unlinking and signing out everywhere need a recent sign-in?
