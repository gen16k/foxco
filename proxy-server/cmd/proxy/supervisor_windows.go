//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Start triggers the user-session RunOnDemand tasks (sidecar first, then web UI).
// Failures are LOGGED, never returned: application.start() must not fail because a
// dependent could not be launched (e.g. no user is logged in) — the proxy comes up
// and fail-closes until the sidecar is healthy. schtasks /Run on an already-running
// task is a no-op (tasks are registered MultipleInstances=IgnoreNew).
func (s *supervisor) Start() {
	for _, t := range []string{s.sidecarTask, s.webTask} {
		if t == "" {
			continue
		}
		if out, err := runCmd("schtasks.exe", "/Run", "/TN", t); err != nil {
			s.log.Warn("supervisor: task run failed (is an interactive user logged in?)",
				"task", t, "err", err, "out", trimOut(out))
		} else {
			s.log.Info("supervisor: task started", "task", t)
		}
	}
}

// Stop terminates the web UI then the sidecar. schtasks /End alone does NOT
// reliably kill the task action's child process tree, so ending the task is paired
// with a port-scoped force-kill: after a short grace period we kill whatever is
// still LISTENING on the known port, but only if its image matches the expected
// process — so an unrelated owner of 8791/3939 is never touched.
func (s *supervisor) Stop() {
	deadline := time.Now().Add(s.stopTimeout)
	s.stopOne(s.webTask, s.webPort, "node", deadline)
	s.stopOne(s.sidecarTask, s.sidecarPort, "llama-server", deadline)
}

func (s *supervisor) stopOne(task string, port int, image string, deadline time.Time) {
	if task != "" {
		if out, err := runCmd("schtasks.exe", "/End", "/TN", task); err != nil {
			// "task is not running" lands here and is fine.
			s.log.Debug("supervisor: task end (non-fatal)", "task", task, "err", err, "out", trimOut(out))
		} else {
			s.log.Info("supervisor: task ended", "task", task)
		}
	}
	if port <= 0 {
		return
	}
	for {
		pid := listenerPID(port, image)
		if pid == 0 {
			return // port free, or held by something that isn't ours — leave it.
		}
		if time.Now().After(deadline) {
			if out, err := runCmd("taskkill.exe", "/PID", strconv.Itoa(pid), "/T", "/F"); err != nil {
				s.log.Warn("supervisor: force kill failed", "port", port, "pid", pid, "image", image, "err", err, "out", trimOut(out))
			} else {
				s.log.Info("supervisor: force killed lingering child", "port", port, "pid", pid, "image", image)
			}
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// listenerPID returns the PID LISTENING on port iff its process image matches
// name (e.g. "llama-server", "node"); otherwise 0. Scoping by image keeps the
// force-kill from ever touching an unrelated process that happens to hold the port.
func listenerPID(port int, name string) int {
	ps := fmt.Sprintf(
		"$c = Get-NetTCPConnection -LocalPort %d -State Listen -ErrorAction SilentlyContinue | Select-Object -First 1; "+
			"if ($c) { $p = Get-Process -Id $c.OwningProcess -ErrorAction SilentlyContinue; "+
			"if ($p -and $p.ProcessName -eq '%s') { $p.Id } }",
		port, name)
	out, err := runCmd("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", ps)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0
	}
	return pid
}

func runCmd(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return string(out), err
}

// trimOut keeps log lines short and single-ish-line for tool output.
func trimOut(s string) string {
	return strings.TrimSpace(s)
}
