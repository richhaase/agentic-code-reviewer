package github

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupMockGH(t *testing.T, response string) string {
	t.Helper()
	scriptDir := t.TempDir()
	script := "#!/bin/sh\n" +
		"DIR=$(cd \"$(dirname \"$0\")\" && pwd -P)\n" +
		"pwd -P >> \"$DIR/cwd.log\"\n" +
		"echo \"$@\" >> \"$DIR/args.log\"\n" +
		"cat \"$DIR/response\" 2>/dev/null\n"
	if err := os.WriteFile(filepath.Join(scriptDir, "gh"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptDir, "response"), []byte(response), 0644); err != nil {
		t.Fatal(err)
	}

	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", scriptDir+string(os.PathListSeparator)+originalPath)
	return scriptDir
}

func setupMockGHRoutedByArgs(t *testing.T, routes map[string]string) string {
	t.Helper()
	scriptDir := t.TempDir()
	script := "#!/bin/sh\n" +
		"DIR=$(cd \"$(dirname \"$0\")\" && pwd -P)\n" +
		"pwd -P >> \"$DIR/cwd.log\"\n" +
		"echo \"$@\" >> \"$DIR/args.log\"\n" +
		"case \"$1 $2\" in\n"
	for prefix, response := range routes {
		responseFile := strings.ReplaceAll(prefix, " ", "_") + ".response"
		if err := os.WriteFile(filepath.Join(scriptDir, responseFile), []byte(response), 0644); err != nil {
			t.Fatal(err)
		}
		script += "\"" + prefix + "\") cat \"$DIR/" + responseFile + "\" ;;\n"
	}
	script += "esac\n"
	if err := os.WriteFile(filepath.Join(scriptDir, "gh"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", scriptDir+string(os.PathListSeparator)+originalPath)
	return scriptDir
}

func readCapturedCwd(t *testing.T, scriptDir string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(scriptDir, "cwd.log"))
	if err != nil {
		t.Fatalf("failed to read cwd.log: %v", err)
	}
	var lines []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func readCapturedArgs(t *testing.T, scriptDir string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(scriptDir, "args.log"))
	if err != nil {
		t.Fatalf("failed to read args.log: %v", err)
	}
	var lines []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func resolvedDir(t *testing.T, dir string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestGetCurrentPRNumber_ScopesToRepositoryRoot(t *testing.T) {
	repoRoot := t.TempDir()
	scriptDir := setupMockGH(t, "42")

	got, err := GetCurrentPRNumber(context.Background(), repoRoot, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "42" {
		t.Errorf("expected 42, got %q", got)
	}

	cwds := readCapturedCwd(t, scriptDir)
	if len(cwds) != 1 || cwds[0] != resolvedDir(t, repoRoot) {
		t.Errorf("expected gh invoked in %s, got %v", resolvedDir(t, repoRoot), cwds)
	}
}

func TestGetPRWatchState_ScopesToRepositoryRoot(t *testing.T) {
	repoRoot := t.TempDir()
	scriptDir := setupMockGH(t, `{"headRefOid":"abc123","state":"OPEN","reviewRequests":[]}`)

	st, err := GetPRWatchState(context.Background(), repoRoot, "7")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st.HeadSHA != "abc123" {
		t.Errorf("expected headSHA abc123, got %q", st.HeadSHA)
	}

	cwds := readCapturedCwd(t, scriptDir)
	if len(cwds) != 1 || cwds[0] != resolvedDir(t, repoRoot) {
		t.Errorf("expected gh invoked in %s, got %v", resolvedDir(t, repoRoot), cwds)
	}
}

func TestApprovePR_ScopesToRepositoryRoot(t *testing.T) {
	repoRoot := t.TempDir()
	scriptDir := setupMockGH(t, "")

	if err := ApprovePR(context.Background(), repoRoot, "7", "lgtm"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cwds := readCapturedCwd(t, scriptDir)
	if len(cwds) != 1 || cwds[0] != resolvedDir(t, repoRoot) {
		t.Errorf("expected gh invoked in %s, got %v", resolvedDir(t, repoRoot), cwds)
	}
}

func TestCheckCIStatus_ScopesToRepositoryRoot(t *testing.T) {
	repoRoot := t.TempDir()
	scriptDir := setupMockGH(t, `[]`)

	status := CheckCIStatus(context.Background(), repoRoot, "7")
	if !status.AllPassed {
		t.Errorf("expected all passed for empty checks, got %+v", status)
	}

	cwds := readCapturedCwd(t, scriptDir)
	if len(cwds) != 1 || cwds[0] != resolvedDir(t, repoRoot) {
		t.Errorf("expected gh invoked in %s, got %v", resolvedDir(t, repoRoot), cwds)
	}
}

func TestGetPRAuthor_ScopesToRepositoryRoot(t *testing.T) {
	repoRoot := t.TempDir()
	scriptDir := setupMockGH(t, "octocat")

	author := GetPRAuthor(context.Background(), repoRoot, "7")
	if author != "octocat" {
		t.Errorf("expected octocat, got %q", author)
	}

	cwds := readCapturedCwd(t, scriptDir)
	if len(cwds) != 1 || cwds[0] != resolvedDir(t, repoRoot) {
		t.Errorf("expected gh invoked in %s, got %v", resolvedDir(t, repoRoot), cwds)
	}
}

func TestIsSelfReview_BothGHCallsScopeToRepositoryRoot(t *testing.T) {
	repoRoot := t.TempDir()
	scriptDir := setupMockGHRoutedByArgs(t, map[string]string{
		"api user": "octocat",
		"pr view":  "octocat",
	})

	if !IsSelfReview(context.Background(), repoRoot, "7") {
		t.Error("expected self-review to be detected")
	}

	cwds := readCapturedCwd(t, scriptDir)
	args := readCapturedArgs(t, scriptDir)
	if len(cwds) != 2 || len(args) != 2 {
		t.Fatalf("expected 2 gh invocations (current user + pr author), got cwds=%v args=%v", cwds, args)
	}

	for i, a := range args {
		switch {
		case strings.HasPrefix(a, "api user"):
			if cwds[i] == resolvedDir(t, repoRoot) {
				t.Errorf("expected GetCurrentUser (host-scoped, not repo-scoped) to NOT run in %s", repoRoot)
			}
		case strings.HasPrefix(a, "pr view"):
			if cwds[i] != resolvedDir(t, repoRoot) {
				t.Errorf("expected GetPRAuthor to run in %s, got %s", resolvedDir(t, repoRoot), cwds[i])
			}
		default:
			t.Errorf("unexpected gh invocation: %s", a)
		}
	}
}

func TestTwoRepositoriesScopeIndependentlyWithoutChdir(t *testing.T) {
	repoA := t.TempDir()
	repoB := t.TempDir()

	scriptDirA := setupMockGH(t, "1")
	got, err := GetCurrentPRNumber(context.Background(), repoA, "")
	if err != nil || got != "1" {
		t.Fatalf("unexpected result for repoA: %q, %v", got, err)
	}

	scriptDirB := setupMockGH(t, "2")
	got, err = GetCurrentPRNumber(context.Background(), repoB, "")
	if err != nil || got != "2" {
		t.Fatalf("unexpected result for repoB: %q, %v", got, err)
	}

	cwdsA := readCapturedCwd(t, scriptDirA)
	cwdsB := readCapturedCwd(t, scriptDirB)
	if len(cwdsA) != 1 || cwdsA[0] != resolvedDir(t, repoA) {
		t.Errorf("expected repoA call scoped to %s, got %v", resolvedDir(t, repoA), cwdsA)
	}
	if len(cwdsB) != 1 || cwdsB[0] != resolvedDir(t, repoB) {
		t.Errorf("expected repoB call scoped to %s, got %v", resolvedDir(t, repoB), cwdsB)
	}
}
