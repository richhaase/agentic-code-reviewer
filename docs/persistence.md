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

Issue #196 defined the schemas. Issue #197 added the filesystem-backed store
that writes and reads these records. Issue #223 added the adjudication /
economics / loop-decision logic (`store.ResolveFindingAdjudication`,
`store.ResolveAdjudicationPolicy`) that produces and interprets
`AdjudicationRecordV1`, `ReviewEconomicsV1`, `LoopDecisionV1`, and
`AdjudicationPolicyV1` values. Issue #198 added the `acr desk history` and
`acr desk forget` commands documented below.

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
  outside the access the source repository itself already has.
- **Deletion is per pull request and irreversible.** `acr desk forget
  <owner/repo#number>` (see below) permanently removes every stored record for
  that pull request — its snapshot, runs, events, adjudications, loop
  decisions, and economics records, including any that failed to parse. There
  is no undo and no partial deletion of one record kind; forgetting a pull
  request forgets all of its local history at once. Other pull requests'
  history, and the data directory itself, are left untouched.
- **The data directory location and filesystem store are implemented by issue
  #197.** See the section below.

## Filesystem storage (issue #197)

`store.DataDir()` resolves the application-data directory: an `ACR_DATA_DIR`
environment variable override, or `os.UserCacheDir()/acr` by default. Every
write goes through `atomicWriteFile`: content is written to a hidden temporary
sibling file (`.tmp-<name>-*`) in the same directory, `fsync`'d, `chmod`'d,
then atomically renamed over the destination; the containing directory is
`fsync`'d afterward on a best-effort basis. A reader never observes a partial
write, and a stray temporary file left behind by a killed process is ignored
by every reader because its hidden name never matches the `*.json` pattern
readers scan for.

Records live under:

```text
<data-dir>/
  desk.lock
  prs/
    <host>/<owner>/<repository>/<number>/
      snapshot.json
      events/
        <timestamp>-<event-id>.json
      runs/
        <timestamp>-<run-id>.json
      adjudications/
        <timestamp>-<adjudication-id>.json
      loop_decisions/
        <timestamp>-<loop-decision-id>.json
      economics/
        <timestamp>-<run-id>.json
```

`RunStore` and `EventStore` (`internal/store/runstore.go`,
`internal/store/eventstore.go`) are append-only: `SaveRun`/`AppendEvent`
refuse to overwrite an existing record at the same path rather than silently
replacing history. `SnapshotStore` (`internal/store/snapshotstore.go`) is the
one mutable record per PR — each poll's `PRSnapshotV1` atomically replaces the
previous one — and `PRSnapshotV1.Age(now)` reports how stale a loaded snapshot
is relative to its `CapturedAt`, which is how a reader (for example `acr desk
--once` rendering a stored snapshot without refreshing it) knows the data's
age.

Listing a PR's runs or events (`ListRuns`/`ListEvents`) never fails outright
because of one bad record: each file is decoded and, for runs and events,
independently validated (`FromReviewRunSchema` / `ReviewEventV1.Validate`);
a file that fails either check is reported as a `store.CorruptRecord{Path,
Err}` alongside the still-readable history rather than aborting the whole
listing. `LoadRun` mentions how many corrupt records were also present so a
user is not left wondering whether their history is silently incomplete.

`AcquireWriteLock(dataDir)` takes an exclusive, non-blocking `flock` on
`desk.lock`; a second call while the first still holds it returns
`ErrWriterLocked` immediately instead of hanging or allowing a second writer.
Because the lock is an OS-level `flock` on the open file description, it is
automatically released if the owning process dies, so a crashed writer never
leaves a stale lock behind. Read-only access through `RunStore`, `EventStore`,
`AdjudicationStore`, `LoopDecisionStore`, `EconomicsStore`, and
`SnapshotStore` never takes this lock; only a process that intends to write —
today, only `acr desk forget` — acquires it.

`AdjudicationStore`, `LoopDecisionStore`, and `EconomicsStore`
(`internal/store/adjudicationstore.go`, `internal/store/loopdecisionstore.go`,
`internal/store/economicsstore.go`) follow the same append-only,
never-overwrite pattern as `RunStore` and `EventStore`, under
`adjudications/`, `loop_decisions/`, and `economics/` inside each pull
request's directory. `ListEconomics` returns each `ReviewEconomicsV1` paired
with the `RecordedAt` timestamp it was saved with (`EconomicsRecordV1`),
because the schema itself carries no timestamp of its own; the timestamp is
recovered from the record's filename, which every store already timestamps.

## The `acr desk` command (issue #198)

`acr desk` is the parent command for locally inspecting and managing the
persistent review workspace. It currently has two subcommands; later epic
phases add discovery and dispatch subcommands under the same parent.

### `acr desk history <owner/repo#number>`

Reads every stored run, event, adjudication, loop-decision, and economics
record for the given pull request and renders them as one chronological
timeline (`internal/desk.LoadHistory` / `internal/desk.BuildTimeline`), sorted
by each record's own timestamp (`OccurredAt`, `CompletedAt`/`StartedAt`,
`RecordedAt`, `DecidedAt`). `<owner/repo#number>` is parsed by
`internal/desk.ParsePullRequestRef`; the host is always `github.com`, the only
host this repository's GitHub integration supports.

This is a read-only command: it never acquires the write lock, so history
remains inspectable while another `acr` process (for example a long-running
`acr desk` writer, once later phases add one) owns the workspace.

Every adjudication record renders its own disposition, rationale, scope
(head/configuration fingerprint), and invalidation conditions. Because
reopening, correcting, or superseding an adjudication is always a new record
(`RelationToPrior` + `SupersedesRecordID`) rather than an edit, the timeline
naturally shows the full original → reopened → corrected chain in the order
it happened. Loop decisions render their reason and budget state, printing
`budget: unknown` rather than a fabricated zero when `BudgetStateV1.Known` is
false. Economics records render provider usage the same way: known usage
prints its token/cost figures, and usage recorded with `Known: false` prints
as `usage unknown`, never as `0`.

A pull request with no stored history renders `No stored history found for
<key>` rather than an error. A record that fails to decode or validate is
listed separately at the end of the output as an unreadable record and does
not prevent the rest of that pull request's history — or any other pull
request's history — from rendering.

### `acr desk forget <owner/repo#number>`

Permanently deletes a pull request's entire stored history:
`store.ForgetPullRequest` removes its snapshot and every run, event,
adjudication, loop-decision, and economics record (including any that are
individually corrupt), then reports what it removed. A pull request with no
stored history is reported as such and is not an error.

`forget` is a mutation: it first calls `store.AcquireWriteLock`, and if
another process already holds `desk.lock` it refuses immediately with an
error identifying the conflict, rather than deleting partial state or
blocking. It only ever removes the requested pull request's own directory;
sibling pull requests under the same owner/repository are untouched.
