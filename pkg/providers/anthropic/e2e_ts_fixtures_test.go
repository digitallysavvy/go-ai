package anthropic

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
)

// End-to-end scenarios driven by the TS SDK's recorded wire fixtures
// (packages/anthropic/src/__fixtures__, copied to testdata/ts). Each runs the
// whole provider path through an httptest server and asserts what TS asserts
// (its vitest snapshots) about the resulting parts.

func tsFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "ts", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func fixtureModel(t *testing.T, body func(w http.ResponseWriter, r *http.Request)) (*LanguageModel, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(body))
	p := New(Config{APIKey: "test-key", BaseURL: srv.URL})
	return NewLanguageModel(p, "claude-sonnet-4-5-20250929", nil), srv.Close
}

func serveJSONFixture(name string, t *testing.T) func(http.ResponseWriter, *http.Request) {
	data := tsFixture(t, name+".json")
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}
}

// serveChunksFixture mirrors the TS test helper: every line becomes an SSE
// data event and a [DONE] terminator follows.
func serveChunksFixture(name string, t *testing.T) func(http.ResponseWriter, *http.Request) {
	data := tsFixture(t, name+".chunks.txt")
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, l := range strings.Split(string(data), "\n") {
			fmt.Fprintf(w, "data: %s\n\n", l)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}
}

func drain(t *testing.T, s provider.TextStream) []*provider.StreamChunk {
	t.Helper()
	defer s.Close() //nolint:errcheck
	var out []*provider.StreamChunk
	for {
		c, err := s.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
		out = append(out, c)
	}
}

func chunkSeq(chunks []*provider.StreamChunk) []string {
	var seq []string
	for _, c := range chunks {
		name := string(c.Type)
		switch c.Type {
		case provider.ChunkTypeToolCall, provider.ChunkTypeToolInputStart, provider.ChunkTypeToolInputEnd:
			if c.ToolCall != nil {
				name += ":" + c.ToolCall.ToolName
			}
		case provider.ChunkTypeToolResult:
			if c.ToolResult != nil {
				name += ":" + c.ToolResult.ToolName
			}
		}
		if n := len(seq); n > 0 && seq[n-1] == name && (c.Type == provider.ChunkTypeText || c.Type == provider.ChunkTypeToolInputDelta || c.Type == provider.ChunkTypeSource || c.Type == provider.ChunkTypeReasoning) {
			continue
		}
		seq = append(seq, name)
	}
	return seq
}

func TestFixtureTextGenerateAndStream(t *testing.T) {
	model, closeFn := fixtureModel(t, serveJSONFixture("anthropic-text", t))
	res, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	closeFn()
	if err != nil {
		t.Fatal(err)
	}
	if res.FinishReason != types.FinishReasonStop || res.RawFinishReason != "end_turn" || res.Text == "" {
		t.Fatalf("generate: %+v", res)
	}
	if res.ResponseMetadata == nil || res.ResponseMetadata.ID == "" {
		t.Fatalf("response metadata = %+v", res.ResponseMetadata)
	}

	model, closeFn = fixtureModel(t, serveChunksFixture("anthropic-text", t))
	defer closeFn()
	s, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	chunks := drain(t, s)
	var text strings.Builder
	var meta *provider.ResponseMetadata
	var finish *provider.StreamChunk
	for _, c := range chunks {
		switch c.Type {
		case provider.ChunkTypeText:
			text.WriteString(c.Text)
		case provider.ChunkTypeResponseMetadata:
			if c.ResponseMetadata.ID != "" {
				meta = c.ResponseMetadata
			}
		case provider.ChunkTypeFinish:
			finish = c
		}
	}
	if text.String() != "Hello! I'm doing well, thank you for asking. How are you doing today? Is there anything I can help you with?" {
		t.Fatalf("text = %q", text.String())
	}
	// TS: response-metadata {id, modelId} comes from message_start.
	if meta == nil || meta.ID != "msg_01QC4g3HwBThD4BaNtBckFDJ" || meta.ModelID != "claude-sonnet-4-5-20250929" {
		t.Fatalf("response metadata = %+v", meta)
	}
	if finish == nil || finish.FinishReason != types.FinishReasonStop || finish.RawFinishReason != "end_turn" {
		t.Fatalf("finish = %+v", finish)
	}
	if finish.Usage.InputTokens == nil || *finish.Usage.InputTokens != 12 || finish.Usage.OutputTokens == nil || *finish.Usage.OutputTokens != 30 {
		t.Fatalf("usage = %+v", finish.Usage)
	}
}

