// Package anthropic models the subset of the Anthropic Messages API that the
// DLP proxy needs to inspect and rewrite. Every structure round-trips through a
// raw field map so that unknown fields (added by future API versions or by
// Claude Code) are preserved verbatim when forwarding upstream. See spec §24.1.
package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"
)

// BlockNoticeSentinel is a non-secret marker embedded in DLP block-notice
// assistant messages. It lets the sanitizer recognize and remove a prior block
// notice together with the blocked turn it replied to. It is NOT a security
// mechanism (no signature) — under the honest-mistake threat model it only needs
// to be self-identifying.
const BlockNoticeSentinel = "<!-- LOCAL_DLP_NOTE -->"

// Request is a parsed /v1/messages (or count_tokens) request body. Top-level
// fields are kept as raw JSON so unrecognized keys survive a parse/serialize
// round-trip. Only the keys the proxy understands are decoded on demand.
type Request struct {
	F map[string]json.RawMessage
}

// ParseRequest decodes a request body, preserving all top-level fields.
func ParseRequest(body []byte) (*Request, error) {
	m := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("parse request: %w", err)
	}
	return &Request{F: m}, nil
}

// Marshal re-serializes the request, including any preserved unknown fields.
func (r *Request) Marshal() ([]byte, error) { return json.Marshal(r.F) }

// Stream reports whether the client requested an SSE stream.
func (r *Request) Stream() bool {
	var b bool
	_ = json.Unmarshal(r.F["stream"], &b)
	return b
}

// Model returns the requested model id (may be empty).
func (r *Request) Model() string { return rawString(r.F["model"]) }

// System returns the system prompt text. The system field may be a plain
// string or an array of text blocks; both are flattened to text.
func (r *Request) System() string {
	raw, ok := r.F["system"]
	if !ok {
		return ""
	}
	return flattenContentRaw(raw)
}

// Messages decodes the messages array.
func (r *Request) Messages() ([]Message, error) {
	raw, ok := r.F["messages"]
	if !ok {
		return nil, nil
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, fmt.Errorf("parse messages: %w", err)
	}
	out := make([]Message, 0, len(arr))
	for _, m := range arr {
		mm := map[string]json.RawMessage{}
		if err := json.Unmarshal(m, &mm); err != nil {
			return nil, fmt.Errorf("parse message: %w", err)
		}
		out = append(out, Message{F: mm})
	}
	return out, nil
}

// SetMessages writes the (possibly rewritten) messages array back into the
// request, replacing the previous value.
func (r *Request) SetMessages(msgs []Message) error {
	arr := make([]json.RawMessage, 0, len(msgs))
	for _, m := range msgs {
		b, err := json.Marshal(m.F)
		if err != nil {
			return err
		}
		arr = append(arr, b)
	}
	b, err := json.Marshal(arr)
	if err != nil {
		return err
	}
	r.F["messages"] = b
	return nil
}

// Message is a single conversation message, round-trip safe.
type Message struct {
	F map[string]json.RawMessage
}

// Role returns "user" or "assistant".
func (m Message) Role() string { return rawString(m.F["role"]) }

// ContentIsString reports whether content is a bare string (vs a block array).
func (m Message) ContentIsString() bool {
	raw, ok := m.F["content"]
	if !ok {
		return false
	}
	return len(raw) > 0 && raw[0] == '"'
}

// StringContent returns the content when it is a bare string.
func (m Message) StringContent() string { return rawString(m.F["content"]) }

// Blocks decodes content into typed blocks. A bare-string content yields a
// single synthetic text block (its Synthetic flag is set so callers know not to
// rely on structural identity).
func (m Message) Blocks() ([]Block, error) {
	raw, ok := m.F["content"]
	if !ok {
		return nil, nil
	}
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		return []Block{{Synthetic: true, F: map[string]json.RawMessage{
			"type": json.RawMessage(`"text"`),
			"text": raw,
		}}}, nil
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, fmt.Errorf("parse content: %w", err)
	}
	out := make([]Block, 0, len(arr))
	for _, b := range arr {
		bm := map[string]json.RawMessage{}
		if err := json.Unmarshal(b, &bm); err != nil {
			return nil, fmt.Errorf("parse content block: %w", err)
		}
		out = append(out, Block{F: bm})
	}
	return out, nil
}

