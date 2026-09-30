// Package langchain adapts LangChain and LangGraph stream events to AI SDK UI
// message stream chunks.
package langchain

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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
}

// StreamCallbacks mirrors the TypeScript adapter callback surface for stream
// lifecycle hooks. Callback errors are reported on the returned error channel.
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
		for event := range events {
			streamEvent := StreamEvent{Mode: "model", Data: event}
			if tuple, ok := asSlice(event); ok {
				streamEvent = ParseLangGraphEvent(tuple)
			} else if m, ok := asMap(event); ok {
				if stringValue(m["event"]) != "" {
					streamEvent = StreamEvent{Mode: "streamEvents", Data: event}
				}
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
		if callbacks != nil && callbacks.OnStart != nil {
			if err := callbacks.OnStart(); err != nil {
				reportCallbackError(errs, callbacks, err)
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
					for _, chunk := range finishChunks(state, sendFinishEnabled(callbacks)) {
						if err := handleCallbackChunk(callbacks, &textChunks, chunk); err != nil {
							reportCallbackError(errs, callbacks, err)
							return
						}
						if !sendChunk(ctx, out, chunk) {
							finalizeCallbacks(errs, callbacks, textChunks, lastValuesData, ctx.Err())
							return
						}
					}
					finalizeCallbacks(errs, callbacks, textChunks, lastValuesData, nil)
					return
				}
				if event.Mode == "values" {
					lastValuesData = event.Data
				}
				for _, chunk := range processLangGraphEvent(state, event) {
					if err := handleCallbackChunk(callbacks, &textChunks, chunk); err != nil {
						reportCallbackError(errs, callbacks, err)
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

func finalizeCallbacks(errs chan<- error, callbacks *StreamCallbacks, textChunks []string, finalState interface{}, terminalErr error) {
	completion := strings.Join(textChunks, "")
	if callbacks != nil && callbacks.OnFinal != nil {
		if err := callbacks.OnFinal(completion); err != nil {
			reportCallbackError(errs, callbacks, err)
			return
		}
	}
	if terminalErr != nil {
		if callbacks != nil && callbacks.OnAbort != nil && isContextAbort(terminalErr) {
			if err := callbacks.OnAbort(); err != nil {
				reportCallbackError(errs, callbacks, err)
				return
			}
		} else if callbacks != nil && callbacks.OnError != nil {
			if err := callbacks.OnError(terminalErr); err != nil {
				reportCallbackError(errs, callbacks, err)
				return
			}
		}
		errs <- terminalErr
		return
	}
	if callbacks != nil && callbacks.OnFinish != nil {
		if err := callbacks.OnFinish(finalState); err != nil {
			reportCallbackError(errs, callbacks, err)
			return
		}
	}
}

func reportCallbackError(errs chan<- error, callbacks *StreamCallbacks, err error) {
	if callbacks != nil && callbacks.OnError != nil {
		_ = callbacks.OnError(err)
	}
	errs <- err
}

func isContextAbort(err error) bool {
	return err == context.Canceled || err == context.DeadlineExceeded
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
			out = append(out, LangChainMessage{"type": "system", "content": contentText(message.Content)})
		case types.RoleUser:
			out = append(out, LangChainMessage{"type": "human", "content": convertUserContent(message.Content)})
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

	// per-namespace step lifecycle bookkeeping, keyed by namespaceKey(...)
	currentStepsByNamespace                    map[string]int
	messageIdsInCurrentStepByNamespace         map[string]map[string]bool
	emittedToolCallsInCurrentStepByNamespace   map[string]map[string]bool
	emittedToolInputsInCurrentStepByNamespace  map[string]map[string]bool
	emittedToolOutputsInCurrentStepByNamespace map[string]map[string]bool

	// direct model / streamEvents stream state
	streamMessageID        string
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

		currentStepsByNamespace:                    map[string]int{},
		messageIdsInCurrentStepByNamespace:         map[string]map[string]bool{},
		emittedToolCallsInCurrentStepByNamespace:   map[string]map[string]bool{},
		emittedToolInputsInCurrentStepByNamespace:  map[string]map[string]bool{},
		emittedToolOutputsInCurrentStepByNamespace: map[string]map[string]bool{},
	}
}

type messageSeenState struct {
	text      bool
	reasoning bool
	tools     map[string]bool
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
		delete(state.messageSeen, id)
		delete(state.messageNamespaces, id)
	}
	return chunks, hasConcurrentMessageParts
}

func sortedMessageSeenIDs(state *eventState) []string {
	ids := make([]string, 0, len(state.messageSeen))
	for id := range state.messageSeen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
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
	out := make(ai.UIMessageChunk, len(chunk)+1)
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
	return []ai.UIMessageChunk{{
		"type":      "data-" + customTypeName,
		"id":        partID,
		"transient": partID == nil,
		"data":      data,
	}}
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

	msgID := messageID(message)
	if msgID == "" {
		msgID = "langchain-msg-1"
	}

	eventNS := namespaceKey(event.Namespace)
	if event.HasNamespace {
		state.messageNamespaces[msgID] = event.Namespace
	}

	var chunks []ai.UIMessageChunk

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
				}
				state.startNamespaceStep(eventNS, step)
			case step != currentStep:
				closeChunks, hasConcurrentMessageParts := closeStepNamespaceMessages(state, eventNS)
				chunks = append(chunks, closeChunks...)
				if !hasConcurrentMessageParts {
					chunks = append(chunks, ai.UIMessageChunk{"type": "finish-step"})
				}
				chunks = append(chunks, ai.UIMessageChunk{"type": "start-step"})
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

	if isToolMessage(message) {
		chunks = append(chunks, processToolMessage(state, message, eventNS)...)
		return chunks
	}

	for _, call := range extractToolCallChunks(message) {
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
		if seen.tools == nil {
			seen.tools = map[string]bool{}
		}
		if !seen.tools[call.id] {
			seen.tools[call.id] = true
			if !state.hasEmittedToolCallInCurrentStep(call.id, eventNS) {
				state.markToolCallEmitted(call.id, eventNS)
				chunks = append(chunks, ai.UIMessageChunk{"type": "tool-input-start", "toolCallId": call.id, "toolName": firstString(call.name, "unknown"), "dynamic": true})
			}
		}
		if argDelta, ok := call.args.(string); ok && argDelta != "" {
			chunks = append(chunks, ai.UIMessageChunk{"type": "tool-input-delta", "toolCallId": call.id, "inputTextDelta": argDelta})
		}
	}
	if text := messageText(message); text != "" {
		seen := state.seenFor(msgID)
		// Close any reasoning part still open on this message before
		// starting/continuing text, mirroring LangGraph model turns that
		// stream reasoning followed by text on the same message ID.
		if seen.reasoning && !seen.text {
			chunks = append(chunks, ai.UIMessageChunk{"type": "reasoning-end", "id": msgID})
			seen.reasoning = false
		}
		if !seen.text {
			chunks = append(chunks, ai.UIMessageChunk{"type": "text-start", "id": msgID})
			seen.text = true
		}
		chunks = append(chunks, ai.UIMessageChunk{"type": "text-delta", "delta": text, "id": msgID})
	}
	if reasoning := messageReasoning(message); reasoning != "" {
		seen := state.seenFor(msgID)
		if !seen.reasoning {
			chunks = append(chunks, ai.UIMessageChunk{"type": "reasoning-start", "id": msgID})
			seen.reasoning = true
		}
		chunks = append(chunks, ai.UIMessageChunk{"type": "reasoning-delta", "delta": reasoning, "id": msgID})
	}
	chunks = append(chunks, emitSourceChunks(state, msgID, extractCitations(message))...)
	return chunks
}

func processValuesEvent(state *eventState, event StreamEvent) []ai.UIMessageChunk {
	root, ok := asMap(event.Data)
	if !ok {
		return nil
	}
	eventNS := namespaceKey(event.Namespace)
	var chunks []ai.UIMessageChunk

	toolCallsByID := extractToolCallsByID(root)
	chunks = append(chunks, finalizeSeenPartsForValues(state, toolCallsByID)...)

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
				outputID := messageID(msg)
				if outputID == "" {
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

			for _, call := range extractToolCalls(msg) {
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
					state.markToolCallEmitted(call.id, lifecycleNS)
					key := toolCallKey(call.name, call.args)
					state.emittedToolCallsByKey[key] = call.id
					chunks = append(chunks, ai.UIMessageChunk{"type": "tool-input-start", "toolCallId": call.id, "toolName": call.name, "dynamic": true})
					state.markToolInputEmitted(call.id, lifecycleNS)
					chunks = append(chunks, ai.UIMessageChunk{"type": "tool-input-available", "toolCallId": call.id, "toolName": call.name, "input": call.args, "dynamic": true})
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
						chunks = append(chunks, ai.UIMessageChunk{"type": "tool-output-available", "toolCallId": call.id, "output": trailing.data["content"]})
					}
				}
			}

			chunks = append(chunks, emitSourceChunks(state, msgID, extractCitations(msg))...)
		}
	}

	chunks = append(chunks, actionRequestChunks(state, root, eventNS)...)
	return chunks
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
			toolCallID = fmt.Sprintf("hitl-%s-%d", request.name, time.Now().UnixMilli())
		}
		if !state.emittedToolCalls[toolCallID] {
			state.markToolCallEmitted(toolCallID, eventNS)
			state.emittedToolCallsByKey[key] = toolCallID
			chunks = append(chunks, ai.UIMessageChunk{"type": "tool-input-start", "toolCallId": toolCallID, "toolName": request.name, "dynamic": true})
			state.markToolInputEmitted(toolCallID, eventNS)
			chunks = append(chunks, ai.UIMessageChunk{"type": "tool-input-available", "toolCallId": toolCallID, "toolName": request.name, "input": request.input, "dynamic": true})
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
	toolName := firstString(payload["name"], "unknown")
	ns := namespaceKey(event.Namespace)

	var chunks []ai.UIMessageChunk
	switch stringValue(payload["event"]) {
	case "on_tool_start":
		key := toolCallKey(toolName, payload["input"])
		state.emittedToolCallsByKey[key] = toolCallID
		chunks = append(chunks, ensureToolInputLifecycle(state, toolCallID, toolName, ns, payload["input"], false)...)
	case "on_tool_event":
		chunks = append(chunks, ensureToolInputLifecycle(state, toolCallID, toolName, ns, payload["input"], true)...)
		chunks = append(chunks, ai.UIMessageChunk{"type": "tool-output-available", "toolCallId": toolCallID, "output": payload["data"], "preliminary": true})
	case "on_tool_end":
		chunks = append(chunks, ensureToolInputLifecycle(state, toolCallID, toolName, ns, payload["input"], true)...)
		state.markToolOutputEmitted(toolCallID, ns)
		chunks = append(chunks, ai.UIMessageChunk{"type": "tool-output-available", "toolCallId": toolCallID, "output": payload["output"]})
	case "on_tool_error":
		chunks = append(chunks, ensureToolInputLifecycle(state, toolCallID, toolName, ns, payload["input"], true)...)
		state.markToolOutputEmitted(toolCallID, ns)
		chunks = append(chunks, ai.UIMessageChunk{"type": "tool-output-error", "toolCallId": toolCallID, "errorText": formatToolError(payload["error"])})
	}
	return chunks
}

