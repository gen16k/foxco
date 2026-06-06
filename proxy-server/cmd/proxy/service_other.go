//go:build !windows

package main

import (
	"errors"
	"log/slog"
)

// isWindowsService is always false off Windows; the proxy runs as a console
// process. The Windows service mode lives in service_windows.go.
func isWindowsService() bool { return false }

func runAsService(string, *application, *slog.Logger) error {
	return errors.New("windows service mode is only supported on Windows")
}
