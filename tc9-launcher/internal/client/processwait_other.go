//go:build !windows

package client

import "time"

func waitForProcessExit(_ int, _ time.Duration) error {
	return nil
}
