package bedrock

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/bedrock/eventstream"
)

// bedrockStreamErrorMetadata maps a modeled Converse-stream exception type to
// its HTTP status code and retryability, mirroring TS
// amazon-bedrock-stream-error.ts#getAmazonBedrockStreamErrorMetadata.
func bedrockStreamErrorMetadata(exceptionType string) (statusCode int, isRetryable bool) {
	switch exceptionType {
	case "internalServerException", "InternalServerException":
		return 500, true
	case "modelStreamErrorException", "ModelStreamErrorException":
		return 424, true
	case "serviceUnavailableException", "ServiceUnavailableException":
		return 503, true
	case "throttlingException", "ThrottlingException":
		return 429, true
	case "validationException", "ValidationException":
		return 400, false
	default:
		return 0, false
	}
}

// bedrockStreamContentBlock tracks in-flight state for one Converse content
// block index during streaming, mirroring the TS SDK's `contentBlocks` map in
// doStream().
type bedrockStreamContentBlock struct {
	Kind               string // "text" | "tool-call" | "reasoning"
	ToolCallID         string
	ToolName           string
	JSONText           string
	IsJSONResponseTool bool
	RedactedContent    string
}

// bedrockConverseStream implements provider.TextStream by decoding the AWS
// event-stream framing from a Converse-stream HTTP response and translating
// it into normalized provider.StreamChunk values. Ports TS
// amazon-bedrock-chat-language-model.ts#doStream's TransformStream.
type bedrockConverseStream struct {
	decoder *eventstream.Decoder
	body    io.Closer

	isMistral            bool
	usesJSONResponseTool bool
	extractor            *jsonObjectTextExtractor

	warnings          []types.Warning
	modelID           string
	responseHeaders   map[string]string
	requestID         string
	responseTimestamp *time.Time

	contentBlocks map[int]*bedrockStreamContentBlock
	queue         []*provider.StreamChunk

	// includeRawChunks emits a ChunkTypeRaw chunk ({payloadType: payload},
	// TS rawValue) ahead of the parts each event produces.
	includeRawChunks bool

	startEmitted    bool
	finishReason    types.FinishReason
	rawFinishReason string
	usage           types.Usage
	hasUsage        bool

	providerMetadataPayload map[string]interface{}
	isJSONResponseFromTool  bool
	stopSequence            *string
	stopSequenceSet         bool

	done bool
	err  error

	// requestBody is the raw request body this stream was opened with,
	// exposed via RequestBody() (provider.StreamRequestBody, hand-off:
	// "stream request body field").
	requestBody interface{}
}

// RequestBody implements provider.StreamRequestBody, exposing the raw
// request body that was sent to open this stream.
func (s *bedrockConverseStream) RequestBody() interface{} { return s.requestBody }

func (s *bedrockConverseStream) enqueue(chunk *provider.StreamChunk) {
	s.queue = append(s.queue, chunk)
}

