package store

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	prsDirName       = "prs"
	runsDirName      = "runs"
	eventsDirName    = "events"
	snapshotFileName = "snapshot.json"
)

func pullRequestDir(dataDir string, key PullRequestKeyV1) (string, error) {
	if err := key.Validate(); err != nil {
		return "", fmt.Errorf("pull request key: %w", err)
	}
	return filepath.Join(dataDir, prsDirName, key.Host, key.Owner, key.Repository, strconv.Itoa(key.Number)), nil
}

func runsDir(dataDir string, key PullRequestKeyV1) (string, error) {
	prDir, err := pullRequestDir(dataDir, key)
	if err != nil {
		return "", err
	}
	return filepath.Join(prDir, runsDirName), nil
}

func eventsDir(dataDir string, key PullRequestKeyV1) (string, error) {
	prDir, err := pullRequestDir(dataDir, key)
	if err != nil {
		return "", err
	}
	return filepath.Join(prDir, eventsDirName), nil
}

func snapshotFilePath(dataDir string, key PullRequestKeyV1) (string, error) {
	prDir, err := pullRequestDir(dataDir, key)
	if err != nil {
		return "", err
	}
	return filepath.Join(prDir, snapshotFileName), nil
}

func validateRecordID(kind, id string) error {
	if err := validateNonEmpty(kind+" id", id); err != nil {
		return err
	}
	if id != filepath.Base(id) || id == "." || id == ".." || strings.ContainsAny(id, "/\\") {
		return fmt.Errorf("%s id %q is not safe to use in a stored record filename", kind, id)
	}
	return nil
}

const recordTimestampFormat = "20060102T150405.000000000Z"

func recordFileName(kind, id string, ts time.Time) (string, error) {
	if err := validateRecordID(kind, id); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s.json", ts.UTC().Format(recordTimestampFormat), id), nil
}
