package streaming

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

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
// Index is the provider's streaming correlation index; ID is the provider's
// streaming correlation ID. Either, both, or neither may be present on any
// given delta -- providers are free to omit, blank, repeat, or change them
// mid-stream, and the tracker is responsible for correlating fragments
// correctly regardless (see StreamingToolCallTracker).
type ToolCallDelta struct {
	Index            *int
	ID               string
	Name             string
	ArgumentsDelta   string
	ProviderMetadata map[string]interface{}
}

// ToolCallChunk is a chunk emitted by the tracker while processing or
// finalizing tool call deltas.
type ToolCallChunk = provider.StreamChunk

// trackedToolCall is the tracker's internal record for one tool call,
// corresponding to TS's TrackedToolCall (streaming-tool-call-tracker.ts).
type trackedToolCall struct {
	// id is the tool call's final, externally-visible ID. Blank until the
	// call's name is known (see createToolCallID / processExistingToolCall's
	// promotion path), since generating or claiming an ID has observable
	// side effects (claiming a wire ID, advancing bounded suffix counters)
	// that must not happen for a call that's still unattributable.
	id string
	// creationWireID is the wire ID (if any) carried by the delta that first
	// created this call. It is reused -- rather than re-derived -- if the
	// call's name only arrives on a later, correlated delta.
	creationWireID string
	index          *int
	sequence       int
	name           string
	args           string
	argumentState  *StreamingToolCallArgumentState
	hasFinished    bool
	inputStarted   bool
	metadata       map[string]interface{}
}

// resolutionKind mirrors TS's ToolCallResolution discriminated union
// ('existing' | 'new' | 'ambiguous').
type resolutionKind int

const (
	resolutionExisting resolutionKind = iota
	resolutionNew
	resolutionAmbiguous
)

type resolution struct {
	kind resolutionKind
	call *trackedToolCall
}

// StreamingToolCallTracker tracks streaming tool call state across multiple
// deltas from an OpenAI-compatible chat completion stream. It correlates
// fragments using wire ID, index, function name, incremental argument
// structure, explicit-start evidence, and ambiguity -- the same precedence
// TS's StreamingToolCallTracker uses -- rather than treating any single
// label as authoritative, since providers may omit, blank, repeat, or change
// IDs and indexes mid-stream.
//
// Used by openai, openai-compatible (and its embedders: fireworks, together,
// mistral, ollama, xai, azure, perplexity), groq, deepseek, alibaba, and
// moonshot.
type StreamingToolCallTracker struct {
	toolCalls               []*trackedToolCall
	toolCallsByID           map[string][]*trackedToolCall
	toolCallsByIndex        map[int][]*trackedToolCall
	usedToolCallIDs         map[string]struct{}
	nextGeneratedIDSuffixes map[string]int
	generateIDFunc          func() string
}

// NewStreamingToolCallTracker creates a tracker with the default ID generator.
func NewStreamingToolCallTracker() *StreamingToolCallTracker {
	return NewStreamingToolCallTrackerWithIDGenerator(nil)
}

// NewStreamingToolCallTrackerWithIDGenerator creates a tracker with a custom
// fallback ID generator used only when a tool call has no usable wire ID.
func NewStreamingToolCallTrackerWithIDGenerator(generateID func() string) *StreamingToolCallTracker {
	if generateID == nil {
		generateID = GenerateID
	}
	return &StreamingToolCallTracker{
		toolCallsByID:           make(map[string][]*trackedToolCall),
		toolCallsByIndex:        make(map[int][]*trackedToolCall),
		usedToolCallIDs:         make(map[string]struct{}),
		nextGeneratedIDSuffixes: make(map[string]int),
		generateIDFunc:          generateID,
	}
}

// Track records a streaming tool call delta. It emits tool input lifecycle
// chunks once the tool name is known; callers should emit Flush results when
// the provider stream finishes to close inputs and emit final tool-call
// chunks.
func (t *StreamingToolCallTracker) Track(index *int, id, name, delta string) []ToolCallChunk {
	return t.TrackDelta(ToolCallDelta{
		Index:          index,
		ID:             id,
		Name:           name,
		ArgumentsDelta: delta,
	})
}

