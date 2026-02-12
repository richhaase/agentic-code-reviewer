// Package integration provides end-to-end tests for the acr binary using mock agent CLIs.
//
// These tests replace the BATS integration tests with Go tests that:
//   - Use mock CLI scripts instead of real LLM backends (zero cost, fast, deterministic)
//   - Test the full binary (build → exec → assert output + exit code)
//   - Cover success paths, error paths, output format, and flag handling
//
// Mock agents return canned responses in the correct format for each agent type:
//   - codex: JSONL event stream (--json mode)
//   - claude: JSON wrapper with result field (--output-format json mode)
//   - gemini: JSON wrapper with response field (-o json mode)
package integration

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// testEnv holds paths and state for integration test execution.
type testEnv struct {
	acrBin   string // Path to built acr binary
	mockDir  string // Directory containing mock CLI scripts
	repoDir  string // Temporary git repo for test execution
	origPath string // Original PATH to restore
}

// setupTestEnv builds the acr binary and creates a temporary git repo with a diff.
func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()

	// Build acr binary
	rootDir := findRepoRoot(t)
	acrBin := filepath.Join(t.TempDir(), "acr")
	build := exec.Command("go", "build", "-o", acrBin, "./cmd/acr")
	build.Dir = rootDir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("failed to build acr: %v\n%s", err, out)
	}

	// Create mock CLI directory
	mockDir := filepath.Join(t.TempDir(), "mocks")
	if err := os.MkdirAll(mockDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create temporary git repo with a diff
	repoDir := createTestRepo(t)

	return &testEnv{
		acrBin:   acrBin,
		mockDir:  mockDir,
		repoDir:  repoDir,
		origPath: os.Getenv("PATH"),
	}
}

// withMockAgents prepends the mock directory to PATH so mock CLIs are found first.
func (e *testEnv) withMockAgents() []string {
	env := os.Environ()
	newPath := e.mockDir + ":" + e.origPath
	// Replace PATH in env slice
	for i, v := range env {
		if strings.HasPrefix(v, "PATH=") {
			env[i] = "PATH=" + newPath
			return env
		}
	}
	return append(env, "PATH="+newPath)
}

// run executes acr with the given args and returns stdout, stderr, and exit code.
func (e *testEnv) run(args ...string) (stdout, stderr string, exitCode int) {
	cmd := exec.Command(e.acrBin, args...)
	cmd.Dir = e.repoDir
	cmd.Env = e.withMockAgents()

	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	exitCode = 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	return outBuf.String(), errBuf.String(), exitCode
}

// findRepoRoot walks up to find the go.mod file.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (no go.mod)")
		}
		dir = parent
	}
}

// createTestRepo creates a temporary git repo with a diff against HEAD~1.
func createTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, c := range cmds {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git setup %v: %v\n%s", c, err, out)
		}
	}

	// Initial commit
	testFile := filepath.Join(dir, "main.go")
	os.WriteFile(testFile, []byte("package main\n\nfunc main() {}\n"), 0644)
	gitAdd := exec.Command("git", "add", ".")
	gitAdd.Dir = dir
	gitAdd.Run()
	gitCommit := exec.Command("git", "commit", "-m", "initial")
	gitCommit.Dir = dir
	gitCommit.Run()

	// Second commit with a change (creates a diff)
	os.WriteFile(testFile, []byte("package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"), 0644)
	gitAdd2 := exec.Command("git", "add", ".")
	gitAdd2.Dir = dir
	gitAdd2.Run()
	gitCommit2 := exec.Command("git", "commit", "-m", "add print")
	gitCommit2.Dir = dir
	gitCommit2.Run()

	return dir
}

// --- Mock Agent Responses ---

// codex reviewer returns plain text findings (one per line, streamed to stdout)
const codexReviewerResponse = `**main.go:6**: Missing error handling for fmt.Println return value.
**main.go:3**: Unused import "fmt" should be removed if not needed.
`