// ensureToolInputLifecycle emits tool-input-start/tool-input-available for a
// tools-mode tool call if they have not already been emitted. When
// allowPreviousStep is true and the call was already emitted in a prior step
// (globally, but not in the current step), it is treated as a delayed event
// for that prior step's call and no new input lifecycle is synthesized at all.
func ensureToolInputLifecycle(state *eventState, toolCallID, toolName, ns string, input interface{}, allowPreviousStep bool) []ai.UIMessageChunk {
	var chunks []ai.UIMessageChunk
	if !state.hasEmittedToolCallInCurrentStep(toolCallID, ns) {
		if allowPreviousStep && state.emittedToolCalls[toolCallID] {
			return nil
		}
		state.markToolCallEmitted(toolCallID, ns)
		chunks = append(chunks, ai.UIMessageChunk{"type": "tool-input-start", "toolCallId": toolCallID, "toolName": toolName, "dynamic": true})
	}
	if !state.hasEmittedToolInputInCurrentStep(toolCallID, ns) {
		state.markToolInputEmitted(toolCallID, ns)
		chunks = append(chunks, ai.UIMessageChunk{"type": "tool-input-available", "toolCallId": toolCallID, "toolName": toolName, "input": input, "dynamic": true})
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
		data, err := json.Marshal(e)
		if err != nil {
			return fmt.Sprint(e)
		}
		return string(data)
	}
}