func TestFixtureToolWithoutArgs(t *testing.T) {
	model, closeFn := fixtureModel(t, serveChunksFixture("anthropic-tool-no-args", t))
	defer closeFn()
	s, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	seq := chunkSeq(drain(t, s))
	want := []string{"stream-start", "response-metadata", "response-metadata", "custom", "text-start", "text", "text-end",
		"tool-input-start:updateIssueList", "tool-input-end:", "tool-call:updateIssueList", "finish"}
	if strings.Join(seq, ",") != strings.Join(want, ",") {
		t.Fatalf("sequence\n got %v\nwant %v", seq, want)
	}

	model, closeFn2 := fixtureModel(t, serveJSONFixture("anthropic-tool-no-args", t))
	defer closeFn2()
	res, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].ToolName != "updateIssueList" || res.FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("generate: %+v", res)
	}
}

func TestFixtureRefusal(t *testing.T) {
	model, closeFn := fixtureModel(t, serveJSONFixture("anthropic-refusal", t))
	res, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	closeFn()
	if err != nil {
		t.Fatal(err)
	}
	if res.FinishReason != types.FinishReasonContentFilter || res.RawFinishReason != "refusal" {
		t.Fatalf("generate: %v %q", res.FinishReason, res.RawFinishReason)
	}
	model, closeFn = fixtureModel(t, serveChunksFixture("anthropic-refusal", t))
	defer closeFn()
	s, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	var finish *provider.StreamChunk
	for _, c := range drain(t, s) {
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
	}
	if finish == nil || finish.FinishReason != types.FinishReasonContentFilter {
		t.Fatalf("finish = %+v", finish)
	}
}

// TS snapshot "doStream > web search tool": tool input streams first, then
// the tool-call, the tool-result and then one source per search hit.
func TestFixtureWebSearchStreamOrder(t *testing.T) {
	model, closeFn := fixtureModel(t, serveChunksFixture("anthropic-web-search-tool.1", t))
	defer closeFn()
	s, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	seq := chunkSeq(drain(t, s))
	head := strings.Join(seq[:10], ",")
	want := "stream-start,response-metadata,response-metadata,custom,tool-input-start:web_search,tool-input-delta,tool-input-end:,tool-call:web_search,tool-result:web_search,source"
	if head != want {
		t.Fatalf("head\n got %s\nwant %s", head, want)
	}
}

// TS snapshot "doGenerate > web search tool > with fixture": content keeps the
// order the model produced it; sources follow their tool-result.
func TestFixtureWebSearchGenerateOrderAndCitations(t *testing.T) {
	model, closeFn := fixtureModel(t, serveJSONFixture("anthropic-web-search-tool.1", t))
	defer closeFn()
	res, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	citations := 0
	for _, c := range res.Content {
		switch p := c.(type) {
		case types.ToolCallContent:
			kinds = append(kinds, "call")
		case types.ToolResultContent:
			kinds = append(kinds, "result")
		case types.SourceContent:
			kinds = append(kinds, "source")
		case types.TextContent:
			kinds = append(kinds, "text")
			citations += len(p.Citations)
		}
	}
	got := strings.Join(kinds, ",")
	if !strings.HasPrefix(got, "call,result,source,source,source,source,source,source,source,source,source,source,text,call,result,text") {
		t.Fatalf("content order = %s", got)
	}
	if citations == 0 {
		t.Fatal("expected normalized citations on text parts")
	}
}

// Code execution results arrive as their own content blocks; TS streams a
// tool-result for each, named after the code_execution tool.
func TestFixtureCodeExecutionStreamResults(t *testing.T) {
	model, closeFn := fixtureModel(t, serveChunksFixture("anthropic-code-execution-20250825.1", t))
	defer closeFn()
	s, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		Tools:  []types.Tool{tools.CodeExecution20250825()},
	})
	if err != nil {
		t.Fatal(err)
	}
	results := 0
	for _, c := range drain(t, s) {
		if c.Type == provider.ChunkTypeToolResult {
			results++
			if c.ToolResult.Result == nil {
				t.Fatalf("empty result: %+v", c.ToolResult)
			}
		}
	}
	if results != 2 {
		t.Fatalf("tool results = %d, want 2 (text_editor + bash)", results)
	}
}

