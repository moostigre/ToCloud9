//go:build windows

package client

import (
	"fmt"
	"time"

	"golang.org/x/sys/windows"
)

func waitForProcessExit(processID int, timeout time.Duration) error {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(processID))
	if err != nil {
		// The process may already have exited between starting the helper and
		// opening its handle, which is the desired state.
		if err == windows.ERROR_INVALID_PARAMETER {
			return nil
		}
		return err
	}
	defer windows.CloseHandle(handle)

	waitMilliseconds := uint32(timeout / time.Millisecond)
	result, err := windows.WaitForSingleObject(handle, waitMilliseconds)
	if err != nil {
		return err
	}
	if result == uint32(windows.WAIT_TIMEOUT) {
		return fmt.Errorf("launcher process %d did not exit within %s", processID, timeout)
	}
	if result != uint32(windows.WAIT_OBJECT_0) {
		return fmt.Errorf("unexpected process wait result %d", result)
	}
	return nil
}
