package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetWorkDir(t *testing.T) {
	tests := []struct {
		name    string
		workDir string
		wantErr bool
	}{
		{
			name:    "non-empty workDir is returned as-is",
			workDir: "/tmp/test",
			wantErr: false,
		},
		{
			name:    "empty workDir returns os.Getwd()",
			workDir: "",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetWorkDir(tt.workDir)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetWorkDir() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.workDir != "" && got != tt.workDir {
				t.Errorf("GetWorkDir() = %v, want %v", got, tt.workDir)
			}
			if tt.workDir == "" && got == "" {
				t.Error("GetWorkDir() returned empty string for empty input")
			}
		})
	}
}

func TestWriteDiffToTempFile(t *testing.T) {
	tmpDir := t.TempDir()
	diff := "diff --git a/test.go b/test.go\n+func Test() {}\n"

	absPath, err := WriteDiffToTempFile(tmpDir, diff)
	if err != nil {
		t.Fatalf("WriteDiffToTempFile() error = %v", err)
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		t.Errorf("WriteDiffToTempFile() file not created at %s", absPath)
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("Failed to read temp file: %v", err)
	}
	if string(content) != diff {
		t.Errorf("WriteDiffToTempFile() content = %q, want %q", string(content), diff)
	}

	if !strings.Contains(filepath.Base(absPath), ".acr-diff-") {
		t.Errorf("WriteDiffToTempFile() filename = %s, want pattern .acr-diff-*", filepath.Base(absPath))
	}

	if !filepath.IsAbs(absPath) {
		t.Errorf("WriteDiffToTempFile() path = %s, want absolute path", absPath)
	}

	if err := CleanupTempFile(absPath); err != nil {
		t.Fatalf("CleanupTempFile() error = %v", err)
	}
	if _, err := os.Stat(absPath); !os.IsNotExist(err) {
		t.Errorf("CleanupTempFile() failed to remove file")
	}
}

func TestWriteInputToTempFile(t *testing.T) {
	tmpDir := t.TempDir()
	input := []byte(`{"findings": []}`)

	absPath, err := WriteInputToTempFile(tmpDir, input, "test-input.json")
	if err != nil {
		t.Fatalf("WriteInputToTempFile() error = %v", err)
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		t.Errorf("WriteInputToTempFile() file not created at %s", absPath)
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("Failed to read temp file: %v", err)
	}
	if string(content) != string(input) {
		t.Errorf("WriteInputToTempFile() content = %q, want %q", string(content), string(input))
	}

	if !filepath.IsAbs(absPath) {
		t.Errorf("WriteInputToTempFile() path = %s, want absolute path", absPath)
	}

	if err := CleanupTempFile(absPath); err != nil {
		t.Fatalf("CleanupTempFile() error = %v", err)
	}
}

func TestCleanupTempFile(t *testing.T) {
	t.Run("cleanup existing file", func(t *testing.T) {
		tmpDir := t.TempDir()
		tmpFile := filepath.Join(tmpDir, "test-cleanup.txt")
		if err := os.WriteFile(tmpFile, []byte("test"), 0600); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		if err := CleanupTempFile(tmpFile); err != nil {
			t.Fatalf("CleanupTempFile() error = %v", err)
		}

		if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
			t.Error("CleanupTempFile() failed to remove existing file")
		}
	})

	t.Run("cleanup non-existent file does not panic", func(t *testing.T) {

		if err := CleanupTempFile("/nonexistent/path/file.txt"); err != nil {
			t.Fatalf("CleanupTempFile() error = %v", err)
		}
	})

	t.Run("cleanup empty path does nothing", func(t *testing.T) {

		if err := CleanupTempFile(""); err != nil {
			t.Fatalf("CleanupTempFile() error = %v", err)
		}
	})

	t.Run("cleanup failure returns error without stderr", func(t *testing.T) {
		cleanupPath := filepath.Join(t.TempDir(), "not-empty")
		if err := os.Mkdir(cleanupPath, 0700); err != nil {
			t.Fatalf("create cleanup directory: %v", err)
		}
		if err := os.WriteFile(filepath.Join(cleanupPath, "child"), []byte("data"), 0600); err != nil {
			t.Fatalf("create cleanup child: %v", err)
		}
		stderrPath := filepath.Join(t.TempDir(), "stderr")
		stderr, err := os.Create(stderrPath)
		if err != nil {
			t.Fatalf("create stderr capture: %v", err)
		}
		originalStderr := os.Stderr
		os.Stderr = stderr
		cleanupErr := CleanupTempFile(cleanupPath)
		os.Stderr = originalStderr
		if err := stderr.Close(); err != nil {
			t.Fatalf("close stderr capture: %v", err)
		}
		if cleanupErr == nil || !strings.Contains(cleanupErr.Error(), "failed to clean up temp file") {
			t.Fatalf("CleanupTempFile() error = %v", cleanupErr)
		}
		captured, err := os.ReadFile(stderrPath)
		if err != nil {
			t.Fatalf("read stderr capture: %v", err)
		}
		if len(captured) != 0 {
			t.Fatalf("CleanupTempFile() wrote stderr: %q", captured)
		}
	})
}

func TestRefFileSizeThreshold(t *testing.T) {

	expected := 100 * 1024
	if RefFileSizeThreshold != expected {
		t.Errorf("RefFileSizeThreshold = %d, want %d", RefFileSizeThreshold, expected)
	}
}