// SetStringContent writes a bare-string content value, preserving the
// string-content shape (vs converting to a block array).
func (m *Message) SetStringContent(s string) {
	raw, _ := json.Marshal(s)
	m.F["content"] = raw
}

// SetBlocks writes blocks back as an array content.
func (m *Message) SetBlocks(blocks []Block) error {
	arr := make([]json.RawMessage, 0, len(blocks))
	for _, b := range blocks {
		raw, err := json.Marshal(b.F)
		if err != nil {
			return err
		}
		arr = append(arr, raw)
	}
	b, err := json.Marshal(arr)
	if err != nil {
		return err
	}
	m.F["content"] = b
	return nil
}

// FlatText returns all text carried by the message (string content, text
// blocks, and tool_result text), joined by newlines. Used to detect the block
// notice sentinel.
func (m Message) FlatText() string {
	if m.ContentIsString() {
		return m.StringContent()
	}
	blocks, err := m.Blocks()
	if err != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		switch b.Type() {
		case "text":
			parts = append(parts, b.Text())
		case "tool_result":
			parts = append(parts, b.ToolResultText())
		}
	}
	return strings.Join(parts, "\n")
}

// HasBlockNotice reports whether this message is (or contains) a DLP block
// notice, identified by the sentinel.
func (m Message) HasBlockNotice() bool {
	return strings.Contains(m.FlatText(), BlockNoticeSentinel)
}

// Block is a content block (text, tool_use, tool_result, ...), round-trip safe.
type Block struct {
	// Synthetic marks a block produced from a bare-string content; it has no
	// independent identity in the original JSON.
	Synthetic bool
	F         map[string]json.RawMessage
}

// Type returns the block type ("text", "tool_use", "tool_result", ...).
func (b Block) Type() string { return rawString(b.F["type"]) }

// Text returns the text of a text block.
func (b Block) Text() string { return rawString(b.F["text"]) }

// SetText replaces the text of a text block, preserving any other fields. Used
// by the bypass path to strip the override marker before forwarding.
func (b *Block) SetText(s string) {
	raw, _ := json.Marshal(s)
	b.F["text"] = raw
}

// ToolUseID returns the id of a tool_use block.
func (b Block) ToolUseID() string { return rawString(b.F["id"]) }

// ToolResultID returns the tool_use_id a tool_result block responds to.
func (b Block) ToolResultID() string { return rawString(b.F["tool_use_id"]) }

// ToolResultText flattens a tool_result's content (string or array of blocks).
func (b Block) ToolResultText() string { return flattenContentRaw(b.F["content"]) }

// SetToolResultText replaces a tool_result's content with placeholder text,
// preserving the tool_use_id pairing. Used only by the sanitizer fallback.
func (b *Block) SetToolResultText(s string) {
	raw, _ := json.Marshal(s)
	b.F["content"] = raw
}

// rawString decodes a JSON string value, returning "" for anything else.
func rawString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// flattenContentRaw extracts all text from a value that may be a string, or an
// array of content blocks (each possibly with a "text" or nested "content").
func flattenContentRaw(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		return rawString(raw)
	}
	var arr []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return ""
	}
	out := ""
	for _, blk := range arr {
		if t, ok := blk["text"]; ok {
			if out != "" {
				out += "\n"
			}
			out += rawString(t)
		} else if c, ok := blk["content"]; ok {
			if s := flattenContentRaw(c); s != "" {
				if out != "" {
					out += "\n"
				}
				out += s
			}
		}
	}
	return out
}
