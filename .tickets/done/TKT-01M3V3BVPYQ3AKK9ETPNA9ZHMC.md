---
schema: 3
id: TKT-01M3V3BVPYQ3AKK9ETPNA9ZHMC
title: Drive Phase 3 to done autonomously
type: task
status: done
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
updated_at: 2026-10-02T10:12:30Z
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

- [x] Every phase-3 ticket other than the epics and this one is done or archived, or blocked on a question docs/14 says the maintainer must answer
- [x] Every run decision, recorded disagreement, unticked criterion and open question is listed in the summary for the maintainer
- [x] docs/11 counts are refreshed when the run ends

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

**agent:claude-code/phase3-run-orchestrator** at 2026-10-01T17:14:50Z

Orchestrator log, 2026-10-01 (merges #320 to #327).

- #320 TKT-01M29CTYA0 live SSE + new-posts pill merged 1dbf9b0; reviews 1778 (3 medium), 1779, 1780 fixed; clean ba5ec8c9 on 09ade8a / base 40b9c2d; CI 2140; reader 60823 -> 62035 B (+1212). Run decision (docs/06 Live updates): lens pages hold no live subscription.
- Main push run 2155 completed: success (first green completed push run since 1872).
- #324 TKT-01M29CTY7K cross-forum lists merged 0ed44eb (code 276ff09); clean 493c7374 on 3d69ac4 / base 1dbf9b0, first round; CI 2152; migration 0023. Benchmark (10k threads): first pages 0.5-1.2 ms, worst /unread,/new ~10 ms, MeCounts 5.4-6 ms (37 ms at 9,970 read positions); docs/04. Run decisions (docs/06): one shared read boundary (dbq.ReadBoundary) for page, unread lens, list rows and read PUT/DELETE; divider time is the mark's time when the mark moved it; forum mark writes no row for threads the site mark already read.
- #325 -> #326 TKT-01M3V73J8K composer attachments: #325 CI failed in go test -race (TestMutationsGoThroughPerm refused the e2e seed's direct GrantPermission; lane had not run -race locally); seed now grants through perm. Two medium review findings fixed, 5e23629b clean on #325; carried onto main as #326 (5d8f360 / 0ed44eb), fresh full review. Reader 62035 -> 63878 B (+1843).
- #323 -> #327 TKT-01M29CRJ2T notification generator: carried after #324 (migration renumbered 0024, MeCounts merged). Review 1781 on #323: HIGH security (a reader of the old forum could see/count a notification for a thread moved away before the projection caught up; now held until the projection reaches the move, threadlog.Reader.Moved, TestNotificationsTrailingMove) and medium (edit upgraded a read notification) both fixed with tests that fail on revert.
- #326 TKT-01M3V73J8K composer attachments merged 5d8f360 (code c133544); clean 61bb5ee5 on 5d8f360 / base 0ed44eb; CI 2169; reader 62035 -> 63878 B (+1843).
- #323 -> #327 TKT-01M29CRJ2T notification generator merged e855e1a (code d0a6213, b768b4f); reviews 1781 (high security: moved-thread notification visible before projection caught up; medium) and 1784 (medium: SQLite variable limit stalled catch-up) fixed; 1791 clean on 1675897 / base 5d8f360; CI 2178 (new non-race backlog step); migration 0024. Run decisions: threads with an unprojected move are held from list and badge; only unread notifications upgrade.
- /tmp (7.8 GB tmpfs) filled to 100% and broke every lane. The run freed its own Playwright run directories and moved lane temp files to ~/.cache/agent-scratch (run brief "Temporary files"); the maintainer then asked for /tmp to be cleaned, and 4133 of their entries older than 2026-10-01, none in use, were removed (mostly old acp-sess-* harness directories), leaving 13% used.
- Maintainer decision: TKT-01M3V6XSW2 (recent sign-in) answered "I don't think we need a recent fresh signin for any of those actions"; recorded in docs/05 and archived.

**agent:claude-code/phase3-run-orchestrator** at 2026-10-01T17:42:00Z

Maintainer decision, 2026-10-01: raise the reader bundle budget to 120000 B gzip ("Let's go with 120"), resolving the class 5 question. The reader stood at 63878 B. The remaining Phase 3 client plans were estimated to land the reader at 80 to 85 KB. 100 KB was the alternative put to the maintainer. Changed in web/client/budget.json, its test, web/client/README.md, docs/07 and docs/14.

**agent:claude-code/phase3-run-orchestrator** at 2026-10-01T20:40:18Z

Maintainer decision, 2026-10-01: the run decisions are approved ("Yes your run decisions are approved"). That covers every line marked "Decided by the run (..., pending maintainer review)" in docs/03 to docs/07 and every ticket note headed "run decision: pending maintainer review" made by this run, including those the queued client lanes reported to the maintainer before landing. The closing PR rewrites those markers as approved. Decisions made after this note still need review.

Orchestrator log, 2026-10-01 (merges #328 to #336, and the runner investigation).

- #328 -> #330 TKT-01M29CTY71 lens header and switcher merged (8e7c604, b1b9c2f, dfd23a6); reviews 1792 (medium: First unread kept unread:B above the reader) and 1793 (medium: Full thread lost position; lens header now sticky) fixed; clean 8f330c24 on c264e78 / base 367c90d; CI 2196; reader 63878 -> 66189 B (+2311). Run decisions (docs/06): header wording, divider only canonical/unread, switcher authors from visible posts, since date shows UTC day, sticky lens header; read-tracking decision amended for 1730/1741/1792. Remote branch run/cty71-lens-client (#328, closed, carried) left for end-of-run cleanup.
- #331 budget raised to 120000 B (maintainer) merged; clean on 0b55baa, CI 2193.
- #332 TKT-01M3W9VRTV notifications test race (run 2184) merged 004be7a; test-only (author's restore reaches the projection via the bus; docs/04 sets no time bound); reproduced 4/20 under load before, 60/60 after; clean 0963125a on 4ba1b6f / base 86752c4; CI 2201. No other racy Phase 3 tests in cmd/ketju.
- #333 TKT-01M3TSZHSW slow-mode phone e2e merged a3ed233; cause: ModerateDialog set its slow-mode choice once in useState initialisers, so a panel reopened before the refetch landed opened on the old value (not the runner freeze); clean 810f1d90 on 208180b / base 86752c4, carried to ticket-only 4122030 (it removed a duplicate draft filed inside the PR, never on main); CI 2215; reader 66189 -> 66220 B.
- Note from TSZHSW lane: run 1779's phone lock-test failure is a different problem (30 s budget exhausted while the server took 2-4 s per page request: runner overload); recorded in the TSZHSW cause note, not fixed.
- #334 TKT-01M3S3Z9E1 phone post header/footer merged 598f0d3; review 1797 (medium: stacked-header CSS reached the reply expander's copies) fixed; clean 46691910 on 598f0d3 / base a3ed233; CI 2222; reader 66220 -> 66354 B.
- #335 TKT-01M29CTY86 notifications UI merged 26bea62; review 1798 (medium: failed repeat mark-all re-marked cleared rows) fixed; clean 48b5b765 on 26bea62 / base 598f0d3; CI 2229; reader 66354 -> 67802 B. Run decision (docs/06 Thread list views): Older appends in place; mark-all clears marking of rows shown.
- #336 TKT-01M3V70X0F search screen merged 9978cd3; review 1799: HIGH (access invariant: retained results shown after the query was cleared; fixed in useResource, which now drops its answer on a null key) and medium (re-submit of the current address issued no request) fixed; clean d7d01e61 on 9978cd3 / base 26bea62; CI 2237; reader 67802 -> 70218 B. Run decisions (docs/07 Search): no held search answers, #N beside titles, the screen's own query errors.

- Runner b3cc8959 (forgejo-runner v13.0.0) is build-1 (tailnet 100.105.215.101): KVM, 6 vCPU, 3.76 GiB RAM, 8 GB swap on a disk partition (/dev/sda5), ~420 MB in use. Runner build-0 (v12.1.0) is build-0 (192.168.1.35): KVM, 4 vCPU, 4.9 GiB RAM, 4 GB swap, ~1 GB in use, and it also hosts Forgejo, MariaDB and a dind container.
- build-1 live sample (120 s, 2-3 concurrent jobs, one ketju Build and Test): each job ~1 GiB; MemAvailable fell to 1.8 GiB; a full memory stall in 103 of 120 seconds (short); ~160 MB swapped in and out. No 10 s freeze caught in that window. The system journal for the two freeze windows is not readable from the sothr account (not in adm).
- build-0 at the same time: load 36 on 4 vCPU, CPU pressure some avg10 95%, run queue 52-62, swapping out 16-81 MB/s, 4 concurrent jobs plus Forgejo and its DB.
- Reading: build-1's freezes most likely come from memory overcommit (several heavy jobs in 3.76 GiB, swapping to disk). build-0 is CPU-starved but slows evenly. Recommended: build-1 capacity 1 or 8 GB RAM; build-0 capacity 1-2, or move its runner off the Forgejo VM.
- Main push run 2184 failed on build-0 in TestServeGeneratesNotifications (test race after delete/restore under load); a lane is fixing it.
- #328 -> #330 TKT-01M29CTY71 lens header and switcher merged (8e7c604, b1b9c2f, dfd23a6); reviews 1792 (medium: First unread kept unread:B above the reader) and 1793 (medium: Full thread lost position; lens header now sticky) fixed; clean 8f330c24 on c264e78 / base 367c90d; CI 2196; reader 63878 -> 66189 B (+2311). Run decisions (docs/06): header wording, divider only canonical/unread, switcher authors from visible posts, since date shows UTC day, sticky lens header; read-tracking decision amended for 1730/1741/1792. Remote branch run/cty71-lens-client (#328, closed, carried) left for end-of-run cleanup.
- #331 budget raised to 120000 B (maintainer) merged; clean on 0b55baa, CI 2193.
- #332 TKT-01M3W9VRTV notifications test race (run 2184) merged 004be7a; test-only (author's restore reaches the projection via the bus; docs/04 sets no time bound); reproduced 4/20 under load before, 60/60 after; clean 0963125a on 4ba1b6f / base 86752c4; CI 2201. No other racy Phase 3 tests in cmd/ketju.

**agent:claude-code/phase3-run-orchestrator** at 2026-10-02T10:12:08Z

Orchestrator log, 2026-10-01 to 2026-10-02: merges #337 to #352. Each gate was checked on the exact merged head: CI's newest pull_request run, and the clean comment naming that head.

- #337 c0727ee6: recorded the maintainer's approval of the run decisions and logged #328 to #336.
- #338 d2b4f208, TKT-01M3V3CFMA, profile and posts screens.
  - Review 1800 found two mediums, both fixed: quote links in the history, and a failed profile request with no retry.
  - Clean on d69f2c14; CI 2249. Reader 71808 B (+1590).
  - Run decision, after the approval: the phone ordinal is plain text (docs/06 Mobile).
- #339 c9065c2a, TKT-01M3V6Y4JP, the unread, new, participated and watching lists, Mark all read, and the New badge.
  - Reviews 1801 and 1802 found two mediums, both fixed: a wipe watermark, and a marks signal.
  - Clean 3de7748a; CI 2259. Reader 72754 B (+946). AC5 unticked: its figure is the old 70000 B budget.
  - Run decision, after the approval: the front page has Mark all read from 720 px (docs/06).
- #340 0009f715, TKT-01M29CGBZD, phone page picker and lens sheets.
  - Clean on the first round; CI 2264. Reader 73762 B (+1008).
  - Run decision, after the approval: First unread in the sheet runs the thread page's own handler (docs/06 Mobile, Pages).
- #341 dd840125, TKT-01M3V6Y87D, watch and mute control.
  - Seven rounds. 1820, 1822, 1829, 1832 and 1833 were mediums about late or out-of-order saves, all fixed. 1830 was a misreading, confirmed by 1831. The orchestrator granted rounds six and seven, and seven was clean.
  - CI 2286. Reader 75237 B (+1475).
- #342 14074f85, TKT-01M29CXK4E, settings screens.
  - 1838, 1875, 1876 and 1879 were mediums about save ordering, /me against saves, and the page-size wipe, all fixed.
  - Round five, 1883, raised the medium "Isolate preference changes per Playwright project". It was recorded as a disagreement pending maintainer review, because playwright.config.ts asserts workers 1 and fullyParallel false. The PR merged at the cap on CI 2310. The remaining risk: a failed afterEach reset would leave carol at 10 per page and dark.
  - Reader 78491 B (+3254).
  - Run decision, after the approval: a session names its provider by key (docs/05).
- #343 25ad977a: the maintainer accepted the runner evidence. TKT-01M3VBWMA8 is done, TKT-01M3VDYENN stays a draft, and review 1846's medium was fixed.
- #344 de37cccb, TKT-01M3S3ZRYD, front page footer.
  - Review 1887's medium was fixed: `?` on the index failed WCAG 2.1.4, so no key is bound there.
  - CI 2316 failed with two phone timeouts on build-0, a slowdown recorded in #345; re-run 2320 was green. Reader 79006 B (+515).
  - AC2 waits on TKT-01M3V9DWTN.
  - Run decision, after the approval: no shortcut key on the index (docs/06).
- #345 b7d9e58e: a TKT-01M3VDYENN note classifying run 2316.
- #347 1fb5db0c, TKT-01M29CGBYY, manifest and icons. It replaced #346 after main moved.
  - Clean 87d17586; CI 2333. Reader 79088 B (+82).
  - AC6 is unticked for its stale figure. AC7, the real-device install, is left for a person.
- #348 628f1a6b, TKT-01M3S3ZZSZ, visual gaps T4, T10, P1, P4, M1, D2 and F9. D1 was already in docs/06.
  - Clean on the first round; CI 2339. Reader 79192 B (+104).
- #349 78a84635, TKT-01M3XNQ772, a bug the exit spec found: the unread alert link is now underlined.
  - Review 2346 clean.
  - CI 2345 failed at the 10-minute race limit on build-0, and 2348 on the build-1 freeze. The orchestrator granted a second re-run as an exception, and 2349 was green. Reader 79205 B (+13).
- #350 492405f3, TKT-01M3XP6ZRZ, a bug the exit spec found: a read beacon that lands drops the held lists.
  - Clean on the first round; CI 2352. Reader 79213 B (+8).
- #351 8ae5acf8, TKT-01M3V3CFRE, the Phase 3 exit check.
  - Review 1909's medium was fixed: an oversized upload is checked against the store.
  - Clean 706d5e5f; CI 2359. Comment 23528 quotes the step-log lines.
  - Reader 79118 B against the 120000 B budget.
- #352 bdadf65c: ticket-only close of TKT-01M3V3CFRE, with a clean review of its own.

## Summary

Phase 3 is done. Every phase-3 ticket except the epic TKT-01M29BN36D and this one is done or archived: 44 done and 1 archived (TKT-01M3S3Z9GT, merged into TKT-01M3S3Z9E1 in grooming). The exit check, TKT-01M3V3CFRE, merged in #351. Comment 23528 there quotes the merged head's step log: all five TestPhase3Exit* tests and every phase3-exit.spec.ts test passed at 390 and 1440 px. The reader bundle is 79118 B gzip, against a budget of 120000 B.

The epic TKT-01M29BN36D (Lenses, unread surfaces, search, and live updates) is left in draft. All 24 of its children are done, but its six criteria are design-level, and nobody has checked them against the code as a whole. Closing it is the maintainer's call.

### Decided by the maintainer during the run
- **Bundle budget:** raised from 70000 B to 120000 B (2026-10-01, #331).
- **Re-authentication:** no recent sign-in is needed to sign out everywhere, link an identity or remove one (2026-10-01, #329, docs/05).
- **Run decisions:** approved as they stood on 2026-10-01 (#337). The 40 doc markers merged before then now read "maintainer approved".
- **Runner evidence:** accepted (2026-10-02, #343). TKT-01M3VBWMA8 is done, and TKT-01M3VDYENN stays a draft as the record.

### Still pending maintainer review
Five run decisions were made after the approval and are still marked "pending maintainer review":
1. On the phone, the post ordinal is plain text. The permalink is under More (docs/06 Mobile, TKT-01M3V3CFMA, #338).
2. A session row names its provider by key, and a removed provider reads "No longer configured (key)" (docs/05, TKT-01M29CXK4E, #342).
3. The front page has a site-wide Mark all read from 720 px (docs/06, TKT-01M3V6Y4JP, #339).
4. First unread in the phone page sheet runs the thread page's own handler (docs/06 Mobile, Pages, TKT-01M29CGBZD, #340).
5. No shortcut key is bound on the forum index until single-key shortcuts can be turned off (docs/06, TKT-01M3S3ZRYD, #344).

One recorded disagreement: #342 merged with review 1883's medium open, "Isolate preference changes per Playwright project". The run disagreed because the suite runs one test at a time and session.ts asserts it. The alternative is a dedicated member per project, which costs about 24 s of pacing per project.

### Open questions for the maintainer
- **CI time limit.** On build-0, cmd/ketju under -race runs close to Go's default 10-minute package limit. Run 2345 took 15m16s and failed; run 2359 took 10m11s. Raising the race step's -timeout, to 20m for example, changes an existing CI step, and docs/12 "Changing CI" lets a run only add steps.
- **The runner.** TKT-01M3VDYENN records three build-1 freeze episodes (runs 1592, 1790 and 2348) and two build-0 slowdowns (runs 2316 and 2345). The recommendation still stands: give build-1 capacity 1 or 8 GB of RAM, and give build-0 capacity 1 to 2 or move its runner off the Forgejo VM.
- **Leftover branches.** Carried PRs left local and remote branches that this run did not delete, because their commits were rewritten on the way to main and could not be proven superseded one by one. Every ticket they belong to is done. The remote ones are origin/groom/p3-* (17 branches), origin/run/cty71-lens-client and origin/groom/phase-1-autonomous-run. A detached worktree also remains at ~/.cache/agent-scratch/ketju/p270.eGUW/wt.

### Unticked criteria
- **The old 70000 B figure.** These hold except for that number: TKT-01M3V6Y4JP AC5, TKT-01M29CGBYY AC6 and TKT-01M3V3CFRE AC3. #331 superseded the figure, and each ticket has a note saying so.
- **TKT-01M29CGBYY AC7.** A person must install it on a real iOS and a real Android device.
- **TKT-01M3S3ZRYD AC2.** "Press ? for keyboard shortcuts" waits on TKT-01M3V9DWTN, the off switch that WCAG 2.1.4 needs.
- **TKT-01M3VBWMA8 AC3.** It asks for runs on a changed runner, and the maintainer accepted the evidence without that.
- **TKT-01M3VHSYFR AC9.** CI's race step does not name its tests. Follow-up: TKT-01M3VRFRFW, a draft with no milestone.
- **TKT-01M3V73DP, the duplicate part of AC2.** A repeated hash is kept once rather than refused. That is a run decision in docs/07 "Drafts", approved on 2026-10-01.
- **TKT-01M29CRJ3B AC9.** It is unticked only for "moved", which that build could not produce.

### Requirements carried from review
Grooming PRs past the cap carried their non-security findings as "Requirement carried from review <id>" notes on the tickets they named. Each implementing lane read them as binding. None is left open.

### What the next run should know
- docs/14 "Amended during the run" records how the run's procedure changed: review caps, carries, the client dist queue, push CI cancellations, the runner decision and the approval.
- e2e/phase3-exit.spec.ts and cmd/ketju/phase3_exit_test.go are the Phase 3 regression net, and CI runs them by name.
