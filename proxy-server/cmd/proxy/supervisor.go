package main

import (
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"promptgate/internal/config"
)

// supervisor triggers and tears down the user-session sidecar (llama-server) and
// admin web UI tasks on behalf of the Windows service. The dependents must run in
// the interactive session (the iGPU/Vulkan path and node are unavailable to a
// Session-0 service), so the service controls them via RunOnDemand scheduled
// tasks rather than spawning them as its own children. See config.Supervise.
//
// Platform-specific behavior lives in supervisor_windows.go (real: schtasks +
// scoped force-kill) and supervisor_other.go (no-op), so the rest of the binary
// builds and tests on any OS.
type supervisor struct {
	log         *slog.Logger
	sidecarTask string
	webTask     string
	sidecarPort int // derived from inference.endpoint, for the stop force-kill fallback
	webPort     int
	stopTimeout time.Duration
}

func newSupervisor(cfg config.Config, log *slog.Logger) *supervisor {
	return &supervisor{
		log:         log,
		sidecarTask: cfg.Supervise.SidecarTask,
		webTask:     cfg.Supervise.WebTask,
		sidecarPort: portFromEndpoint(cfg.Inference.Endpoint),
		webPort:     cfg.Supervise.WebPort,
		stopTimeout: time.Duration(cfg.Supervise.StopTimeoutMS) * time.Millisecond,
	}
}

// portFromEndpoint extracts the TCP port from a URL-ish endpoint such as
// "http://127.0.0.1:8791". Returns 0 when it cannot be parsed (force-kill skipped).
func portFromEndpoint(endpoint string) int {
	s := endpoint
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	_, portStr, err := net.SplitHostPort(s)
	if err != nil {
		return 0
	}
	p, err := strconv.Atoi(portStr)
	if err != nil {
		return 0
	}
	return p
}
