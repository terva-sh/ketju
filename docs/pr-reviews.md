# Targeted PR reviews

The separate `terva-review` workflow runs a pinned Terva review action. It is
advisory and does not replace the project's build, test and release checks. Request one
review when a PR is ready and again after substantive fixes; bookkeeping and
formatting changes normally need no new model run. Use follow-ups for specific
questions. No automatic review-on-push trigger is installed.

## Request a review

From this repository, use the actual PR number:

```sh
tea actions workflows dispatch terva-review.yml --ref main \
  --input pr=PR_NUMBER --input request-id=ready-review
```

An empty-JSON dispatch error may still mean the run was
created; check the Actions UI before retrying. Reuse the request ID for recovery;
change it for an intentional fresh review of the same revision.

After the workflow lands on the default branch, the pilot comment allowlist
initially admits `warricksothr` (verified repository owner). Approved maintainers
can be added through a PR changing both allowlists in the workflow. Unlisted
commenters are skipped before job setup and use unique per-run ignored groups; other
authorized maintainers can use manual dispatch. The action still checks current
repository permissions for every request. New PR discussion comments can request
work:

```text
/terva review HEAD_SHA BASE_SHA
/terva follow-up REVIEW_ID Your question.
/terva follow-up run:RUN_UUID Your question about a clean result.
```

Use full lowercase current head/base SHAs from the PR API. Review IDs come from
the reviews API; they are not the comment ID in a URL. Clean run UUIDs appear in
the maintained summary. Follow-ups require the same current revisions and profile.
Only write/admin/owner actors are accepted; forks, edited commands and inline
review replies are unsupported. A follow-up is a fresh isolated review of supplied
context, not a persistent agent conversation.

## Results and operation

Clean full reviews update one maintained summary. Findings and explicit follow-up
answers remain visible reviews. Read low-severity findings even if the workflow
passes. A medium-or-higher finding fails the configured gate while review execution
may have succeeded. Runtime or publication failure is not a clean review.

The summary retains at most 32 clean results / 240 KiB of checkpoint data; capacity
failure preserves history and does not pass the gate. Do not remove hidden markers.
Main and follow-up contexts are `terva-review/code` and `terva-follow-up/code`.
Statuses name an exact head; later commits do not inherit an earlier review.

Record the reviewed head/base, request/run/review links and finding dispositions
in the ticket (or the rollout ticket linked from the installation PR when this
repository has no store). Fix accepted findings, document evidence for disagreements, and
link deferred work. A passing model review is evidence, not merge permission.
The action only sees bounded diff/discussion; it does not execute our tests or
explore the full checkout. Keep normal CI as the deterministic validation gate.

## Trusted configuration

The workflow fetches only reviewer commit
`c8730fee5650346b17c61babd5da8fdd9982c5d3` into `.terva-review-action`; no consumer
PR code is executed with review credentials. Terva 0.137.0 Linux amd64 is checked
against its pinned SHA-256. The checkout helper is pinned to mirror commit
`d23441a48e516b6c34aea4fa41551a30e30af803` (v6.1.0). The runner requires Node >=24
and verifies it. System packages/image tags remain provisioning dependencies.
`BOT_TOKEN` supplies private reviewer checkout and publication permissions;
`CPA_API_KEY` supplies inference authentication. Only secret references belong in
source. Existing organization secrets must be available to this repository.

Provider, URL, model, thinking and profile are trusted workflow inputs. Defaults
are openai-compatible, CPA API, gpt-5.6-sol, low, and code. The `summary` policy
keeps feedback quiet. To select `always`, change the trusted workflow through
a PR; dispatch exposes only the PR number and request ID. A fresh request ID
does not change publication policy. Review pin/runtime changes through a PR, test them on the action's
fixture, and keep all publishers for this PR under the same concurrency group.
Do not revoke shared credentials or change required-check settings as part of
routine troubleshooting. Ask only for safe error codes, never full private logs.

## Declined and deferred findings

Follow the [finding disposition process](https://git.local.sothr.com/terva-sh/terva-action-code-review/src/branch/main/docs/finding-dispositions.md).
Preserve the original result and failing status. Add a separate disposition with
review/finding ID, exact head/base, evidence, decision, author and any follow-up
ticket. State that maintainer acceptance is pending until a maintainer accepts it.
Dispositions are advisory; they neither clear the model gate nor grant merge
permission. A model retraction is not maintainer acceptance. Verify that any
acceptance covers the current revisions before making a merge decision.