func (s *bedrockConverseStream) Next() (*provider.StreamChunk, error) {
	for {
		if len(s.queue) > 0 {
			chunk := s.queue[0]
			s.queue = s.queue[1:]
			return chunk, nil
		}
		if s.done {
			return nil, io.EOF
		}

		if !s.startEmitted {
			s.startEmitted = true
			responseMetadata := &provider.ResponseMetadata{
				ID:      s.requestID,
				ModelID: s.modelID,
				Headers: s.responseHeaders,
			}
			if s.responseTimestamp != nil {
				responseMetadata.Timestamp = *s.responseTimestamp
			}
			s.enqueue(&provider.StreamChunk{Type: provider.ChunkTypeStreamStart, Warnings: s.warnings})
			s.enqueue(&provider.StreamChunk{
				Type:             provider.ChunkTypeResponseMetadata,
				ResponseMetadata: responseMetadata,
			})
			continue
		}

		event, err := s.decoder.Next()
		if err != nil {
			if err == io.EOF {
				s.done = true
				return s.flush()
			}
			s.err = err
			s.done = true
			return nil, err
		}

		payloadType := ""
		switch event.MessageType {
		case "event":
			payloadType = event.EventType
		case "exception":
			payloadType = event.ExceptionType
		}
		if payloadType == "" {
			continue
		}

		// Decode into interface{} like TS safeParseJSON: a payload that is valid
		// JSON but not an object (5, "x", []) is not a parse error; it is wrapped
		// under its payload type and then fails (or, for an unknown payload type,
		// passes) schema validation below.
		var rawPayload interface{}
		if err := json.Unmarshal(event.Data, &rawPayload); err != nil {
			// Mirrors TS doStream's !chunk.success branch (amazon-bedrock-chat-
			// language-model.ts): report the error but keep reading — the stream
			// is not aborted — and mark finishReason as error in case the stream
			// ends (via decoder EOF) before any later messageStop overwrites it.
			s.finishReason = types.FinishReasonError
			s.rawFinishReason = "" // TS: {unified: 'error', raw: undefined}
			if s.includeRawChunks {
				// TS safeParseJSON: rawValue is undefined for unparsable JSON.
				s.enqueue(&provider.StreamChunk{Type: provider.ChunkTypeRaw, Raw: nil})
			}
			parseErr := providererrors.NewJSONParseError(string(event.Data), err)
			s.enqueue(&provider.StreamChunk{Type: provider.ChunkTypeError, Text: parseErr.Error(), Err: parseErr})
			continue
		}
		if rawPayload == nil {
			// TS runs `delete (parsedDataResult.value as any).p` on the parsed
			// value, which throws a TypeError for a JSON null and errors the whole
			// stream (no error part, no finish part).
			s.err = errors.New("Cannot convert undefined or null to object") //nolint:staticcheck // TS TypeError text
			s.done = true
			return nil, s.err
		}
		payload, _ := rawPayload.(map[string]interface{})
		delete(payload, "p") // AWS event-stream padding hint; irrelevant to consumers.

		// TS validates {payloadType: payload} against AmazonBedrockStreamSchema; a
		// mismatch is an error part (the event is skipped) and the finish reason
		// becomes error, like the unparsable-JSON branch above.
		if verr := validateConverseStreamEvent(payloadType, rawPayload); verr != nil {
			s.finishReason = types.FinishReasonError
			s.rawFinishReason = "" // TS: {unified: 'error', raw: undefined}
			if s.includeRawChunks {
				s.enqueue(&provider.StreamChunk{Type: provider.ChunkTypeRaw, Raw: map[string]interface{}{payloadType: rawPayload}})
			}
			s.enqueue(&provider.StreamChunk{Type: provider.ChunkTypeError, Text: verr.Message, Err: invalidConverseResponseError(verr)})
			continue
		}

		// Wrap the raw payload under its event/exception type key, mirroring TS
		// amazon-bedrock-event-stream-response-handler.ts's
		// `{ [payloadType]: parsedDataResult.value }`, so downstream handling
		// matches the documented AmazonBedrockStreamSchema shape.
		value := map[string]interface{}{payloadType: rawPayload}
		if payload == nil {
			// Only an unknown payload type can pass validation with a non-object
			// payload; zod strips the unknown key, so TS emits the raw chunk and
			// nothing else.
			if s.includeRawChunks {
				s.enqueue(&provider.StreamChunk{Type: provider.ChunkTypeRaw, Raw: value})
			}
			continue
		}
		if s.includeRawChunks {
			s.enqueue(&provider.StreamChunk{Type: provider.ChunkTypeRaw, Raw: value})
		}

		switch payloadType {
		case "internalServerException", "modelStreamErrorException", "serviceUnavailableException", "throttlingException", "validationException":
			s.finishReason = types.FinishReasonError
			s.rawFinishReason = "" // TS: {unified: 'error', raw: undefined}
			// Do NOT set s.done here. TS's enqueueError (amazon-bedrock-chat-
			// language-model.ts) records the error and sets finishReason but lets
			// the ReadableStream's own flush() run when the underlying connection
			// closes, so a modeled exception is always followed by a terminal
			// 'finish' part carrying finishReason:'error' and whatever usage/
			// providerMetadata had accrued. Mirroring that: the next decoder.Next()
			// call naturally returns io.EOF once AWS closes the stream after the
			// exception frame, which drives s.done=true and s.flush() below.
			message := fmt.Sprintf("Amazon Bedrock stream failed with %s", payloadType)
			// TS: `typeof error.message === 'string'`, so an empty message is kept.
			if m, ok := payload["message"].(string); ok {
				message = m
			}
			statusCode, isRetryable := bedrockStreamErrorMetadata(payloadType)
			// Surface the modeled exception's status code (and, via ErrorCode, its
			// type) on a structured error so callers inspecting stream.Err() after
			// the stream ends get more than a bare message. modelStreamErrorException
			// maps to HTTP 424 but is still retryable per TS
			// getAmazonBedrockStreamErrorMetadata, so set Retryable explicitly
			// (ProviderError.IsRetryable()'s generic 429/5xx heuristic would
			// otherwise call 424 non-retryable) — TS parity, P1-1c part 2.
			s.err = &providererrors.ProviderError{
				Provider:   "amazon-bedrock",
				StatusCode: statusCode,
				ErrorCode:  payloadType,
				Message:    message,
				Data:       payload,
				Retryable:  &isRetryable,
			}
			// Attach the same structured error to the stream chunk (via
			// StreamProviderError) so a mid-stream consumer inspecting the
			// ChunkTypeError chunk itself (before the stream ends and Err() is
			// read) also sees the correct type/statusCode/isRetryable, mirroring
			// TS's typed AmazonBedrockStreamError on the enqueued 'error' part
			// (P1-1c part 2: provider.StreamProviderError normalization).
			// TS's createAmazonBedrockStreamError never sets `code` (only
			// message/type/statusCode/isRetryable/data via
			// `...getAmazonBedrockStreamErrorMetadata(type)`) — leave Code nil
			// rather than duplicating the exception type into it.
			// TS passes `data: chunk.rawValue`, the wrapped {[payloadType]: payload}.
			chunkErr := providererrors.NewStreamProviderError(message, "amazon-bedrock", payloadType, nil, &statusCode, &isRetryable, value)
			s.enqueue(&provider.StreamChunk{Type: provider.ChunkTypeError, Text: message, Err: chunkErr})
			continue

		case "messageStop":
			if stopReason, ok := payload["stopReason"].(string); ok {
				s.finishReason = mapBedrockFinishReason(stopReason, s.isJSONResponseFromTool)
				s.rawFinishReason = stopReason
			}
			// TS reassigns stopSequence on every messageStop (`?? null`).
			s.stopSequence = nil
			s.stopSequenceSet = false
			if amf, ok := payload["additionalModelResponseFields"].(map[string]interface{}); ok {
				if delta, ok := amf["delta"].(map[string]interface{}); ok {
					if ss, ok := delta["stop_sequence"].(string); ok {
						s.stopSequence = &ss
						s.stopSequenceSet = true
					}
				}
			}
			continue

		case "metadata":
			s.handleMetadata(payload)
			continue

		case "contentBlockStart":
			s.handleContentBlockStart(value)
			continue

		case "contentBlockDelta":
			s.handleContentBlockDelta(value)
			continue

		case "contentBlockStop":
			s.handleContentBlockStop(value)
			continue
		}

		continue

	}
}