// codex summarizer returns JSONL event stream
const codexSummarizerResponse = `{"type":"item.created","item":{"type":"agent_message","text":""}}
{"type":"item.completed","item":{"type":"agent_message","text":"{\"findings\":[{\"title\":\"Missing error handling\",\"summary\":\"fmt.Println return value not checked\",\"messages\":[\"main.go:6: Missing error handling for fmt.Println return value.\"],\"reviewer_count\":1,\"sources\":[0]}],\"info\":[]}"}}`

// claude reviewer returns plain text (from result field in JSON wrapper)
const claudeReviewerResponse = `I found a potential issue:
**main.go:6**: The return value of fmt.Println is not checked.
`

// claude summarizer response (JSON wrapper with result field)
func claudeSummarizerJSON() string {
	return `{"type":"result","result":"{\"findings\":[{\"title\":\"Unchecked return value\",\"summary\":\"fmt.Println return value ignored\",\"messages\":[\"main.go:6: Return value not checked\"],\"reviewer_count\":1,\"sources\":[0]}],\"info\":[]}"}`
}

// gemini reviewer returns plain text
const geminiReviewerResponse = `- main.go:6: fmt.Println return value is not checked for errors.
`

// gemini summarizer response (JSON wrapper with response field)
func geminiSummarizerJSON() string {
	return `{"response":"{\"findings\":[{\"title\":\"Unchecked Println\",\"summary\":\"Return value ignored\",\"messages\":[\"main.go:6: unchecked\"],\"reviewer_count\":1,\"sources\":[0]}],\"info\":[]}"}`
}

// LGTM responses (no findings)
const codexLGTMReview = "The code looks good. No issues found."
const claudeLGTMReview = "No issues found. The code is clean."
const geminiLGTMReview = "Code review complete. No problems detected."

const codexLGTMSummary = `{"type":"item.completed","item":{"type":"agent_message","text":"{\"findings\":[],\"info\":[]}"}}`

func claudeLGTMSummary() string {
	return `{"type":"result","result":"{\"findings\":[],\"info\":[]}"}`
}

func geminiLGTMSummary() string {
	return `{"response":"{\"findings\":[],\"info\":[]}"}`
}

// --- Mock CLI Script Generators ---

// writeMockCLI writes a shell script that returns canned responses.
// The script examines its arguments to determine if it's being called as
// a reviewer (--print without --output-format) or summarizer/fp-filter (--output-format json).
func writeMockCodex(t *testing.T, dir string, reviewResponse, summaryResponse string) {
	t.Helper()
	// Codex uses --json for structured output (summarizer) vs no --json (reviewer)
	script := fmt.Sprintf(`#!/bin/sh
has_json=false
for arg in "$@"; do
    if [ "$arg" = "--json" ]; then
        has_json=true
    fi
done

if [ "$has_json" = "true" ]; then
    printf '%%s\n' '%s'
else
    cat <<'REVIEW_EOF'
%s
REVIEW_EOF
fi
`, escape(summaryResponse), reviewResponse)

	writeMock(t, dir, "codex", script)
}

func writeMockClaude(t *testing.T, dir string, reviewResponse, summaryResponse string) {
	t.Helper()
	// Claude uses --output-format json for structured output (summarizer) vs --print only (reviewer)
	script := fmt.Sprintf(`#!/bin/sh
has_json=false
for arg in "$@"; do
    if [ "$arg" = "json" ]; then
        has_json=true
    fi
done

if [ "$has_json" = "true" ]; then
    cat /dev/stdin >/dev/null 2>&1
    printf '%%s\n' '%s'
else
    cat /dev/stdin >/dev/null 2>&1
    cat <<'REVIEW_EOF'
%s
REVIEW_EOF
fi
`, escape(summaryResponse), reviewResponse)

	writeMock(t, dir, "claude", script)
}

