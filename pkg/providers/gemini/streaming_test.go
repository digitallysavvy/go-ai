package gemini

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// sseStream builds a minimal SSE response body from JSON payloads.
func sseStream(payloads ...string) io.ReadCloser {
	var sb strings.Builder
	for _, p := range payloads {
		sb.WriteString("data: ")
		sb.WriteString(p)
		sb.WriteString("\n\n")
	}
	return io.NopCloser(strings.NewReader(sb.String()))
}

// newTestStream creates a stream with a default test Config (Google semantics,
// code execution enabled) for use in white-box tests.
func newTestStream(reader io.ReadCloser) *stream {
	return newStream(reader, Config{
		MetadataKey: "google",
	}, nil, nil, "")
}

// stripResponseMetadata asserts the stream's first chunk is the single
// response-metadata chunk (emitted once, from the first SSE event; see
// processSSEEvent) and returns the remaining chunks.
func stripResponseMetadata(t *testing.T, chunks []*provider.StreamChunk) []*provider.StreamChunk {
	t.Helper()
	if len(chunks) == 0 || chunks[0].Type != provider.ChunkTypeResponseMetadata {
		t.Fatalf("expected a leading response-metadata chunk, got: %v", chunkTypes(chunks))
	}
	return chunks[1:]
}

// chunkTypes returns chunk type strings for diagnostic output.
func chunkTypes(chunks []*provider.StreamChunk) []string {
	out := make([]string, len(chunks))
	for i, c := range chunks {
		out[i] = string(c.Type)
	}
	return out
}

func mustMarshal(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// --- streaming tests ---------------------------------------------------------

// TestStream_MalformedChunkEmitsErrorChunkAndContinues is a P1-1c part 2
// regression test, porting TS google-language-model.ts's
// `if (!chunk.success) { controller.enqueue({type:'error', error:
// chunk.error}); return; }`: a malformed SSE payload must surface as a
// ChunkTypeError chunk (with a structured Err) and the stream must continue
// reading subsequent events, not terminate. Previously Go returned this as
// a terminal Next() error, ending the stream outright.
func TestStream_MalformedChunkEmitsErrorChunkAndContinues(t *testing.T) {
	s := newTestStream(sseStream(
		`{not valid json`,
		mustMarshal(Response{
			Candidates: []Candidate{{
				Content: struct {
					Parts []Part `json:"parts"`
					Role  string `json:"role"`
				}{Parts: []Part{{Text: "hello"}}},
			}},
		}),
	))
	defer s.Close() //nolint:errcheck

	chunk, err := s.Next()
	if err != nil {
		t.Fatalf("Next() error = %v, want a ChunkTypeError chunk with nil error (stream continues)", err)
	}
	if chunk.Type != provider.ChunkTypeError {
		t.Fatalf("chunk.Type = %v, want ChunkTypeError", chunk.Type)
	}
	if chunk.Err == nil {
		t.Fatal("chunk.Err is nil, want a structured *providererrors.StreamProviderError")
	}

	// The stream must continue: the next real chunk should follow, not EOF.
	found := false
	for i := 0; i < 5; i++ {
		chunk, err = s.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next() error after malformed chunk: %v", err)
		}
		if chunk.Type == provider.ChunkTypeText && chunk.Text == "hello" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected the stream to continue past the malformed chunk and emit the following text")
	}
}