func (s *bedrockConverseStream) handleMetadata(metadata map[string]interface{}) {
	// TS builds a fresh metadata payload for each metadata event and, when it
	// is non-empty, replaces providerMetadata with it (no merge across events).
	payload := map[string]interface{}{}
	if usage, ok := metadata["usage"].(map[string]interface{}); ok {
		usageBytes, _ := json.Marshal(usage)
		s.usage = convertBedrockConverseUsage(usageBytes)
		s.hasUsage = true

		meta := map[string]interface{}{}
		if v := usage["cacheWriteInputTokens"]; v != nil {
			meta["cacheWriteInputTokens"] = v
		}
		if v := usage["cacheDetails"]; v != nil {
			meta["cacheDetails"] = v
		}
		if len(meta) > 0 {
			payload["usage"] = meta
		}
	}
	if trace := metadata["trace"]; jsTruthy(trace) {
		payload["trace"] = trace
	}
	if pc := metadata["performanceConfig"]; pc != nil {
		payload["performanceConfig"] = pc
	}
	if st := metadata["serviceTier"]; st != nil {
		payload["serviceTier"] = st
	}
	if len(payload) > 0 {
		s.providerMetadataPayload = payload
	}
}

// jsTruthy reports whether a decoded JSON value is truthy in JavaScript.
func jsTruthy(v interface{}) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		return x != ""
	default:
		return true // objects and arrays
	}
}

func (s *bedrockConverseStream) ensureProviderMetadata() {
	if s.providerMetadataPayload == nil {
		s.providerMetadataPayload = map[string]interface{}{}
	}
}