// TrackDelta records a streaming tool call delta and preserves provider
// metadata for the finalized tool call. It mirrors TS
// StreamingToolCallTracker.processDelta exactly for well-formed streams,
// and ports its correlation precedence for unreliable ones (see
// resolveToolCall).
func (t *StreamingToolCallTracker) TrackDelta(delta ToolCallDelta) []ToolCallChunk {
	if t == nil {
		return nil
	}

	wireID := nonBlank(delta.ID)
	name := nonBlank(delta.Name)
	index := delta.Index
	hasExplicitCallStart := name != "" && StartsWithStructuredValue(delta.ArgumentsDelta)

	res := t.resolveToolCall(wireID, index, name, hasExplicitCallStart)
	if res.kind == resolutionAmbiguous {
		return nil
	}

	var call *trackedToolCall
	var chunks []ToolCallChunk

	if res.kind == resolutionNew {
		call = t.processNewToolCall(delta, wireID, index, name)
		if name != "" {
			chunks = t.startChunksForNewCall(call)
		}
		// A blank, whitespace-only, or entirely missing function name cannot
		// start a usable call. TS ignores a present-but-blank name here and
		// throws synchronously for an entirely absent one (a distinction Go
		// cannot represent, since a plain string can't tell "absent" from
		// "blank" apart); Track/TrackDelta never returns an error, so both
		// are handled the same deferred way: the call is buffered without
		// emitting input-start/delta chunks, and either gets promoted once a
		// correlated delta supplies a name (see processExistingToolCall), or
		// is reported as an error chunk from Flush if one never arrives.
	} else {
		call = res.call
		if wireID != "" {
			t.associateWireID(call, wireID)
		}
		chunks = t.processExistingToolCall(call, delta, name)
	}

	if index != nil {
		t.associateIndex(call, *index)
	}

	return chunks
}

// Flush finalizes any unfinished tool calls. Should be called during the
// stream's flush handler to ensure all tool calls are properly completed.
func (t *StreamingToolCallTracker) Flush() []ToolCallChunk {
	if t == nil || len(t.toolCalls) == 0 {
		return nil
	}

	// Index order is reliable only when every call has an index. For mixed
	// streams, keep insertion order rather than moving all index-less calls
	// behind indexed calls.
	allIndexed := true
	for _, call := range t.toolCalls {
		if call.index == nil {
			allIndexed = false
			break
		}
	}

	calls := append([]*trackedToolCall(nil), t.toolCalls...)
	if allIndexed {
		sort.SliceStable(calls, func(i, j int) bool {
			if *calls[i].index != *calls[j].index {
				return *calls[i].index < *calls[j].index
			}
			return calls[i].sequence < calls[j].sequence
		})
	}

	chunks := make([]ToolCallChunk, 0, len(calls))
	for _, call := range calls {
		if call.hasFinished {
			continue
		}
		chunks = append(chunks, t.finishToolCall(call)...)
	}

	t.toolCalls = nil
	t.toolCallsByID = make(map[string][]*trackedToolCall)
	t.toolCallsByIndex = make(map[int][]*trackedToolCall)
	return chunks
}