func TestStream_ThoughtPartsEmitReasoning(t *testing.T) {
	// Two SSE events: thought part, then text part + STOP.
	// Expected: reasoning-start, reasoning-delta, reasoning-end,
	//           text-start, text-delta, text-end, finish(stop).
	thoughtEvent := mustMarshal(Response{
		Candidates: []Candidate{{
			Content: struct {
				Parts []Part `json:"parts"`
				Role  string `json:"role"`
			}{Parts: []Part{{Text: "I am reasoning", Thought: true}}},
		}},
	})
	textEvent := mustMarshal(Response{
		Candidates: []Candidate{{
			Content: struct {
				Parts []Part `json:"parts"`
				Role  string `json:"role"`
			}{Parts: []Part{{Text: "The answer is 7"}}},
			FinishReason: "STOP",
		}},
	})

	s := newTestStream(sseStream(thoughtEvent, textEvent))
	defer s.Close() //nolint:errcheck //nolint:errcheck

	var chunks []*provider.StreamChunk
	for {
		c, err := s.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		chunks = append(chunks, c)
	}
	chunks = stripResponseMetadata(t, chunks)

	if len(chunks) != 7 {
		t.Fatalf("expected 7 chunks, got %d: %v", len(chunks), chunkTypes(chunks))
	}
	if chunks[0].Type != provider.ChunkTypeReasoningStart {
		t.Errorf("chunks[0]: got %v, want reasoning-start", chunks[0].Type)
	}
	if chunks[1].Type != provider.ChunkTypeReasoning || chunks[1].Reasoning != "I am reasoning" {
		t.Errorf("chunks[1]: got type=%v reasoning=%q, want reasoning 'I am reasoning'", chunks[1].Type, chunks[1].Reasoning)
	}
	if chunks[2].Type != provider.ChunkTypeReasoningEnd {
		t.Errorf("chunks[2]: got %v, want reasoning-end", chunks[2].Type)
	}
	if chunks[3].Type != provider.ChunkTypeTextStart {
		t.Errorf("chunks[3]: got %v, want text-start", chunks[3].Type)
	}
	if chunks[4].Type != provider.ChunkTypeText || chunks[4].Text != "The answer is 7" {
		t.Errorf("chunks[4]: got type=%v text=%q, want text 'The answer is 7'", chunks[4].Type, chunks[4].Text)
	}
	if chunks[5].Type != provider.ChunkTypeTextEnd {
		t.Errorf("chunks[5]: got %v, want text-end", chunks[5].Type)
	}
	if chunks[6].Type != provider.ChunkTypeFinish || chunks[6].FinishReason != types.FinishReasonStop {
		t.Errorf("chunks[6]: got type=%v reason=%v, want finish stop", chunks[6].Type, chunks[6].FinishReason)
	}
}

func TestStream_MultiplePartsInSingleEvent(t *testing.T) {
	// One SSE event: thought + text + STOP.
	// Expected: reasoning-start, reasoning-delta, reasoning-end,
	//           text-start, text-delta, text-end, finish(stop).
	event := mustMarshal(Response{
		Candidates: []Candidate{{
			Content: struct {
				Parts []Part `json:"parts"`
				Role  string `json:"role"`
			}{Parts: []Part{
				{Text: "thinking", Thought: true},
				{Text: "answer"},
			}},
			FinishReason: "STOP",
		}},
	})

	s := newTestStream(sseStream(event))
	defer s.Close() //nolint:errcheck

	var chunks []*provider.StreamChunk
	for {
		c, err := s.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		chunks = append(chunks, c)
	}
	chunks = stripResponseMetadata(t, chunks)

	if len(chunks) != 7 {
		t.Fatalf("expected 7 chunks, got %d: %v", len(chunks), chunkTypes(chunks))
	}
	if chunks[0].Type != provider.ChunkTypeReasoningStart {
		t.Errorf("chunks[0]: got %v, want reasoning-start", chunks[0].Type)
	}
	if chunks[1].Type != provider.ChunkTypeReasoning || chunks[1].Reasoning != "thinking" {
		t.Errorf("chunks[1]: got type=%v reasoning=%q, want reasoning 'thinking'", chunks[1].Type, chunks[1].Reasoning)
	}
	if chunks[2].Type != provider.ChunkTypeReasoningEnd {
		t.Errorf("chunks[2]: got %v, want reasoning-end", chunks[2].Type)
	}
	if chunks[3].Type != provider.ChunkTypeTextStart {
		t.Errorf("chunks[3]: got %v, want text-start", chunks[3].Type)
	}
	if chunks[4].Type != provider.ChunkTypeText || chunks[4].Text != "answer" {
		t.Errorf("chunks[4]: got type=%v text=%q, want text 'answer'", chunks[4].Type, chunks[4].Text)
	}
	if chunks[5].Type != provider.ChunkTypeTextEnd {
		t.Errorf("chunks[5]: got %v, want text-end", chunks[5].Type)
	}
	if chunks[6].Type != provider.ChunkTypeFinish || chunks[6].FinishReason != types.FinishReasonStop {
		t.Errorf("chunks[6]: got type=%v reason=%v, want finish stop", chunks[6].Type, chunks[6].FinishReason)
	}
}