func (s *bedrockConverseStream) handleContentBlockStart(value map[string]interface{}) {
	cbs, _ := value["contentBlockStart"].(map[string]interface{})
	if cbs == nil {
		return
	}
	idxF, ok := cbs["contentBlockIndex"].(float64)
	if !ok {
		return
	}
	idx := int(idxF)

	start, _ := cbs["start"].(map[string]interface{})
	toolUse, hasToolUse := start["toolUse"].(map[string]interface{})

	if !hasToolUse {
		// TS sets the block and emits text-start unconditionally.
		s.contentBlocks[idx] = &bedrockStreamContentBlock{Kind: "text"}
		s.enqueue(&provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: strconv.Itoa(idx)})
		return
	}

	toolUseID, _ := toolUse["toolUseId"].(string)
	name, _ := toolUse["name"].(string)
	isJSONResponseTool := s.usesJSONResponseTool && name == "json"
	normalizedID := normalizeToolCallID(toolUseID, s.isMistral)

	s.contentBlocks[idx] = &bedrockStreamContentBlock{
		Kind:               "tool-call",
		ToolCallID:         normalizedID,
		ToolName:           name,
		IsJSONResponseTool: isJSONResponseTool,
	}

	if !isJSONResponseTool {
		s.enqueue(&provider.StreamChunk{
			Type:     provider.ChunkTypeToolInputStart,
			ToolCall: &types.ToolCall{ID: normalizedID, ToolName: name},
		})
	}
}

func (s *bedrockConverseStream) handleContentBlockDelta(value map[string]interface{}) {
	cbd, _ := value["contentBlockDelta"].(map[string]interface{})
	if cbd == nil {
		return
	}
	idxF, _ := cbd["contentBlockIndex"].(float64)
	idx := int(idxF)
	delta, _ := cbd["delta"].(map[string]interface{})
	if delta == nil {
		return
	}

	// TS parses delta with a zod union: the first matching member wins and
	// strips every other key, so pick the member in union order before acting
	// (an empty text with a toolUse beside it is still the text member).
	if text, ok := delta["text"].(string); ok {
		if text == "" {
			return
		}
		if _, exists := s.contentBlocks[idx]; !exists {
			s.contentBlocks[idx] = &bedrockStreamContentBlock{Kind: "text"}
			s.enqueue(&provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: strconv.Itoa(idx)})
		}
		textDelta := text
		if s.extractor != nil {
			textDelta = s.extractor.process(text)
		}
		if len(textDelta) > 0 {
			s.enqueue(&provider.StreamChunk{Type: provider.ChunkTypeText, ID: strconv.Itoa(idx), Text: textDelta})
		}
		return
	}

	if _, ok := delta["citation"].(map[string]interface{}); ok {
		return
	}

	if toolUse, ok := delta["toolUse"].(map[string]interface{}); ok {
		if _, ok := toolUse["input"].(string); !ok {
			return
		}
		block, exists := s.contentBlocks[idx]
		if !exists || block.Kind != "tool-call" {
			return
		}
		inputDelta, _ := toolUse["input"].(string)
		if !block.IsJSONResponseTool {
			s.enqueue(&provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, ID: block.ToolCallID, Text: inputDelta})
		}
		block.JSONText += inputDelta
		return
	}

	if rc, ok := delta["reasoningContent"].(map[string]interface{}); ok {
		s.handleReasoningDelta(idx, rc)
		return
	}

	// Citation deltas and other future delta shapes are accepted without
	// erroring but currently produce no stream chunk.
}

func (s *bedrockConverseStream) handleReasoningDelta(idx int, rc map[string]interface{}) {
	ensureReasoningBlock := func() {
		if _, exists := s.contentBlocks[idx]; !exists {
			s.contentBlocks[idx] = &bedrockStreamContentBlock{Kind: "reasoning"}
			s.enqueue(&provider.StreamChunk{Type: provider.ChunkTypeReasoningStart, ID: strconv.Itoa(idx)})
		}
	}

	// Union order: the first reasoningContent member whose string field is
	// present wins, even when that field is empty.
	if text, ok := rc["text"].(string); ok {
		if text == "" {
			return
		}
		ensureReasoningBlock()
		s.enqueue(&provider.StreamChunk{Type: provider.ChunkTypeReasoning, ID: strconv.Itoa(idx), Reasoning: text})
		return
	}
	if signature, ok := rc["signature"].(string); ok {
		if signature == "" {
			return
		}
		ensureReasoningBlock()
		meta := map[string]interface{}{
			"amazonBedrock": map[string]interface{}{"signature": signature},
			"bedrock":       map[string]interface{}{"signature": signature},
		}
		data, _ := json.Marshal(meta)
		s.enqueue(&provider.StreamChunk{Type: provider.ChunkTypeReasoning, ID: strconv.Itoa(idx), Reasoning: "", ProviderMetadata: data})
		return
	}
	if redactedData, ok := rc["data"].(string); ok {
		if redactedData == "" {
			return
		}
		ensureReasoningBlock()
		meta := map[string]interface{}{
			"amazonBedrock": map[string]interface{}{"redactedData": redactedData},
			"bedrock":       map[string]interface{}{"redactedData": redactedData},
		}
		data, _ := json.Marshal(meta)
		s.enqueue(&provider.StreamChunk{Type: provider.ChunkTypeReasoning, ID: strconv.Itoa(idx), Reasoning: "", ProviderMetadata: data})
		return
	}
	if redactedContent, ok := rc["redactedContent"].(string); ok && redactedContent != "" {
		ensureReasoningBlock()
		block := s.contentBlocks[idx]
		if block.Kind == "reasoning" {
			block.RedactedContent += redactedContent
		}
	}
}

