package harness

import (
	"context"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// HistoryMessage is one message from the conversation history a runtime
// itself persisted, normalized by the adapter. Reuses the same content-part
// vocabulary as types.Message (text, reasoning, tool-call with input,
// tool-result with output, ...) so a UI can render history and the live
// stream with the same components, and carries the runtime's raw record via
// HarnessMetadata so a host can fall back to adapter-specific rendering.
//
// Mirrors TS `HarnessV1Message`: a discriminated union of
// HarnessV1UserMessage/HarnessV1AssistantMessage/HarnessV1ToolMessage, each
// restricted to its role's valid V4 prompt content-part types with
// `providerOptions`/`providerMetadata` omitted. Go does not enforce which
// types.ContentPart variants are valid for a given Role at compile time the
// way TS's per-role union does, and types.Message's ProviderOptions field
// has no TS equivalent here; adapters are expected to populate only the
// TS-valid subset and leave ProviderOptions unset.
type HistoryMessage struct {
	types.Message

	// At is the message's timestamp, when the adapter's runtime records one.
	At string `json:"at,omitempty"`

	// HarnessMetadata is adapter-namespaced opaque data for this message,
	// e.g. the runtime's raw record.
	HarnessMetadata Metadata `json:"harnessMetadata,omitempty"`
}

// ReadHistoryResult is the result of AgentSession.ReadHistory /
// Session.DoReadHistory. Mirrors TS `HarnessV1ReadHistoryResult`.
type ReadHistoryResult struct {
	Messages []HistoryMessage `json:"messages"`

	// Cursor is an opaque position; pass it back as the next read's Since to
	// read only what follows.
	Cursor string `json:"cursor"`
}

// HistoryReader is an optional Session capability: adapters that can read
// their runtime's persisted conversation history implement it. A session's
// history can grow outside the harness contract: the same runtime
// conversation may be continued interactively (e.g. `claude --resume`), by
// another process, or before this session attached. Hosts that render a
// continuous record of the conversation — not just the turns they drove —
// need to read that history back, and the runtime's own store is the only
// source that has it. The adapter owns its runtime's persistence format, so
// the read belongs here rather than in every host.
//
// since is a previous result's Cursor; the result then contains only
// messages recorded after it. The cursor is adapter-owned and opaque to the
// host. An empty string means "from the start".
//
// An adapter that cannot read its runtime's store from the current
// environment (e.g. it lives inside a remote sandbox) returns
// HistoryUnavailableError. A conversation with no recorded messages yet is
// not an error — it resolves to an empty Messages slice. Mirrors TS
// `HarnessV1Session.doReadHistory`.
type HistoryReader interface {
	DoReadHistory(ctx context.Context, since string) (*ReadHistoryResult, error)
}

// ReadHistory reads the conversation history the runtime itself persisted,
// normalized by the adapter. Pass a previous result's Cursor as since to
// read only the delta.
//
// Returns CapabilityUnsupportedError when the adapter does not implement
// HistoryReader, and whatever error the adapter's DoReadHistory returns
// otherwise (typically HistoryUnavailableError when the adapter supports
// history reads but cannot reach the runtime's store from this
// environment). Mirrors TS `HarnessAgentSession.readHistory`.
func (s *AgentSession) ReadHistory(ctx context.Context, since string) (*ReadHistoryResult, error) {
	s.mu.Lock()
	active := s.sessionState == SessionStateActive
	underlying := s.underlying
	s.mu.Unlock()
	if !active || underlying == nil {
		return nil, fmt.Errorf("harness session '%s' is not active and cannot read history", s.sessionID)
	}
	reader, ok := underlying.(HistoryReader)
	if !ok {
		return nil, NewCapabilityUnsupportedError(
			fmt.Sprintf("Harness '%s' does not support reading the runtime's conversation history.", s.harness.HarnessID()),
			s.harness.HarnessID(), nil,
		)
	}
	return reader.DoReadHistory(ctx, since)
}
