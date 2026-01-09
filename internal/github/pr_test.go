package github

import (
	"testing"
)

func TestParseCIChecks_AllPassed(t *testing.T) {
	json := `[
		{"name": "build", "bucket": "pass"},
		{"name": "lint", "bucket": "pass"},
		{"name": "test", "bucket": "pass"}
	]`

	status := ParseCIChecks([]byte(json))

	if !status.AllPassed {
		t.Error("expected AllPassed to be true")
	}
	if len(status.Pending) != 0 {
		t.Errorf("expected no pending checks, got %v", status.Pending)
	}
	if len(status.Failed) != 0 {
		t.Errorf("expected no failed checks, got %v", status.Failed)
	}
	if status.Error != "" {
		t.Errorf("expected no error, got %q", status.Error)
	}
}

func TestParseCIChecks_WithPending(t *testing.T) {
	json := `[
		{"name": "build", "bucket": "pass"},
		{"name": "deploy", "bucket": "pending"},
		{"name": "e2e", "bucket": "pending"}
	]`

	status := ParseCIChecks([]byte(json))

	if status.AllPassed {
		t.Error("expected AllPassed to be false with pending checks")
	}
	if len(status.Pending) != 2 {
		t.Errorf("expected 2 pending checks, got %d", len(status.Pending))
	}
	// Verify the actual check names are captured
	found := map[string]bool{}
	for _, name := range status.Pending {
		found[name] = true
	}
	if !found["deploy"] || !found["e2e"] {
		t.Errorf("pending checks should contain 'deploy' and 'e2e', got %v", status.Pending)
	}
}

func TestParseCIChecks_WithFailures(t *testing.T) {
	json := `[
		{"name": "build", "bucket": "pass"},
		{"name": "lint", "bucket": "fail"},
		{"name": "security", "bucket": "fail"}
	]`

	status := ParseCIChecks([]byte(json))

	if status.AllPassed {
		t.Error("expected AllPassed to be false with failures")
	}
	if len(status.Failed) != 2 {
		t.Errorf("expected 2 failed checks, got %d", len(status.Failed))
	}
	found := map[string]bool{}
	for _, name := range status.Failed {
		found[name] = true
	}
	if !found["lint"] || !found["security"] {
		t.Errorf("failed checks should contain 'lint' and 'security', got %v", status.Failed)
	}
}

func TestParseCIChecks_SkippingTreatedAsPass(t *testing.T) {
	json := `[
		{"name": "build", "bucket": "pass"},
		{"name": "optional-check", "bucket": "skipping"}
	]`

	status := ParseCIChecks([]byte(json))

	if !status.AllPassed {
		t.Error("expected AllPassed to be true with skipping checks")
	}
	if len(status.Failed) != 0 {
		t.Errorf("skipping should not be in failed, got %v", status.Failed)
	}
}

func TestParseCIChecks_CancelTreatedAsFailure(t *testing.T) {
	json := `[
		{"name": "build", "bucket": "pass"},
		{"name": "slow-test", "bucket": "cancel"}
	]`

	status := ParseCIChecks([]byte(json))

	if status.AllPassed {
		t.Error("expected AllPassed to be false with cancelled check")
	}
	if len(status.Failed) != 1 || status.Failed[0] != "slow-test" {
		t.Errorf("cancelled check should be in failed, got %v", status.Failed)
	}
}

func TestParseCIChecks_CaseInsensitiveBucket(t *testing.T) {
	json := `[
		{"name": "build", "bucket": "PASS"},
		{"name": "lint", "bucket": "Pass"},
		{"name": "test", "bucket": "PENDING"}
	]`

	status := ParseCIChecks([]byte(json))

	if status.AllPassed {
		t.Error("expected AllPassed to be false with pending (uppercase)")
	}
	if len(status.Pending) != 1 || status.Pending[0] != "test" {
		t.Errorf("expected 'test' in pending, got %v", status.Pending)
	}
}

func TestParseCIChecks_EmptyChecks(t *testing.T) {
	json := `[]`

	status := ParseCIChecks([]byte(json))

	// No CI checks configured - should allow approval
	if !status.AllPassed {
		t.Error("expected AllPassed to be true when no checks exist")
	}
}

func TestParseCIChecks_InvalidJSON(t *testing.T) {
	status := ParseCIChecks([]byte(`not valid json`))

	if status.Error == "" {
		t.Error("expected error for invalid JSON")
	}
	if status.AllPassed {
		t.Error("AllPassed should be false on parse error")
	}
}

func TestParseCIChecks_MixedStatuses(t *testing.T) {
	json := `[
		{"name": "build", "bucket": "pass"},
		{"name": "lint", "bucket": "fail"},
		{"name": "deploy", "bucket": "pending"},
		{"name": "optional", "bucket": "skipping"}
	]`

	status := ParseCIChecks([]byte(json))

	if status.AllPassed {
		t.Error("expected AllPassed to be false with mixed statuses")
	}
	if len(status.Pending) != 1 {
		t.Errorf("expected 1 pending, got %d", len(status.Pending))
	}
	if len(status.Failed) != 1 {
		t.Errorf("expected 1 failed, got %d", len(status.Failed))
	}
}

func TestParseCIChecks_UnknownBucketTreatedAsFailure(t *testing.T) {
	json := `[
		{"name": "custom-check", "bucket": "unknown_status"}
	]`

	status := ParseCIChecks([]byte(json))

	if status.AllPassed {
		t.Error("expected AllPassed to be false with unknown bucket")
	}
	if len(status.Failed) != 1 {
		t.Errorf("unknown bucket should be treated as failure, got %v failed", status.Failed)
	}
}
