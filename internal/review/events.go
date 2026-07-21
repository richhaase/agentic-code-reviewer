package review

import (
	"sync"
	"time"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

type EventKind string

const (
	EventRunStarted        EventKind = "run_started"
	EventPhaseStarted      EventKind = "phase_started"
	EventPhaseCompleted    EventKind = "phase_completed"
	EventWarning           EventKind = "warning"
	EventReviewerStarted   EventKind = "reviewer_started"
	EventReviewerOutput    EventKind = "reviewer_output"
	EventReviewerRetrying  EventKind = "reviewer_retrying"
	EventReviewerCompleted EventKind = "reviewer_completed"
	EventRunCompleted      EventKind = "run_completed"
)

type Event struct {
	Sequence       uint64
	At             time.Time
	RunID          string
	Kind           EventKind
	Phase          domain.ReviewPhase
	Message        string
	ReviewerID     int
	AgentName      string
	ReviewerResult domain.ReviewerResult
	Status         domain.ReviewStatus
	Conclusion     domain.ReviewConclusion
}

type EventSink interface {
	HandleReviewEvent(Event)
}

type EventSinkFunc func(Event)

func (f EventSinkFunc) HandleReviewEvent(event Event) {
	f(event)
}

type eventEmitter struct {
	mu       sync.Mutex
	sink     EventSink
	now      func() time.Time
	runID    string
	sequence uint64
	closed   bool
}

func newEventEmitter(sink EventSink, now func() time.Time, runID string) *eventEmitter {
	return &eventEmitter{sink: sink, now: now, runID: runID}
}

func (e *eventEmitter) emit(event Event) {
	if e == nil || e.sink == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return
	}
	e.sequence++
	event.Sequence = e.sequence
	event.At = e.now()
	event.RunID = e.runID
	e.sink.HandleReviewEvent(event)
}

func (e *eventEmitter) close() {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.closed = true
	e.mu.Unlock()
}
