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
	open     []domain.ReviewPhase
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
	if event.Kind == EventRunCompleted {
		for len(e.open) > 0 {
			e.emitLocked(Event{Kind: EventPhaseCompleted, Phase: e.open[0]})
		}
	}
	e.emitLocked(event)
}

func (e *eventEmitter) emitLocked(event Event) {
	if event.Kind == EventPhaseStarted {
		if !e.phaseOpen(event.Phase) {
			e.open = append(e.open, event.Phase)
		}
	}
	if event.Kind == EventPhaseCompleted {
		e.closePhase(event.Phase)
	}
	e.sequence++
	event.Sequence = e.sequence
	event.At = e.now()
	event.RunID = e.runID
	e.sink.HandleReviewEvent(event)
}

func (e *eventEmitter) phaseOpen(phase domain.ReviewPhase) bool {
	for _, candidate := range e.open {
		if candidate == phase {
			return true
		}
	}
	return false
}

func (e *eventEmitter) closePhase(phase domain.ReviewPhase) {
	for i, candidate := range e.open {
		if candidate == phase {
			e.open = append(e.open[:i], e.open[i+1:]...)
			return
		}
	}
}

func (e *eventEmitter) close() {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.closed = true
	e.mu.Unlock()
}
