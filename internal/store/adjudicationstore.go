package store

import (
	"encoding/json"
	"fmt"
	"path/filepath"
)

type AdjudicationStore interface {
	SaveAdjudication(record AdjudicationRecordV1) (string, error)
	ListAdjudications(key PullRequestKeyV1) ([]AdjudicationRecordV1, []CorruptRecord, error)
}

type FilesystemAdjudicationStore struct {
	dataDir string
}

func NewFilesystemAdjudicationStore(dataDir string) *FilesystemAdjudicationStore {
	return &FilesystemAdjudicationStore{dataDir: dataDir}
}

var _ AdjudicationStore = (*FilesystemAdjudicationStore)(nil)

func (s *FilesystemAdjudicationStore) SaveAdjudication(record AdjudicationRecordV1) (string, error) {
	if err := record.Validate(); err != nil {
		return "", err
	}
	existing, _, err := s.ListAdjudications(record.Scope.PullRequest)
	if err != nil {
		return "", err
	}
	for _, e := range existing {
		if e.ID == record.ID {
			return "", fmt.Errorf("adjudication record %s already exists for %s", record.ID, record.Scope.PullRequest.String())
		}
	}
	dir, err := adjudicationsDir(s.dataDir, record.Scope.PullRequest)
	if err != nil {
		return "", err
	}
	name, err := recordFileName("adjudication record", record.ID, record.RecordedAt)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)

	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal adjudication record %s: %w", record.ID, err)
	}
	if err := writeNewFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("save adjudication record %s: %w", record.ID, err)
	}
	return path, nil
}

func (s *FilesystemAdjudicationStore) ListAdjudications(key PullRequestKeyV1) ([]AdjudicationRecordV1, []CorruptRecord, error) {
	dir, err := adjudicationsDir(s.dataDir, key)
	if err != nil {
		return nil, nil, err
	}
	return listRecords(dir, decodeAdjudicationRecord)
}
