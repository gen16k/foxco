//go:build !windows

package main

// Non-Windows builds have no scheduled tasks / iGPU sidecar; the supervisor is a
// no-op so the binary still builds and tests on any OS.
func (s *supervisor) Start() {}

func (s *supervisor) Stop() {}
