package store

import (
	"encoding/json"
	"fmt"
	"path/filepath"
)

type EventStore interface {
	AppendEvent(event ReviewEventV1) (string, error)
	ListEvents(key PullRequestKeyV1) ([]ReviewEventV1, []CorruptRecord, error)
}

type FilesystemEventStore struct {
	dataDir string
}

func NewFilesystemEventStore(dataDir string) *FilesystemEventStore {
	return &FilesystemEventStore{dataDir: dataDir}
}

var _ EventStore = (*FilesystemEventStore)(nil)

func (s *FilesystemEventStore) AppendEvent(event ReviewEventV1) (string, error) {
	if err := event.Validate(); err != nil {
		return "", err
	}
	existing, _, err := s.ListEvents(event.PullRequest)
	if err != nil {
		return "", err
	}
	for _, e := range existing {
		if e.ID == event.ID {
			return "", fmt.Errorf("review event %s already exists for %s", event.ID, event.PullRequest.String())
		}
	}
	dir, err := eventsDir(s.dataDir, event.PullRequest)
	if err != nil {
		return "", err
	}
	name, err := recordFileName("review event", event.ID, event.OccurredAt)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)

	data, err := json.MarshalIndent(event, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal review event %s: %w", event.ID, err)
	}
	if err := writeNewFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("append review event %s: %w", event.ID, err)
	}
	return path, nil
}

func (s *FilesystemEventStore) ListEvents(key PullRequestKeyV1) ([]ReviewEventV1, []CorruptRecord, error) {
	dir, err := eventsDir(s.dataDir, key)
	if err != nil {
		return nil, nil, err
	}
	return listRecords(dir, decodeReviewEvent)
}