// resolveToolCall implements the correlation precedence for streamed deltas,
// porting TS StreamingToolCallTracker.resolveToolCall's table exactly:
//
//	| ID evidence | index/name evidence | start evidence | resolution |
//	| --- | --- | --- | --- |
//	| known | matching | any | matching call, new call, or ambiguity |
//	| known | conflicting | named | new call |
//	| unseen | matching | structured start | new call |
//	| unseen | matching | continuation | matching call or ambiguity |
//	| absent | matching | any | matching call, new call, or ambiguity |
//	| absent | absent | named | new call |
//	| absent | absent | unnamed | sole unfinished call, new call, or ambiguity |
func (t *StreamingToolCallTracker) resolveToolCall(wireID string, index *int, name string, hasExplicitCallStart bool) resolution {
	var indexedCalls []*trackedToolCall
	if index != nil {
		indexedCalls = t.toolCallsByIndex[*index]
	}
	matchingIndexed := filterByName(indexedCalls, name)

	if wireID != "" {
		idCalls := t.toolCallsByID[wireID]

		if idCalls != nil {
			if index != nil {
				matching := intersectCalls(matchingIndexed, idCalls)
				matchingToolCall := resolveMatchingToolCall(matching, hasExplicitCallStart)
				if matchingToolCall.kind != resolutionNew {
					return matchingToolCall
				}

				// A named delta with a distinct index starts a new call even
				// when its wire ID and function name repeat. Providers may
				// reuse IDs across parallel calls, so the ID/name pair
				// cannot override index evidence.
				if name != "" {
					return resolution{kind: resolutionNew}
				}

				// Conflicting labels on a continuation cannot be resolved
				// safely.
				if indexedCalls != nil {
					return resolution{kind: resolutionAmbiguous}
				}

				return resolveMatchingToolCall(idCalls, false)
			}

			if name != "" {
				return resolveMatchingToolCall(filterByName(idCalls, name), hasExplicitCallStart)
			}

			return resolveMatchingToolCall(idCalls, false)
		}

		if len(matchingIndexed) > 0 {
			// A previously unseen ID plus a named structured argument start
			// is stronger evidence of a distinct call than a reused
			// index/name. This also keeps interleaved same-name calls
			// separate while still allowing IDs to change on ordinary
			// continuation fragments.
			if hasExplicitCallStart {
				return resolution{kind: resolutionNew}
			}
			return resolveMatchingToolCall(matchingIndexed, false)
		}

		return resolution{kind: resolutionNew}
	}

	if indexedCalls != nil {
		// Repeated names are valid on continuations. A different name at the
		// same index is evidence of a new call from a provider that reuses
		// indices across parallel calls.
		return resolveMatchingToolCall(matchingIndexed, hasExplicitCallStart)
	}

	if name != "" {
		return resolution{kind: resolutionNew}
	}

	var unfinished []*trackedToolCall
	for _, call := range t.toolCalls {
		if !call.hasFinished {
			unfinished = append(unfinished, call)
		}
	}
	if len(unfinished) == 1 {
		return resolution{kind: resolutionExisting, call: unfinished[0]}
	}
	if len(unfinished) > 1 {
		return resolution{kind: resolutionAmbiguous}
	}
	return resolution{kind: resolutionNew}
}

// filterByName narrows calls to those whose name matches, porting TS's
// filterToolCallsByName. A blank requested name matches everything (no
// filter). A candidate call that is itself still nameless (Go's deferred
// buffering for a call whose name hasn't arrived yet -- see
// processNewToolCall) matches any requested name, since it has not yet
// committed to one; TS has no equivalent state here, as it never creates a
// tracked call without a name.
func filterByName(calls []*trackedToolCall, name string) []*trackedToolCall {
	if name == "" {
		return calls
	}
	var out []*trackedToolCall
	for _, call := range calls {
		if call.name == "" || call.name == name {
			out = append(out, call)
		}
	}
	return out
}

// resolveMatchingToolCall ports TS's StreamingToolCallTracker.resolveMatchingToolCall.
func resolveMatchingToolCall(calls []*trackedToolCall, hasExplicitCallStart bool) resolution {
	if len(calls) == 0 {
		return resolution{kind: resolutionNew}
	}

	if !hasExplicitCallStart {
		if len(calls) == 1 {
			return resolution{kind: resolutionExisting, call: calls[0]}
		}
		return resolution{kind: resolutionAmbiguous}
	}

	// A repeated name can occur on continuations. A fresh structured
	// argument prefix is evidence of another call only after the matching
	// call has completed its own structured argument payload.
	var continuable []*trackedToolCall
	for _, call := range calls {
		if !call.argumentState.HasCompleteStructuredValue() {
			continuable = append(continuable, call)
		}
	}

	if len(continuable) == 1 {
		return resolution{kind: resolutionExisting, call: continuable[0]}
	}
	if len(continuable) > 1 {
		return resolution{kind: resolutionAmbiguous}
	}
	return resolution{kind: resolutionNew}
}