func TestStream_FinishReasonEmittedAfterText(t *testing.T) {
	// One SSE event: text + MAX_TOKENS.
	// Expected: text-start, text-delta, text-end, finish(length).
	event := mustMarshal(Response{
		Candidates: []Candidate{{
			Content: struct {
				Parts []Part `json:"parts"`
				Role  string `json:"role"`
			}{Parts: []Part{{Text: "last word"}}},
			FinishReason: "MAX_TOKENS",
		}},
	})

	s := newTestStream(sseStream(event))
	defer s.Close() //nolint:errcheck //nolint:errcheck

	var chunks []*provider.StreamChunk
	for {
		c, err := s.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		chunks = append(chunks, c)
	}
	chunks = stripResponseMetadata(t, chunks)

	if len(chunks) != 4 {
		t.Fatalf("expected 4 chunks, got %d: %v", len(chunks), chunkTypes(chunks))
	}
	if chunks[0].Type != provider.ChunkTypeTextStart {
		t.Errorf("chunks[0]: got %v, want text-start", chunks[0].Type)
	}
	if chunks[1].Type != provider.ChunkTypeText || chunks[1].Text != "last word" {
		t.Errorf("chunks[1]: got type=%v text=%q, want text 'last word'", chunks[1].Type, chunks[1].Text)
	}
	if chunks[2].Type != provider.ChunkTypeTextEnd {
		t.Errorf("chunks[2]: got %v, want text-end", chunks[2].Type)
	}
	if chunks[3].Type != provider.ChunkTypeFinish || chunks[3].FinishReason != types.FinishReasonLength {
		t.Errorf("chunks[3]: got type=%v reason=%v, want finish length", chunks[3].Type, chunks[3].FinishReason)
	}
}

func TestStream_CodeExecution(t *testing.T) {
	// One SSE event: executableCode part + codeExecutionResult part.
	codeEvent := mustMarshal(Response{
		Candidates: []Candidate{{
			Content: struct {
				Parts []Part `json:"parts"`
				Role  string `json:"role"`
			}{Parts: []Part{
				{ExecutableCode: &struct {
					Language string `json:"language"`
					Code     string `json:"code"`
				}{Language: "PYTHON", Code: "print('hi')"}},
				{CodeExecutionResult: &struct {
					Outcome string `json:"outcome"`
					Output  string `json:"output"`
				}{Outcome: "OK", Output: "hi\n"}},
			}},
			FinishReason: "STOP",
		}},
	})

	s := newTestStream(sseStream(codeEvent))
	defer s.Close() //nolint:errcheck

	var chunks []*provider.StreamChunk
	for {
		c, err := s.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		chunks = append(chunks, c)
	}
	chunks = stripResponseMetadata(t, chunks)

	// Expect: tool-call (code exec), tool-result, finish.
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d: %v", len(chunks), chunkTypes(chunks))
	}
	if chunks[0].Type != provider.ChunkTypeToolCall {
		t.Errorf("chunks[0]: got %v, want tool-call", chunks[0].Type)
	}
	if chunks[0].ToolCall == nil || !chunks[0].ToolCall.ProviderExecuted {
		t.Errorf("chunks[0]: expected ProviderExecuted tool call")
	}
	if chunks[1].Type != provider.ChunkTypeToolResult {
		t.Errorf("chunks[1]: got %v, want tool-result", chunks[1].Type)
	}
	if chunks[2].Type != provider.ChunkTypeFinish {
		t.Errorf("chunks[2]: got %v, want finish", chunks[2].Type)
	}
}

