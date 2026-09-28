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
answers remain visible reviews. Read low-severity findings even when the status passes. The `terva-review/code`
status is the gate: `failure` means findings at medium or higher. The Actions
job succeeds whenever the review published, so a red job means the review did
not run or did not publish, and an `error` status on the head names the code
when the head was known. Runtime or publication failure is not a clean review.

The summary retains at most 32 clean results / 240 KiB of checkpoint data; capacity
failure preserves history and does not pass the gate. Do not remove hidden markers.
Main and follow-up contexts are `terva-review/code` and `terva-follow-up/code`.
Statuses name an exact head, and later commits do not inherit an earlier review,
except by a carry. After commits that touch only `.tickets/`, dispatch the
workflow with `carry` set to `true`: it copies the last review's result to the
head without a model run, or refuses and names the path that needs a real
review.

Record the reviewed head/base, request/run/review links and finding dispositions
in the ticket (or the rollout ticket linked from the installation PR when this
repository has no store). Fix accepted findings, document evidence for disagreements, and
link deferred work. A passing model review is evidence, not merge permission.
The action only sees bounded diff/discussion; it does not execute our tests or
explore the full checkout. Keep normal CI as the deterministic validation gate.

## Trusted configuration

The workflow runs the reviewer image
`container.local.sothr.com/terva-sh/terva-action-code-review`, release v0.5.0,
pinned by digest
`sha256:64a7ba59ca8a0932e2b4d4cf8e50a2231f46fa6eaefa046acbef84f2a2886252`. The
image carries the action's source, prompt templates, locked dependencies and a
checksum-pinned Terva 0.138.2. The job checks out nothing and installs nothing,
so no code from this repository or its PRs runs with review credentials.
`BOT_TOKEN` supplies publication permissions;
`CPA_API_KEY` supplies inference authentication. Only secret references belong in
source. Existing organization secrets must be available to this repository.

Provider, URL, model and thinking are not set here. The action has no defaults
for them; the workflow reads the terva-sh organization Actions variables
`TERVA_REVIEW_PROVIDER`, `TERVA_REVIEW_BASE_URL`, `TERVA_REVIEW_MODEL` and
`TERVA_REVIEW_THINKING`, and a run with one unset fails naming it
(`missing_provider`, `missing_provider_url`, `missing_model`,
`missing_thinking`). The organization's current choice is recorded in
[the action's installation guide](https://git.local.sothr.com/terva-sh/terva-action-code-review/src/branch/main/docs/installation.md#organization-settings),
and each review reports the model and thinking level it ran with. Variables
carry settings only: the image digest stays pinned in this workflow, because it names executable code. Profile `code`,
publication policy and fail severity are trusted workflow inputs. The `summary` policy
keeps feedback quiet. To select `always`, change the trusted workflow through
a PR; dispatch exposes only the PR number and request ID. A fresh request ID
does not change publication policy. Review pin/runtime changes through a PR, dispatch the PR's review from its own branch, and keep all publishers for this PR under the same concurrency group.
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
