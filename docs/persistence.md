# Persistence schema (internal/store)

This document describes the on-disk record shapes ACR's persistent review
workspace (epic #191, "Persistent Review Workspace") uses to store pull
request snapshots, review runs, review history events, and the adjudication /
review-economics / loop-decision records owned by issue #223. The Go types
live in `internal/store` and are deliberately separate from the in-memory
types in `internal/domain`: a stored record's shape is a durability contract
that must survive changes to how ACR represents state in memory, so every
stored type has its own DTO and an explicit mapping function
(`ToXSchema`/`FromXSchema` or `ToDomain`) rather than being serialized
directly.

This issue (#196) defines the schemas only. The filesystem-backed store that
writes and reads these records, the `acr desk` command surface, and the
actual adjudication/economics/loop-decision logic that produces
`AdjudicationRecordV1`, `ReviewEconomicsV1`, and `LoopDecisionV1` values are
later work (issues #197, #198, #223).

## Versioning

Every top-level record carries a `schema_version` field. The package exposes
a single `store.CurrentSchemaVersion` for the whole schema family; decoding a
record whose `schema_version` does not exactly match the version this build
supports fails explicitly with an error naming the record kind and both
version numbers. There is no silent best-effort decoding of an unsupported
version and no implicit migration. When the schema needs to change in an
incompatible way, `CurrentSchemaVersion` increments and old records are read
by a version-specific decoder added at that time, not guessed at from the new
one.

## Record kinds

- **`PRSnapshotV1`** — a timestamped, immutable observation of GitHub pull
  request state (state, draft flag, head/base object IDs, review requests,
  latest reviews, check-rollup state, merge state). Has no `internal/domain`
  counterpart yet; the discovery/workspace phase that produces and consumes
  it is later work in this epic.
- **`ReviewRunV1`** — the complete typed output of one ACR execution, mapped
  from and to `domain.ReviewRun` via `store.ToReviewRunSchema` /
  `store.FromReviewRunSchema`. Preserves reviewer identities and the
  configuration fingerprint, the pre-filter summary, exact-match aggregation,
  the false-positive and exclude-filter dispositions, final findings and
  informational results, and the terminal status/conclusion/failure. A
  successful, failed, or interrupted run's original outcome is never
  rewritten in place: `RunLifecycleV1` records later desk-level observations
  (the run became `stale` because the head moved, or was `superseded` by a
  later run) alongside the original record, not instead of it.
- **`ReviewEventV1`** — an append-only entry in a pull request's local
  history. `ReviewEventTypeV1` enumerates the full event vocabulary from epic
  #191's Core Domain Model: PR discovery/refresh; review queued, started,
  completed, failed, interrupted, superseded, and stale; finding selected,
  dismissed, and posted; comment/request-changes/approval actions posted; PR
  closed/merged; and user deferred/snoozed/retried/resolved actions. Events
  are immutable; a correction is always a new event, never an edit.
- **`AdjudicationRecordV1`** (issue #223) — a durable, additive decision
  record for a finding or finding cluster, with the disposition vocabulary
  `fixed`, `false_positive`, `duplicate`, `accepted_risk`, `policy_decision`,
  `deferred`, `obsolete`; the deciding actor; rationale and evidence; PR/head/
  configuration scope; and invalidation conditions. Reopening, correcting, or
  superseding a decision is always a new record referencing the one it acts
  on (`RelationToPrior` + `SupersedesRecordID`); the original record is never
  mutated.
- **`ReviewEconomicsV1`** (issue #223) — reviewer/model call counts, duration,
  and provider usage/cost for a run. `ProviderUsageV1.Known` distinguishes
  genuinely unavailable usage/cost data from a measured zero; validation
  rejects a record that claims `Known: false` while also carrying nonzero
  measurements, so "unknown" can never be silently reinterpreted as "zero."
- **`LoopDecisionV1`** (issue #223) — one continue/stop/escalate decision from
  the review convergence loop, with its reason, iteration counters, budget
  state (`BudgetStateV1`, with the same known/unknown distinction as provider
  usage), and the adjudication records that informed it.
- **`AdjudicationPolicyV1`** (issue #223) — the budget policy, stop policy,
  and evaluation guidance used by the convergence loop. `Source` is a
  `PolicySourceV1`, which mirrors `config.SourceIdentity` — the exact trust
  mechanism issue #220 established for resolving `.acr.yaml` — rather than
  inventing a second trust boundary. `PolicySourceV1.Validate` rejects a raw
  filesystem source outright (it resolves relative to whatever directory a
  caller passes at run time, which could be a reviewed PR's own worktree),
  and `store.ValidatePolicySourceOutsideReview` additionally rejects a pinned
  repository-revision source whose revision equals the head of the PR under
  review. Both checks exist so a reviewed PR cannot supply or alter its own
  adjudication memory, budget policy, stop policy, or evaluation guidance.

## Retention and sensitivity

Stored records are local application data, not a general audit log intended
for sharing. In particular:

- **No full diffs and no raw agent transcripts are persisted by default.**
  `ReviewRunV1` stores the structured findings, dispositions, and summarizer/
  false-positive-filter outcomes produced during a run, not the diff that was
  reviewed or the raw stdout/stderr of the underlying LLM CLI invocations.
- **Findings may contain code excerpts.** Reviewer output (`FindingV1.Text`,
  `FindingGroupV1.Messages`/`Summary`, adjudication `Rationale`/`Evidence`)
  can quote snippets of the reviewed code. Treat the data directory as
  containing source-derived content: do not assume it is safe to share
  outside the access the source repository itself already has, and provide a
  deletion path (an `acr desk forget <owner/repo#number>`-style command,
  added in a later issue) so a user can remove a PR's stored history.
  Deletion applies to the record kinds above; it is not yet implemented by
  this issue.
- **The data directory location is out of scope for this issue.** It is
  established by issue #197 using the standard library's user-data-directory
  resolution plus an `ACR_DATA_DIR` override, following the same
  flags-over-env-vars-over-config-over-defaults precedence style already used
  elsewhere in this repo.