func TestStream_GroundingMetadataAccumulatedOnFinish(t *testing.T) {
	// First chunk carries groundingMetadata but no finishReason.
	// Second chunk carries finishReason. Metadata from the first must appear on finish.
	groundingMeta := `{"groundingSupport":[{"segment":{"startIndex":0}}]}`

	earlyChunk := mustMarshal(Response{
		Candidates: []Candidate{{
			Content: struct {
				Parts []Part `json:"parts"`
				Role  string `json:"role"`
			}{Parts: []Part{{Text: "some text"}}},
			GroundingMetadata: json.RawMessage(groundingMeta),
		}},
	})
	finishChunk := mustMarshal(Response{
		Candidates: []Candidate{{
			FinishReason: "STOP",
		}},
	})

	s := newTestStream(sseStream(earlyChunk, finishChunk))
	defer s.Close() //nolint:errcheck

	var chunks []*provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		chunks = append(chunks, c)
	}

	var finish *provider.StreamChunk
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
	}
	if finish == nil {
		t.Fatal("expected a ChunkTypeFinish chunk")
	}
	if finish.ProviderMetadata == nil {
		t.Fatal("finish chunk must carry ProviderMetadata with grounding info")
	}
	var meta map[string]interface{}
	if err := json.Unmarshal(finish.ProviderMetadata, &meta); err != nil {
		t.Fatalf("ProviderMetadata unmarshal: %v", err)
	}
	googleMeta, ok := meta["google"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'google' key, got %v", meta)
	}
	if _, has := googleMeta["groundingMetadata"]; !has {
		t.Error("finish chunk must include groundingMetadata in ProviderMetadata")
	}
}

func TestStream_ToolInputDeltaCarriesID(t *testing.T) {
	// Verify that tool-input-delta carries the same ID as tool-input-start/end,
	// matching the TS SDK's `id: toolCall.toolCallId` on tool-input-delta.
	event := mustMarshal(Response{
		Candidates: []Candidate{{
			Content: struct {
				Parts []Part `json:"parts"`
				Role  string `json:"role"`
			}{Parts: []Part{
				{FunctionCall: &FunctionCall{Name: "my_tool", Args: map[string]interface{}{"key": "val"}, ArgsSet: true}},
			}},
			FinishReason: "STOP",
		}},
	})

	s := newTestStream(sseStream(event))
	defer s.Close() //nolint:errcheck

	var chunks []*provider.StreamChunk
	for {
		c, err := s.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		chunks = append(chunks, c)
	}
	chunks = stripResponseMetadata(t, chunks)

	// Expected: tool-input-start, tool-input-delta, tool-input-end, tool-call, finish
	if len(chunks) != 5 {
		t.Fatalf("expected 5 chunks, got %d: %v", len(chunks), chunkTypes(chunks))
	}
	startID := chunks[0].ToolCall.ID
	if startID == "" {
		t.Fatal("tool-input-start must have a non-empty ID")
	}
	if chunks[1].Type != provider.ChunkTypeToolInputDelta {
		t.Errorf("chunks[1]: got %v, want tool-input-delta", chunks[1].Type)
	}
	if chunks[1].ID != startID {
		t.Errorf("tool-input-delta ID = %q, want %q (same as tool-input-start)", chunks[1].ID, startID)
	}
	if chunks[2].ToolCall.ID != startID {
		t.Errorf("tool-input-end ID = %q, want %q", chunks[2].ToolCall.ID, startID)
	}
}

func TestStream_FunctionCallChunkCarriesThoughtSignature(t *testing.T) {
	chunkJSON := `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"tool","args":{"x":1}},"thoughtSignature":"stream-sig"}]}}]}`
	s := newTestStream(sseStream(chunkJSON, "[DONE]"))

	var toolCallChunk *provider.StreamChunk
	for {
		chunk, err := s.Next()
		if err != nil {
			break
		}
		if chunk.Type == provider.ChunkTypeToolCall {
			toolCallChunk = chunk
			break
		}
	}

	if toolCallChunk == nil {
		t.Fatal("expected a ChunkTypeToolCall chunk")
	}
	if toolCallChunk.ToolCall == nil {
		t.Fatal("ToolCall must be set on ChunkTypeToolCall chunk")
	}
	if toolCallChunk.ToolCall.ThoughtSignature != "stream-sig" {
		t.Errorf("ThoughtSignature = %q, want %q", toolCallChunk.ToolCall.ThoughtSignature, "stream-sig")
	}
}

func TestStream_FunctionCallWithoutArgsEmitsEmptyObject(t *testing.T) {
	chunkJSON := `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"tool"}}]},"finishReason":"STOP"}]}`
	s := newTestStream(sseStream(chunkJSON, "[DONE]"))

	var toolCallChunk *provider.StreamChunk
	for {
		chunk, err := s.Next()
		if err != nil {
			break
		}
		if chunk.Type == provider.ChunkTypeToolCall {
			toolCallChunk = chunk
			break
		}
	}

	if toolCallChunk == nil {
		t.Fatal("expected a ChunkTypeToolCall chunk")
	}
	if toolCallChunk.ToolCall.Arguments == nil {
		t.Fatal("ToolCall.Arguments must be an empty object, not nil")
	}
	if len(toolCallChunk.ToolCall.Arguments) != 0 {
		t.Errorf("ToolCall.Arguments = %#v, want empty object", toolCallChunk.ToolCall.Arguments)
	}
}

