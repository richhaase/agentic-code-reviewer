package main

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
)

type CycleOutcome struct {
	Kind         OutcomeKind
	LGTMBody     string
	CIDowngraded bool
}

func (o ReviewOpts) record(kind OutcomeKind) {
	if o.Outcome != nil {
		o.Outcome.Kind = kind
	}
}
