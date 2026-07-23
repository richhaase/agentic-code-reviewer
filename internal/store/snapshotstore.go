package store

import (
	"encoding/json"
	"fmt"
	"os"
)

type SnapshotStore interface {
	SaveSnapshot(snapshot PRSnapshotV1) error
	LoadSnapshot(key PullRequestKeyV1) (PRSnapshotV1, error)
}

type FilesystemSnapshotStore struct {
	dataDir string
}

func NewFilesystemSnapshotStore(dataDir string) *FilesystemSnapshotStore {
	return &FilesystemSnapshotStore{dataDir: dataDir}
}

var _ SnapshotStore = (*FilesystemSnapshotStore)(nil)

func (s *FilesystemSnapshotStore) SaveSnapshot(snapshot PRSnapshotV1) error {
	if err := snapshot.Validate(); err != nil {
		return err
	}
	path, err := snapshotFilePath(s.dataDir, snapshot.PullRequest)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal pull request snapshot: %w", err)
	}
	if err := atomicWriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("save pull request snapshot for %s: %w", snapshot.PullRequest.String(), err)
	}
	return nil
}

func (s *FilesystemSnapshotStore) LoadSnapshot(key PullRequestKeyV1) (PRSnapshotV1, error) {
	path, err := snapshotFilePath(s.dataDir, key)
	if err != nil {
		return PRSnapshotV1{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return PRSnapshotV1{}, fmt.Errorf("no stored snapshot for %s: %w", key.String(), err)
		}
		return PRSnapshotV1{}, fmt.Errorf("read pull request snapshot for %s: %w", key.String(), err)
	}
	snapshot, err := decodePRSnapshot(data)
	if err != nil {
		return PRSnapshotV1{}, fmt.Errorf("stored snapshot for %s is corrupt: %w", key.String(), err)
	}
	return snapshot, nil
}
