// Package langchain adapts LangChain and LangGraph stream events to AI SDK UI
// message stream chunks.
package langchain

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/internal/intsafe"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// StreamEvent is a LangGraph stream event. Mode is commonly "messages" or
// "values"; Data contains the event payload from LangGraph. Namespace carries
// the LangGraph subgraph namespace segments when the raw event tuple included
// one (a 3-element [namespace, type, data] tuple whose first element is an
// array of strings); HasNamespace distinguishes "no namespace" (a 2-element
// tuple) from an explicit empty namespace ([]).
type StreamEvent struct {
	Mode         string
	Data         interface{}
	Namespace    []string
	HasNamespace bool

	// Err, when set, reports that the upstream stream failed (TS: the
	// iterator throws). The adapter calls the final and error/abort callbacks
	// and ends the UI stream with an `error` chunk.
	Err error
}

// StreamCallbacks mirrors the TypeScript adapter callback surface for stream
// lifecycle hooks. A callback error during the stream follows the TS catch
// block: OnFinal, then OnError (OnAbort for a cancellation), then an `error`
// chunk with the error text. An OnStart error, or an error from OnFinal,
// OnAbort or OnError while handling a failure, errors the stream: it arrives
// on the returned error channel.
type StreamCallbacks struct {
	OnStart  func() error
	OnToken  func(token string) error
	OnText   func(text string) error
	OnFinal  func(completion string) error
	OnFinish func(finalState interface{}) error
	OnError  func(error) error
	OnAbort  func() error

	// SendStart controls whether the outer "start" UI message stream chunk is
	// emitted. Defaults to true when nil.
	SendStart *bool
	// SendFinish controls whether the outer "finish" UI message stream chunk
	// is emitted. Defaults to true when nil. This does not affect per-step
	// "finish-step" chunks, which are always emitted when a step was started.
	SendFinish *bool
}

func sendStartEnabled(callbacks *StreamCallbacks) bool {
	if callbacks == nil || callbacks.SendStart == nil {
		return true
	}
	return *callbacks.SendStart
}

func sendFinishEnabled(callbacks *StreamCallbacks) bool {
	if callbacks == nil || callbacks.SendFinish == nil {
		return true
	}
	return *callbacks.SendFinish
}

// ParseLangGraphEvent converts a raw LangGraph event tuple into a StreamEvent.
// It supports both [type, data] and [namespace, type, data] shapes.
func ParseLangGraphEvent(event []interface{}) StreamEvent {
	var rawNamespace, modeVal, dataVal interface{}
	switch {
	case len(event) == 3:
		rawNamespace, modeVal, dataVal = event[0], event[1], event[2]
	case len(event) >= 2:
		modeVal, dataVal = event[0], event[1]
	}
	namespace, hasNamespace := parseLangGraphNamespace(rawNamespace)
	return StreamEvent{Mode: stringValue(modeVal), Data: dataVal, Namespace: namespace, HasNamespace: hasNamespace}
}