func TestStream_FunctionCallArgumentsAreAccumulatedAcrossChunks(t *testing.T) {
	chunk1 := mustMarshal(Response{
		Candidates: []Candidate{{
			Content: struct {
				Parts []Part `json:"parts"`
				Role  string `json:"role"`
			}{Parts: []Part{
				{FunctionCall: &FunctionCall{Name: "tool", Args: map[string]interface{}{"a": "hel"}, ArgsSet: true}},
			}},
		}},
	})
	chunk2 := mustMarshal(Response{
		Candidates: []Candidate{{
			Content: struct {
				Parts []Part `json:"parts"`
				Role  string `json:"role"`
			}{Parts: []Part{
				{FunctionCall: &FunctionCall{Name: "tool", Args: map[string]interface{}{"a": "hello"}, ArgsSet: true}},
			}},
			FinishReason: "STOP",
		}},
	})
	s := newTestStream(sseStream(chunk1, chunk2))

	var deltas []string
	var final *provider.StreamChunk
	for {
		chunk, err := s.Next()
		if err != nil {
			break
		}
		if chunk.Type == provider.ChunkTypeToolInputDelta {
			deltas = append(deltas, chunk.Text)
		}
		if chunk.Type == provider.ChunkTypeToolCall {
			final = chunk
		}
	}

	if len(deltas) == 0 {
		t.Fatal("expected at least one tool input delta")
	}
	if final == nil || final.ToolCall == nil {
		t.Fatal("expected final tool call chunk")
	}
	got, _ := final.ToolCall.Arguments["a"].(string)
	if got != "hello" {
		t.Fatalf("final tool arg a = %q, want %q", got, "hello")
	}
}

func TestStream_ReasoningChunkCarriesThoughtSignature(t *testing.T) {
	chunkJSON := `{"candidates":[{"content":{"parts":[{"text":"reasoning...","thought":true,"thoughtSignature":"rsig-xyz"}]}}]}`
	s := newTestStream(sseStream(chunkJSON, "[DONE]"))

	var reasoningChunk *provider.StreamChunk
	for {
		chunk, err := s.Next()
		if err != nil {
			break
		}
		if chunk.Type == provider.ChunkTypeReasoning {
			reasoningChunk = chunk
			break
		}
	}

	if reasoningChunk == nil {
		t.Fatal("expected a ChunkTypeReasoning chunk")
	}
	if len(reasoningChunk.ProviderMetadata) == 0 {
		t.Fatal("ProviderMetadata must be set on reasoning chunk with ThoughtSignature")
	}
	var meta map[string]interface{}
	if err := json.Unmarshal(reasoningChunk.ProviderMetadata, &meta); err != nil {
		t.Fatalf("failed to unmarshal ProviderMetadata: %v", err)
	}
	googleMeta, ok := meta["google"].(map[string]interface{})
	if !ok {
		t.Fatalf("meta[google] type = %T", meta["google"])
	}
	if googleMeta["thoughtSignature"] != "rsig-xyz" {
		t.Errorf("thoughtSignature = %v, want rsig-xyz", googleMeta["thoughtSignature"])
	}
}

