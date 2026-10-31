package workflow

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports of packages/workflow/src/to-ui-message-chunk.test.ts.

func runTransform(t *testing.T, parts []ModelCallStreamPart, opts ...ModelCallToUIChunkTransformOptions) []ai.UIMessageChunk {
	t.Helper()
	tr, err := CreateModelCallToUIChunkTransform(opts...)
	if err != nil {
		t.Fatal(err)
	}
	in := make(chan ModelCallStreamPart, len(parts))
	for _, p := range parts {
		in <- p
	}
	close(in)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var chunks []ai.UIMessageChunk
	for c := range tr.Pipe(ctx, in) {
		chunks = append(chunks, c)
	}
	return chunks
}

func uc(kv ...interface{}) ai.UIMessageChunk {
	c := ai.UIMessageChunk{}
	for i := 0; i < len(kv); i += 2 {
		c[kv[i].(string)] = kv[i+1]
	}
	return c
}

func textStart(id string) ModelCallStreamPart {
	return ModelCallStreamPart{Type: provider.ChunkTypeTextStart, ID: id}
}
func textDelta(id, s string) ModelCallStreamPart {
	return ModelCallStreamPart{Type: provider.ChunkTypeText, ID: id, Text: s}
}
func textEnd(id string) ModelCallStreamPart {
	return ModelCallStreamPart{Type: provider.ChunkTypeTextEnd, ID: id}
}

func rawToolTurn() []ModelCallStreamPart {
	args := map[string]interface{}{"city": "London"}
	return []ModelCallStreamPart{
		textStart("text-1"), textDelta("text-1", "Checking"), textEnd("text-1"),
		{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "weather"}},
		{Type: provider.ChunkTypeToolInputDelta, ID: "call-1", Text: `{"city":"London"}`},
		{Type: provider.ChunkTypeToolInputEnd, ID: "call-1"},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "weather", Arguments: args}},
		{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-1", ToolName: "weather", Input: args, Result: map[string]interface{}{"temperature": 18}}},
		textStart("text-2"), textDelta("text-2", "It is 18°C."), textEnd("text-2"),
	}
}

func TestModelCallToUIChunkTransformPreservesOutputWithoutOptions(t *testing.T) {
	got := runTransform(t, []ModelCallStreamPart{textStart("text-1"), textDelta("text-1", "Hello"), textEnd("text-1")})
	want := []ai.UIMessageChunk{
		uc("type", "start"), uc("type", "start-step"),
		uc("type", "text-start", "id", "text-1"),
		uc("type", "text-delta", "id", "text-1", "delta", "Hello"),
		uc("type", "text-end", "id", "text-1"),
		uc("type", "finish-step"), uc("type", "finish"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

func TestModelCallToUIChunkTransformResumesInUIChunkIndexSpace(t *testing.T) {
	canonical := runTransform(t, rawToolTurn())
	if len(canonical) != 14 {
		t.Fatalf("canonical length = %d, want 14", len(canonical))
	}
	var types4to7 []string
	for _, c := range canonical[4:7] {
		types4to7 = append(types4to7, c["type"].(string))
	}
	if !reflect.DeepEqual(types4to7, []string{"text-end", "tool-input-start", "tool-input-delta"}) {
		t.Fatalf("chunk types 4..7 = %v", types4to7)
	}
	got := runTransform(t, rawToolTurn(), ModelCallToUIChunkTransformOptions{UIStartIndex: 4})
	if !reflect.DeepEqual(got, canonical[4:]) {
		t.Fatalf("resumed = %v, want %v", got, canonical[4:])
	}
}

func TestModelCallToUIChunkTransformReconstructsCanonicalStreamAtEveryCursor(t *testing.T) {
	args := map[string]interface{}{}
	fixtures := map[string][]ModelCallStreamPart{
		"text":      {textStart("text-1"), textDelta("text-1", "Hello"), textEnd("text-1")},
		"tool call": rawToolTurn(),
		"tool error": {
			{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "weather"}},
			{Type: provider.ChunkTypeToolInputDelta, ID: "call-1", Text: "{}"},
			{Type: provider.ChunkTypeToolInputEnd, ID: "call-1"},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "weather", Arguments: args}},
			{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-1", ToolName: "weather", Input: args, Error: errors.New("Weather lookup failed")}},
		},
		"ignored raw parts": {
			textStart("text-1"),
			{Type: "model-call-start"},
			{Type: "model-call-response-metadata"},
			{Type: provider.ChunkTypeRaw, Raw: map[string]interface{}{"ignored": true}},
			textEnd("text-1"),
		},
		"step boundaries": {
			textStart("text-1"), textEnd("text-1"),
			{Type: provider.ChunkTypeFinishStep}, {Type: provider.ChunkTypeStartStep},
			textStart("text-2"), textEnd("text-2"),
		},
		"signed tool approval": {
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "weather", Arguments: map[string]interface{}{"city": "London"}}},
			{Type: provider.ChunkTypeToolApprovalRequest, ToolApprovalRequest: &types.ToolApprovalRequestContent{ApprovalID: "approval-call-1", ToolCallID: "call-1", Signature: "signed-approval"}},
		},
	}
	for name, parts := range fixtures {
		t.Run(name, func(t *testing.T) {
			canonical := runTransform(t, parts)
			for cursor := 0; cursor <= len(canonical); cursor++ {
				resumed := runTransform(t, parts, ModelCallToUIChunkTransformOptions{UIStartIndex: cursor})
				joined := append(append([]ai.UIMessageChunk{}, canonical[:cursor]...), resumed...)
				if !reflect.DeepEqual(joined, canonical) {
					t.Fatalf("cursor %d: got %v want %v", cursor, joined, canonical)
				}
			}
		})
	}
	// The tool-error fixture maps the failed result to tool-output-error.
	got := runTransform(t, fixtures["tool error"])
	last := got[len(got)-3]
	if !reflect.DeepEqual(last, uc("type", "tool-output-error", "toolCallId", "call-1", "errorText", "Weather lookup failed")) {
		t.Fatalf("tool error chunk = %v", last)
	}
}