// processNewToolCall creates a tracked call for a resolutionNew delta. If
// name is blank the call is left pending (no ID claimed yet): see the
// TrackDelta and processExistingToolCall doc comments for why.
func (t *StreamingToolCallTracker) processNewToolCall(delta ToolCallDelta, wireID string, index *int, name string) *trackedToolCall {
	call := &trackedToolCall{
		creationWireID: wireID,
		index:          cloneIntPtr(index),
		sequence:       len(t.toolCalls),
		name:           name,
		args:           delta.ArgumentsDelta,
		argumentState:  NewStreamingToolCallArgumentState(delta.ArgumentsDelta),
		metadata:       delta.ProviderMetadata,
	}
	if name != "" {
		call.id = t.createToolCallID(wireID)
	}
	t.toolCalls = append(t.toolCalls, call)
	if wireID != "" {
		t.associateWireID(call, wireID)
	}
	return call
}

// startChunksForNewCall emits the tool-input-start (and, if arguments are
// already present, an initial tool-input-delta) for a freshly named call.
func (t *StreamingToolCallTracker) startChunksForNewCall(call *trackedToolCall) []ToolCallChunk {
	call.inputStarted = true
	chunks := []ToolCallChunk{
		{
			Type:     provider.ChunkTypeToolInputStart,
			ToolCall: &types.ToolCall{ID: call.id, ToolName: call.name},
		},
	}
	if call.args != "" {
		chunks = append(chunks, provider.StreamChunk{
			Type:     provider.ChunkTypeToolInputDelta,
			Text:     call.args,
			ToolCall: &types.ToolCall{ID: call.id, ToolName: call.name},
		})
	}
	return chunks
}

// processExistingToolCall appends a continuation delta's arguments to an
// already-resolved call.
//
// If the call is still pending (its name has never arrived -- see
// processNewToolCall) and this delta supplies one, it is promoted exactly
// like a brand-new call: tool-input-start, then one combined delta for
// everything buffered while nameless, using the wire ID (if any) it was
// originally created with. This is what lets a name that arrives on a later,
// correlated delta still produce a usable tool call instead of being
// silently lost.
func (t *StreamingToolCallTracker) processExistingToolCall(call *trackedToolCall, delta ToolCallDelta, name string) []ToolCallChunk {
	if call.hasFinished {
		return nil
	}

	if len(delta.ProviderMetadata) > 0 && call.metadata == nil {
		call.metadata = delta.ProviderMetadata
	}

	wasPending := call.name == ""
	if wasPending && name != "" {
		call.name = name
	}

	if delta.ArgumentsDelta != "" {
		call.argumentState.Append(delta.ArgumentsDelta)
		call.args += delta.ArgumentsDelta
	}

	if call.name == "" {
		// Still pending: nothing to emit until a name arrives.
		return nil
	}

	if wasPending {
		call.id = t.createToolCallID(call.creationWireID)
		return t.startChunksForNewCall(call)
	}

	if delta.ArgumentsDelta == "" {
		return nil
	}
	return []ToolCallChunk{
		{
			Type:     provider.ChunkTypeToolInputDelta,
			Text:     delta.ArgumentsDelta,
			ToolCall: &types.ToolCall{ID: call.id, ToolName: call.name},
		},
	}
}