// parseLangGraphNamespace mirrors the TypeScript adapter's namespace guard:
// only a slice whose every element is a string counts as a namespace.
func parseLangGraphNamespace(v interface{}) ([]string, bool) {
	items, ok := asSlice(v)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

// namespaceKey normalizes a namespace slice (including an absent one) into a
// stable map key, mirroring the TypeScript adapter's
// `JSON.stringify(namespace ?? [])`.
func namespaceKey(namespace []string) string {
	if namespace == nil {
		namespace = []string{}
	}
	data, _ := json.Marshal(namespace)
	return string(data)
}

// ToUIMessageStreamFromRaw converts raw LangGraph event tuples into AI SDK UI
// message stream chunks.
func ToUIMessageStreamFromRaw(ctx context.Context, events <-chan []interface{}) (<-chan ai.UIMessageChunk, <-chan error) {
	return ToUIMessageStreamFromRawWithCallbacks(ctx, events, nil)
}

// ToUIMessageStreamFromRawWithCallbacks converts raw LangGraph event tuples and
// invokes stream lifecycle callbacks.
func ToUIMessageStreamFromRawWithCallbacks(ctx context.Context, events <-chan []interface{}, callbacks *StreamCallbacks) (<-chan ai.UIMessageChunk, <-chan error) {
	converted := make(chan StreamEvent)
	go func() {
		defer close(converted)
		for event := range events {
			select {
			case <-ctx.Done():
				return
			case converted <- ParseLangGraphEvent(event):
			}
		}
	}()
	return ToUIMessageStreamWithCallbacks(ctx, converted, callbacks)
}

// ToUIMessageStreamFromAny converts a mixed LangChain stream into AI SDK UI
// message chunks. It mirrors the TypeScript adapter's stream-type detection:
// LangGraph tuple arrays are parsed as messages/values/custom events,
// streamEvents objects are handled as streamEvents, and every other value is
// treated as a direct model stream chunk.
func ToUIMessageStreamFromAny(ctx context.Context, events <-chan interface{}) (<-chan ai.UIMessageChunk, <-chan error) {
	return ToUIMessageStreamFromAnyWithCallbacks(ctx, events, nil)
}

// ToUIMessageStreamFromAnyWithCallbacks converts a mixed LangChain stream and
// invokes stream lifecycle callbacks.
func ToUIMessageStreamFromAnyWithCallbacks(ctx context.Context, events <-chan interface{}, callbacks *StreamCallbacks) (<-chan ai.UIMessageChunk, <-chan error) {
	converted := make(chan StreamEvent)
	go func() {
		defer close(converted)
		// The stream type is decided by the first value, like TS.
		streamType := ""
		for event := range events {
			if streamType == "" {
				switch {
				case isTuple(event):
					streamType = "langgraph"
				case isStreamEventsEvent(event):
					streamType = "streamEvents"
				default:
					streamType = "model"
				}
			}
			var streamEvent StreamEvent
			switch streamType {
			case "langgraph":
				tuple, _ := asSlice(event)
				streamEvent = ParseLangGraphEvent(tuple)
			case "streamEvents":
				streamEvent = StreamEvent{Mode: "streamEvents", Data: event}
			default:
				streamEvent = StreamEvent{Mode: "model", Data: event}
			}
			select {
			case <-ctx.Done():
				return
			case converted <- streamEvent:
			}
		}
	}()
	return ToUIMessageStreamWithCallbacks(ctx, converted, callbacks)
}

func isTuple(v interface{}) bool {
	_, ok := asSlice(v)
	return ok
}

// isStreamEventsEvent mirrors TS isStreamEventsEvent: an object with a string
// `event` and a `data` that is an object or null.
func isStreamEventsEvent(v interface{}) bool {
	m, ok := asMap(v)
	if !ok {
		return false
	}
	if _, ok := m["event"].(string); !ok {
		return false
	}
	data, present := m["data"]
	if !present {
		return false
	}
	if data == nil {
		return true
	}
	// TS `typeof obj.data === 'object'`: an array counts.
	if _, ok := asSlice(data); ok {
		return true
	}
	_, ok = asMap(data)
	return ok
}

// ToUIMessageStream converts LangGraph messages/values stream events into AI SDK
// UI message stream chunks.
func ToUIMessageStream(ctx context.Context, events <-chan StreamEvent) (<-chan ai.UIMessageChunk, <-chan error) {
	return ToUIMessageStreamWithCallbacks(ctx, events, nil)
}

// ToUIMessageStreamWithCallbacks converts LangGraph messages/values stream
// events into AI SDK UI message stream chunks and invokes lifecycle callbacks.
func ToUIMessageStreamWithCallbacks(ctx context.Context, events <-chan StreamEvent, callbacks *StreamCallbacks) (<-chan ai.UIMessageChunk, <-chan error) {
	out := make(chan ai.UIMessageChunk)
	errs := make(chan error, 1)
	go func() {
		defer close(out)
		defer close(errs)

		state := newEventState()
		var textChunks []string
		var lastValuesData interface{}
		// TS detects the stream type on the first value: a stream with no
		// values emits no finish chunk.
		received := false
		if callbacks != nil && callbacks.OnStart != nil {
			if err := callbacks.OnStart(); err != nil {
				// TS awaits onStart outside its try block: a throw errors
				// the stream (no onError, no error chunk).
				errs <- err
				return
			}
		}
		if sendStartEnabled(callbacks) {
			if !sendChunk(ctx, out, ai.UIMessageChunk{"type": "start"}) {
				finalizeCallbacks(errs, callbacks, textChunks, nil, ctx.Err())
				return
			}
		}
		for {
			// Check cancellation first: when ctx is done and an event is also
			// ready, select picks either case at random, so a cancelled
			// stream could still process (and report) one more event.
			if ctx.Err() != nil {
				finalizeCallbacks(errs, callbacks, textChunks, nil, ctx.Err())
				return
			}
			select {
			case <-ctx.Done():
				finalizeCallbacks(errs, callbacks, textChunks, nil, ctx.Err())
				return
			case event, ok := <-events:
				if !ok {
					for _, chunk := range finishChunks(state, received && sendFinishEnabled(callbacks)) {
						if err := handleCallbackChunk(callbacks, &textChunks, chunk); err != nil {
							failStream(ctx, out, errs, callbacks, textChunks, err)
							return
						}
						if !sendChunk(ctx, out, chunk) {
							finalizeCallbacks(errs, callbacks, textChunks, lastValuesData, ctx.Err())
							return
						}
					}
					completeStream(ctx, out, errs, callbacks, textChunks, lastValuesData)
					return
				}
				if event.Err != nil {
					// TS catch block: onFinal, then onAbort/onError, then an
					// `error` chunk; the stream still closes normally.
					failStream(ctx, out, errs, callbacks, textChunks, event.Err)
					return
				}
				received = true
				if event.Mode == "values" {
					lastValuesData = event.Data
				}
				for _, chunk := range processLangGraphEvent(state, event) {
					if err := handleCallbackChunk(callbacks, &textChunks, chunk); err != nil {
						failStream(ctx, out, errs, callbacks, textChunks, err)
						return
					}
					if !sendChunk(ctx, out, chunk) {
						finalizeCallbacks(errs, callbacks, textChunks, lastValuesData, ctx.Err())
						return
					}
				}
			}
		}
	}()
	return out, errs
}

// completeStream runs the end-of-stream callbacks (onFinal, then onFinish). A
// callback that fails lands in the TS catch block, like any other error of
// the stream body.
func completeStream(ctx context.Context, out chan<- ai.UIMessageChunk, errs chan<- error, callbacks *StreamCallbacks, textChunks []string, finalState interface{}) {
	if callbacks == nil {
		return
	}
	if callbacks.OnFinal != nil {
		if err := callbacks.OnFinal(strings.Join(textChunks, "")); err != nil {
			failStream(ctx, out, errs, callbacks, textChunks, err)
			return
		}
	}
	if callbacks.OnFinish != nil {
		if err := callbacks.OnFinish(finalState); err != nil {
			failStream(ctx, out, errs, callbacks, textChunks, err)
		}
	}
}

// failStream ends a stream whose upstream or callback failed, like the TS
// catch block of toUIMessageStream: onFinal, then onAbort or onError, then an
// `error` chunk; the stream still closes normally. A failure of the catch
// block's own callbacks errors the stream instead.
func failStream(ctx context.Context, out chan<- ai.UIMessageChunk, errs chan<- error, callbacks *StreamCallbacks, textChunks []string, cause error) {
	if callbacks != nil && callbacks.OnFinal != nil {
		if err := callbacks.OnFinal(strings.Join(textChunks, "")); err != nil {
			errs <- err
			return
		}
	}
	if callbacks != nil {
		var err error
		// TS isAbortError matches name === 'AbortError' only: a deadline is a
		// TimeoutError there and goes to onError.
		if errors.Is(cause, context.Canceled) {
			if callbacks.OnAbort != nil {
				err = callbacks.OnAbort()
			}
		} else if callbacks.OnError != nil {
			err = callbacks.OnError(cause)
		}
		if err != nil {
			errs <- err
			return
		}
	}
	sendChunk(ctx, out, ai.UIMessageChunk{"type": "error", "errorText": cause.Error()})
}

func handleCallbackChunk(callbacks *StreamCallbacks, textChunks *[]string, chunk ai.UIMessageChunk) error {
	if callbacks == nil || chunk["type"] != "text-delta" {
		return nil
	}
	delta := stringValue(chunk["delta"])
	if delta == "" {
		return nil
	}
	// Only buffer text when OnFinal is actually configured, mirroring the
	// TypeScript adapter's lazy `if (options?.onFinal) { textChunks.push(...) }`.
	if callbacks.OnFinal != nil {
		*textChunks = append(*textChunks, delta)
	}
	if callbacks.OnToken != nil {
		if err := callbacks.OnToken(delta); err != nil {
			return err
		}
	}
	if callbacks.OnText != nil {
		if err := callbacks.OnText(delta); err != nil {
			return err
		}
	}
	return nil
}

// finalizeCallbacks ends a stream cut short by terminalErr (a cancelled
// context): onFinal, then onAbort or onError, and the error on the channel.
// A callback that fails errors the stream with its own error.
func finalizeCallbacks(errs chan<- error, callbacks *StreamCallbacks, textChunks []string, finalState interface{}, terminalErr error) {
	if terminalErr == nil {
		return
	}
	completion := strings.Join(textChunks, "")
	if callbacks != nil && callbacks.OnFinal != nil {
		if err := callbacks.OnFinal(completion); err != nil {
			errs <- err
			return
		}
	}
	// TS: an abort reaches onAbort only (never onError), whether or not an
	// onAbort callback is configured.
	if callbacks != nil && isContextAbort(terminalErr) {
		if callbacks.OnAbort != nil {
			if err := callbacks.OnAbort(); err != nil {
				errs <- err
				return
			}
		}
	} else if callbacks != nil && callbacks.OnError != nil {
		if err := callbacks.OnError(terminalErr); err != nil {
			errs <- err
			return
		}
	}
	errs <- terminalErr
}

// isContextAbort reports a cancellation (TS AbortError). A deadline is a
// TimeoutError in TS, which toUIMessageStream reports through onError.
func isContextAbort(err error) bool {
	return err == context.Canceled
}

// LangChainMessage is a JSON-compatible representation of a LangChain message.
// It preserves the public fields used by LangChain's HumanMessage, AIMessage,
// SystemMessage, and ToolMessage constructors.
type LangChainMessage map[string]interface{}

// ConvertModelMessages converts AI SDK model messages to LangChain-compatible
// message maps.
func ConvertModelMessages(messages []types.Message) []LangChainMessage {
	out := make([]LangChainMessage, 0, len(messages))
	for _, message := range messages {
		switch message.Role {
		case types.RoleSystem:
			out = append(out, wireMessage(LangChainMessage{"type": "system", "content": contentText(message.Content)}))
		case types.RoleUser:
			out = append(out, wireMessage(LangChainMessage{"type": "human", "content": convertUserContent(message.Content)}))
		case types.RoleAssistant:
			out = append(out, convertAssistantContent(message.Content))
		case types.RoleTool:
			for _, part := range message.Content {
				if result, ok := part.(types.ToolResultContent); ok {
					out = append(out, convertToolResultPart(result))
				}
			}
		}
	}
	return out
}

// ToBaseMessages is an alias for ConvertModelMessages, matching the TypeScript
// adapter's public conversion entry point.
func ToBaseMessages(messages []types.Message) []LangChainMessage {
	return ConvertModelMessages(messages)
}

func sendChunk(ctx context.Context, out chan<- ai.UIMessageChunk, chunk ai.UIMessageChunk) bool {
	select {
	case <-ctx.Done():
		return false
	case out <- chunk:
		return true
	}
}

// eventState tracks bookkeeping across the whole LangGraph stream. Namespaced
// LangGraph subgraphs each get their own step/lifecycle scope (see the
// `*ByNamespace` fields) so that a step transition or a reused provider tool
// call ID in one subgraph does not close or suppress lifecycles that are
// still active in a different subgraph.
type eventState struct {
	// stream-wide bookkeeping
	emittedToolCalls            map[string]bool
	emittedToolCallsByKey       map[string]string
	emittedToolInputs           map[string]bool
	emittedToolOutputCallIDs    map[string]bool
	emittedToolOutputMessageIDs map[string]bool
	emittedSources              map[string]bool
	messageSeen                 map[string]*messageSeenState
	messageNamespaces           map[string][]string
	toolCallInfoByIndex         map[string]map[int]toolCall

	// messageOrder lists messageSeen keys in first-seen order.
	messageOrder []string
	// emittedImages, emittedReasoningIDs and messageReasoningIDs mirror the TS
	// state of the same names (image ids already emitted; GPT-5 reasoning ids
	// already emitted, and the id each message streamed its reasoning under).
	emittedImages       map[string]bool
	emittedReasoningIDs map[string]bool
	messageReasoningIDs map[string]string

	// per-namespace step lifecycle bookkeeping, keyed by namespaceKey(...)
	currentStepsByNamespace                    map[string]int
	messageIdsInCurrentStepByNamespace         map[string]map[string]bool
	emittedToolCallsInCurrentStepByNamespace   map[string]map[string]bool
	emittedToolInputsInCurrentStepByNamespace  map[string]map[string]bool
	emittedToolOutputsInCurrentStepByNamespace map[string]map[string]bool

	// unfinishedToolCallsByNamespace tracks tool calls still awaiting a
	// terminal output in each namespace, distinct from
	// emittedToolCalls/emittedToolCallsInCurrentStepByNamespace: those never
	// forget a toolCallId once it's been started, even after a provider
	// reuses the same id for an unrelated later call (TS #21552, "preserve
	// ordered bare LangChain tool lifecycles when tool-call IDs are
	// reused"). markToolCallStarted adds; markToolOutputEmitted removes.
	unfinishedToolCallsByNamespace map[string]map[string]bool

	// direct model / streamEvents stream state
	streamMessageID        string
	streamStarted          bool
	streamTextStarted      bool
	streamTextID           string
	streamReasoningStarted bool
	streamReasoningID      string
}

func newEventState() *eventState {
	return &eventState{
		emittedToolCalls:            map[string]bool{},
		emittedToolCallsByKey:       map[string]string{},
		emittedToolInputs:           map[string]bool{},
		emittedToolOutputCallIDs:    map[string]bool{},
		emittedToolOutputMessageIDs: map[string]bool{},
		emittedSources:              map[string]bool{},
		messageSeen:                 map[string]*messageSeenState{},
		messageNamespaces:           map[string][]string{},
		toolCallInfoByIndex:         map[string]map[int]toolCall{},
		emittedImages:               map[string]bool{},
		emittedReasoningIDs:         map[string]bool{},
		messageReasoningIDs:         map[string]string{},

		currentStepsByNamespace:                    map[string]int{},
		messageIdsInCurrentStepByNamespace:         map[string]map[string]bool{},
		emittedToolCallsInCurrentStepByNamespace:   map[string]map[string]bool{},
		emittedToolInputsInCurrentStepByNamespace:  map[string]map[string]bool{},
		emittedToolOutputsInCurrentStepByNamespace: map[string]map[string]bool{},

		unfinishedToolCallsByNamespace: map[string]map[string]bool{},
	}
}

type messageSeenState struct {
	text      bool
	reasoning bool
	tools     map[string]bool
	toolOrder []string
}

func (s *messageSeenState) hasTool(id string) bool { return s.tools[id] }

// addTool records a tool call id, keeping first-seen order (a JS Set).
func (s *messageSeenState) addTool(id string) {
	if s.tools == nil {
		s.tools = map[string]bool{}
	}
	if !s.tools[id] {
		s.tools[id] = true
		s.toolOrder = append(s.toolOrder, id)
	}
}

// startNamespaceStep begins a new LangGraph step scope for the given
// namespace key, resetting the per-step emitted-call/input/output sets.
func (state *eventState) startNamespaceStep(ns string, step int) {
	state.currentStepsByNamespace[ns] = step
	state.messageIdsInCurrentStepByNamespace[ns] = map[string]bool{}
	state.emittedToolCallsInCurrentStepByNamespace[ns] = map[string]bool{}
	state.emittedToolInputsInCurrentStepByNamespace[ns] = map[string]bool{}
	state.emittedToolOutputsInCurrentStepByNamespace[ns] = map[string]bool{}
}

func (state *eventState) hasEmittedToolCallInCurrentStep(toolCallID, ns string) bool {
	if _, ok := state.currentStepsByNamespace[ns]; !ok {
		return state.emittedToolCalls[toolCallID]
	}
	return state.emittedToolCallsInCurrentStepByNamespace[ns][toolCallID]
}

func (state *eventState) markToolCallEmitted(toolCallID, ns string) {
	state.emittedToolCalls[toolCallID] = true
	if _, ok := state.currentStepsByNamespace[ns]; ok {
		if state.emittedToolCallsInCurrentStepByNamespace[ns] == nil {
			state.emittedToolCallsInCurrentStepByNamespace[ns] = map[string]bool{}
		}
		state.emittedToolCallsInCurrentStepByNamespace[ns][toolCallID] = true
	}
}

// markToolCallStarted mirrors TS `markToolCallStarted`: marks the call
// emitted and additionally records it as unfinished (awaiting output) in
// this namespace, so a same-id call reused in a different namespace/step
// isn't mistaken for a delayed output of this one.
func (state *eventState) markToolCallStarted(toolCallID, ns string) {
	state.markToolCallEmitted(toolCallID, ns)
	if state.unfinishedToolCallsByNamespace[ns] == nil {
		state.unfinishedToolCallsByNamespace[ns] = map[string]bool{}
	}
	state.unfinishedToolCallsByNamespace[ns][toolCallID] = true
}

// hasUnfinishedToolCall mirrors TS `hasUnfinishedToolCall`.
func (state *eventState) hasUnfinishedToolCall(toolCallID, ns string) bool {
	return state.unfinishedToolCallsByNamespace[ns][toolCallID]
}

func (state *eventState) hasEmittedToolInputInCurrentStep(toolCallID, ns string) bool {
	if _, ok := state.currentStepsByNamespace[ns]; !ok {
		return state.emittedToolInputs[toolCallID]
	}
	return state.emittedToolInputsInCurrentStepByNamespace[ns][toolCallID]
}

func (state *eventState) markToolInputEmitted(toolCallID, ns string) {
	state.emittedToolInputs[toolCallID] = true
	if _, ok := state.currentStepsByNamespace[ns]; ok {
		if state.emittedToolInputsInCurrentStepByNamespace[ns] == nil {
			state.emittedToolInputsInCurrentStepByNamespace[ns] = map[string]bool{}
		}
		state.emittedToolInputsInCurrentStepByNamespace[ns][toolCallID] = true
	}
}

func (state *eventState) hasEmittedToolOutputInCurrentStep(toolCallID, ns string) bool {
	if _, ok := state.currentStepsByNamespace[ns]; !ok {
		return state.emittedToolOutputCallIDs[toolCallID]
	}
	return state.emittedToolOutputsInCurrentStepByNamespace[ns][toolCallID]
}

func (state *eventState) markToolOutputEmitted(toolCallID, ns string) {
	state.emittedToolOutputCallIDs[toolCallID] = true
	delete(state.unfinishedToolCallsByNamespace[ns], toolCallID)
	if _, ok := state.currentStepsByNamespace[ns]; ok {
		if state.emittedToolOutputsInCurrentStepByNamespace[ns] == nil {
			state.emittedToolOutputsInCurrentStepByNamespace[ns] = map[string]bool{}
		}
		state.emittedToolOutputsInCurrentStepByNamespace[ns][toolCallID] = true
	}
}

// findMessageCurrentStepNamespace returns the namespace key whose current
// step observed msgID, if any.
func (state *eventState) findMessageCurrentStepNamespace(msgID string) (string, bool) {
	for ns, ids := range state.messageIdsInCurrentStepByNamespace {
		if ids[msgID] {
			return ns, true
		}
	}
	return "", false
}

// closeStepNamespaceMessages ends and clears message state for every message
// belonging to stepNamespace. It returns whether another namespace still has
// active text/reasoning parts that would be invalidated by a global
// finish-step chunk (in which case the caller should suppress finish-step).
func closeStepNamespaceMessages(state *eventState, stepNamespace string) ([]ai.UIMessageChunk, bool) {
	hasConcurrentMessageParts := false
	var chunks []ai.UIMessageChunk
	for _, id := range sortedMessageSeenIDs(state) {
		seen := state.messageSeen[id]
		ns, hasNS := state.messageNamespaces[id]
		if namespaceKey(ns) != stepNamespace {
			if seen.text || seen.reasoning {
				hasConcurrentMessageParts = true
			}
			continue
		}
		// Tag with the per-message namespace before deleting it below: the
		// namespace map entry will be gone by the time any later, generic
		// post-processing pass could look it up.
		if seen.text {
			chunk := ai.UIMessageChunk{"type": "text-end", "id": id}
			if hasNS {
				chunk = tagNamespaceChunk(chunk, ns)
			}
			chunks = append(chunks, chunk)
		}
		if seen.reasoning {
			chunk := ai.UIMessageChunk{"type": "reasoning-end", "id": id}
			if hasNS {
				chunk = tagNamespaceChunk(chunk, ns)
			}
			chunks = append(chunks, chunk)
		}
		state.deleteSeen(id)
	}
	return chunks, hasConcurrentMessageParts
}

// sortedMessageSeenIDs returns the ids of the messages with open parts in the
// order they were first seen (a JS Map keeps insertion order; chunk order
// across concurrent messages must not depend on the ids).
func sortedMessageSeenIDs(state *eventState) []string {
	ids := make([]string, 0, len(state.messageSeen))
	for _, id := range state.messageOrder {
		if state.messageSeen[id] != nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// deleteSeen forgets a message's open-part bookkeeping.
func (state *eventState) deleteSeen(id string) {
	delete(state.messageSeen, id)
	delete(state.messageNamespaces, id)
	delete(state.messageReasoningIDs, id)
	for i, existing := range state.messageOrder {
		if existing == id {
			state.messageOrder = append(state.messageOrder[:i:i], state.messageOrder[i+1:]...)
			break
		}
	}
}

// namespaceEligibleTypes mirrors the TypeScript adapter's
// `addLangGraphNamespace` switch: only these chunk types receive
// `providerMetadata.langchain.namespace` tagging.
var namespaceEligibleTypes = map[string]bool{
	"text-start": true, "text-delta": true, "text-end": true,
	"reasoning-start": true, "reasoning-delta": true, "reasoning-end": true,
	"tool-input-start": true, "tool-input-available": true, "tool-input-error": true,
	"tool-approval-response": true, "tool-output-available": true, "tool-output-error": true,
	"source-url": true, "source-document": true, "file": true, "reasoning-file": true,
}

// tagAllChunks applies LangGraph namespace tagging to every eligible chunk
// produced while processing one raw LangGraph event tuple, mirroring the
// TypeScript adapter's namespace-wrapping controller.
func tagAllChunks(state *eventState, chunks []ai.UIMessageChunk, ns []string, hasNS bool) []ai.UIMessageChunk {
	if len(chunks) == 0 {
		return chunks
	}
	out := make([]ai.UIMessageChunk, len(chunks))
	for i, chunk := range chunks {
		out[i] = applyNamespaceToChunk(state, chunk, ns, hasNS)
	}
	return out
}

func applyNamespaceToChunk(state *eventState, chunk ai.UIMessageChunk, ambientNS []string, ambientHasNS bool) ai.UIMessageChunk {
	typ, _ := chunk["type"].(string)
	if !namespaceEligibleTypes[typ] {
		return chunk
	}
	// Some finalize helpers (closeStepNamespaceMessages,
	// finalizeSeenPartsForValues, finalizeOpenMessageParts) tag their own
	// text-end/reasoning-end chunks inline, before deleting the per-message
	// namespace bookkeeping this function would otherwise need to look up.
	// Don't override an already-resolved namespace with the ambient ones.
	if namespaceAlreadyTagged(chunk) {
		return chunk
	}
	ns := ambientNS
	hasNS := ambientHasNS
	switch typ {
	case "text-start", "text-delta", "text-end", "reasoning-start", "reasoning-delta", "reasoning-end":
		if id, ok := chunk["id"].(string); ok {
			if messageNS, found := state.messageNamespaces[id]; found {
				ns = messageNS
				hasNS = true
			}
		}
	}
	if !hasNS {
		return chunk
	}
	return tagNamespaceChunk(chunk, ns)
}

func namespaceAlreadyTagged(chunk ai.UIMessageChunk) bool {
	pm, ok := chunk["providerMetadata"].(map[string]interface{})
	if !ok {
		return false
	}
	lc, ok := pm["langchain"].(map[string]interface{})
	if !ok {
		return false
	}
	_, ok = lc["namespace"]
	return ok
}

// tagNamespaceChunk merges providerMetadata.langchain.namespace into chunk
// without discarding any provider metadata (such as citation metadata)
// already present.
func tagNamespaceChunk(chunk ai.UIMessageChunk, ns []string) ai.UIMessageChunk {
	out := make(ai.UIMessageChunk, intsafe.AddCap(len(chunk), 1))
	for k, v := range chunk {
		out[k] = v
	}
	langchainMeta := map[string]interface{}{}
	newProviderMetadata := map[string]interface{}{}
	if existing, ok := out["providerMetadata"].(map[string]interface{}); ok {
		for k, v := range existing {
			if k == "langchain" {
				if lc, ok := v.(map[string]interface{}); ok {
					for lk, lv := range lc {
						langchainMeta[lk] = lv
					}
				}
				continue
			}
			newProviderMetadata[k] = v
		}
	}
	langchainMeta["namespace"] = ns
	newProviderMetadata["langchain"] = langchainMeta
	out["providerMetadata"] = newProviderMetadata
	return out
}

func processLangGraphEvent(state *eventState, event StreamEvent) []ai.UIMessageChunk {
	switch event.Mode {
	case "custom":
		return tagAllChunks(state, processCustomEvent(event.Data), event.Namespace, event.HasNamespace)
	case "messages":
		return tagAllChunks(state, processMessagesEvent(state, event), event.Namespace, event.HasNamespace)
	case "values":
		return tagAllChunks(state, processValuesEvent(state, event), event.Namespace, event.HasNamespace)
	case "tools":
		return tagAllChunks(state, processToolsEvent(state, event), event.Namespace, event.HasNamespace)
	case "streamEvents", "stream-events":
		return processStreamEventsEvent(state, event.Data)
	case "model":
		return processModelChunk(state, event.Data)
	default:
		return nil
	}
}

func finishChunks(state *eventState, sendFinish bool) []ai.UIMessageChunk {
	chunks := finalizeOpenMessageParts(state)
	chunks = append(chunks, finalizeStreamEventParts(state)...)
	if len(state.currentStepsByNamespace) > 0 {
		chunks = append(chunks, ai.UIMessageChunk{"type": "finish-step"})
	}
	if sendFinish {
		chunks = append(chunks, ai.UIMessageChunk{"type": "finish"})
	}
	return chunks
}

func processCustomEvent(data interface{}) []ai.UIMessageChunk {
	customTypeName := "custom"
	var partID interface{}
	if m, ok := asMap(data); ok {
		if typ := stringValue(m["type"]); typ != "" {
			customTypeName = typ
		}
		if id := stringValue(m["id"]); id != "" {
			partID = id
		}
	}
	chunk := ai.UIMessageChunk{
		"type":      "data-" + customTypeName,
		"transient": partID == nil,
		"data":      data,
	}
	// TS leaves `id` undefined (absent on the wire) for a transient part; a
	// JSON null would fail the client's `id: string | undefined` schema.
	if partID != nil {
		chunk["id"] = partID
	}
	return []ai.UIMessageChunk{chunk}
}

func processMessagesEvent(state *eventState, event StreamEvent) []ai.UIMessageChunk {
	items, ok := asSlice(event.Data)
	if !ok || len(items) == 0 {
		return nil
	}
	message, ok := asMap(items[0])
	if !ok {
		return nil
	}

	// TS: a message without an id is ignored.
	msgID := messageID(message)
	if msgID == "" {
		return nil
	}

	eventNS := namespaceKey(event.Namespace)
	if event.HasNamespace {
		state.messageNamespaces[msgID] = event.Namespace
	}

	var chunks []ai.UIMessageChunk

	// startedUIReducerStep mirrors TS `startedUIReducerStep`: whether THIS
	// event already opened a new reducer step scope below, so
	// processToolMessage's bare-ToolMessage lifecycle synthesis knows
	// whether it still needs to open its own.
	var startedUIReducerStep bool

	// Each namespace has an independent LangGraph step counter. Advancing a
	// namespace starts a new reducer scope so provider-scoped tool call IDs
	// can be reused in that namespace. A finish-step chunk clears every
	// active UI text/reasoning part, so it is omitted when another namespace
	// is still active (hasConcurrentMessageParts).
	if metadata, ok := metadataMap(items); ok {
		if step, ok := numberAsInt(metadata["langgraph_step"]); ok {
			currentStep, exists := state.currentStepsByNamespace[eventNS]
			switch {
			case !exists:
				if len(state.currentStepsByNamespace) == 0 {
					chunks = append(chunks, ai.UIMessageChunk{"type": "start-step"})
					startedUIReducerStep = true
				}
				state.startNamespaceStep(eventNS, step)
			case step != currentStep:
				closeChunks, hasConcurrentMessageParts := closeStepNamespaceMessages(state, eventNS)
				chunks = append(chunks, closeChunks...)
				if !hasConcurrentMessageParts {
					chunks = append(chunks, ai.UIMessageChunk{"type": "finish-step"})
				}
				chunks = append(chunks, ai.UIMessageChunk{"type": "start-step"})
				startedUIReducerStep = true
				state.startNamespaceStep(eventNS, step)
			}
		}
	}
	if _, exists := state.currentStepsByNamespace[eventNS]; exists {
		if state.messageIdsInCurrentStepByNamespace[eventNS] == nil {
			state.messageIdsInCurrentStepByNamespace[eventNS] = map[string]bool{}
		}
		state.messageIdsInCurrentStepByNamespace[eventNS][msgID] = true
	}

	if isAIMessageChunk(message) {
		return append(chunks, processAIMessageChunk(state, message, msgID, eventNS)...)
	}
	if isToolMessage(message) {
		chunks = append(chunks, processToolMessage(state, message, eventNS, startedUIReducerStep)...)
	}
	return chunks
}

// processAIMessageChunk mirrors the isAIMessageChunk branch of TS
// processLangGraphEvent for a messages-mode chunk: image outputs, then tool
// call chunks (which end the chunk's processing), then reasoning, text and
// citations.
func processAIMessageChunk(state *eventState, message map[string]interface{}, msgID, eventNS string) []ai.UIMessageChunk {
	var chunks []ai.UIMessageChunk
	source := messageDataSource(message)
	chunks = append(chunks, imageOutputChunks(state, source)...)

	if toolChunks := extractToolCallChunks(message); len(toolChunks) > 0 {
		return append(chunks, processToolCallChunks(state, msgID, eventNS, toolChunks)...)
	}

	if reasoningID := extractReasoningID(message); reasoningID != "" {
		if _, ok := state.messageReasoningIDs[msgID]; !ok {
			state.messageReasoningIDs[msgID] = reasoningID
		}
		state.emittedReasoningIDs[reasoningID] = true
	}
	if reasoning := messageReasoning(message); reasoning != "" {
		reasoningID := firstString(state.messageReasoningIDs[msgID], firstString(extractReasoningID(message), msgID))
		seen := state.messageSeen[msgID]
		if seen == nil || !seen.reasoning {
			chunks = append(chunks, ai.UIMessageChunk{"type": "reasoning-start", "id": msgID})
			state.seenFor(msgID).reasoning = true
		}
		chunks = append(chunks, ai.UIMessageChunk{"type": "reasoning-delta", "delta": reasoning, "id": msgID})
		state.emittedReasoningIDs[reasoningID] = true
	}

	if text := messageText(message); text != "" {
		seen := state.messageSeen[msgID]
		if seen != nil && seen.reasoning && !seen.text {
			chunks = append(chunks, ai.UIMessageChunk{"type": "reasoning-end", "id": msgID})
			seen.reasoning = false
		}
		if seen == nil || !seen.text {
			chunks = append(chunks, ai.UIMessageChunk{"type": "text-start", "id": msgID})
			state.seenFor(msgID).text = true
		}
		chunks = append(chunks, ai.UIMessageChunk{"type": "text-delta", "delta": text, "id": msgID})
	}
	return append(chunks, emitSourceChunks(state, msgID, extractCitations(message))...)
}

// processToolCallChunks emits the tool-input lifecycle for streamed tool call
// chunks. The first chunk of a call carries its id and name; later chunks only
// carry an index and argument text, so the id and name are remembered by index.
func processToolCallChunks(state *eventState, msgID, eventNS string, calls []toolCall) []ai.UIMessageChunk {
	var chunks []ai.UIMessageChunk
	for _, call := range calls {
		if call.id != "" {
			if state.toolCallInfoByIndex[msgID] == nil {
				state.toolCallInfoByIndex[msgID] = map[int]toolCall{}
			}
			state.toolCallInfoByIndex[msgID][call.index] = call
		} else if stored := state.toolCallInfoByIndex[msgID][call.index]; stored.id != "" {
			call.id = stored.id
			if call.name == "" {
				call.name = stored.name
			}
		}
		if call.id == "" {
			continue
		}
		seen := state.seenFor(msgID)
		if !seen.hasTool(call.id) {
			seen.addTool(call.id)
			if !state.hasEmittedToolCallInCurrentStep(call.id, eventNS) {
				state.markToolCallStarted(call.id, eventNS)
				chunks = append(chunks, ai.UIMessageChunk{"type": "tool-input-start", "toolCallId": call.id, "toolName": firstString(call.name, "unknown"), "dynamic": true})
			}
		}
		if argDelta, ok := call.args.(string); ok && argDelta != "" {
			chunks = append(chunks, ai.UIMessageChunk{"type": "tool-input-delta", "toolCallId": call.id, "inputTextDelta": argDelta})
		}
	}
	return chunks
}

// isAIMessageChunk mirrors TS isAIMessageChunk for JSON messages: type "ai"
// (TypeScript langchain-core), "AIMessageChunk" (Python) or a serialized
// constructor whose id path names AIMessageChunk or AIMessage.
func isAIMessageChunk(message map[string]interface{}) bool {
	switch stringValue(message["type"]) {
	case "ai", "AIMessageChunk":
		return true
	case "constructor":
		ids, _ := asSlice(message["id"])
		for _, id := range ids {
			if name := stringValue(id); name == "AIMessageChunk" || name == "AIMessage" {
				return true
			}
		}
	}
	return false
}

// imageOutputChunks emits a file part for each image_generation_call in
// additional_kwargs.tool_outputs that carries data and was not emitted yet.
func imageOutputChunks(state *eventState, source map[string]interface{}) []ai.UIMessageChunk {
	additional, _ := asMap(source["additional_kwargs"])
	outputs, _ := asSlice(additional["tool_outputs"])
	var chunks []ai.UIMessageChunk
	for _, raw := range outputs {
		output, ok := asMap(raw)
		if !ok || stringValue(output["type"]) != "image_generation_call" {
			continue
		}
		result := stringValue(output["result"])
		id := stringValue(output["id"])
		if result == "" || state.emittedImages[id] {
			continue
		}
		state.emittedImages[id] = true
		mediaType := "image/" + firstString(output["output_format"], "png")
		chunks = append(chunks, ai.UIMessageChunk{"type": "file", "mediaType": mediaType, "url": "data:" + mediaType + ";base64," + result})
	}
	return chunks
}

// extractReasoningID mirrors TS extractReasoningId: the GPT-5 reasoning block
// id, consistent across streaming and values events.
func extractReasoningID(message map[string]interface{}) string {
	source := kwargsSource(message)
	if additional, ok := asMap(source["additional_kwargs"]); ok {
		if reasoning, ok := asMap(additional["reasoning"]); ok {
			if id := stringValue(reasoning["id"]); id != "" {
				return id
			}
		}
	}
	if metadata, ok := asMap(source["response_metadata"]); ok {
		items, _ := asSlice(metadata["output"])
		for _, item := range items {
			if block, ok := asMap(item); ok && stringValue(block["type"]) == "reasoning" {
				if _, isArray := asSlice(block["summary"]); isArray {
					return stringValue(block["id"])
				}
			}
		}
	}
	return ""
}

// summaryTexts joins the text of reasoning summary items.
func summaryTexts(summary []interface{}, requireNonEmpty bool) string {
	var b strings.Builder
	for _, item := range summary {
		block, ok := asMap(item)
		if !ok {
			continue
		}
		if text, ok := block["text"].(string); ok && (!requireNonEmpty || text != "") {
			b.WriteString(text)
		}
	}
	return b.String()
}

// reasoningFromValuesMessage mirrors TS extractReasoningFromValuesMessage: the
// accumulated reasoning in response_metadata.output (GPT-5 final format), with
// additional_kwargs.reasoning.summary as a fallback.
func reasoningFromValuesMessage(message map[string]interface{}) string {
	source := kwargsSource(message)
	if metadata, ok := asMap(source["response_metadata"]); ok {
		items, _ := asSlice(metadata["output"])
		var b strings.Builder
		for _, item := range items {
			block, ok := asMap(item)
			if !ok || stringValue(block["type"]) != "reasoning" {
				continue
			}
			if summary, ok := asSlice(block["summary"]); ok {
				b.WriteString(summaryTexts(summary, true))
			}
		}
		if b.Len() > 0 {
			return b.String()
		}
	}
	if additional, ok := asMap(source["additional_kwargs"]); ok {
		if reasoning, ok := asMap(additional["reasoning"]); ok {
			if summary, ok := asSlice(reasoning["summary"]); ok {
				return summaryTexts(summary, false)
			}
		}
	}
	return ""
}

func processValuesEvent(state *eventState, event StreamEvent) []ai.UIMessageChunk {
	eventNS := namespaceKey(event.Namespace)
	var chunks []ai.UIMessageChunk

	// TS finalizes the open message parts before it looks at the data, so a
	// values event whose data is null or not an object still closes them.
	chunks = append(chunks, finalizeSeenPartsForValues(state)...)
	root, ok := asMap(event.Data)
	if !ok {
		return chunks
	}

	messages := messageMaps(root)
	if len(messages) > 0 {
		// First pass: collect every tool call ID that has been responded to
		// by a ToolMessage, and separately track which of those ToolMessages
		// are "trailing" (the tail run of ToolMessages at the end of the
		// message list). Calls followed by another non-tool message are
		// historical; trailing ToolMessages can be the only evidence of a
		// completed call when the node producing them isn't streamed.
		completedToolCallIDs := map[string]bool{}
		type trailingToolMessage struct {
			data     map[string]interface{}
			outputID string
		}
		trailingToolMessages := map[string]trailingToolMessage{}
		trailingStart := len(messages)
		for trailingStart > 0 && isToolMessage(messages[trailingStart-1]) {
			trailingStart--
		}
		for idx, msg := range messages {
			if !isToolMessage(msg) {
				continue
			}
			source := messageDataSource(msg)
			toolCallID := stringValue(source["tool_call_id"])
			if toolCallID == "" {
				continue
			}
			completedToolCallIDs[toolCallID] = true
			if idx >= trailingStart {
				outputID, hasID := messageIDOpt(msg)
				if !hasID {
					outputID = fmt.Sprintf("%s:%d", toolCallID, idx)
				}
				trailingToolMessages[toolCallID] = trailingToolMessage{data: source, outputID: outputID}
			}
		}

		// Second pass: emit tool lifecycles for new tool calls, including
		// calls completed by trailing ToolMessages that were never streamed.
		for _, msg := range messages {
			msgID := messageID(msg)
			if msgID == "" {
				continue
			}

			msgCalls := extractToolCalls(msg)
			for _, call := range msgCalls {
				if call.id == "" {
					continue
				}
				messageNS, wasObservedInCurrentStep := state.findMessageCurrentStepNamespace(msgID)
				lifecycleNS := eventNS
				if wasObservedInCurrentStep {
					lifecycleNS = messageNS
				}
				wasToolCallEmittedInCurrentStep := state.hasEmittedToolCallInCurrentStep(call.id, lifecycleNS)
				trailing, hasTrailing := trailingToolMessages[call.id]
				canRecoverCompletedToolCall := wasObservedInCurrentStep || wasToolCallEmittedInCurrentStep

				// Emit tool calls recovered from a message in the current
				// step, even when a prior step used the same provider-scoped
				// ID. Otherwise, preserve stream-wide suppression for
				// historical calls while recovering completed calls observed
				// in the current step or an earlier values snapshot before
				// their trailing ToolMessage arrived.
				if !wasToolCallEmittedInCurrentStep &&
					(wasObservedInCurrentStep || (!state.emittedToolCalls[call.id] && !completedToolCallIDs[call.id])) {
					state.markToolCallStarted(call.id, lifecycleNS)
					key := toolCallKey(call.name, call.args)
					state.emittedToolCallsByKey[key] = call.id
					chunks = append(chunks, ai.UIMessageChunk{"type": "tool-input-start", "toolCallId": call.id, "toolName": call.name, "dynamic": true})
					state.markToolInputEmitted(call.id, lifecycleNS)
					chunks = append(chunks, toolInputAvailableChunk(call.id, call.name, call.args))
				} else if state.emittedToolCalls[call.id] {
					key := toolCallKey(call.name, call.args)
					state.emittedToolCallsByKey[key] = call.id
				}

				if hasTrailing &&
					!state.emittedToolOutputMessageIDs[trailing.outputID] &&
					!state.hasEmittedToolOutputInCurrentStep(call.id, lifecycleNS) &&
					canRecoverCompletedToolCall {
					state.emittedToolOutputMessageIDs[trailing.outputID] = true
					state.markToolOutputEmitted(call.id, lifecycleNS)
					if stringValue(trailing.data["status"]) == "error" {
						errText := "Tool execution failed"
						if s, ok := trailing.data["content"].(string); ok {
							errText = s
						}
						chunks = append(chunks, ai.UIMessageChunk{"type": "tool-output-error", "toolCallId": call.id, "errorText": errText})
					} else {
						chunks = append(chunks, toolOutputChunk(call.id, trailing.data, "content", false))
					}
				}
			}

			// Reasoning that was not streamed (GPT-5 final format). A message
			// with tool calls that was not streamed in this request is
			// historical (a resumed HITL run) and is skipped, as is reasoning
			// already emitted under the same reasoning id.
			if reasoningID := extractReasoningID(msg); reasoningID != "" && !state.emittedReasoningIDs[reasoningID] &&
				(state.messageSeen[msgID] != nil || len(msgCalls) == 0) {
				if reasoning := reasoningFromValuesMessage(msg); reasoning != "" {
					chunks = append(chunks,
						ai.UIMessageChunk{"type": "reasoning-start", "id": msgID},
						ai.UIMessageChunk{"type": "reasoning-delta", "delta": reasoning, "id": msgID},
						ai.UIMessageChunk{"type": "reasoning-end", "id": msgID},
					)
					state.emittedReasoningIDs[reasoningID] = true
				}
			}

			chunks = append(chunks, emitSourceChunks(state, msgID, extractCitations(msg))...)
		}
	}

	chunks = append(chunks, actionRequestChunks(state, root, eventNS)...)
	return chunks
}

// nowMillis is Date.now(); a variable so tests can pin it like TS vi.spyOn.
var nowMillis = func() int64 { return time.Now().UnixMilli() }

// toolOutputChunk builds a tool-output-available chunk with the output read
// from source[key]: an absent member is left off the wire like a TS undefined
// (JSON.stringify drops it); an explicit null is kept.
func toolOutputChunk(toolCallID string, source map[string]interface{}, key string, preliminary bool) ai.UIMessageChunk {
	chunk := ai.UIMessageChunk{"type": "tool-output-available", "toolCallId": toolCallID}
	if output, present := source[key]; present {
		chunk["output"] = output
	}
	if preliminary {
		chunk["preliminary"] = true
	}
	return chunk
}

// stringOr is `typeof v === 'string' ? v : fallback`: unlike firstString an
// empty string is kept.
func stringOr(v interface{}, fallback string) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fallback
}

// toolInputAvailableChunk builds a tool-input-available chunk; an undefined
// (nil) input is left off the wire like TS, not sent as null.
func toolInputAvailableChunk(toolCallID, toolName string, input interface{}) ai.UIMessageChunk {
	chunk := ai.UIMessageChunk{"type": "tool-input-available", "toolCallId": toolCallID, "toolName": toolName, "dynamic": true}
	if input != nil {
		chunk["input"] = input
	}
	return chunk
}

func actionRequestChunks(state *eventState, root map[string]interface{}, eventNS string) []ai.UIMessageChunk {
	var chunks []ai.UIMessageChunk
	for _, request := range actionRequests(root) {
		if request.name == "" {
			continue
		}
		key := toolCallKey(request.name, request.input)
		toolCallID := state.emittedToolCallsByKey[key]
		if toolCallID == "" {
			toolCallID = request.id
		}
		if toolCallID == "" {
			toolCallID = fmt.Sprintf("hitl-%s-%d", request.name, nowMillis())
		}
		if !state.emittedToolCalls[toolCallID] {
			state.markToolCallStarted(toolCallID, eventNS)
			state.emittedToolCallsByKey[key] = toolCallID
			chunks = append(chunks, ai.UIMessageChunk{"type": "tool-input-start", "toolCallId": toolCallID, "toolName": request.name, "dynamic": true})
			state.markToolInputEmitted(toolCallID, eventNS)
			chunks = append(chunks, toolInputAvailableChunk(toolCallID, request.name, request.input))
		}
		chunks = append(chunks, ai.UIMessageChunk{"type": "tool-approval-request", "approvalId": toolCallID, "toolCallId": toolCallID})
	}
	return chunks
}

// processToolsEvent handles LangGraph's "tools" stream mode, which surfaces
// LangChain tool run lifecycle events (on_tool_start/on_tool_event/
// on_tool_end/on_tool_error) directly, independent of the "messages"/"values"
// modes. on_tool_event carries preliminary (streamed) tool output.
func processToolsEvent(state *eventState, event StreamEvent) []ai.UIMessageChunk {
	payload, ok := asMap(event.Data)
	if !ok {
		return nil
	}
	toolCallID := stringValue(payload["toolCallId"])
	if toolCallID == "" {
		return nil
	}
	toolName := stringOr(payload["name"], "unknown")
	ns := namespaceKey(event.Namespace)

	var chunks []ai.UIMessageChunk
	switch stringValue(payload["event"]) {
	case "on_tool_start":
		key := toolCallKey(toolName, payload["input"])
		state.emittedToolCallsByKey[key] = toolCallID
		chunks = append(chunks, ensureToolInputLifecycle(state, toolCallID, toolName, ns, payload, false)...)
	case "on_tool_event":
		chunks = append(chunks, ensureToolInputLifecycle(state, toolCallID, toolName, ns, payload, true)...)
		chunks = append(chunks, toolOutputChunk(toolCallID, payload, "data", true))
	case "on_tool_end":
		chunks = append(chunks, ensureToolInputLifecycle(state, toolCallID, toolName, ns, payload, true)...)
		state.markToolOutputEmitted(toolCallID, ns)
		chunks = append(chunks, toolOutputChunk(toolCallID, payload, "output", false))
	case "on_tool_error":
		chunks = append(chunks, ensureToolInputLifecycle(state, toolCallID, toolName, ns, payload, true)...)
		state.markToolOutputEmitted(toolCallID, ns)
		errorText := "undefined" // TS String(undefined) for a missing error
		if rawErr, present := payload["error"]; present {
			errorText = formatToolError(rawErr)
		}
		chunks = append(chunks, ai.UIMessageChunk{"type": "tool-output-error", "toolCallId": toolCallID, "errorText": errorText})
	}
	return chunks
}

// ensureToolInputLifecycle emits tool-input-start/tool-input-available for a
// tools-mode tool call if they have not already been emitted. When
// allowPreviousStep is true and the call was already emitted in a prior step
// (globally, but not in the current step), it is treated as a delayed event
// for that prior step's call and no new input lifecycle is synthesized at all.
func ensureToolInputLifecycle(state *eventState, toolCallID, toolName, ns string, payload map[string]interface{}, allowPreviousStep bool) []ai.UIMessageChunk {
	var chunks []ai.UIMessageChunk
	if !state.hasEmittedToolCallInCurrentStep(toolCallID, ns) {
		if allowPreviousStep && state.hasUnfinishedToolCall(toolCallID, ns) {
			return nil
		}
		state.markToolCallStarted(toolCallID, ns)
		chunks = append(chunks, ai.UIMessageChunk{"type": "tool-input-start", "toolCallId": toolCallID, "toolName": toolName, "dynamic": true})
	}
	if !state.hasEmittedToolInputInCurrentStep(toolCallID, ns) {
		state.markToolInputEmitted(toolCallID, ns)
		chunk := ai.UIMessageChunk{"type": "tool-input-available", "toolCallId": toolCallID, "toolName": toolName, "dynamic": true}
		// TS passes payload.input as is: an absent input is undefined and
		// left off the wire, not sent as null.
		if input, ok := payload["input"]; ok {
			chunk["input"] = input
		}
		chunks = append(chunks, chunk)
	}
	return chunks
}

func formatToolError(v interface{}) string {
	switch e := v.(type) {
	case nil:
		return "null"
	case string:
		return e
	case error:
		return e.Error()
	default:
		data, err := jsonstr.Marshal(e)
		if err != nil {
			return fmt.Sprint(e)
		}
		return string(data)
	}
}

// processToolMessage mirrors TS `processLangGraphEvent`'s bare
// `isToolMessageType(msg)` branch. A bare messages-mode ToolMessage may
// arrive with no preceding tool-call lifecycle ever observed for its
// toolCallId in this namespace/step — synthesize the missing tool-input-start
// first so the UI always sees a started lifecycle before its output (TS
// #21552). A delayed output for a lifecycle already started earlier in the
// SAME namespace (hasUnfinishedToolCall) is not re-started; a reused
// provider-scoped id that belongs to a DIFFERENT (already finished)
// lifecycle gets its own synthesized start, in its own reducer step scope
// when one hasn't already been opened for this event, so it doesn't
// overwrite the prior namespace's tool part.
func processToolMessage(state *eventState, message map[string]interface{}, ns string, startedUIReducerStep bool) []ai.UIMessageChunk {
	source := messageDataSource(message)
	toolCallID := stringValue(firstPresent(source, "tool_call_id", "toolCallId"))
	if toolCallID == "" {
		return nil
	}
	var chunks []ai.UIMessageChunk
	wasEmittedInCurrentStep := state.hasEmittedToolCallInCurrentStep(toolCallID, ns)
	isDelayedOutputForPreviousLifecycle := !wasEmittedInCurrentStep && state.hasUnfinishedToolCall(toolCallID, ns)
	if !wasEmittedInCurrentStep && !isDelayedOutputForPreviousLifecycle {
		if _, hasStep := state.currentStepsByNamespace[ns]; !startedUIReducerStep && hasStep && state.emittedToolCalls[toolCallID] {
			chunks = append(chunks, ai.UIMessageChunk{"type": "start-step"})
		}
		state.markToolCallStarted(toolCallID, ns)
		chunks = append(chunks, ai.UIMessageChunk{"type": "tool-input-start", "toolCallId": toolCallID, "toolName": stringOr(source["name"], "unknown"), "dynamic": true})
	}
	if msgID := messageID(message); msgID != "" {
		state.emittedToolOutputMessageIDs[msgID] = true
	}
	state.markToolOutputEmitted(toolCallID, ns)
	if status := stringValue(source["status"]); status == "error" {
		errText := "Tool execution failed"
		if s, ok := source["content"].(string); ok {
			errText = s
		}
		chunks = append(chunks, ai.UIMessageChunk{"type": "tool-output-error", "toolCallId": toolCallID, "errorText": errText})
		return chunks
	}
	chunks = append(chunks, toolOutputChunk(toolCallID, source, "content", false))
	return chunks
}

// modelMessageID is the id of the current direct-model / streamEvents message
// (TS modelState.messageId, initially "langchain-msg-1").
func (state *eventState) modelMessageID() string {
	return firstString(state.streamMessageID, "langchain-msg-1")
}

// modelReasoning and modelText emit reasoning and text for a direct model or
// streamEvents chunk. They mirror the shared TS logic: reasoning ends when
// text starts (unless text was already open), and each part keeps the id it
// started under.
func (state *eventState) modelReasoning(reasoning string) []ai.UIMessageChunk {
	var chunks []ai.UIMessageChunk
	if !state.streamReasoningStarted {
		state.streamReasoningID = state.modelMessageID()
		chunks = append(chunks, ai.UIMessageChunk{"type": "reasoning-start", "id": state.streamReasoningID})
		state.streamReasoningStarted = true
		state.streamStarted = true
	}
	return append(chunks, ai.UIMessageChunk{"type": "reasoning-delta", "delta": reasoning, "id": firstString(state.streamReasoningID, state.modelMessageID())})
}

func (state *eventState) modelText(text string) []ai.UIMessageChunk {
	var chunks []ai.UIMessageChunk
	if state.streamReasoningStarted && !state.streamTextStarted {
		chunks = append(chunks, ai.UIMessageChunk{"type": "reasoning-end", "id": firstString(state.streamReasoningID, state.modelMessageID())})
		state.streamReasoningStarted = false
	}
	if !state.streamTextStarted {
		state.streamTextID = state.modelMessageID()
		chunks = append(chunks, ai.UIMessageChunk{"type": "text-start", "id": state.streamTextID})
		state.streamTextStarted = true
		state.streamStarted = true
	}
	return append(chunks, ai.UIMessageChunk{"type": "text-delta", "delta": text, "id": firstString(state.streamTextID, state.modelMessageID())})
}

// processModelChunk handles one chunk of a direct model stream (TS
// processModelChunk).
func processModelChunk(state *eventState, data interface{}) []ai.UIMessageChunk {
	chunk, ok := asMap(data)
	if !ok {
		return nil
	}
	if id := messageID(chunk); id != "" {
		state.streamMessageID = id
	}
	var chunks []ai.UIMessageChunk
	images := imageOutputChunks(state, messageDataSource(chunk))
	if len(images) > 0 {
		state.streamStarted = true
	}
	chunks = append(chunks, images...)
	// For a direct model stream there is no values event that could duplicate
	// accumulated reasoning, so both sources are read.
	reasoning := messageReasoning(chunk)
	if reasoning == "" {
		reasoning = reasoningFromValuesMessage(chunk)
	}
	if reasoning != "" {
		chunks = append(chunks, state.modelReasoning(reasoning)...)
	}
	if text := messageText(chunk); text != "" {
		chunks = append(chunks, state.modelText(text)...)
	}
	return append(chunks, emitSourceChunks(state, state.modelMessageID(), extractCitations(chunk))...)
}

// processStreamEventsEvent handles one LangChain streamEvents (v2) event (TS
// processStreamEventsEvent).
func processStreamEventsEvent(state *eventState, data interface{}) []ai.UIMessageChunk {
	event, ok := asMap(data)
	if !ok {
		return nil
	}
	// The run id on the event names the message until output has started.
	if runID := stringValue(event["run_id"]); runID != "" && !state.streamStarted {
		state.streamMessageID = runID
	}
	payload, ok := asMap(event["data"])
	if !ok {
		// TS skips only a null/undefined data; an array is a truthy object
		// with none of the properties read below.
		if _, isArray := asSlice(event["data"]); !isArray {
			return nil
		}
		payload = map[string]interface{}{}
	}
	switch stringValue(event["event"]) {
	case "on_chat_model_start":
		// Close the previous model turn's parts so deltas of the next turn
		// cannot attach to them.
		chunks := finalizeStreamEventParts(state)
		if runID := firstString(event["run_id"], stringValue(payload["run_id"])); runID != "" {
			state.streamMessageID = runID
		}
		return chunks
	case "on_chat_model_stream":
		chunk, ok := asMap(payload["chunk"])
		if !ok {
			return nil
		}
		if id := stringValue(chunk["id"]); id != "" {
			state.streamMessageID = id
		}
		var chunks []ai.UIMessageChunk
		if reasoning := messageReasoning(chunk); reasoning != "" {
			chunks = append(chunks, state.modelReasoning(reasoning)...)
		}
		if text := messageText(chunk); text != "" {
			chunks = append(chunks, state.modelText(text)...)
		}
		return append(chunks, emitSourceChunks(state, state.modelMessageID(), extractCitations(chunk))...)
	case "on_tool_start":
		runID := firstString(event["run_id"], stringValue(payload["run_id"]))
		name := firstString(event["name"], stringValue(payload["name"]))
		if runID == "" || name == "" {
			return nil
		}
		return []ai.UIMessageChunk{{"type": "tool-input-start", "toolCallId": runID, "toolName": name, "dynamic": true}}
	case "on_tool_end":
		runID := firstString(event["run_id"], stringValue(payload["run_id"]))
		if runID == "" {
			return nil
		}
		return []ai.UIMessageChunk{toolOutputChunk(runID, payload, "output", false)}
	default:
		return nil
	}
}

func (state *eventState) seenFor(messageID string) *messageSeenState {
	seen := state.messageSeen[messageID]
	if seen == nil {
		seen = &messageSeenState{}
		state.messageSeen[messageID] = seen
		state.messageOrder = append(state.messageOrder, messageID)
	}
	return seen
}

func finalizeOpenMessageParts(state *eventState) []ai.UIMessageChunk {
	ids := sortedMessageSeenIDs(state)
	chunks := make([]ai.UIMessageChunk, 0, len(ids)*2)
	for _, id := range ids {
		seen := state.messageSeen[id]
		ns, hasNS := state.messageNamespaces[id]
		if seen.text {
			chunk := ai.UIMessageChunk{"type": "text-end", "id": id}
			if hasNS {
				chunk = tagNamespaceChunk(chunk, ns)
			}
			chunks = append(chunks, chunk)
		}
		if seen.reasoning {
			chunk := ai.UIMessageChunk{"type": "reasoning-end", "id": id}
			if hasNS {
				chunk = tagNamespaceChunk(chunk, ns)
			}
			chunks = append(chunks, chunk)
		}
		state.deleteSeen(id)
	}
	return chunks
}

func finalizeStreamEventParts(state *eventState) []ai.UIMessageChunk {
	var chunks []ai.UIMessageChunk
	if state.streamReasoningStarted {
		chunks = append(chunks, ai.UIMessageChunk{"type": "reasoning-end", "id": state.streamReasoningID})
		state.streamReasoningStarted = false
		state.streamReasoningID = ""
	}
	if state.streamTextStarted {
		chunks = append(chunks, ai.UIMessageChunk{"type": "text-end", "id": state.streamTextID})
		state.streamTextStarted = false
		state.streamTextID = ""
	}
	return chunks
}

func finalizeSeenPartsForValues(state *eventState) []ai.UIMessageChunk {
	var chunks []ai.UIMessageChunk
	for _, id := range sortedMessageSeenIDs(state) {
		seen := state.messageSeen[id]
		ns, hasNS := state.messageNamespaces[id]

		// Tag with the per-message namespace before deleting it below.
		if seen.text {
			chunk := ai.UIMessageChunk{"type": "text-end", "id": id}
			if hasNS {
				chunk = tagNamespaceChunk(chunk, ns)
			}
			chunks = append(chunks, chunk)
		}
		// TS emits tool-input-available here only from an accumulated
		// AIMessageChunk class instance (messageConcat); RemoteGraph yields
		// plain JSON, so it never does. The values message pass below
		// registers the key mapping for streamed calls instead.
		if seen.reasoning {
			chunk := ai.UIMessageChunk{"type": "reasoning-end", "id": id}
			if hasNS {
				chunk = tagNamespaceChunk(chunk, ns)
			}
			chunks = append(chunks, chunk)
		}
		state.deleteSeen(id)
	}
	return chunks
}

type toolCall struct {
	id    string
	name  string
	args  interface{}
	index int
}

type actionRequest struct {
	id    string
	name  string
	input interface{}
}

func extractToolCallChunks(message map[string]interface{}) []toolCall {
	source := messageDataSource(message)
	raw, ok := asSlice(source["tool_call_chunks"])
	if !ok {
		return nil
	}
	calls := make([]toolCall, 0, len(raw))
	for _, item := range raw {
		m, ok := asMap(item)
		if !ok {
			continue
		}
		index, _ := numberAsInt(m["index"])
		calls = append(calls, toolCall{
			id:    stringValue(m["id"]),
			name:  stringValue(m["name"]),
			args:  stringValue(m["args"]),
			index: index,
		})
	}
	return calls
}

// extractToolCalls mirrors the values-event tool call lookup of TS
// processLangGraphEvent: only AI messages carry tool calls; the normalized
// `tool_calls` array is used as is (a call without an id is skipped by the
// caller), and `additional_kwargs.tool_calls` (OpenAI format) is converted with
// the `call_<index>` / `unknown` / `{}` fallbacks.
func extractToolCalls(message map[string]interface{}) []toolCall {
	if !isAIMessageChunk(message) {
		return nil
	}
	source := messageDataSource(message)
	if raw, ok := asSlice(source["tool_calls"]); ok {
		calls := make([]toolCall, 0, len(raw))
		for _, item := range raw {
			m, ok := asMap(item)
			if !ok {
				continue
			}
			calls = append(calls, toolCall{id: stringValue(m["id"]), name: stringValue(m["name"]), args: m["args"]})
		}
		return calls
	}
	raw := openAIToolCalls(source)
	calls := make([]toolCall, 0, len(raw))
	for i, item := range raw {
		m, ok := asMap(item)
		if !ok {
			continue
		}
		fn, _ := asMap(m["function"])
		calls = append(calls, toolCall{
			id:   firstString(m["id"], fmt.Sprintf("call_%d", i)),
			name: firstString(fn["name"], "unknown"),
			args: parseArgs(fn["arguments"]),
		})
	}
	return calls
}

func openAIToolCalls(source map[string]interface{}) []interface{} {
	additional, ok := asMap(source["additional_kwargs"])
	if !ok {
		return nil
	}
	raw, _ := asSlice(additional["tool_calls"])
	return raw
}

func messageMaps(root map[string]interface{}) []map[string]interface{} {
	raw, ok := asSlice(root["messages"])
	if !ok {
		return nil
	}
	messages := make([]map[string]interface{}, 0, len(raw))
	for _, item := range raw {
		if msg, ok := asMap(item); ok {
			messages = append(messages, msg)
		}
	}
	return messages
}

func actionRequests(root map[string]interface{}) []actionRequest {
	rawInterrupts, ok := asSlice(root["__interrupt__"])
	if !ok {
		return nil
	}
	var out []actionRequest
	for _, interrupt := range rawInterrupts {
		item, ok := asMap(interrupt)
		if !ok {
			continue
		}
		value, ok := asMap(item["value"])
		if !ok {
			continue
		}
		// TS: `interruptValue.actionRequests || interruptValue.action_requests`.
		requests := value["actionRequests"]
		if !jsTruthy(requests) {
			requests = value["action_requests"]
		}
		rawRequests, ok := asSlice(requests)
		if !ok {
			continue
		}
		for _, raw := range rawRequests {
			req, ok := asMap(raw)
			if !ok {
				continue
			}
			// TS: `actionRequest.args || actionRequest.arguments`.
			input := req["args"]
			if !jsTruthy(input) {
				input = req["arguments"]
			}
			out = append(out, actionRequest{
				id:    stringValue(req["id"]),
				name:  stringValue(req["name"]),
				input: input,
			})
		}
	}
	return out
}

// messageDataSource is the object a message's fields live on: kwargs for a
// serialized `{type: "constructor", kwargs}` message, the message itself
// otherwise (TS `msgObj.type === 'constructor' && msgObj.kwargs`).
func messageDataSource(message map[string]interface{}) map[string]interface{} {
	if message["type"] == "constructor" {
		if kwargs, ok := asMap(message["kwargs"]); ok {
			return kwargs
		}
	}
	return message
}

// kwargsSource is the object the TS reasoning and citation extractors read: the
// message's kwargs whenever it has an object kwargs (no `type: "constructor"`
// check there), the message itself otherwise.
func kwargsSource(message map[string]interface{}) map[string]interface{} {
	if kwargs, ok := asMap(message["kwargs"]); ok {
		return kwargs
	}
	return message
}

// isToolMessage mirrors TS isToolMessageType for plain objects: type "tool", or
// a serialized constructor whose id path names ToolMessage.
func isToolMessage(message map[string]interface{}) bool {
	if message["type"] == "tool" {
		return true
	}
	if message["type"] != "constructor" {
		return false
	}
	ids, ok := asSlice(message["id"])
	if !ok {
		return false
	}
	for _, id := range ids {
		if stringValue(id) == "ToolMessage" {
			return true
		}
	}
	return false
}

func metadataMap(items []interface{}) (map[string]interface{}, bool) {
	if len(items) < 2 {
		return nil, false
	}
	return asMap(items[1])
}

func messageID(message map[string]interface{}) string {
	id, _ := messageIDOpt(message)
	return id
}

// messageIDOpt is TS getMessageId: `typeof id === 'string'` on the message,
// else on its kwargs for a serialized constructor. An empty string id is an
// id (present), which matters for `getMessageId(msg) ?? fallback`.
func messageIDOpt(message map[string]interface{}) (string, bool) {
	if id, ok := message["id"].(string); ok {
		return id, true
	}
	source := messageDataSource(message)
	id, ok := source["id"].(string)
	return id, ok
}

func messageText(message map[string]interface{}) string {
	source := messageDataSource(message)
	content := source["content"]
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var b strings.Builder
		for _, item := range v {
			block, ok := asMap(item)
			if !ok || stringValue(block["type"]) != "text" {
				continue
			}
			b.WriteString(stringValue(block["text"]))
		}
		return b.String()
	default:
		// TS getMessageText: content that is neither a string nor an array
		// (or absent) yields no text.
		return ""
	}
}

// messageReasoning mirrors TS extractReasoningFromContentBlocks, for streaming
// chunks whose content is delta text: reasoning/thinking content blocks first,
// then additional_kwargs.reasoning.summary. It never reads
// response_metadata.output, which holds accumulated text.
func messageReasoning(message map[string]interface{}) string {
	source := kwargsSource(message)
	var b strings.Builder
	parts := 0
	for _, block := range langChainContentBlocks(source) {
		switch stringValue(block["type"]) {
		case "reasoning":
			if text, ok := block["reasoning"].(string); ok {
				b.WriteString(text)
				parts++
			}
		case "thinking":
			if text, ok := block["thinking"].(string); ok {
				b.WriteString(text)
				parts++
			}
		}
	}
	// TS returns once any block contributed, even an empty string: the
	// additional_kwargs fallback is only for messages with no such block.
	if parts > 0 {
		return b.String()
	}
	if additional, ok := asMap(source["additional_kwargs"]); ok {
		if reasoning, ok := asMap(additional["reasoning"]); ok {
			if summary, ok := asSlice(reasoning["summary"]); ok {
				return summaryTexts(summary, false)
			}
		}
	}
	return ""
}

type langChainCitation struct {
	URL       string
	Title     string
	CitedText string
	// TitleEmpty and CitedTextEmpty mark a title or cited text that is the
	// empty string rather than absent: TS `??` and `typeof === 'string'`
	// tell the two apart.
	TitleEmpty     bool
	CitedTextEmpty bool
	Start          interface{}
	End            interface{}
	Source         interface{}
}

func extractCitations(message map[string]interface{}) []langChainCitation {
	source := kwargsSource(message)
	var out []langChainCitation
	for _, block := range langChainContentBlocks(source) {
		rawAnnotations, ok := asSlice(block["annotations"])
		if !ok {
			continue
		}
		for _, raw := range rawAnnotations {
			annotation, ok := asMap(raw)
			if !ok || stringValue(annotation["type"]) != "citation" {
				continue
			}
			citedText := firstPresent(annotation, "cited_text", "citedText", "text")
			citation := langChainCitation{
				URL:       stringValue(annotation["url"]),
				Title:     stringValue(annotation["title"]),
				CitedText: stringValue(citedText),
				Start:     firstPresent(annotation, "start_index", "startIndex"),
				End:       firstPresent(annotation, "end_index", "endIndex"),
				Source:    annotation["source"],
			}
			if title, ok := annotation["title"].(string); ok && title == "" {
				citation.TitleEmpty = true
			}
			if text, ok := citedText.(string); ok && text == "" {
				citation.CitedTextEmpty = true
			}
			out = append(out, citation)
		}
	}
	return out
}

func langChainContentBlocks(source map[string]interface{}) []map[string]interface{} {
	var raw []interface{}
	if blocks, ok := asSlice(source["content_blocks"]); ok {
		raw = blocks
	} else if blocks, ok := asSlice(source["contentBlocks"]); ok {
		raw = blocks
	} else if blocks, ok := asSlice(source["content"]); ok {
		raw = blocks
	}
	if len(raw) == 0 {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(raw))
	for _, item := range raw {
		if block, ok := asMap(item); ok {
			out = append(out, block)
		}
	}
	return out
}

func emitSourceChunks(state *eventState, messageID string, citations []langChainCitation) []ai.UIMessageChunk {
	var chunks []ai.UIMessageChunk
	for _, citation := range citations {
		sourceID := citation.URL
		chunkType := "source-url"
		// TS: a document source is labelled by title ?? source; a url source
		// carries only the citation's own title.
		label := citation.Title
		if citation.URL == "" {
			// TS `citation.title ?? citation.source`: an empty title is not
			// absent, so it does not fall back to the source.
			if label == "" && !citation.TitleEmpty {
				label = stringValue(citation.Source)
			}
			if label == "" {
				continue
			}
			sourceID = messageID + ":" + label + ":" + citation.CitedText + ":" + stringValue(citation.Start) + ":" + stringValue(citation.End)
			chunkType = "source-document"
		}
		if sourceID == "" || state.emittedSources[sourceID] {
			continue
		}
		state.emittedSources[sourceID] = true
		chunk := ai.UIMessageChunk{
			"type":     chunkType,
			"sourceId": sourceID,
		}
		if metadata := citationProviderMetadata(citation); len(metadata) > 0 {
			chunk["providerMetadata"] = map[string]interface{}{"langchain": metadata}
		}
		if label != "" {
			chunk["title"] = label
		}
		if citation.URL != "" {
			chunk["url"] = citation.URL
		} else {
			chunk["mediaType"] = "text/plain"
		}
		chunks = append(chunks, chunk)
	}
	return chunks
}

func citationProviderMetadata(citation langChainCitation) map[string]interface{} {
	metadata := map[string]interface{}{}
	if citation.CitedText != "" || citation.CitedTextEmpty {
		metadata["citedText"] = citation.CitedText
	}
	if isNumber(citation.Start) {
		metadata["startIndex"] = citation.Start
	}
	if isNumber(citation.End) {
		metadata["endIndex"] = citation.End
	}
	// TS `typeof citation.source === 'string'`: an empty string counts.
	if _, ok := citation.Source.(string); ok {
		metadata["source"] = citation.Source
	}
	return metadata
}

func parseArgs(raw interface{}) interface{} {
	switch v := raw.(type) {
	case nil:
		return map[string]interface{}{}
	case string:
		if v == "" {
			return map[string]interface{}{}
		}
		var decoded interface{}
		if err := json.Unmarshal([]byte(v), &decoded); err == nil {
			return decoded
		}
		return map[string]interface{}{}
	case map[string]interface{}:
		// TS JSON.parse(object) coerces it to "[object Object]", fails and
		// falls back to {}.
		return map[string]interface{}{}
	default:
		return v
	}
}

func contentText(parts []types.ContentPart) string {
	var b strings.Builder
	for _, part := range parts {
		if text, ok := part.(types.TextContent); ok {
			b.WriteString(text.Text)
		}
	}
	return b.String()
}

func convertAssistantContent(parts []types.ContentPart) LangChainMessage {
	textParts := make([]string, 0)
	toolCalls := make([]interface{}, 0)
	for _, part := range parts {
		switch p := part.(type) {
		case types.TextContent:
			textParts = append(textParts, p.Text)
		case types.ToolCallContent:
			// Compare the concrete map (not an interface{} wrapping it,
			// which would never compare equal to nil even when the
			// underlying map is nil) so the parseArgs(p.Input) fallback
			// actually runs for tool calls without parsed Arguments.
			var args interface{}
			if p.Arguments == nil {
				args = parseArgs(p.Input)
			} else {
				args = p.Arguments
			}
			toolCalls = append(toolCalls, map[string]interface{}{
				"id":   p.ToolCallID,
				"name": p.ToolName,
				"args": args,
			})
		}
	}
	// langchain-core's AIMessage always carries tool_calls and
	// invalid_tool_calls (empty when there are none) in toDict().data.
	msg := LangChainMessage{
		"type":               "ai",
		"content":            strings.Join(textParts, ""),
		"tool_calls":         toolCalls,
		"invalid_tool_calls": []interface{}{},
	}
	return wireMessage(msg)
}

// wireMessage adds the fields TS RemoteGraph serializes with every message
// ({...message.toDict().data, role: message.getType()}).
func wireMessage(msg LangChainMessage) LangChainMessage {
	msg["role"] = msg["type"]
	msg["additional_kwargs"] = map[string]interface{}{}
	msg["response_metadata"] = map[string]interface{}{}
	return msg
}

func convertUserContent(parts []types.ContentPart) interface{} {
	blocks := make([]interface{}, 0, len(parts))
	for _, part := range parts {
		switch p := part.(type) {
		case types.TextContent:
			blocks = append(blocks, map[string]interface{}{"type": "text", "text": p.Text})
		case types.ImageContent:
			blocks = append(blocks, imageContentBlock(p))
		case types.FileContent:
			blocks = append(blocks, fileContentBlock(p))
		}
	}
	// TS: when every block is text the message content is the joined string.
	var joined strings.Builder
	allText := true
	for _, block := range blocks {
		m := block.(map[string]interface{})
		if m["type"] != "text" {
			allText = false
			break
		}
		joined.WriteString(m["text"].(string))
	}
	if allText {
		return joined.String()
	}
	return blocks
}

func convertToolResultPart(result types.ToolResultContent) LangChainMessage {
	return wireMessage(LangChainMessage{
		"type":         "tool",
		"tool_call_id": result.ToolCallID,
		"content":      toolResultContent(result),
	})
}

func toolResultContent(result types.ToolResultContent) string {
	if result.Output == nil {
		if result.Error != "" {
			return result.Error
		}
		if result.Result == nil {
			return ""
		}
		if s, ok := result.Result.(string); ok {
			return s
		}
		data, err := jsonstr.Marshal(result.Result)
		if err != nil {
			return fmt.Sprint(result.Result)
		}
		return string(data)
	}
	switch result.Output.Type {
	case types.ToolResultOutputText, types.ToolResultOutputErrorText:
		return stringValue(result.Output.Value)
	case types.ToolResultOutputJSON, types.ToolResultOutputErrorJSON:
		data, err := jsonstr.Marshal(result.Output.Value)
		if err != nil {
			return ""
		}
		return string(data)
	case types.ToolResultOutputContent:
		var b strings.Builder
		for _, block := range result.Output.Content {
			if text, ok := block.(types.TextContentBlock); ok {
				b.WriteString(text.Text)
			}
		}
		return b.String()
	default:
		return ""
	}
}

// imageContentBlock converts an ImageContent part into LangChain's canonical
// image content block shape (`{type:'image', url}` or
// `{type:'image', data, mimeType}`), matching
// `convertImageToContentBlock` in the TypeScript adapter.
func imageContentBlock(part types.ImageContent) map[string]interface{} {
	if isNonWebURL(part.URL) {
		return map[string]interface{}{"type": "image", "url": part.URL}
	}
	return canonicalImageContentBlock(part.URL, "", part.Image, part.MimeType)
}

// canonicalImageContentBlock builds LangChain's canonical image content
// block. httpUrlOrData may be a plain http(s) URL, a `data:` URL, or empty;
// dataString is a plain (non-data-URL) base64 string; rawData is used only
// when neither string source is present. mediaType defaults to "image/png".
func canonicalImageContentBlock(urlOrDataURL string, dataString string, rawData []byte, mediaType string) map[string]interface{} {
	if mediaType == "" {
		mediaType = "image/png"
	}
	if urlOrDataURL != "" {
		return canonicalImageBlockFromString(urlOrDataURL, mediaType)
	}
	if dataString != "" {
		return canonicalImageBlockFromString(dataString, mediaType)
	}
	return map[string]interface{}{"type": "image", "data": base64.StdEncoding.EncodeToString(rawData), "mimeType": mediaType}
}

// canonicalImageBlockFromString mirrors the TypeScript adapter's
// `convertImageToContentBlock` string handling: http(s) URLs and non-base64
// data URLs become `{type:'image', url}`; `data:...;base64,...` URLs are
// split into `{type:'image', data, mimeType}`; anything else is treated as a
// bare base64 string.
func canonicalImageBlockFromString(data string, mediaType string) map[string]interface{} {
	if strings.HasPrefix(data, "http://") || strings.HasPrefix(data, "https://") {
		return map[string]interface{}{"type": "image", "url": data}
	}
	if strings.HasPrefix(data, "data:") {
		if mime, b64, ok := parseBase64DataURL(data); ok {
			return map[string]interface{}{"type": "image", "data": b64, "mimeType": mime}
		}
		return map[string]interface{}{"type": "image", "url": data}
	}
	return map[string]interface{}{"type": "image", "data": data, "mimeType": mediaType}
}

// parseBase64DataURL parses a `data:<mime>;base64,<data>` URL, mirroring the
// TypeScript adapter's `/^data:([^;]+);base64,(.+)$/` regex.
func parseBase64DataURL(dataURL string) (mime string, data string, ok bool) {
	rest := strings.TrimPrefix(dataURL, "data:")
	const marker = ";base64,"
	idx := strings.Index(rest, marker)
	if idx < 0 {
		return "", "", false
	}
	mime = rest[:idx]
	data = rest[idx+len(marker):]
	// The TS pattern is /^data:([^;]+);base64,(.+)$/: the media type cannot
	// contain another parameter.
	// `.` does not match a line terminator, and the pattern is anchored at
	// both ends without the m flag, so a payload with a line break is not a
	// base64 data URL (TS keeps it as a plain url).
	if mime == "" || data == "" || strings.Contains(mime, ";") || strings.ContainsAny(data, "\n\r\u2028\u2029") {
		return "", "", false
	}
	return mime, data, true
}

// fileData returns the file's data the way the TS adapter normalizes the
// tagged data shape: a URL, inline text or base64 string; a provider reference
// becomes "".
func fileData(part types.FileContent) string {
	switch {
	case part.URL != "":
		return part.URL
	case part.Text != "":
		return part.Text
	case part.Reference != "":
		return ""
	}
	switch part.FileData.Type {
	case types.FileDataTypeURL:
		return part.FileData.URL
	case types.FileDataTypeText:
		return part.FileData.Text
	case types.FileDataTypeReference:
		return ""
	}
	if part.FileData.DataString != "" {
		return part.FileData.DataString
	}
	data := part.Data
	if len(data) == 0 {
		data = part.FileData.Data
	}
	return base64.StdEncoding.EncodeToString(data)
}

// fileContentBlock mirrors the non-image branch of TS convertUserContent:
// {type:'file', url | data, mimeType, filename}.
func fileContentBlock(part types.FileContent) map[string]interface{} {
	mediaType := part.MediaType
	if mediaType == "" {
		mediaType = part.MimeType
	}
	isURL := part.URL != "" || (part.FileData.Type == types.FileDataTypeURL && part.FileData.URL != "")
	if strings.HasPrefix(mediaType, "image/") {
		if isURL && isNonWebURL(fileData(part)) {
			return map[string]interface{}{"type": "image", "url": fileData(part)}
		}
		return canonicalImageBlockFromString(fileData(part), mediaType)
	}
	filename := part.Filename
	if filename == "" {
		filename = defaultFilename(mediaType, "file")
	}
	data := fileData(part)
	if isURL && isNonWebURL(data) {
		// A URL object whose protocol is not data: becomes a url block
		// whatever its scheme (TS `data instanceof URL`).
		return map[string]interface{}{"type": "file", "url": data, "mimeType": mediaType, "filename": filename}
	}
	block := func(key, value, mime string) map[string]interface{} {
		return map[string]interface{}{"type": "file", key: value, "mimeType": mime, "filename": filename}
	}
	switch {
	case strings.HasPrefix(data, "http://") || strings.HasPrefix(data, "https://"):
		return block("url", data, mediaType)
	case strings.HasPrefix(data, "data:"):
		if mime, b64, ok := parseBase64DataURL(data); ok {
			return block("data", b64, mime)
		}
		return block("url", data, mediaType)
	default:
		return block("data", data, mediaType)
	}
}

// isNonWebURL reports a URL value (a TS URL object, not a bare string) whose
// scheme is neither http(s) nor data:, such as gs:// or s3://. TS
// convertImageToContentBlock / convertUserContent send those as `{url}`; only
// plain strings fall through to the base64 data branch.
func isNonWebURL(raw string) bool {
	scheme, _, ok := strings.Cut(raw, ":")
	if !ok || scheme == "" {
		return false
	}
	for i, r := range scheme {
		isAlpha := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if !isAlpha && (i == 0 || ((r < '0' || r > '9') && r != '+' && r != '-' && r != '.')) {
			return false
		}
	}
	switch strings.ToLower(scheme) {
	case "http", "https", "data":
		return false
	}
	return true
}

func defaultFilename(mediaType, prefix string) string {
	parts := strings.Split(mediaType, "/")
	if len(parts) > 1 && parts[1] != "" {
		return prefix + "." + parts[1]
	}
	return prefix + ".bin"
}

func toolCallKey(name string, args interface{}) string {
	return name + ":" + stableJSONString(args)
}

func stableJSONString(v interface{}) string {
	normalized := normalizeForStableJSON(v)
	data, err := jsonstr.Marshal(normalized)
	if err != nil {
		return "null"
	}
	return string(data)
}

func normalizeForStableJSON(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make(map[string]interface{}, len(x))
		for _, k := range keys {
			out[k] = normalizeForStableJSON(x[k])
		}
		return out
	case map[string]string:
		out := make(map[string]interface{}, len(x))
		for k, v := range x {
			out[k] = v
		}
		return normalizeForStableJSON(out)
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, item := range x {
			out[i] = normalizeForStableJSON(item)
		}
		return out
	default:
		return v
	}
}