func TestModelCallToUIChunkTransformOmitsFullStreamPastEnd(t *testing.T) {
	got := runTransform(t, rawToolTurn(), ModelCallToUIChunkTransformOptions{UIStartIndex: 100})
	if len(got) != 0 {
		t.Fatalf("got %v, want empty", got)
	}
}

func TestModelCallToUIChunkTransformRejectsInvalidUIStartIndex(t *testing.T) {
	_, err := CreateModelCallToUIChunkTransform(ModelCallToUIChunkTransformOptions{UIStartIndex: -1})
	if err == nil || err.Error() != "uiStartIndex must be a non-negative safe integer" {
		t.Fatalf("err = %v", err)
	}
}

func TestModelCallToUIChunkTransformStopsOnContextCancel(t *testing.T) {
	tr, _ := CreateModelCallToUIChunkTransform()
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan ModelCallStreamPart)
	out := tr.Pipe(ctx, in)
	<-out // start
	cancel()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case _, ok := <-out:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("pipe did not stop after cancel")
		}
	}
}

func TestWorkflowUIStreamResetStep(t *testing.T) {
	got, ok := ToUIMessageChunk(ModelCallStreamPart{Type: ChunkTypeResetStep})
	if !ok || !reflect.DeepEqual(got, uc("type", "reset-step")) {
		t.Fatalf("got %v %v", got, ok)
	}

	in := make(chan ai.UIMessageChunk, 5)
	for _, c := range []ai.UIMessageChunk{
		uc("type", "text-start", "id", "text-1"),
		uc("type", "reset-step"),
		uc("type", "text-start", "id", "text-1"),
		uc("type", "text-delta", "id", "text-1", "delta", "retry"),
		uc("type", "text-end", "id", "text-1"),
	} {
		in <- c
	}
	close(in)
	var chunks []ai.UIMessageChunk
	for c := range NormalizeUIMessageStreamParts(context.Background(), in) {
		chunks = append(chunks, c)
	}
	want := []ai.UIMessageChunk{
		uc("type", "text-start", "id", "text-1"),
		uc("type", "reset-step"),
		uc("type", "text-start", "id", "text-1"),
		uc("type", "text-delta", "id", "text-1", "delta", "retry"),
		uc("type", "text-end", "id", "text-1"),
	}
	if !reflect.DeepEqual(chunks, want) {
		t.Fatalf("got %v\nwant %v", chunks, want)
	}
}

