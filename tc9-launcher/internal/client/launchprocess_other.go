//go:build !windows

package client

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func launchProcess(root string) (*os.Process, error) {
	if _, err := Validate(root); err != nil {
		return nil, err
	}
	if err := clearItemQueryCache(root); err != nil {
		return nil, fmt.Errorf("clear stale item cache: %w", err)
	}
	cmd := exec.Command(filepath.Join(root, "Wow.exe"))
	cmd.Dir = root
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd.Process, nil
}