func TestStream_MetadataKeyAppearsInFinishChunk(t *testing.T) {
	// Verify that the MetadataKey is used correctly in the finish chunk ProviderMetadata.
	// Test with "vertex" key to confirm both providers work.
	event := mustMarshal(Response{
		Candidates: []Candidate{{
			Content: struct {
				Parts []Part `json:"parts"`
				Role  string `json:"role"`
			}{Parts: []Part{{Text: "hi"}}},
			FinishReason:  "STOP",
			SafetyRatings: json.RawMessage(`[{"category":"HARM_CATEGORY_HATE_SPEECH"}]`),
		}},
	})

	// Test with vertex key.
	sv := newStream(sseStream(event), Config{MetadataKey: "vertex"}, nil, nil, "")
	defer sv.Close() //nolint:errcheck
	var vertexChunks []*provider.StreamChunk
	for {
		c, err := sv.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		vertexChunks = append(vertexChunks, c)
	}
	finish := vertexChunks[len(vertexChunks)-1]
	if finish.Type != provider.ChunkTypeFinish {
		t.Fatalf("last chunk: got %v, want finish", finish.Type)
	}
	var meta map[string]interface{}
	if err := json.Unmarshal(finish.ProviderMetadata, &meta); err != nil {
		t.Fatalf("ProviderMetadata unmarshal: %v", err)
	}
	if _, ok := meta["vertex"]; !ok {
		t.Errorf("expected 'vertex' key in ProviderMetadata, got keys: %v", meta)
	}
	if _, ok := meta["google"]; ok {
		t.Errorf("unexpected 'google' key in vertex stream ProviderMetadata")
	}
}

// drainChunks reads all chunks from the stream until EOF or error.
func drainChunks(t *testing.T, s *stream) []*provider.StreamChunk {
	t.Helper()
	var chunks []*provider.StreamChunk
	for {
		c, err := s.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		chunks = append(chunks, c)
	}
	return chunks
}

// --- serviceTier streaming tests ---

func TestStreamServiceTierAccumulated(t *testing.T) {
	// First chunk sets serviceTier; second chunk has text + finish.
	chunk1 := `{"serviceTier":"SERVICE_TIER_STANDARD","candidates":[{"content":{"parts":[{"text":"Hi"}]}}]}`
	chunk2 := `{"candidates":[{"content":{"parts":[{"text":"!"}]},"finishReason":"STOP"}]}`

	s := newTestStream(sseStream(chunk1, chunk2))
	chunks := drainChunks(t, s)

	var finishChunk *provider.StreamChunk
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeFinish {
			finishChunk = c
		}
	}
	if finishChunk == nil {
		t.Fatal("no finish chunk found")
	}
	if finishChunk.ProviderMetadata == nil {
		t.Fatal("finish chunk has no ProviderMetadata")
	}
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(finishChunk.ProviderMetadata, &meta); err != nil {
		t.Fatalf("unmarshal outer meta: %v", err)
	}
	googleRaw, ok := meta["google"]
	if !ok {
		t.Fatal("no 'google' key in finish chunk ProviderMetadata")
	}
	var googleMeta map[string]json.RawMessage
	if err := json.Unmarshal(googleRaw, &googleMeta); err != nil {
		t.Fatalf("unmarshal google meta: %v", err)
	}
	var serviceTier string
	if err := json.Unmarshal(googleMeta["serviceTier"], &serviceTier); err != nil {
		t.Fatalf("unmarshal serviceTier: %v", err)
	}
	if serviceTier != "SERVICE_TIER_STANDARD" {
		t.Errorf("serviceTier = %q, want %q", serviceTier, "SERVICE_TIER_STANDARD")
	}
}

func TestStreamServiceTierNullWhenAbsent(t *testing.T) {
	// When no chunk carries serviceTier, metadata should still have serviceTier: null (TS parity).
	chunk1 := `{"candidates":[{"content":{"parts":[{"text":"Hi"}]},"finishReason":"STOP"}]}`

	s := newTestStream(sseStream(chunk1))
	chunks := drainChunks(t, s)

	var finishChunk *provider.StreamChunk
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeFinish {
			finishChunk = c
		}
	}
	if finishChunk == nil || finishChunk.ProviderMetadata == nil {
		t.Fatal("no finish chunk or no metadata")
	}
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(finishChunk.ProviderMetadata, &meta); err != nil {
		t.Fatalf("unmarshal outer meta: %v", err)
	}
	googleRaw, ok := meta["google"]
	if !ok {
		t.Fatal("no 'google' key in metadata")
	}
	var googleMeta map[string]json.RawMessage
	if err := json.Unmarshal(googleRaw, &googleMeta); err != nil {
		t.Fatalf("unmarshal google meta: %v", err)
	}
	raw, has := googleMeta["serviceTier"]
	if !has {
		t.Fatal("serviceTier should always be present (as null when absent from chunks)")
	}
	if string(raw) != "null" {
		t.Errorf("serviceTier = %s, want null", raw)
	}
}

