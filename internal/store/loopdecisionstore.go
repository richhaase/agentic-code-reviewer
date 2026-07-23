package store

import (
	"encoding/json"
	"fmt"
	"path/filepath"
)

type LoopDecisionStore interface {
	SaveLoopDecision(decision LoopDecisionV1) (string, error)
	ListLoopDecisions(key PullRequestKeyV1) ([]LoopDecisionV1, []CorruptRecord, error)
}

type FilesystemLoopDecisionStore struct {
	dataDir string
}

func NewFilesystemLoopDecisionStore(dataDir string) *FilesystemLoopDecisionStore {
	return &FilesystemLoopDecisionStore{dataDir: dataDir}
}

var _ LoopDecisionStore = (*FilesystemLoopDecisionStore)(nil)

func (s *FilesystemLoopDecisionStore) SaveLoopDecision(decision LoopDecisionV1) (string, error) {
	if err := decision.Validate(); err != nil {
		return "", err
	}
	existing, _, err := s.ListLoopDecisions(decision.PullRequest)
	if err != nil {
		return "", err
	}
	for _, e := range existing {
		if e.ID == decision.ID {
			return "", fmt.Errorf("loop decision %s already exists for %s", decision.ID, decision.PullRequest.String())
		}
	}
	dir, err := loopDecisionsDir(s.dataDir, decision.PullRequest)
	if err != nil {
		return "", err
	}
	name, err := recordFileName("loop decision", decision.ID, decision.DecidedAt)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)

	data, err := json.MarshalIndent(decision, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal loop decision %s: %w", decision.ID, err)
	}
	if err := writeNewFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("save loop decision %s: %w", decision.ID, err)
	}
	return path, nil
}

func (s *FilesystemLoopDecisionStore) ListLoopDecisions(key PullRequestKeyV1) ([]LoopDecisionV1, []CorruptRecord, error) {
	dir, err := loopDecisionsDir(s.dataDir, key)
	if err != nil {
		return nil, nil, err
	}
	return listRecords(dir, decodeLoopDecision)
}
