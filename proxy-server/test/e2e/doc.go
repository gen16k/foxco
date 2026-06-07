// Package e2e contains opt-in end-to-end tests that drive a real `claude` child
// process through the proxy, the way the proxy is actually used.
//
// The tests are gated behind the `e2e` build tag and additionally self-skip
// unless `claude` is on PATH (and, for the LFM scenario, the llama.cpp sidecar
// is healthy). So the default `go test ./...` does not run them and is
// unaffected by this directory. This file (no build tag) only exists so the
// package always has a buildable Go file under the default tag set.
//
// See README.md for how to run both the free mock-upstream dry-run and the
// real-upstream (subscription) pass.
package e2e