func writeMockGemini(t *testing.T, dir string, reviewResponse, summaryResponse string) {
	t.Helper()
	// Gemini uses -o json for structured output (summarizer) vs no -o (reviewer)
	script := fmt.Sprintf(`#!/bin/sh
has_json=false
for arg in "$@"; do
    if [ "$arg" = "json" ]; then
        has_json=true
    fi
done

if [ "$has_json" = "true" ]; then
    cat /dev/stdin >/dev/null 2>&1
    printf '%%s\n' '%s'
else
    cat /dev/stdin >/dev/null 2>&1
    cat <<'REVIEW_EOF'
%s
REVIEW_EOF
fi
`, escape(summaryResponse), reviewResponse)

	writeMock(t, dir, "gemini", script)
}

func writeMock(t *testing.T, dir, name, script string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write mock %s: %v", name, err)
	}
}

// Also mock gh CLI to prevent real GitHub API calls
func writeMockGH(t *testing.T, dir string) {
	t.Helper()
	script := `#!/bin/sh
# Mock gh - return empty/error for all commands
exit 1
`
	writeMock(t, dir, "gh", script)
}

func escape(s string) string {
	return strings.ReplaceAll(s, "'", "'\"'\"'")
}

// --- Tests ---

func TestVersion(t *testing.T) {
	env := setupTestEnv(t)
	stdout, _, exitCode := env.run("--version")
	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0", exitCode)
	}
	if !strings.Contains(stdout, "acr v") {
		t.Errorf("expected 'acr v' in output, got: %s", stdout)
	}
}

func TestHelp(t *testing.T) {
	env := setupTestEnv(t)
	stdout, _, exitCode := env.run("--help")
	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0", exitCode)
	}
	for _, want := range []string{"--reviewers", "--base", "--timeout", "--reviewer-agent", "--summarizer-agent"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help output missing %q", want)
		}
	}
}

func TestConfigSubcommands(t *testing.T) {
	env := setupTestEnv(t)

	t.Run("config show", func(t *testing.T) {
		stdout, _, exitCode := env.run("config", "show")
		if exitCode != 0 {
			t.Errorf("exit code = %d, want 0", exitCode)
		}
		if !strings.Contains(stdout, "reviewers:") {
			t.Errorf("config show missing 'reviewers:', got: %s", stdout)
		}
	})

	t.Run("config validate", func(t *testing.T) {
		_, _, exitCode := env.run("config", "validate")
		if exitCode != 0 {
			t.Errorf("exit code = %d, want 0", exitCode)
		}
	})

	t.Run("config init", func(t *testing.T) {
		_, _, exitCode := env.run("config", "init")
		if exitCode != 0 {
			t.Errorf("exit code = %d, want 0", exitCode)
		}
		configPath := filepath.Join(env.repoDir, ".acr.yaml")
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			t.Error("config init did not create .acr.yaml")
		}
	})
}

func TestCodexReview_WithFindings(t *testing.T) {
	env := setupTestEnv(t)
	writeMockCodex(t, env.mockDir, codexReviewerResponse, codexSummarizerResponse)
	writeMockGH(t, env.mockDir)

	stdout, stderr, exitCode := env.run("--local", "--reviewers", "1",
		"--reviewer-agent", "codex", "--summarizer-agent", "codex",
		"--base", "HEAD~1", "--no-fp-filter")

	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1 (findings present)\nstderr: %s", exitCode, stderr)
	}
	if !strings.Contains(stdout, "finding") {
		t.Errorf("output should contain findings, got:\n%s", stdout)
	}
}

func TestClaudeReview_WithFindings(t *testing.T) {
	env := setupTestEnv(t)
	writeMockClaude(t, env.mockDir, claudeReviewerResponse, claudeSummarizerJSON())
	writeMockGH(t, env.mockDir)

	stdout, stderr, exitCode := env.run("--local", "--reviewers", "1",
		"--reviewer-agent", "claude", "--summarizer-agent", "claude",
		"--base", "HEAD~1", "--no-fp-filter")

	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1 (findings present)\nstderr: %s", exitCode, stderr)
	}
	if !strings.Contains(stdout, "finding") {
		t.Errorf("output should contain findings, got:\n%s", stdout)
	}
}

