package store

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestConcurrentWriterAndReader_NoRaceAndNoTornReads(t *testing.T) {
	dir := t.TempDir()
	key := testPullRequestKey()

	writeLock, err := AcquireWriteLock(dir)
	if err != nil {
		t.Fatalf("acquire write lock: %v", err)
	}
	defer writeLock.Release()

	runStore := NewFilesystemRunStore(dir)
	eventStore := NewFilesystemEventStore(dir)

	const writes = 40
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		base := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
		for i := 0; i < writes; i++ {
			run := buildTestReviewRunSchema(t, fmt.Sprintf("run-race-%d", i), base.Add(time.Duration(i)*time.Second))
			if _, err := runStore.SaveRun(run); err != nil {
				t.Errorf("save run %d: %v", i, err)
				return
			}
			event := testReviewEvent(fmt.Sprintf("event-race-%d", i), base.Add(time.Duration(i)*time.Second))
			if _, err := eventStore.AppendEvent(event); err != nil {
				t.Errorf("append event %d: %v", i, err)
				return
			}
		}
	}()

	readerStore := NewFilesystemRunStore(dir)
	readerEvents := NewFilesystemEventStore(dir)
	stop := make(chan struct{})
	var readerWG sync.WaitGroup
	readerWG.Add(2)
	go func() {
		defer readerWG.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			runs, corrupt, err := readerStore.ListRuns(key)
			if err != nil {
				t.Errorf("concurrent list runs: %v", err)
				return
			}
			if len(corrupt) != 0 {
				t.Errorf("concurrent reader observed corrupt runs: %+v", corrupt)
				return
			}
			for _, run := range runs {
				if run.ID == "" {
					t.Errorf("concurrent reader observed a torn run record")
					return
				}
			}
		}
	}()
	go func() {
		defer readerWG.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			events, corrupt, err := readerEvents.ListEvents(key)
			if err != nil {
				t.Errorf("concurrent list events: %v", err)
				return
			}
			if len(corrupt) != 0 {
				t.Errorf("concurrent reader observed corrupt events: %+v", corrupt)
				return
			}
			for _, event := range events {
				if event.ID == "" {
					t.Errorf("concurrent reader observed a torn event record")
					return
				}
			}
		}
	}()

	wg.Wait()
	close(stop)
	readerWG.Wait()

	runs, corrupt, err := readerStore.ListRuns(key)
	if err != nil {
		t.Fatalf("final list runs: %v", err)
	}
	if len(corrupt) != 0 {
		t.Fatalf("expected no corrupt runs, got %+v", corrupt)
	}
	if len(runs) != writes {
		t.Fatalf("expected %d runs, got %d", writes, len(runs))
	}
}

func TestSecondWriterRefusesWhileFirstHoldsLock(t *testing.T) {
	dir := t.TempDir()

	firstWriter, err := AcquireWriteLock(dir)
	if err != nil {
		t.Fatalf("first writer acquire: %v", err)
	}
	defer firstWriter.Release()

	readerStore := NewFilesystemRunStore(dir)
	if _, _, err := readerStore.ListRuns(testPullRequestKey()); err != nil {
		t.Fatalf("read-only access must not require the writer lock: %v", err)
	}

	if _, err := AcquireWriteLock(dir); err == nil {
		t.Fatal("expected a second writer to refuse cleanly while the first holds the lock")
	}
}