func TestWorkflowSignedToolApprovals(t *testing.T) {
	got, ok := ToUIMessageChunk(ModelCallStreamPart{Type: provider.ChunkTypeToolApprovalRequest, ToolApprovalRequest: &types.ToolApprovalRequestContent{
		ApprovalID: "approval-call-1", ToolCallID: "call-1", Signature: "signed-approval",
	}})
	want := uc("type", "tool-approval-request", "approvalId", "approval-call-1", "toolCallId", "call-1", "signature", "signed-approval")
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}

	chunks := runTransform(t, []ModelCallStreamPart{{Type: provider.ChunkTypeToolApprovalRequest, ToolApprovalRequest: &types.ToolApprovalRequestContent{
		ApprovalID: "approval-call-1", ToolCallID: "call-1", InputSchemaInput: map[string]interface{}{"value": " requested "},
	}}})
	wantC := uc("type", "tool-approval-request", "approvalId", "approval-call-1", "toolCallId", "call-1",
		"inputSchemaInput", map[string]interface{}{"value": " requested "})
	found := false
	for _, c := range chunks {
		if reflect.DeepEqual(c, wantC) {
			found = true
		}
	}
	if !found {
		t.Fatalf("approval chunk with inputSchemaInput missing: %v", chunks)
	}
}

func TestToUIMessageChunkMapping(t *testing.T) {
	cases := []struct {
		name string
		part ModelCallStreamPart
		want ai.UIMessageChunk // nil: no chunk
	}{
		{"metadata on text", ModelCallStreamPart{Type: provider.ChunkTypeTextStart, ID: "a", ProviderMetadata: []byte(`{"p":{"k":1}}`)},
			uc("type", "text-start", "id", "a", "providerMetadata", map[string]interface{}{"p": map[string]interface{}{"k": float64(1)}})},
		{"reasoning delta", ModelCallStreamPart{Type: provider.ChunkTypeReasoning, ID: "r", Reasoning: "hm"}, uc("type", "reasoning-delta", "id", "r", "delta", "hm")},
		{"source url", ModelCallStreamPart{Type: provider.ChunkTypeSource, SourceContent: &types.SourceContent{SourceType: "url", ID: "s", URL: "https://x", Title: "T"}},
			uc("type", "source-url", "sourceId", "s", "url", "https://x", "title", "T")},
		{"source document", ModelCallStreamPart{Type: provider.ChunkTypeSource, SourceContent: &types.SourceContent{SourceType: "document", ID: "s", MediaType: "text/plain", Title: "T", Filename: "f.txt"}},
			uc("type", "source-document", "sourceId", "s", "mediaType", "text/plain", "title", "T", "filename", "f.txt")},
		{"file", ModelCallStreamPart{Type: provider.ChunkTypeFile, ProviderMetadata: []byte(`{"p":{}}`), GeneratedFileContent: &types.GeneratedFileContent{MediaType: "image/png", Data: []byte("hi")}},
			uc("type", "file", "mediaType", "image/png", "url", "data:image/png;base64,aGk=")},
		{"tool input start provider executed", ModelCallStreamPart{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: "c", ToolName: "t", ProviderExecuted: true}},
			uc("type", "tool-input-start", "toolCallId", "c", "toolName", "t", "providerExecuted", true)},
		{"invalid tool call", ModelCallStreamPart{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c", ToolName: "t", Invalid: true, Error: errors.New("bad input")}},
			uc("type", "tool-input-error", "toolCallId", "c", "toolName", "t", "input", map[string]interface{}(nil), "errorText", "bad input")},
		{"invalid tool call without error", ModelCallStreamPart{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c", ToolName: "t", Invalid: true}},
			uc("type", "tool-input-error", "toolCallId", "c", "toolName", "t", "input", map[string]interface{}(nil), "errorText", "Invalid tool call")},
		{"error is not masked", ModelCallStreamPart{Type: provider.ChunkTypeError, Err: errors.New("boom")}, uc("type", "error", "errorText", "boom")},
		{"tool output denied", ModelCallStreamPart{Type: provider.ChunkTypeToolOutputDenied, ToolResult: &types.ToolResult{ToolCallID: "c"}}, uc("type", "tool-output-denied", "toolCallId", "c")},
		{"tool input end ignored", ModelCallStreamPart{Type: provider.ChunkTypeToolInputEnd, ID: "c"}, nil},
		{"raw ignored", ModelCallStreamPart{Type: provider.ChunkTypeRaw}, nil},
		{"finish ignored", ModelCallStreamPart{Type: provider.ChunkTypeFinish}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ToUIMessageChunk(tc.part)
			if tc.want == nil {
				if ok {
					t.Fatalf("got %v, want none", got)
				}
				return
			}
			if !ok || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v\nwant %#v", got, tc.want)
			}
		})
	}
}

