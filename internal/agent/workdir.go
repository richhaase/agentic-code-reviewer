package agent

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func NewIsolatedWorkDir() (string, func(), error) {
	dir, err := os.MkdirTemp("", "acr-postprocess-")
	if err != nil {
		return "", nil, err
	}
	cmd := exec.Command("git", "init", "--quiet", "--template=", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.RemoveAll(dir)
		message := strings.TrimSpace(string(out))
		if message != "" {
			return "", nil, fmt.Errorf("initialize isolated agent workspace (%s): %w", message, err)
		}
		return "", nil, fmt.Errorf("initialize isolated agent workspace: %w", err)
	}
	return dir, func() { _ = os.RemoveAll(dir) }, nil
}
