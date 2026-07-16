package main

// OutcomeKind classifies what a review cycle produced and what was posted.
// Watch mode uses it to decide whether a cycle was terminal; one-shot review
// runs leave ReviewOpts.Outcome nil and never record.
type OutcomeKind int

const (
	// OutcomeNone means nothing was recorded (cycle errored before a result).
	OutcomeNone OutcomeKind = iota
	// OutcomeNoChanges means the diff was empty; nothing was reviewed.
	OutcomeNoChanges
	// OutcomeFindings means the review produced findings (posted or handled
	// interactively, including a user skip).
	OutcomeFindings
	// OutcomeLGTMApproved means an LGTM was posted as an approval.
	OutcomeLGTMApproved
	// OutcomeLGTMComment means an LGTM was posted as a comment review.
	OutcomeLGTMComment
	// OutcomeLGTMDeclined means the user chose not to post an LGTM result.
	OutcomeLGTMDeclined
	// OutcomeLGTMSkipped means an LGTM result could not be posted (e.g. no PR).
	OutcomeLGTMSkipped
	// OutcomeStaleHead means nothing was posted because the PR head moved
	// while the review ran; the result no longer describes the PR.
	OutcomeStaleHead
)

// CycleOutcome records what a single review cycle did, for the watch loop.
type CycleOutcome struct {
	Kind OutcomeKind
	// LGTMBody is the rendered LGTM review body, kept so watch mode can post
	// the approval later once CI goes green.
	LGTMBody string
	// CIDowngraded is set when an intended approval was posted as a comment
	// because CI was not green.
	CIDowngraded bool
}

// record stores kind into the outcome sink, if one is attached.
func (o ReviewOpts) record(kind OutcomeKind) {
	if o.Outcome != nil {
		o.Outcome.Kind = kind
	}
}