func processToolMessage(state *eventState, message map[string]interface{}, ns string) []ai.UIMessageChunk {
	source := messageDataSource(message)
	toolCallID := stringValue(firstPresent(source, "tool_call_id", "toolCallId"))
	if toolCallID == "" {
		return nil
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
		return []ai.UIMessageChunk{{"type": "tool-output-error", "toolCallId": toolCallID, "errorText": errText}}
	}
	return []ai.UIMessageChunk{{"type": "tool-output-available", "toolCallId": toolCallID, "output": source["content"]}}
}

func processModelChunk(state *eventState, data interface{}) []ai.UIMessageChunk {
	chunk, ok := asMap(data)
	if !ok {
		return nil
	}
	if id := messageID(chunk); id != "" {
		state.streamMessageID = id
	}
	msgID := firstString(state.streamMessageID, "langchain-msg-1")
	var chunks []ai.UIMessageChunk
	if reasoning := messageReasoning(chunk); reasoning != "" {
		if !state.streamReasoningStarted {
			state.streamReasoningStarted = true
			state.streamReasoningID = msgID
			chunks = append(chunks, ai.UIMessageChunk{"type": "reasoning-start", "id": msgID})
		}
		chunks = append(chunks, ai.UIMessageChunk{"type": "reasoning-delta", "delta": reasoning, "id": state.streamReasoningID})
	}
	if text := messageText(chunk); text != "" {
		if state.streamReasoningStarted && !state.streamTextStarted {
			chunks = append(chunks, ai.UIMessageChunk{"type": "reasoning-end", "id": state.streamReasoningID})
			state.streamReasoningStarted = false
			state.streamReasoningID = ""
		}
		if !state.streamTextStarted {
			state.streamTextStarted = true
			state.streamTextID = msgID
			chunks = append(chunks, ai.UIMessageChunk{"type": "text-start", "id": msgID})
		}
		chunks = append(chunks, ai.UIMessageChunk{"type": "text-delta", "delta": text, "id": state.streamTextID})
	}
	chunks = append(chunks, emitSourceChunks(state, msgID, extractCitations(chunk))...)
	return chunks
}

