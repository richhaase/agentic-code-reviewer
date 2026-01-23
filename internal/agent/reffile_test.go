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

	// Verify file exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		t.Errorf("WriteDiffToTempFile() file not created at %s", absPath)
	}

	// Verify file contents
	content, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("Failed to read temp file: %v", err)
	}
	if string(content) != diff {
		t.Errorf("WriteDiffToTempFile() content = %q, want %q", string(content), diff)
	}

	// Verify file has correct naming pattern
	if !strings.Contains(filepath.Base(absPath), ".acr-diff-") {
		t.Errorf("WriteDiffToTempFile() filename = %s, want pattern .acr-diff-*", filepath.Base(absPath))
	}

	// Verify path is absolute
	if !filepath.IsAbs(absPath) {
		t.Errorf("WriteDiffToTempFile() path = %s, want absolute path", absPath)
	}

	// Cleanup
	CleanupTempFile(absPath)
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

	// Verify file exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		t.Errorf("WriteInputToTempFile() file not created at %s", absPath)
	}

	// Verify file contents
	content, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("Failed to read temp file: %v", err)
	}
	if string(content) != string(input) {
		t.Errorf("WriteInputToTempFile() content = %q, want %q", string(content), string(input))
	}

	// Verify path is absolute
	if !filepath.IsAbs(absPath) {
		t.Errorf("WriteInputToTempFile() path = %s, want absolute path", absPath)
	}

	// Cleanup
	CleanupTempFile(absPath)
}

func TestCleanupTempFile(t *testing.T) {
	t.Run("cleanup existing file", func(t *testing.T) {
		tmpDir := t.TempDir()
		tmpFile := filepath.Join(tmpDir, "test-cleanup.txt")
		if err := os.WriteFile(tmpFile, []byte("test"), 0600); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		CleanupTempFile(tmpFile)

		if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
			t.Error("CleanupTempFile() failed to remove existing file")
		}
	})

	t.Run("cleanup non-existent file does not panic", func(t *testing.T) {
		// Should not panic or error
		CleanupTempFile("/nonexistent/path/file.txt")
	})

	t.Run("cleanup empty path does nothing", func(t *testing.T) {
		// Should not panic or error
		CleanupTempFile("")
	})
}

func TestRefFileSizeThreshold(t *testing.T) {
	// Verify threshold is 100KB
	expected := 100 * 1024
	if RefFileSizeThreshold != expected {
		t.Errorf("RefFileSizeThreshold = %d, want %d", RefFileSizeThreshold, expected)
	}
}