func TestStreamServiceTierLastValueWins(t *testing.T) {
	// When multiple chunks set serviceTier, the last non-empty value wins.
	chunk1 := `{"serviceTier":"SERVICE_TIER_STANDARD","candidates":[{"content":{"parts":[{"text":"Hi"}]}}]}`
	chunk2 := `{"serviceTier":"SERVICE_TIER_PRIORITY","candidates":[{"content":{"parts":[{"text":"!"}]},"finishReason":"STOP"}]}`

	s := newTestStream(sseStream(chunk1, chunk2))
	chunks := drainChunks(t, s)

	var finishChunk *provider.StreamChunk
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeFinish {
			finishChunk = c
		}
	}
	if finishChunk == nil || finishChunk.ProviderMetadata == nil {
		t.Fatal("no finish chunk or no metadata")
	}
	var meta map[string]json.RawMessage
	json.Unmarshal(finishChunk.ProviderMetadata, &meta)
	var googleMeta map[string]json.RawMessage
	json.Unmarshal(meta["google"], &googleMeta)
	var serviceTier string
	json.Unmarshal(googleMeta["serviceTier"], &serviceTier)
	if serviceTier != "SERVICE_TIER_PRIORITY" {
		t.Errorf("serviceTier = %q, want last value %q", serviceTier, "SERVICE_TIER_PRIORITY")
	}
}

// TestStreamNext_NoStackGrowthOnLongChunklessRun is a regression test for
// BF5 (bug-review R6-1, Critical): stream.Next() used to recurse via
// `return s.Next()` whenever an SSE event produced no bufferable chunk --
// e.g. a usageMetadata-only keep-alive with no candidates, which real
// Gemini responses send during long tool-call/thinking loops. Go does not
// eliminate that tail call, so a long run of such events within one
// external Next() call grew the goroutine stack without bound, eventually
// crashing the process with an unrecoverable `fatal error: stack
// overflow` -- not a panic, not recover()-able.
//
// This feeds 300,000 usageMetadata-only keep-alive events (no candidates),
// then one real event with text content, then EOF, in a subprocess with
// debug.SetMaxStack lowered so a regression would crash deterministically
// well within the test's 2s budget instead of needing gigabytes of real
// stack.
func TestStreamNext_NoStackGrowthOnLongChunklessRun(t *testing.T) {
	if os.Getenv("GOAI_BF5_RECURSION_CHILD") == "1" {
		runGeminiStreamRecursionChild()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestStreamNext_NoStackGrowthOnLongChunklessRun", "-test.v")
	cmd.Env = append(os.Environ(), "GOAI_BF5_RECURSION_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("BUG: child process crashed (stack overflow?) after a long run of chunkless SSE events: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(string(out), "BF5_CHILD_OK") {
		t.Fatalf("child process did not report success; output:\n%s", out)
	}
}

func runGeminiStreamRecursionChild() {
	debug.SetMaxStack(8 << 20) // 8 MiB, so a regression crashes quickly and deterministically.
	const n = 300_000
	var sb strings.Builder
	sb.Grow(n*48 + 256)
	for i := 0; i < n; i++ {
		sb.WriteString(`data: {"usageMetadata":{"promptTokenCount":1}}` + "\n\n")
	}
	sb.WriteString(`data: {"candidates":[{"content":{"parts":[{"text":"hello"}]},"finishReason":"STOP"}]}` + "\n\n")

	s := newTestStream(io.NopCloser(strings.NewReader(sb.String())))

	_, err := s.Next() // response-metadata chunk (emitted on the first SSE event)
	if err != nil {
		fmt.Println("BF5_CHILD_FAIL: unexpected error on first Next():", err)
		return
	}
	// This call used to recurse once per chunkless keep-alive event -- 300,000
	// deep -- before reaching the real text-start chunk.
	chunk, err := s.Next()
	if err != nil {
		fmt.Println("BF5_CHILD_FAIL: unexpected error on second Next():", err)
		return
	}
	if chunk == nil || chunk.Type != provider.ChunkTypeTextStart {
		fmt.Printf("BF5_CHILD_FAIL: unexpected chunk: %+v\n", chunk)
		return
	}
	fmt.Println("BF5_CHILD_OK")
}