func processStreamEventsEvent(state *eventState, data interface{}) []ai.UIMessageChunk {
	event, ok := asMap(data)
	if !ok {
		return nil
	}
	eventName := stringValue(event["event"])
	payload, _ := asMap(event["data"])
	switch eventName {
	case "on_chat_model_start":
		chunks := finalizeStreamEventParts(state)
		state.streamMessageID = firstString(event["run_id"], stringValue(payload["run_id"]))
		return chunks
	case "on_chat_model_stream":
		chunk, _ := asMap(payload["chunk"])
		if id := messageID(chunk); id != "" {
			state.streamMessageID = id
		}
		msgID := firstString(state.streamMessageID, "langchain-stream")
		var chunks []ai.UIMessageChunk
		if reasoning := messageReasoning(chunk); reasoning != "" {
			if !state.streamReasoningStarted {
				state.streamReasoningStarted = true
				state.streamReasoningID = msgID
				chunks = append(chunks, ai.UIMessageChunk{"type": "reasoning-start", "id": msgID})
			}
			chunks = append(chunks, ai.UIMessageChunk{"type": "reasoning-delta", "delta": reasoning, "id": msgID})
		}
		if text := messageText(chunk); text != "" {
			if state.streamReasoningStarted {
				chunks = append(chunks, ai.UIMessageChunk{"type": "reasoning-end", "id": state.streamReasoningID})
				state.streamReasoningStarted = false
				state.streamReasoningID = ""
			}
			if !state.streamTextStarted {
				state.streamTextStarted = true
				state.streamTextID = msgID
				chunks = append(chunks, ai.UIMessageChunk{"type": "text-start", "id": msgID})
			}
			chunks = append(chunks, ai.UIMessageChunk{"type": "text-delta", "delta": text, "id": state.streamTextID})
		}
		chunks = append(chunks, emitSourceChunks(state, msgID, extractCitations(chunk))...)
		return chunks
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
		output := firstPresent(payload, "output", "result")
		return []ai.UIMessageChunk{{"type": "tool-output-available", "toolCallId": runID, "output": output}}
	default:
		return nil
	}
}

