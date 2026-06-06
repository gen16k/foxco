package dlp

import "local-lfm-dlp-proxy/internal/anthropic"

// Segment splits a message list into the atomic text units the proxy inspects.
//
// MVP scope: user-role content is the egress-sensitive surface (what the
// developer types, plus tool_result blocks carrying file reads / shell output /
// git diff). Assistant content is model output (including our own block
// notices) and is not classified. System prompt and tool definitions are out of
// MVP scope (Claude Code's system prompt is boilerplate, not user data).
func Segmentize(msgs []anthropic.Message) ([]Segment, error) {
	var segs []Segment
	for i, m := range msgs {
		if m.Role() != "user" {
			continue
		}
		if m.ContentIsString() {
			text := Normalize(m.StringContent())
			if text == "" {
				continue
			}
			segs = append(segs, Segment{
				Type:       SegUserText,
				Text:       text,
				MsgIndex:   i,
				BlockIndex: -1,
				Role:       m.Role(),
			})
			continue
		}
		blocks, err := m.Blocks()
		if err != nil {
			return nil, err
		}
		for j, b := range blocks {
			switch b.Type() {
			case "text":
				text := Normalize(b.Text())
				if text == "" {
					continue
				}
				segs = append(segs, Segment{
					Type:       SegUserText,
					Text:       text,
					MsgIndex:   i,
					BlockIndex: j,
					Role:       m.Role(),
				})
			case "tool_result":
				text := Normalize(b.ToolResultText())
				if text == "" {
					continue
				}
				segs = append(segs, Segment{
					Type:       SegToolResult,
					Text:       text,
					MsgIndex:   i,
					BlockIndex: j,
					ToolUseID:  b.ToolResultID(),
					Role:       m.Role(),
				})
			}
		}
	}
	return segs, nil
}

// LastMessageIndex returns the index of the final message, or -1 if empty. It
// defines the boundary between the "live" turn (classified for a BLOCK
// decision) and "history" (classified to decide what to sanitize before
// forwarding).
func LastMessageIndex(msgs []anthropic.Message) int {
	return len(msgs) - 1
}
