//go:build windows

package main

import (
	"log/slog"

	"golang.org/x/sys/windows/svc"
)

// isWindowsService reports whether the process was launched by the Windows
// Service Control Manager (vs. an interactive console).
func isWindowsService() bool {
	is, err := svc.IsWindowsService()
	if err != nil {
		return false
	}
	return is
}

// windowsService adapts application to the SCM handler contract.
type windowsService struct {
	app *application
	log *slog.Logger
}

func (m *windowsService) Execute(_ []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}
	if err := m.app.start(); err != nil {
		m.log.Error("service start failed", "err", err)
		return false, 1
	}
	changes <- svc.Status{State: svc.Running, Accepts: accepted}

	for c := range r {
		switch c.Cmd {
		case svc.Interrogate:
			changes <- c.CurrentStatus
		case svc.Stop, svc.Shutdown:
			changes <- svc.Status{State: svc.StopPending}
			m.app.stop()
			changes <- svc.Status{State: svc.Stopped}
			return false, 0
		default:
			m.log.Warn("unexpected service control request", "cmd", c.Cmd)
		}
	}
	return false, 0
}

func runAsService(name string, app *application, log *slog.Logger) error {
	return svc.Run(name, &windowsService{app: app, log: log})
}