func asMap(v interface{}) (map[string]interface{}, bool) {
	if m, ok := v.(map[string]interface{}); ok {
		return m, true
	}
	if chunk, ok := v.(ai.UIMessageChunk); ok {
		return map[string]interface{}(chunk), true
	}
	return nil, false
}

func asSlice(v interface{}) ([]interface{}, bool) {
	switch s := v.(type) {
	case []interface{}:
		return s, true
	case []map[string]interface{}:
		out := make([]interface{}, len(s))
		for i := range s {
			out[i] = s[i]
		}
		return out, true
	}
	return nil, false
}

// jsTruthy reports JavaScript truthiness of a decoded JSON value.
func jsTruthy(v interface{}) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case float64:
		return x != 0
	case int:
		return x != 0
	case int64:
		return x != 0
	case json.Number:
		f, err := x.Float64()
		return err != nil || f != 0
	default:
		return true
	}
}

func firstPresent(m map[string]interface{}, keys ...string) interface{} {
	for _, key := range keys {
		if value, ok := m[key]; ok {
			return value
		}
	}
	return nil
}

func numberAsInt(v interface{}) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	case json.Number:
		i, err := x.Int64()
		return int(i), err == nil
	default:
		return 0, false
	}
}

func isNumber(v interface{}) bool {
	switch v.(type) {
	case int, int64, float64, json.Number:
		return true
	default:
		return false
	}
}

func firstString(v interface{}, fallback string) string {
	if s := stringValue(v); s != "" {
		return s
	}
	return fallback
}

func stringValue(v interface{}) string {
	switch x := v.(type) {
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		return ""
	}
}