// finishToolCall emits the terminal chunks for one call during Flush.
func (t *StreamingToolCallTracker) finishToolCall(call *trackedToolCall) []ToolCallChunk {
	if call.name == "" {
		// Never got a name: TS throws synchronously from processDelta the
		// moment this is known (processNewToolCall); Track/TrackDelta cannot
		// return an error, so Go defers the same failure to Flush.
		if call.id == "" {
			call.id = t.generateIDFunc()
		}
		call.hasFinished = true
		return []ToolCallChunk{
			{
				Type: provider.ChunkTypeError,
				Text: "Expected 'function.name' to be a string.",
				ToolCall: &types.ToolCall{
					ID:           call.id,
					RawArguments: call.args,
				},
			},
		}
	}

	var chunks []ToolCallChunk
	if call.inputStarted {
		chunks = append(chunks, provider.StreamChunk{
			Type:     provider.ChunkTypeToolInputEnd,
			ToolCall: &types.ToolCall{ID: call.id, ToolName: call.name},
		})
	}

	args := parseToolArguments(call.args)
	chunks = append(chunks, provider.StreamChunk{
		Type: provider.ChunkTypeToolCall,
		ToolCall: &types.ToolCall{
			ID:               call.id,
			ToolName:         call.name,
			Arguments:        args,
			RawArguments:     call.args,
			ProviderMetadata: call.metadata,
		},
	})
	call.hasFinished = true
	return chunks
}

func (t *StreamingToolCallTracker) associateWireID(call *trackedToolCall, wireID string) {
	calls := t.toolCallsByID[wireID]
	for _, c := range calls {
		if c == call {
			return
		}
	}
	t.toolCallsByID[wireID] = append(calls, call)
}

func (t *StreamingToolCallTracker) associateIndex(call *trackedToolCall, index int) {
	calls := t.toolCallsByIndex[index]
	for _, c := range calls {
		if c == call {
			return
		}
	}
	t.toolCallsByIndex[index] = append(calls, call)
}

// createToolCallID claims a usable, unique tool call ID: the wire ID if it
// is non-blank and not already claimed, otherwise a generated ID, with
// bounded numeric suffixes to resolve collisions deterministically. Ports TS
// StreamingToolCallTracker.createToolCallId.
func (t *StreamingToolCallTracker) createToolCallID(wireID string) string {
	if wireID != "" {
		if _, used := t.usedToolCallIDs[wireID]; !used {
			t.usedToolCallIDs[wireID] = struct{}{}
			return wireID
		}
	}

	generated := nonBlank(t.generateIDFunc())
	if generated == "" {
		generated = "tool-call"
	}

	if _, used := t.usedToolCallIDs[generated]; !used {
		t.usedToolCallIDs[generated] = struct{}{}
		return generated
	}

	// Resume after the last suffix checked for this generated value. This
	// keeps deterministic generators bounded without repeatedly rescanning
	// the same occupied suffixes.
	initialSuffix := t.nextGeneratedIDSuffixes[generated]
	if initialSuffix == 0 {
		initialSuffix = 1
	}
	maxSuffix := initialSuffix + len(t.usedToolCallIDs)
	for suffix := initialSuffix; suffix <= maxSuffix; suffix++ {
		candidate := fmt.Sprintf("%s-%d", generated, suffix)
		if _, used := t.usedToolCallIDs[candidate]; !used {
			t.usedToolCallIDs[candidate] = struct{}{}
			t.nextGeneratedIDSuffixes[generated] = suffix + 1
			return candidate
		}
	}

	// The bounded search above is guaranteed to return by the pigeonhole
	// principle; TS throws defensively here in case that invariant is ever
	// violated by a future change. Track/TrackDelta cannot return an error,
	// so fall back to a cryptographically random suffix instead -- shared
	// streaming infrastructure must never panic.
	for {
		candidate := generated + "-" + GenerateID()
		if _, used := t.usedToolCallIDs[candidate]; !used {
			t.usedToolCallIDs[candidate] = struct{}{}
			return candidate
		}
	}
}

func intersectCalls(a, b []*trackedToolCall) []*trackedToolCall {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	var out []*trackedToolCall
	for _, c := range a {
		for _, d := range b {
			if c == d {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

func cloneIntPtr(p *int) *int {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// nonBlank returns value unchanged if it has any non-whitespace content,
// otherwise "". Ports TS StreamingToolCallTracker.getNonBlankString; the
// returned string is never trimmed, only tested for blankness.
func nonBlank(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return value
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