// TS maps tool_search results to [{type, toolName}] and error blocks to
// isError results with a camelCase errorCode; the wire shape must not leak.
func TestFixtureToolSearchResultShape(t *testing.T) {
	model, closeFn := fixtureModel(t, serveJSONFixture("anthropic-tool-search-regex.1", t))
	defer closeFn()
	res, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range res.Content {
		if tr, ok := c.(types.ToolResultContent); ok {
			refs, ok := tr.Result.([]interface{})
			if !ok || len(refs) != 1 {
				t.Fatalf("result = %#v", tr.Result)
			}
			ref := refs[0].(map[string]interface{})
			if ref["toolName"] != "get_temp_data" || ref["type"] != "tool_reference" || ref["tool_name"] != nil {
				t.Fatalf("reference = %#v", ref)
			}
			return
		}
	}
	t.Fatal("no tool result in content")
}

func TestConvertDeferredToolResultErrors(t *testing.T) {
	cases := []struct {
		block, raw string
		wantCode   string
	}{
		{"code_execution_tool_result", `{"type":"code_execution_tool_result_error","error_code":"unavailable"}`, "unavailable"},
		{"tool_search_tool_result", `{"type":"tool_search_tool_result_error","error_code":"too_many_requests"}`, "too_many_requests"},
		{"advisor_tool_result", `{"type":"advisor_tool_result_error","error_code":"overloaded"}`, "overloaded"},
	}
	for _, tc := range cases {
		dr, ok := convertDeferredToolResult(tc.block, []byte(tc.raw), nil, "")
		if !ok || !dr.IsError || dr.ErrorCode != tc.wantCode {
			t.Errorf("%s: %+v ok=%v", tc.block, dr, ok)
		}
		if m, _ := dr.Result.(map[string]interface{}); m["errorCode"] != tc.wantCode || m["error_code"] != nil {
			t.Errorf("%s: result = %#v", tc.block, dr.Result)
		}
	}
	if _, ok := convertDeferredToolResult("code_execution_tool_result", []byte(`{"type":"something_new"}`), nil, ""); ok {
		t.Error("unknown code_execution_tool_result payloads emit nothing (TS)")
	}
}

// A 200 response whose first event is an error (overloaded) must surface as a
// retryable provider error at DoStream time, like TS's APICallError(529/500).
func TestStreamOverloadedFirstEvent(t *testing.T) {
	model, closeFn := fixtureModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n")
	})
	defer closeFn()
	s, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err == nil {
		// Errors may also surface on the first Next().
		_, err = s.Next()
		_ = s.Close()
	}
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v (%T), want *ProviderError", err, err)
	}
	if !pe.IsRetryable() {
		t.Fatalf("overloaded must be retryable: %+v", pe)
	}
}

func TestGenerateHTTPErrorIsProviderError(t *testing.T) {
	model, closeFn := fixtureModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("retry-after", "3")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`))
	})
	defer closeFn()
	_, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.StatusCode != 429 || !pe.IsRetryable() || !strings.Contains(pe.Message, "slow down") {
		t.Fatalf("err = %#v", err)
	}
}

func TestUnsupportedModelTypesAreNoSuchModelErrors(t *testing.T) {
	p := New(Config{APIKey: "k"})
	var nsm *providererrors.NoSuchModelError
	if _, err := p.EmbeddingModel("x"); !errors.As(err, &nsm) || nsm.ModelType != "embeddingModel" {
		t.Errorf("EmbeddingModel err = %v", err)
	}
	if _, err := p.ImageModel("x"); !errors.As(err, &nsm) || nsm.ModelType != "imageModel" {
		t.Errorf("ImageModel err = %v", err)
	}
}

// A tool call cut off mid-JSON (max_tokens) must not fail the stream: TS emits
// the tool-call with its raw input so the core can mark it invalid or repair
// it, and the finish reason still arrives.
func TestStreamTruncatedToolInputIsEmittedRaw(t *testing.T) {
	model, closeFn := fixtureModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, l := range []string{
			`{"type":"message_start","message":{"id":"m1","model":"claude-sonnet-4-5","usage":{"input_tokens":3,"output_tokens":1}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"weather","input":{}}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"city\":\"San Fr"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_delta","delta":{"stop_reason":"max_tokens"},"usage":{"output_tokens":9}}`,
			`{"type":"message_stop"}`,
		} {
			fmt.Fprintf(w, "data: %s\n\n", l)
		}
	})
	defer closeFn()
	s, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	var call *types.ToolCall
	var finish *provider.StreamChunk
	for _, c := range drain(t, s) {
		if c.Type == provider.ChunkTypeToolCall {
			call = c.ToolCall
		}
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
	}
	if call == nil || call.RawArguments != `{"city":"San Fr` || call.Arguments != nil {
		t.Fatalf("tool call = %+v", call)
	}
	if finish == nil || finish.FinishReason != types.FinishReasonLength {
		t.Fatalf("finish = %+v", finish)
	}
}
