package bedrock

import (
	"encoding/json"
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

	startEmitted bool
	finishReason types.FinishReason
	usage        types.Usage
	hasUsage     bool

	providerMetadataPayload map[string]interface{}
	isJSONResponseFromTool  bool
	stopSequence            *string
	stopSequenceSet         bool

	done bool
	err  error
}

func (s *bedrockConverseStream) enqueue(chunk *provider.StreamChunk) {
	s.queue = append(s.queue, chunk)
}

func (s *bedrockConverseStream) Next() (*provider.StreamChunk, error) {
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
		return s.Next()
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
		return s.Next()
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		// Mirrors TS doStream's !chunk.success branch (amazon-bedrock-chat-
		// language-model.ts): report the error but keep reading — the stream
		// is not aborted — and mark finishReason as error in case the stream
		// ends (via decoder EOF) before any later messageStop overwrites it.
		s.finishReason = types.FinishReasonError
		return &provider.StreamChunk{Type: provider.ChunkTypeError, Text: fmt.Sprintf("failed to parse Amazon Bedrock stream event: %v", err)}, nil
	}
	delete(payload, "p") // AWS event-stream padding hint; irrelevant to consumers.

	// Wrap the raw payload under its event/exception type key, mirroring TS
	// amazon-bedrock-event-stream-response-handler.ts's
	// `{ [payloadType]: parsedDataResult.value }`, so downstream handling
	// matches the documented AmazonBedrockStreamSchema shape.
	value := map[string]interface{}{payloadType: payload}

	switch payloadType {
	case "internalServerException", "modelStreamErrorException", "serviceUnavailableException", "throttlingException", "validationException":
		s.finishReason = types.FinishReasonError
		// Do NOT set s.done here. TS's enqueueError (amazon-bedrock-chat-
		// language-model.ts) records the error and sets finishReason but lets
		// the ReadableStream's own flush() run when the underlying connection
		// closes, so a modeled exception is always followed by a terminal
		// 'finish' part carrying finishReason:'error' and whatever usage/
		// providerMetadata had accrued. Mirroring that: the next decoder.Next()
		// call naturally returns io.EOF once AWS closes the stream after the
		// exception frame, which drives s.done=true and s.flush() below.
		message := fmt.Sprintf("Amazon Bedrock stream failed with %s", payloadType)
		if m, ok := payload["message"].(string); ok && m != "" {
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
		chunkErr := providererrors.NewStreamProviderError(message, "amazon-bedrock", payloadType, nil, &statusCode, &isRetryable, payload)
		return &provider.StreamChunk{Type: provider.ChunkTypeError, Text: message, Err: chunkErr}, nil

	case "messageStop":
		if stopReason, ok := payload["stopReason"].(string); ok {
			s.finishReason = mapBedrockFinishReason(stopReason, s.isJSONResponseFromTool)
		}
		if amf, ok := payload["additionalModelResponseFields"].(map[string]interface{}); ok {
			if delta, ok := amf["delta"].(map[string]interface{}); ok {
				if ss, ok := delta["stop_sequence"].(string); ok {
					s.stopSequence = &ss
					s.stopSequenceSet = true
				}
			}
		}
		return s.Next()

	case "metadata":
		s.handleMetadata(payload)
		return s.Next()

	case "contentBlockStart":
		s.handleContentBlockStart(value)
		return s.Next()

	case "contentBlockDelta":
		s.handleContentBlockDelta(value)
		return s.Next()

	case "contentBlockStop":
		s.handleContentBlockStop(value)
		return s.Next()
	}

	return s.Next()
}

func (s *bedrockConverseStream) handleMetadata(metadata map[string]interface{}) {
	if usage, ok := metadata["usage"].(map[string]interface{}); ok {
		usageBytes, _ := json.Marshal(usage)
		s.usage = convertBedrockConverseUsage(usageBytes)
		s.hasUsage = true

		meta := map[string]interface{}{}
		if v, ok := usage["cacheWriteInputTokens"]; ok {
			meta["cacheWriteInputTokens"] = v
		}
		if v, ok := usage["cacheDetails"]; ok {
			meta["cacheDetails"] = v
		}
		if len(meta) > 0 {
			s.ensureProviderMetadata()
			s.providerMetadataPayload["usage"] = meta
		}
	}
	if trace, ok := metadata["trace"]; ok && trace != nil {
		s.ensureProviderMetadata()
		s.providerMetadataPayload["trace"] = trace
	}
	if pc, ok := metadata["performanceConfig"]; ok && pc != nil {
		s.ensureProviderMetadata()
		s.providerMetadataPayload["performanceConfig"] = pc
	}
	if st, ok := metadata["serviceTier"]; ok && st != nil {
		s.ensureProviderMetadata()
		s.providerMetadataPayload["serviceTier"] = st
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
		if _, exists := s.contentBlocks[idx]; !exists {
			s.contentBlocks[idx] = &bedrockStreamContentBlock{Kind: "text"}
			s.enqueue(&provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: strconv.Itoa(idx)})
		}
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

	if text, ok := delta["text"].(string); ok && text != "" {
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

	if toolUse, ok := delta["toolUse"].(map[string]interface{}); ok {
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

	if text, ok := rc["text"].(string); ok && text != "" {
		ensureReasoningBlock()
		s.enqueue(&provider.StreamChunk{Type: provider.ChunkTypeReasoning, ID: strconv.Itoa(idx), Reasoning: text})
		return
	}
	if signature, ok := rc["signature"].(string); ok && signature != "" {
		ensureReasoningBlock()
		meta := map[string]interface{}{
			"amazonBedrock": map[string]interface{}{"signature": signature},
			"bedrock":       map[string]interface{}{"signature": signature},
		}
		data, _ := json.Marshal(meta)
		s.enqueue(&provider.StreamChunk{Type: provider.ChunkTypeReasoning, ID: strconv.Itoa(idx), Reasoning: "", ProviderMetadata: data})
		return
	}
	if redactedData, ok := rc["data"].(string); ok && redactedData != "" {
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
		_ = json.Unmarshal([]byte(input), &args)
		if args == nil {
			args = map[string]interface{}{}
		}
		s.enqueue(&provider.StreamChunk{
			Type: provider.ChunkTypeToolCall,
			ToolCall: &types.ToolCall{
				ID:        block.ToolCallID,
				ToolName:  block.ToolName,
				Arguments: args,
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
		Type:         provider.ChunkTypeFinish,
		FinishReason: s.finishReason,
		Usage:        &usage,
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