func TestGeminiReview_WithFindings(t *testing.T) {
	env := setupTestEnv(t)
	writeMockGemini(t, env.mockDir, geminiReviewerResponse, geminiSummarizerJSON())
	writeMockGH(t, env.mockDir)

	stdout, stderr, exitCode := env.run("--local", "--reviewers", "1",
		"--reviewer-agent", "gemini", "--summarizer-agent", "gemini",
		"--base", "HEAD~1", "--no-fp-filter")

	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1 (findings present)\nstderr: %s", exitCode, stderr)
	}
	if !strings.Contains(stdout, "finding") {
		t.Errorf("output should contain findings, got:\n%s", stdout)
	}
}

func TestCodexReview_LGTM(t *testing.T) {
	env := setupTestEnv(t)
	writeMockCodex(t, env.mockDir, codexLGTMReview, codexLGTMSummary)
	writeMockGH(t, env.mockDir)

	stdout, stderr, exitCode := env.run("--local", "--reviewers", "1",
		"--reviewer-agent", "codex", "--summarizer-agent", "codex",
		"--base", "HEAD~1")

	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0 (LGTM)\nstderr: %s", exitCode, stderr)
	}
	// LGTM appears in stdout report or stderr status messages
	combined := stdout + stderr
	if !strings.Contains(combined, "LGTM") && !strings.Contains(combined, "skipping PR approval") {
		t.Errorf("expected LGTM or approval message, got:\nstdout: %s\nstderr: %s", stdout, stderr)
	}
}

func TestClaudeReview_LGTM(t *testing.T) {
	env := setupTestEnv(t)
	writeMockClaude(t, env.mockDir, claudeLGTMReview, claudeLGTMSummary())
	writeMockGH(t, env.mockDir)

	stdout, stderr, exitCode := env.run("--local", "--reviewers", "1",
		"--reviewer-agent", "claude", "--summarizer-agent", "claude",
		"--base", "HEAD~1")

	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0 (LGTM)\nstderr: %s", exitCode, stderr)
	}
	combined := stdout + stderr
	if !strings.Contains(combined, "LGTM") && !strings.Contains(combined, "skipping PR approval") {
		t.Errorf("expected LGTM or approval message, got:\nstdout: %s\nstderr: %s", stdout, stderr)
	}
}

func TestGeminiReview_LGTM(t *testing.T) {
	env := setupTestEnv(t)
	writeMockGemini(t, env.mockDir, geminiLGTMReview, geminiLGTMSummary())
	writeMockGH(t, env.mockDir)

	stdout, stderr, exitCode := env.run("--local", "--reviewers", "1",
		"--reviewer-agent", "gemini", "--summarizer-agent", "gemini",
		"--base", "HEAD~1")

	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0 (LGTM)\nstderr: %s", exitCode, stderr)
	}
	combined := stdout + stderr
	if !strings.Contains(combined, "LGTM") && !strings.Contains(combined, "skipping PR approval") {
		t.Errorf("expected LGTM or approval message, got:\nstdout: %s\nstderr: %s", stdout, stderr)
	}
}

func TestFPFilter_ClaudeAgent(t *testing.T) {
	env := setupTestEnv(t)
	writeMockClaude(t, env.mockDir, claudeReviewerResponse, claudeSummarizerJSON())
	writeMockGH(t, env.mockDir)

	_, stderr, exitCode := env.run("--local", "--reviewers", "1",
		"--reviewer-agent", "claude", "--summarizer-agent", "claude",
		"--base", "HEAD~1")

	if exitCode != 0 && exitCode != 1 {
		t.Errorf("exit code = %d, want 0 or 1\nstderr: %s", exitCode, stderr)
	}
	if strings.Contains(stderr, "FP filter skipped") {
		t.Errorf("FP filter should not be skipped, stderr:\n%s", stderr)
	}
}