// TS forwards providerExecuted whenever it is not undefined, so an explicit
// false reaches the UI chunk; unset stays absent. inputEncoding is not
// forwarded by the TS workflow converter.
func TestToUIMessageChunkProviderExecutedExplicitFalse(t *testing.T) {
	cases := []struct {
		name string
		tc   types.ToolCall
		want interface{}
		has  bool
	}{
		{"unset", types.ToolCall{ID: "c1", ToolName: "t"}, nil, false},
		{"explicit false", types.ToolCall{ID: "c1", ToolName: "t", ProviderExecutedSet: true}, false, true},
		{"true", types.ToolCall{ID: "c1", ToolName: "t", ProviderExecuted: true}, true, true},
	}
	for _, tt := range cases {
		for _, ct := range []provider.ChunkType{provider.ChunkTypeToolInputStart, provider.ChunkTypeToolCall} {
			tc := tt.tc
			tc.InputEncoding = "text"
			c, ok := ToUIMessageChunk(provider.StreamChunk{Type: ct, ToolCall: &tc})
			if !ok {
				t.Fatalf("%s/%s: no chunk", tt.name, ct)
			}
			got, has := c["providerExecuted"]
			if has != tt.has || got != tt.want {
				t.Fatalf("%s/%s: providerExecuted = %#v (present %v), want %#v (present %v)", tt.name, ct, got, has, tt.want, tt.has)
			}
			if _, has := c["inputEncoding"]; has {
				t.Fatalf("%s/%s: inputEncoding forwarded: %#v", tt.name, ct, c)
			}
		}
	}
}

// TS workflow toUIMessageChunk builds tool-output-available and
// tool-output-error from toolCallId plus output/errorText only, so a
// provider-executed result (true or explicit false) carries no
// providerExecuted, providerMetadata or dynamic key, unlike the core
// converter.
func TestToUIMessageChunkToolOutputOmitsProviderExecuted(t *testing.T) {
	for _, tr := range []types.ToolResult{
		{ToolCallID: "c1", ToolName: "t", Result: "ok", ProviderExecuted: true, ProviderExecutedSet: true, Dynamic: true},
		{ToolCallID: "c1", ToolName: "t", Result: "ok", ProviderExecutedSet: true},
		{ToolCallID: "c1", ToolName: "t", Error: errors.New("boom"), ProviderExecuted: true, ProviderExecutedSet: true},
		{ToolCallID: "c1", ToolName: "t", Error: errors.New("boom"), ProviderExecutedSet: true},
	} {
		tr := tr
		c, ok := ToUIMessageChunk(provider.StreamChunk{
			Type:             provider.ChunkTypeToolResult,
			ToolResult:       &tr,
			ProviderMetadata: []byte(`{"p":{"k":1}}`),
		})
		if !ok {
			t.Fatalf("no chunk for %+v", tr)
		}
		want := ai.UIMessageChunk{"type": "tool-output-available", "toolCallId": "c1", "output": "ok"}
		if tr.Error != nil {
			want = ai.UIMessageChunk{"type": "tool-output-error", "toolCallId": "c1", "errorText": "boom"}
		}
		if !reflect.DeepEqual(c, want) {
			t.Fatalf("chunk = %#v, want %#v", c, want)
		}
	}
}
