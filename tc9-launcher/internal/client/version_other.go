//go:build !windows

package client

import "errors"

func executableVersion(string) (string, int, error) {
	return "", 0, errors.New("client validation is supported by the Windows launcher")
}
