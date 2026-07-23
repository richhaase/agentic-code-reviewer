package store

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"
)

type EconomicsRecordV1 struct {
	RecordedAt time.Time
	Economics  ReviewEconomicsV1
}

type EconomicsStore interface {
	SaveEconomics(key PullRequestKeyV1, recordedAt time.Time, economics ReviewEconomicsV1) (string, error)
	ListEconomics(key PullRequestKeyV1) ([]EconomicsRecordV1, []CorruptRecord, error)
}

type FilesystemEconomicsStore struct {
	dataDir string
}

func NewFilesystemEconomicsStore(dataDir string) *FilesystemEconomicsStore {
	return &FilesystemEconomicsStore{dataDir: dataDir}
}

var _ EconomicsStore = (*FilesystemEconomicsStore)(nil)

func (s *FilesystemEconomicsStore) SaveEconomics(key PullRequestKeyV1, recordedAt time.Time, economics ReviewEconomicsV1) (string, error) {
	if err := economics.Validate(); err != nil {
		return "", err
	}
	if recordedAt.IsZero() {
		return "", fmt.Errorf("review economics %s: recorded_at is required", economics.RunID)
	}
	dir, err := economicsDir(s.dataDir, key)
	if err != nil {
		return "", err
	}
	name, err := recordFileName("review economics", economics.RunID, recordedAt)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)

	data, err := json.MarshalIndent(economics, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal review economics %s: %w", economics.RunID, err)
	}
	if err := writeNewFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("save review economics %s: %w", economics.RunID, err)
	}
	return path, nil
}

func (s *FilesystemEconomicsStore) ListEconomics(key PullRequestKeyV1) ([]EconomicsRecordV1, []CorruptRecord, error) {
	dir, err := economicsDir(s.dataDir, key)
	if err != nil {
		return nil, nil, err
	}
	timestamped, corrupt, err := listTimestampedRecords(dir, decodeReviewEconomics)
	if err != nil {
		return nil, nil, err
	}
	records := make([]EconomicsRecordV1, 0, len(timestamped))
	for _, t := range timestamped {
		records = append(records, EconomicsRecordV1{RecordedAt: t.recordedAt, Economics: t.record})
	}
	return records, corrupt, nil
}
