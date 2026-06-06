// Package sanitizer removes sensitive history units from a message list before
// the request is forwarded upstream. It is structure-aware: it removes matched
// tool_use<->tool_result pairs and user<->block-notice turn pairs as whole
// units (the user's "make it vanish" model), cleans up emptied messages, then
// validates that the result is a legal Anthropic message array. If a valid
// structure cannot be produced it returns an error so the caller fails closed.
package sanitizer

import (
	"errors"

	"local-lfm-dlp-proxy/internal/anthropic"
	"local-lfm-dlp-proxy/internal/dlp"
)

// ErrInvalidStructure means removal could not yield a forwardable message array
// (e.g. the leading user turn was removed, or tool_use/tool_result pairing broke
// in a way we could not repair). The caller must fail closed.
var ErrInvalidStructure = errors.New("sanitized messages failed structural validation")

type wmsg struct {
	src       anthropic.Message
	role      string
	isString  bool
	blocks    []anthropic.Block
	dropBlock []bool
	dropMsg   bool
}

type loc struct{ m, b int }

// Sanitize removes the units containing the given sensitive history segments.
// ng must contain only history segments (the caller blocks live-turn hits
// before reaching here). On success it returns a rewritten slice safe to
// forward; on failure it returns ErrInvalidStructure.
func Sanitize(msgs []anthropic.Message, ng []dlp.Segment) ([]anthropic.Message, error) {
	if len(ng) == 0 {
		return msgs, nil
	}

	work := make([]wmsg, len(msgs))
	for i, m := range msgs {
		w := wmsg{src: m, role: m.Role()}
		if m.ContentIsString() {
			w.isString = true
		} else {
			blocks, err := m.Blocks()
			if err != nil {
				return nil, err
			}
			w.blocks = blocks
			w.dropBlock = make([]bool, len(blocks))
		}
		work[i] = w
	}

	// Index tool_use blocks by id so a tool_result removal can take its partner.
	toolUse := map[string]loc{}
	for i := range work {
		for j, b := range work[i].blocks {
			if b.Type() == "tool_use" {
				toolUse[b.ToolUseID()] = loc{i, j}
			}
		}
	}

	for _, s := range ng {
		if s.MsgIndex < 0 || s.MsgIndex >= len(work) {
			continue
		}
		switch s.Type {
		case dlp.SegToolResult:
			markBlock(&work[s.MsgIndex], s.BlockIndex)
			if l, ok := toolUse[s.ToolUseID]; ok {
				markBlock(&work[l.m], l.b)
			}
		case dlp.SegUserText:
			if s.BlockIndex < 0 {
				work[s.MsgIndex].dropMsg = true
				dropFollowingBlockNotice(work, s.MsgIndex)
			} else {
				markBlock(&work[s.MsgIndex], s.BlockIndex)
			}
		}
	}

	var out []anthropic.Message
	for i := range work {
		w := &work[i]
		if w.dropMsg {
			continue
		}
		if w.isString {
			out = append(out, w.src)
			continue
		}
		var kept []anthropic.Block
		for j, b := range w.blocks {
			if w.dropBlock[j] {
				continue
			}
			kept = append(kept, b)
		}
		if len(kept) == 0 {
			// Message emptied by removals: drop it and any block notice replying
			// to it, so the (turn, notice) unit vanishes together.
			dropFollowingBlockNotice(work, i)
			continue
		}
		nm := w.src
		if err := nm.SetBlocks(kept); err != nil {
			return nil, err
		}
		out = append(out, nm)
	}

	if err := validate(out); err != nil {
		return nil, err
	}
	return out, nil
}

func markBlock(w *wmsg, idx int) {
	if idx >= 0 && idx < len(w.dropBlock) {
		w.dropBlock[idx] = true
		return
	}
	if w.isString {
		w.dropMsg = true
	}
}

// dropFollowingBlockNotice marks the assistant block-notice immediately after
// msgIndex (if any) for removal.
func dropFollowingBlockNotice(work []wmsg, msgIndex int) {
	k := msgIndex + 1
	if k < len(work) && work[k].role == "assistant" && work[k].src.HasBlockNotice() {
		work[k].dropMsg = true
	}
}

// validate enforces the Anthropic message-array invariants the proxy relies on:
// non-empty, first message is a user message, and every tool_use has a matching
// tool_result (and vice versa).
func validate(out []anthropic.Message) error {
	if len(out) == 0 {
		return ErrInvalidStructure
	}
	if out[0].Role() != "user" {
		return ErrInvalidStructure
	}
	open := map[string]bool{}
	for _, m := range out {
		blocks, err := m.Blocks()
		if err != nil {
			return ErrInvalidStructure
		}
		for _, b := range blocks {
			switch b.Type() {
			case "tool_use":
				open[b.ToolUseID()] = true
			case "tool_result":
				id := b.ToolResultID()
				if !open[id] {
					return ErrInvalidStructure // tool_result with no preceding tool_use
				}
				delete(open, id)
			}
		}
	}
	if len(open) > 0 {
		return ErrInvalidStructure // tool_use with no following tool_result
	}
	return nil
}
