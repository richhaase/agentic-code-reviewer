package store

import (
	"encoding/json"
	"fmt"
	"path/filepath"
)

type RunStore interface {
	SaveRun(run ReviewRunV1) (string, error)
	ListRuns(key PullRequestKeyV1) ([]ReviewRunV1, []CorruptRecord, error)
	LoadRun(key PullRequestKeyV1, runID string) (ReviewRunV1, error)
}

type FilesystemRunStore struct {
	dataDir string
}

func NewFilesystemRunStore(dataDir string) *FilesystemRunStore {
	return &FilesystemRunStore{dataDir: dataDir}
}

var _ RunStore = (*FilesystemRunStore)(nil)

func (s *FilesystemRunStore) SaveRun(run ReviewRunV1) (string, error) {
	if err := validateSchemaVersion("review run", run.SchemaVersion); err != nil {
		return "", err
	}
	if run.Target.PullRequest == nil {
		return "", fmt.Errorf("review run %s: target has no pull request key; runs can only be stored for pull request reviews", run.ID)
	}
	dir, err := runsDir(s.dataDir, *run.Target.PullRequest)
	if err != nil {
		return "", err
	}
	ts := run.CompletedAt
	if ts.IsZero() {
		ts = run.StartedAt
	}
	name, err := recordFileName("review run", run.ID, ts)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)

	data, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal review run %s: %w", run.ID, err)
	}
	if err := writeNewFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("save review run %s: %w", run.ID, err)
	}
	return path, nil
}

func (s *FilesystemRunStore) ListRuns(key PullRequestKeyV1) ([]ReviewRunV1, []CorruptRecord, error) {
	dir, err := runsDir(s.dataDir, key)
	if err != nil {
		return nil, nil, err
	}
	return listRecords(dir, decodeReviewRun)
}

func (s *FilesystemRunStore) LoadRun(key PullRequestKeyV1, runID string) (ReviewRunV1, error) {
	runs, corrupt, err := s.ListRuns(key)
	if err != nil {
		return ReviewRunV1{}, err
	}
	for _, run := range runs {
		if run.ID == runID {
			return run, nil
		}
	}
	if len(corrupt) > 0 {
		return ReviewRunV1{}, fmt.Errorf("review run %s not found for %s (%d unreadable record(s) also present in history)", runID, key.String(), len(corrupt))
	}
	return ReviewRunV1{}, fmt.Errorf("review run %s not found for %s", runID, key.String())
}
