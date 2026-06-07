package proxy

import (
	"strings"

	"local-lfm-dlp-proxy/internal/anthropic"
)

// containsBypassMarker reports whether the message carries the explicit user
// override marker in its USER-AUTHORED text: a bare-string content or any
// type:"text" block. It deliberately ignores tool_result/tool_use content so a
// file the agent read (or shell output) that happens to contain the marker can
// never silently disable DLP — only text the user actually typed counts.
// Matching is a deterministic case-sensitive substring check (never the LFM),
// preserving the "inspected text is inert data" invariant.
func containsBypassMarker(m anthropic.Message, marker string) bool {
	if marker == "" {
		return false
	}
	if m.ContentIsString() {
		return strings.Contains(m.StringContent(), marker)
	}
	blocks, err := m.Blocks()
	if err != nil {
		return false
	}
	for _, b := range blocks {
		if b.Type() == "text" && strings.Contains(b.Text(), marker) {
			return true
		}
	}
	return false
}

// stripBypassMarker removes the override marker from the message's user-authored
// text (string content + text blocks) so it is not forwarded to Claude. It is
// shape-preserving: string content stays a string, a block array stays a block
// array. Text blocks that become empty after stripping are dropped (the API
// rejects empty text blocks); non-text blocks are left untouched.
//
// It returns residualEmpty=true when stripping would leave the message with no
// sendable content (e.g. the message was only the marker). In that case the
// caller should forward the original message unchanged rather than emit an
// empty-content request.
func stripBypassMarker(m *anthropic.Message, marker string) (residualEmpty bool) {
	if marker == "" {
		return false
	}
	if m.ContentIsString() {
		stripped := strings.ReplaceAll(m.StringContent(), marker, "")
		if strings.TrimSpace(stripped) == "" {
			return true
		}
		m.SetStringContent(stripped)
		return false
	}
	blocks, err := m.Blocks()
	if err != nil {
		return false
	}
	kept := make([]anthropic.Block, 0, len(blocks))
	for i := range blocks {
		b := blocks[i]
		if b.Type() == "text" {
			t := strings.ReplaceAll(b.Text(), marker, "")
			if strings.TrimSpace(t) == "" {
				continue // drop now-empty text block
			}
			b.SetText(t)
		}
		kept = append(kept, b)
	}
	if len(kept) == 0 {
		return true
	}
	if err := m.SetBlocks(kept); err != nil {
		return false
	}
	return false
}