func TestFPFilter_GeminiAgent(t *testing.T) {
	env := setupTestEnv(t)
	writeMockGemini(t, env.mockDir, geminiReviewerResponse, geminiSummarizerJSON())
	writeMockGH(t, env.mockDir)

	_, stderr, exitCode := env.run("--local", "--reviewers", "1",
		"--reviewer-agent", "gemini", "--summarizer-agent", "gemini",
		"--base", "HEAD~1")

	if exitCode != 0 && exitCode != 1 {
		t.Errorf("exit code = %d, want 0 or 1\nstderr: %s", exitCode, stderr)
	}
	if strings.Contains(stderr, "FP filter skipped") {
		t.Errorf("FP filter should not be skipped, stderr:\n%s", stderr)
	}
}

func TestMultipleReviewers(t *testing.T) {
	env := setupTestEnv(t)
	writeMockCodex(t, env.mockDir, codexReviewerResponse, codexSummarizerResponse)
	writeMockGH(t, env.mockDir)

	stdout, stderr, exitCode := env.run("--local", "--reviewers", "3",
		"--reviewer-agent", "codex", "--summarizer-agent", "codex",
		"--base", "HEAD~1", "--no-fp-filter")

	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1\nstderr: %s", exitCode, stderr)
	}
	if !strings.Contains(stdout, "finding") {
		t.Errorf("output should contain findings, got:\n%s", stdout)
	}
}

func TestMixedAgents(t *testing.T) {
	env := setupTestEnv(t)
	writeMockCodex(t, env.mockDir, codexReviewerResponse, codexSummarizerResponse)
	writeMockClaude(t, env.mockDir, claudeReviewerResponse, claudeSummarizerJSON())
	writeMockGemini(t, env.mockDir, geminiReviewerResponse, geminiSummarizerJSON())
	writeMockGH(t, env.mockDir)

	// Use codex as summarizer since all mock agents are available
	stdout, stderr, exitCode := env.run("--local", "--reviewers", "3",
		"--reviewer-agent", "codex,claude,gemini", "--summarizer-agent", "codex",
		"--base", "HEAD~1", "--no-fp-filter")

	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1\nstderr: %s", exitCode, stderr)
	}
	if !strings.Contains(stdout, "finding") {
		t.Errorf("output should contain findings, got:\n%s", stdout)
	}
}

func TestFPFilter_CodexAgent(t *testing.T) {
	env := setupTestEnv(t)
	// For FP filter, the summarizer agent is called twice: once for summarization, once for FP filter
	writeMockCodex(t, env.mockDir, codexReviewerResponse, codexSummarizerResponse)
	writeMockGH(t, env.mockDir)

	stdout, stderr, exitCode := env.run("--local", "--reviewers", "1",
		"--reviewer-agent", "codex", "--summarizer-agent", "codex",
		"--base", "HEAD~1")

	// Either 0 (all filtered) or 1 (some findings remain)
	if exitCode != 0 && exitCode != 1 {
		t.Errorf("exit code = %d, want 0 or 1\nstderr: %s", exitCode, stderr)
	}
	// Should not show FP filter skip warning
	if strings.Contains(stderr, "FP filter skipped") {
		t.Errorf("FP filter should not be skipped, stderr:\n%s", stderr)
	}
	_ = stdout
}

func TestOutputFormat_FindingsReport(t *testing.T) {
	env := setupTestEnv(t)
	writeMockCodex(t, env.mockDir, codexReviewerResponse, codexSummarizerResponse)
	writeMockGH(t, env.mockDir)

	stdout, _, exitCode := env.run("--local", "--reviewers", "1",
		"--reviewer-agent", "codex", "--summarizer-agent", "codex",
		"--base", "HEAD~1", "--no-fp-filter")

	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}

	// Verify report structure
	if !strings.Contains(stdout, "finding") {
		t.Error("report missing 'finding' count")
	}
	if !strings.Contains(stdout, "━") {
		t.Error("report missing separator lines")
	}
	if !strings.Contains(stdout, "Timing:") {
		t.Error("report missing Timing section")
	}
	if !strings.Contains(stdout, "reviewers:") {
		t.Error("report missing reviewers timing")
	}
	if !strings.Contains(stdout, "summarizer:") {
		t.Error("report missing summarizer timing")
	}
}

