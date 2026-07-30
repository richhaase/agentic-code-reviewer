package main

import (
	"github.com/richhaase/agentic-code-reviewer/internal/github"
	"github.com/richhaase/agentic-code-reviewer/internal/watch"
)

type OutcomeKind int

const (
	OutcomeNone OutcomeKind = iota
	OutcomeNoChanges
	OutcomeFindings
	OutcomeLGTMApproved
	OutcomeLGTMComment
	OutcomeLGTMDeclined
	OutcomeLGTMSkipped
	OutcomeStaleHead
	OutcomeReviewComment
	OutcomeReviewRequestChanges
	OutcomeReviewSkipped
)

type CycleOutcome struct {
	Kind             OutcomeKind
	LGTMBody         string
	CIDowngraded     bool
	OwnDiscussionIDs []watch.DiscussionID
}

func (o ReviewOpts) record(kind OutcomeKind) {
	if o.Outcome != nil {
		o.Outcome.Kind = kind
	}
}

func (o ReviewOpts) recordDiscussion(id github.DiscussionID) {
	if o.Outcome == nil || id.ID == 0 {
		return
	}
	o.Outcome.OwnDiscussionIDs = append(o.Outcome.OwnDiscussionIDs, watch.DiscussionID{
		Kind: id.Kind,
		ID:   id.ID,
	})
}
