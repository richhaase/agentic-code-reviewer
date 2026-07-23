package desk

import (
	"sort"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/store"
)

type TimelineEntryKind string

const (
	TimelineEntryEvent        TimelineEntryKind = "event"
	TimelineEntryRun          TimelineEntryKind = "run"
	TimelineEntryAdjudication TimelineEntryKind = "adjudication"
	TimelineEntryLoopDecision TimelineEntryKind = "loop_decision"
	TimelineEntryEconomics    TimelineEntryKind = "economics"
)

type TimelineEntry struct {
	Kind         TimelineEntryKind
	Timestamp    time.Time
	Event        *store.ReviewEventV1
	Run          *store.ReviewRunV1
	Adjudication *store.AdjudicationRecordV1
	LoopDecision *store.LoopDecisionV1
	Economics    *store.EconomicsRecordV1
}

type History struct {
	PullRequest store.PullRequestKeyV1
	Entries     []TimelineEntry
	Corrupt     []store.CorruptRecord
}

func BuildTimeline(
	events []store.ReviewEventV1,
	runs []store.ReviewRunV1,
	adjudications []store.AdjudicationRecordV1,
	loopDecisions []store.LoopDecisionV1,
	economics []store.EconomicsRecordV1,
) []TimelineEntry {
	entries := make([]TimelineEntry, 0, len(events)+len(runs)+len(adjudications)+len(loopDecisions)+len(economics))

	for i := range events {
		event := events[i]
		entries = append(entries, TimelineEntry{Kind: TimelineEntryEvent, Timestamp: event.OccurredAt, Event: &event})
	}
	for i := range runs {
		run := runs[i]
		timestamp := run.CompletedAt
		if timestamp.IsZero() {
			timestamp = run.StartedAt
		}
		entries = append(entries, TimelineEntry{Kind: TimelineEntryRun, Timestamp: timestamp, Run: &run})
	}
	for i := range adjudications {
		adjudication := adjudications[i]
		entries = append(entries, TimelineEntry{Kind: TimelineEntryAdjudication, Timestamp: adjudication.RecordedAt, Adjudication: &adjudication})
	}
	for i := range loopDecisions {
		decision := loopDecisions[i]
		entries = append(entries, TimelineEntry{Kind: TimelineEntryLoopDecision, Timestamp: decision.DecidedAt, LoopDecision: &decision})
	}
	for i := range economics {
		record := economics[i]
		entries = append(entries, TimelineEntry{Kind: TimelineEntryEconomics, Timestamp: record.RecordedAt, Economics: &record})
	}

	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Timestamp.Before(entries[j].Timestamp)
	})
	return entries
}

func LoadHistory(dataDir string, key store.PullRequestKeyV1) (History, error) {
	if err := key.Validate(); err != nil {
		return History{}, err
	}

	events, eventsCorrupt, err := store.NewFilesystemEventStore(dataDir).ListEvents(key)
	if err != nil {
		return History{}, err
	}
	runs, runsCorrupt, err := store.NewFilesystemRunStore(dataDir).ListRuns(key)
	if err != nil {
		return History{}, err
	}
	adjudications, adjudicationsCorrupt, err := store.NewFilesystemAdjudicationStore(dataDir).ListAdjudications(key)
	if err != nil {
		return History{}, err
	}
	loopDecisions, loopDecisionsCorrupt, err := store.NewFilesystemLoopDecisionStore(dataDir).ListLoopDecisions(key)
	if err != nil {
		return History{}, err
	}
	economics, economicsCorrupt, err := store.NewFilesystemEconomicsStore(dataDir).ListEconomics(key)
	if err != nil {
		return History{}, err
	}

	var corrupt []store.CorruptRecord
	corrupt = append(corrupt, eventsCorrupt...)
	corrupt = append(corrupt, runsCorrupt...)
	corrupt = append(corrupt, adjudicationsCorrupt...)
	corrupt = append(corrupt, loopDecisionsCorrupt...)
	corrupt = append(corrupt, economicsCorrupt...)

	return History{
		PullRequest: key,
		Entries:     BuildTimeline(events, runs, adjudications, loopDecisions, economics),
		Corrupt:     corrupt,
	}, nil
}