func (s *bedrockConverseStream) handleContentBlockStop(value map[string]interface{}) {
	cbs, _ := value["contentBlockStop"].(map[string]interface{})
	if cbs == nil {
		return
	}
	idxF, ok := cbs["contentBlockIndex"].(float64)
	if !ok {
		return
	}
	idx := int(idxF)
	block, exists := s.contentBlocks[idx]
	if !exists {
		return
	}
	delete(s.contentBlocks, idx)

	switch block.Kind {
	case "reasoning":
		chunk := &provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: strconv.Itoa(idx)}
		if block.RedactedContent != "" {
			meta := map[string]interface{}{
				"amazonBedrock": map[string]interface{}{"redactedContent": block.RedactedContent},
				"bedrock":       map[string]interface{}{"redactedContent": block.RedactedContent},
			}
			chunk.ProviderMetadata, _ = json.Marshal(meta)
		}
		s.enqueue(chunk)

	case "text":
		s.enqueue(&provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: strconv.Itoa(idx)})

	case "tool-call":
		if block.IsJSONResponseTool {
			s.isJSONResponseFromTool = true
			s.enqueue(&provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: strconv.Itoa(idx)})
			s.enqueue(&provider.StreamChunk{Type: provider.ChunkTypeText, ID: strconv.Itoa(idx), Text: block.JSONText})
			s.enqueue(&provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: strconv.Itoa(idx)})
			return
		}
		s.enqueue(&provider.StreamChunk{Type: provider.ChunkTypeToolInputEnd, ToolCall: &types.ToolCall{ID: block.ToolCallID}})
		input := block.JSONText
		if input == "" {
			input = "{}"
		}
		var args map[string]interface{}
		rawInvalid := ""
		if err := json.Unmarshal([]byte(input), &args); err != nil {
			// Truncated or malformed input: hand the raw text to the core so
			// the call is marked invalid (or repaired) instead of running
			// with silently emptied arguments (TS emits the input string).
			args = nil
			rawInvalid = input
		}
		if args == nil && rawInvalid == "" {
			args = map[string]interface{}{}
		}
		if rawInvalid == "" {
			// TS emits the accumulated input string as the tool call input.
			rawInvalid = input
		}
		s.enqueue(&provider.StreamChunk{
			Type: provider.ChunkTypeToolCall,
			ToolCall: &types.ToolCall{
				ID:           block.ToolCallID,
				ToolName:     block.ToolName,
				Arguments:    args,
				RawArguments: rawInvalid,
			},
		})
	}
}

func (s *bedrockConverseStream) flush() (*provider.StreamChunk, error) {
	if s.stopSequenceSet || s.isJSONResponseFromTool {
		s.ensureProviderMetadata()
		if s.isJSONResponseFromTool {
			s.providerMetadataPayload["isJsonResponseFromTool"] = true
		}
		if s.stopSequenceSet && s.stopSequence != nil {
			s.providerMetadataPayload["stopSequence"] = *s.stopSequence
		} else {
			s.providerMetadataPayload["stopSequence"] = nil
		}
	}

	usage := s.usage
	if !s.hasUsage {
		usage = convertBedrockConverseUsage(nil)
	}

	chunk := &provider.StreamChunk{
		Type:            provider.ChunkTypeFinish,
		FinishReason:    s.finishReason,
		RawFinishReason: s.rawFinishReason,
		Usage:           &usage,
	}
	if s.providerMetadataPayload != nil {
		data, _ := json.Marshal(map[string]interface{}{
			"amazonBedrock": s.providerMetadataPayload,
			"bedrock":       s.providerMetadataPayload,
		})
		chunk.ProviderMetadata = data
	}
	return chunk, nil
}

func (s *bedrockConverseStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

func (s *bedrockConverseStream) Close() error {
	s.done = true
	if s.body != nil {
		return s.body.Close()
	}
	return nil
}