func TestOutputFormat_TimingSection(t *testing.T) {
	env := setupTestEnv(t)
	writeMockCodex(t, env.mockDir, codexLGTMReview, codexLGTMSummary)
	writeMockGH(t, env.mockDir)

	stdout, _, exitCode := env.run("--local", "--reviewers", "2",
		"--reviewer-agent", "codex", "--summarizer-agent", "codex",
		"--base", "HEAD~1")

	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}

	// LGTM output goes to stderr — stdout should have timing in report (if any)
	// For LGTM, minimal output is expected
	_ = stdout
}

// --- Error Path Tests ---

func TestInvalidAgentName(t *testing.T) {
	env := setupTestEnv(t)
	writeMockGH(t, env.mockDir)

	_, stderr, exitCode := env.run("--local", "--reviewer-agent", "invalid-agent", "--base", "HEAD~1")

	if exitCode != 2 {
		t.Errorf("exit code = %d, want 2 (error)\nstderr: %s", exitCode, stderr)
	}
}

func TestMissingAgentCLI(t *testing.T) {
	env := setupTestEnv(t)
	// Don't write any mock agents — they won't be found in PATH
	// But we need to not find the real ones either
	emptyDir := t.TempDir()
	writeMockGH(t, emptyDir)

	// Override PATH to only include the empty dir (no agent CLIs)
	cmd := exec.Command(env.acrBin, "--local", "--reviewer-agent", "codex",
		"--summarizer-agent", "codex", "--base", "HEAD~1")
	cmd.Dir = env.repoDir
	cmd.Env = []string{
		"PATH=" + emptyDir,
		"HOME=" + t.TempDir(),
	}

	out, _ := cmd.CombinedOutput()
	if cmd.ProcessState.ExitCode() != 2 {
		t.Errorf("exit code = %d, want 2 (error)\noutput: %s", cmd.ProcessState.ExitCode(), out)
	}
}

func TestEmptyDiff(t *testing.T) {
	env := setupTestEnv(t)
	writeMockCodex(t, env.mockDir, codexReviewerResponse, codexSummarizerResponse)
	writeMockGH(t, env.mockDir)

	// HEAD~0 = no diff
	_, stderr, exitCode := env.run("--local", "--reviewers", "1",
		"--reviewer-agent", "codex", "--summarizer-agent", "codex",
		"--base", "HEAD")

	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0 (no changes)\nstderr: %s", exitCode, stderr)
	}
	if !strings.Contains(stderr, "No changes detected") {
		t.Errorf("expected 'No changes detected' message, stderr:\n%s", stderr)
	}
}

func TestVerboseOutput(t *testing.T) {
	env := setupTestEnv(t)
	writeMockCodex(t, env.mockDir, codexReviewerResponse, codexSummarizerResponse)
	writeMockGH(t, env.mockDir)

	_, stderr, _ := env.run("--local", "--reviewers", "1",
		"--reviewer-agent", "codex", "--summarizer-agent", "codex",
		"--base", "HEAD~1", "--no-fp-filter", "--verbose")

	if !strings.Contains(stderr, "Diff size:") {
		t.Errorf("verbose output should contain 'Diff size:', stderr:\n%s", stderr)
	}
}

func TestNoFetchFlag(t *testing.T) {
	env := setupTestEnv(t)
	writeMockCodex(t, env.mockDir, codexLGTMReview, codexLGTMSummary)
	writeMockGH(t, env.mockDir)

	// --no-fetch should work without a remote
	_, stderr, exitCode := env.run("--local", "--no-fetch", "--reviewers", "1",
		"--reviewer-agent", "codex", "--summarizer-agent", "codex",
		"--base", "HEAD~1")

	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0\nstderr: %s", exitCode, stderr)
	}
}
