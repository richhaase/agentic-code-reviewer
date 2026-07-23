package store

import (
	"fmt"
	"os"
	"path/filepath"
)

type ForgetReport struct {
	PullRequest          PullRequestKeyV1
	Existed              bool
	SnapshotRemoved      bool
	RunsRemoved          int
	EventsRemoved        int
	AdjudicationsRemoved int
	LoopDecisionsRemoved int
	EconomicsRemoved     int
}

func ForgetPullRequest(dataDir string, key PullRequestKeyV1) (ForgetReport, error) {
	report := ForgetReport{PullRequest: key}

	prDir, err := pullRequestDir(dataDir, key)
	if err != nil {
		return report, err
	}

	if _, err := os.Stat(prDir); err != nil {
		if os.IsNotExist(err) {
			return report, nil
		}
		return report, fmt.Errorf("stat %s: %w", prDir, err)
	}
	report.Existed = true

	if info, err := os.Stat(filepath.Join(prDir, snapshotFileName)); err == nil && !info.IsDir() {
		report.SnapshotRemoved = true
	}
	report.RunsRemoved = countJSONFiles(filepath.Join(prDir, runsDirName))
	report.EventsRemoved = countJSONFiles(filepath.Join(prDir, eventsDirName))
	report.AdjudicationsRemoved = countJSONFiles(filepath.Join(prDir, adjudicationsDirName))
	report.LoopDecisionsRemoved = countJSONFiles(filepath.Join(prDir, loopDecisionsDirName))
	report.EconomicsRemoved = countJSONFiles(filepath.Join(prDir, economicsDirName))

	if err := os.RemoveAll(prDir); err != nil {
		return report, fmt.Errorf("remove pull request history for %s: %w", key.String(), err)
	}
	return report, nil
}

func countJSONFiles(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) == ".json" {
			count++
		}
	}
	return count
}
