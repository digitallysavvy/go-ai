package streaming

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// GenerateID returns a compact random ID for provider deltas that omit a tool
// call ID. It is intentionally local to streaming utilities to avoid coupling
// providers to higher-level core helpers.
func GenerateID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "call_fallback"
	}
	return "call_" + hex.EncodeToString(b[:])
}

// ToolCallDelta is a provider-neutral OpenAI-compatible tool call delta.
// Index is the provider's streaming correlation index. When Index is nil, ID is
// used as the fallback correlation key.
type ToolCallDelta struct {
	Index            *int
	ID               string
	Name             string
	ArgumentsDelta   string
	ProviderMetadata map[string]interface{}
}

// ToolCallChunk is the complete tool call chunk finalized by the tracker.
type ToolCallChunk = provider.StreamChunk

type accumToolCall struct {
	index            *int
	id               string
	name             string
	args             string
	providerMetadata map[string]interface{}
	sequence         int
	inputStarted     bool
}

// StreamingToolCallTracker accumulates streaming tool call deltas by provider
// index, falling back to tool call ID for providers that omit indexes.
//
// Deltas are buffered until the function name arrives. Flush finalizes all
// unfinished calls, even if their JSON arguments are incomplete.
type StreamingToolCallTracker struct {
	accumMap       map[int]*accumToolCall
	fallbackMap    map[string]*accumToolCall
	ordered        []*accumToolCall
	nextSequence   int
	generateIDFunc func() string
}

// NewStreamingToolCallTracker creates a tracker with the default ID generator.
func NewStreamingToolCallTracker() *StreamingToolCallTracker {
	return NewStreamingToolCallTrackerWithIDGenerator(nil)
}

// NewStreamingToolCallTrackerWithIDGenerator creates a tracker with a custom
// fallback ID generator used only when a finalized call has no provider ID.
func NewStreamingToolCallTrackerWithIDGenerator(generateID func() string) *StreamingToolCallTracker {
	if generateID == nil {
		generateID = GenerateID
	}
	return &StreamingToolCallTracker{
		accumMap:       make(map[int]*accumToolCall),
		fallbackMap:    make(map[string]*accumToolCall),
		generateIDFunc: generateID,
	}
}

// Track records a streaming tool call delta. It does not emit partial tool
// calls; callers should emit Flush results when the provider stream finishes.
func (t *StreamingToolCallTracker) Track(index *int, id, name, delta string) []ToolCallChunk {
	return t.TrackDelta(ToolCallDelta{
		Index:          index,
		ID:             id,
		Name:           name,
		ArgumentsDelta: delta,
	})
}

// TrackDelta records a streaming tool call delta and preserves provider
// metadata for the finalized tool call.
func (t *StreamingToolCallTracker) TrackDelta(delta ToolCallDelta) []ToolCallChunk {
	if t == nil {
		return nil
	}
	call := t.lookup(delta.Index, delta.ID)
	if call == nil {
		call = &accumToolCall{sequence: t.nextSequence}
		t.nextSequence++
		t.ordered = append(t.ordered, call)
	}
	if delta.Index != nil {
		idx := *delta.Index
		call.index = &idx
		t.accumMap[idx] = call
	}
	if delta.ID != "" {
		call.id = delta.ID
		t.fallbackMap[delta.ID] = call
	}
	if delta.Name != "" {
		call.name = delta.Name
	}
	if delta.ArgumentsDelta != "" {
		call.args += delta.ArgumentsDelta
	}
	if len(delta.ProviderMetadata) > 0 && call.providerMetadata == nil {
		call.providerMetadata = delta.ProviderMetadata
	}
	return t.buildInputChunks(call, delta.ArgumentsDelta)
}

// Flush finalizes all tracked calls in provider index order where indexes are
// available, followed by fallback-only calls in arrival order.
func (t *StreamingToolCallTracker) Flush() []ToolCallChunk {
	if t == nil || len(t.ordered) == 0 {
		return nil
	}
	calls := append([]*accumToolCall(nil), t.ordered...)
	sort.SliceStable(calls, func(i, j int) bool {
		left, right := calls[i], calls[j]
		switch {
		case left.index != nil && right.index != nil:
			return *left.index < *right.index
		case left.index != nil:
			return true
		case right.index != nil:
			return false
		default:
			return left.sequence < right.sequence
		}
	})

	chunks := make([]ToolCallChunk, 0, len(calls))
	for _, call := range calls {
		id := call.id
		if id == "" {
			id = t.generateIDFunc()
			call.id = id
		}
		if call.inputStarted {
			chunks = append(chunks, provider.StreamChunk{
				Type: provider.ChunkTypeToolInputEnd,
				ToolCall: &types.ToolCall{
					ID:       id,
					ToolName: call.name,
				},
			})
		}
		args := parseToolArguments(call.args)
		chunks = append(chunks, provider.StreamChunk{
			Type: provider.ChunkTypeToolCall,
			ToolCall: &types.ToolCall{
				ID:               id,
				ToolName:         call.name,
				Arguments:        args,
				RawArguments:     call.args,
				ProviderMetadata: call.providerMetadata,
			},
		})
	}
	t.accumMap = make(map[int]*accumToolCall)
	t.fallbackMap = make(map[string]*accumToolCall)
	t.ordered = nil
	return chunks
}

func (t *StreamingToolCallTracker) buildInputChunks(call *accumToolCall, delta string) []ToolCallChunk {
	if call.name == "" {
		return nil
	}
	chunks := make([]ToolCallChunk, 0, 2)
	if !call.inputStarted {
		if call.id == "" {
			call.id = t.generateIDFunc()
		}
		call.inputStarted = true
		chunks = append(chunks, provider.StreamChunk{
			Type: provider.ChunkTypeToolInputStart,
			ToolCall: &types.ToolCall{
				ID:       call.id,
				ToolName: call.name,
			},
		})
		if call.args != "" {
			chunks = append(chunks, provider.StreamChunk{
				Type: provider.ChunkTypeToolInputDelta,
				Text: call.args,
				ToolCall: &types.ToolCall{
					ID:       call.id,
					ToolName: call.name,
				},
			})
		}
		return chunks
	}
	if delta != "" {
		chunks = append(chunks, provider.StreamChunk{
			Type: provider.ChunkTypeToolInputDelta,
			Text: delta,
			ToolCall: &types.ToolCall{
				ID:       call.id,
				ToolName: call.name,
			},
		})
	}
	return chunks
}

func (t *StreamingToolCallTracker) lookup(index *int, id string) *accumToolCall {
	if index != nil {
		if call := t.accumMap[*index]; call != nil {
			return call
		}
	}
	if id != "" {
		if call := t.fallbackMap[id]; call != nil {
			return call
		}
	}
	return nil
}

func parseToolArguments(raw string) map[string]interface{} {
	if raw == "" {
		return map[string]interface{}{}
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil
	}
	if args == nil {
		return map[string]interface{}{}
	}
	return args
}