func (state *eventState) seenFor(messageID string) *messageSeenState {
	seen := state.messageSeen[messageID]
	if seen == nil {
		seen = &messageSeenState{}
		state.messageSeen[messageID] = seen
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
		delete(state.messageSeen, id)
		delete(state.messageNamespaces, id)
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

func finalizeSeenPartsForValues(state *eventState, callsByID map[string]toolCall) []ai.UIMessageChunk {
	var chunks []ai.UIMessageChunk
	for _, id := range sortedMessageSeenIDs(state) {
		seen := state.messageSeen[id]
		ns, hasNS := state.messageNamespaces[id]
		msgNSKey := namespaceKey(ns)

		// Tag with the per-message namespace before deleting it below.
		if seen.text {
			chunk := ai.UIMessageChunk{"type": "text-end", "id": id}
			if hasNS {
				chunk = tagNamespaceChunk(chunk, ns)
			}
			chunks = append(chunks, chunk)
		}
		for callID := range seen.tools {
			call, ok := callsByID[callID]
			if !ok || call.name == "" {
				continue
			}
			state.markToolCallEmitted(callID, msgNSKey)
			state.emittedToolCallsByKey[toolCallKey(call.name, call.args)] = callID
			if !state.hasEmittedToolInputInCurrentStep(callID, msgNSKey) {
				state.markToolInputEmitted(callID, msgNSKey)
				chunks = append(chunks, ai.UIMessageChunk{"type": "tool-input-available", "toolCallId": callID, "toolName": call.name, "input": call.args, "dynamic": true})
			}
		}
		if seen.reasoning {
			chunk := ai.UIMessageChunk{"type": "reasoning-end", "id": id}
			if hasNS {
				chunk = tagNamespaceChunk(chunk, ns)
			}
			chunks = append(chunks, chunk)
		}
		delete(state.messageSeen, id)
		delete(state.messageNamespaces, id)
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

func extractToolCalls(message map[string]interface{}) []toolCall {
	source := messageDataSource(message)
	raw, ok := asSlice(source["tool_calls"])
	if !ok {
		raw = openAIToolCalls(source)
	}
	calls := make([]toolCall, 0, len(raw))
	for i, item := range raw {
		m, ok := asMap(item)
		if !ok {
			continue
		}
		name := stringValue(m["name"])
		args := m["args"]
		if fn, ok := asMap(m["function"]); ok {
			name = stringValue(fn["name"])
			args = parseArgs(fn["arguments"])
		}
		if args == nil {
			args = map[string]interface{}{}
		}
		calls = append(calls, toolCall{
			id:   firstString(m["id"], fmt.Sprintf("call_%d", i)),
			name: name,
			args: args,
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

func extractToolCallsByID(root map[string]interface{}) map[string]toolCall {
	out := map[string]toolCall{}
	for _, msg := range messageMaps(root) {
		for _, call := range extractToolCalls(msg) {
			if call.id != "" {
				out[call.id] = call
			}
		}
	}
	return out
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
		rawRequests, ok := asSlice(firstPresent(value, "actionRequests", "action_requests"))
		if !ok {
			continue
		}
		for _, raw := range rawRequests {
			req, ok := asMap(raw)
			if !ok {
				continue
			}
			input := firstPresent(req, "args", "arguments")
			out = append(out, actionRequest{
				id:    stringValue(req["id"]),
				name:  stringValue(req["name"]),
				input: input,
			})
		}
	}
	return out
}

func messageDataSource(message map[string]interface{}) map[string]interface{} {
	if kwargs, ok := asMap(message["kwargs"]); ok {
		return kwargs
	}
	return message
}

func isToolMessage(message map[string]interface{}) bool {
	if stringValue(message["type"]) == "tool" || stringValue(message["type"]) == "ToolMessage" {
		return true
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
	if id := stringValue(message["id"]); id != "" {
		return id
	}
	source := messageDataSource(message)
	return stringValue(source["id"])
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
		return stringValue(source["text"])
	}
}

func messageReasoning(message map[string]interface{}) string {
	source := messageDataSource(message)
	contentBlocks := langChainContentBlocks(source)
	var b strings.Builder
	for _, block := range contentBlocks {
		typ := stringValue(block["type"])
		switch typ {
		case "reasoning":
			b.WriteString(stringValue(block["reasoning"]))
		case "thinking":
			b.WriteString(stringValue(block["thinking"]))
		}
	}
	if additional, ok := asMap(source["additional_kwargs"]); ok {
		if reasoning := stringValue(additional["reasoning"]); reasoning != "" {
			b.WriteString(reasoning)
		}
		if reasoning, ok := asMap(additional["reasoning"]); ok {
			if summary, ok := asSlice(reasoning["summary"]); ok {
				for _, item := range summary {
					if summaryItem, ok := asMap(item); ok {
						b.WriteString(stringValue(summaryItem["text"]))
					}
				}
			}
		}
	}
	return b.String()
}

type langChainCitation struct {
	URL       string
	Title     string
	CitedText string
	Start     interface{}
	End       interface{}
	Source    interface{}
}

func extractCitations(message map[string]interface{}) []langChainCitation {
	source := messageDataSource(message)
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
			citation := langChainCitation{
				URL:       stringValue(annotation["url"]),
				Title:     firstString(annotation["title"], stringValue(annotation["source"])),
				CitedText: stringValue(firstPresent(annotation, "cited_text", "citedText", "text")),
				Start:     firstPresent(annotation, "start_index", "startIndex"),
				End:       firstPresent(annotation, "end_index", "endIndex"),
				Source:    annotation["source"],
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
		if sourceID == "" {
			sourceID = messageID + ":" + citation.Title + ":" + citation.CitedText + ":" + stringValue(citation.Start) + ":" + stringValue(citation.End)
			chunkType = "source-document"
		}
		if citation.URL == "" && citation.Title == "" {
			continue
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
		if citation.Title != "" {
			chunk["title"] = citation.Title
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
	if citation.CitedText != "" {
		metadata["citedText"] = citation.CitedText
	}
	if isNumber(citation.Start) {
		metadata["startIndex"] = citation.Start
	}
	if isNumber(citation.End) {
		metadata["endIndex"] = citation.End
	}
	if stringValue(citation.Source) != "" {
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
			args := interface{}(p.Arguments)
			if args == nil {
				args = parseArgs(p.Input)
			}
			toolCalls = append(toolCalls, map[string]interface{}{
				"id":   p.ToolCallID,
				"name": p.ToolName,
				"args": args,
			})
		}
	}
	msg := LangChainMessage{"type": "ai", "content": strings.Join(textParts, "")}
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
	}
	return msg
}

func convertUserContent(parts []types.ContentPart) interface{} {
	if len(parts) == 1 {
		if text, ok := parts[0].(types.TextContent); ok {
			return text.Text
		}
	}
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
	return blocks
}

func convertToolResultPart(result types.ToolResultContent) LangChainMessage {
	return LangChainMessage{
		"type":         "tool",
		"tool_call_id": result.ToolCallID,
		"content":      toolResultContent(result),
	}
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
		data, err := json.Marshal(result.Result)
		if err != nil {
			return fmt.Sprint(result.Result)
		}
		return string(data)
	}
	switch result.Output.Type {
	case types.ToolResultOutputText, types.ToolResultOutputErrorText:
		return stringValue(result.Output.Value)
	case types.ToolResultOutputJSON, types.ToolResultOutputErrorJSON:
		data, err := json.Marshal(result.Output.Value)
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
	if mime == "" || data == "" {
		return "", "", false
	}
	return mime, data, true
}

func fileContentBlock(part types.FileContent) map[string]interface{} {
	mediaType := part.MediaType
	if mediaType == "" {
		mediaType = part.MimeType
	}
	if strings.HasPrefix(mediaType, "image/") {
		return canonicalImageBlockForFile(part, mediaType)
	}
	filename := part.Filename
	if filename == "" {
		filename = defaultFilename(mediaType, "file")
	}
	metadata := map[string]interface{}{"filename": filename}
	switch {
	case part.URL != "":
		return map[string]interface{}{"type": "file", "source_type": "url", "url": part.URL, "mime_type": mediaType, "metadata": metadata}
	case part.Text != "":
		return map[string]interface{}{"type": "file", "source_type": "text", "text": part.Text, "mime_type": mediaType, "metadata": metadata}
	case part.Reference != "":
		return map[string]interface{}{"type": "file", "source_type": "reference", "reference": part.Reference, "mime_type": mediaType, "metadata": metadata}
	case part.FileData.Type == types.FileDataTypeURL:
		return map[string]interface{}{"type": "file", "source_type": "url", "url": part.FileData.URL, "mime_type": mediaType, "metadata": metadata}
	default:
		data := part.Data
		if len(data) == 0 && len(part.FileData.Data) > 0 {
			data = part.FileData.Data
		}
		encoded := part.FileData.DataString
		if encoded == "" {
			encoded = base64.StdEncoding.EncodeToString(data)
		}
		return map[string]interface{}{"type": "file", "source_type": "base64", "data": encoded, "mime_type": mediaType, "metadata": metadata}
	}
}

// canonicalImageBlockForFile converts a non-image-typed FileContent whose
// mediaType is an image/* MIME type into LangChain's canonical image content
// block, matching the TypeScript adapter's `isImage` branch in
// `convertUserContent`.
func canonicalImageBlockForFile(part types.FileContent, mediaType string) map[string]interface{} {
	urlOrDataURL := part.URL
	if urlOrDataURL == "" && part.FileData.Type == types.FileDataTypeURL {
		urlOrDataURL = part.FileData.URL
	}
	data := part.Data
	if len(data) == 0 && len(part.FileData.Data) > 0 {
		data = part.FileData.Data
	}
	return canonicalImageContentBlock(urlOrDataURL, part.FileData.DataString, data, mediaType)
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
	data, err := json.Marshal(normalized)
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
