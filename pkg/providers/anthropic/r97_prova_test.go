package anthropic

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r97provaPrompt() *provider.GenerateOptions {
	return &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "q"}}}}},
	}
}

// r97provaForbiddenCause reports a JSONParseError whose cause is the
// secureJsonParse SyntaxError itself (not a nested JSONParseError).
func r97provaForbiddenCause(t *testing.T, err error) {
	t.Helper()
	var parseErr *providererrors.JSONParseError
	if !errors.As(err, &parseErr) {
		t.Fatalf("err = %T %v, want a JSONParseError", err, err)
	}
	if parseErr.Cause != jsonexact.ErrForbiddenPrototype {
		t.Fatalf("cause = %T %v, want the forbidden prototype SyntaxError", parseErr.Cause, parseErr.Cause)
	}
}

// TS createEventSourceResponseHandler parses each frame with safeParseJSON
// (secureJsonParse): a frame with a __proto__ key is a JSONParseError part,
// not a TypeValidationError.
func TestR97provaStreamForbiddenPrototypeFrame(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"model\":\"x\",\"usage\":{\"input_tokens\":1}}}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"ping\",\"__proto__\":{}}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"message_stop\"}\n\n")
	}))
	defer srv.Close()
	model := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), ClaudeSonnet4_5, nil)
	stream, err := model.DoStream(context.Background(), r97provaPrompt())
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	var errPart *provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err != nil {
			break
		}
		if chunk.Type == provider.ChunkTypeError {
			errPart = chunk
		}
	}
	if errPart == nil {
		t.Fatal("no error part")
	}
	r97provaForbiddenCause(t, errPart.Err)
	if want := `JSON parsing failed: Text: {"type":"ping","__proto__":{}}.` + "\nError message: Object contains forbidden prototype property"; errPart.Text != want {
		t.Fatalf("text = %q, want %q", errPart.Text, want)
	}
}

// TS createJsonResponseHandler: a 200 body that is not JSON is the APICallError
// "Invalid JSON response" (status and body kept) caused by the JSONParseError.
func TestR97provaGenerateNonJSONBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()
	model := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), ClaudeSonnet4_5, nil)
	_, err := model.DoGenerate(context.Background(), r97provaPrompt())
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.Message != "Invalid JSON response" || pe.StatusCode != 200 || pe.ResponseBody != "not json" {
		t.Fatalf("err = %#v, want the Invalid JSON response APICallError", err)
	}
	var parseErr *providererrors.JSONParseError
	if !errors.As(err, &parseErr) || parseErr.Text != "not json" {
		t.Fatalf("cause = %v, want a JSONParseError", pe.Cause)
	}
}

// A batch results line with a __proto__ key is TS safeParseJSON's
// JSONParseError, whose cause is the SyntaxError.
func TestR97provaBatchLineForbiddenPrototype(t *testing.T) {
	r97provaForbiddenCause(t, anthropicBatchLineError([]byte(`{"custom_id":"a","result":{},"__proto__":{}}`)))
}

// TS anthropicFallbackContentSchema: from and to are z.object({model}), so
// providerMetadata.anthropic.from / to keep only the model (generate and
// stream), written like JSON.stringify.
func TestR97provaFallbackMetadataStripped(t *testing.T) {
	const want = `{"anthropic":{"from":{"model":"a"},"to":{"model":"b"},"type":"fallback"}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"type":"message","id":"m","model":"x","content":[{"type":"fallback","from":{"model":"a","region":"eu","weight":1e400},"to":{"model":"b","x":1}}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()
	model := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), ClaudeSonnet4_5, nil)
	result, err := model.DoGenerate(context.Background(), r97provaPrompt())
	if err != nil {
		t.Fatal(err)
	}
	var custom *types.CustomContent
	for _, part := range result.Content {
		if c, ok := part.(types.CustomContent); ok && c.Kind == "anthropic.fallback" {
			custom = &c
		}
	}
	if custom == nil || string(custom.ProviderMetadata) != want {
		t.Fatalf("generate fallback part = %+v, want metadata %s", custom, want)
	}

	sse := "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"fallback\",\"from\":{\"model\":\"a\",\"region\":\"eu\",\"weight\":1e400},\"to\":{\"model\":\"b\",\"x\":1}}}\n\n"
	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sse)), false)
	var streamed *types.CustomContent
	for {
		chunk, err := stream.Next()
		if err != nil {
			break
		}
		if chunk.Type == provider.ChunkTypeCustom && chunk.CustomContent != nil && chunk.CustomContent.Kind == "anthropic.fallback" {
			streamed = chunk.CustomContent
		}
	}
	if streamed == nil || string(streamed.ProviderMetadata) != want {
		t.Fatalf("stream fallback part = %+v, want metadata %s", streamed, want)
	}
}

// TS getAnthropicCallerInfo reads tool_id of the zod-parsed caller: the
// direct union member strips it, so toolId is absent.
func TestR97provaDirectCallerWithoutToolID(t *testing.T) {
	info := anthropicCallerInfo(map[string]interface{}{"type": "direct", "tool_id": "srvtoolu_1"})
	if _, has := info["toolId"]; has || info["type"] != "direct" {
		t.Fatalf("caller info = %v, want {type: direct}", info)
	}
	info = anthropicCallerInfo(map[string]interface{}{"type": "code_execution_20250825", "tool_id": "srvtoolu_1"})
	if info["toolId"] != "srvtoolu_1" {
		t.Fatalf("caller info = %v, want the code execution toolId", info)
	}
}

// TS throws InvalidResponseDataError only for `results_url == null`; an empty
// results_url goes to getFromApi, whose URL validation throws the
// DownloadError "Invalid URL: ".
func TestR97provaBatchEmptyResultsURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"b1","type":"message_batch","processing_status":"ended","request_counts":{"processing":0,"succeeded":1,"errored":0,"canceled":0,"expired":0},"created_at":"2024-01-01T00:00:00Z","expires_at":"2024-01-02T00:00:00Z","results_url":""}`))
	}))
	defer srv.Close()
	b := New(Config{APIKey: "k", BaseURL: srv.URL}).ExperimentalBatch()
	_, err := b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "b1"})
	var dl *providererrors.DownloadError
	if !errors.As(err, &dl) || dl.Message != "Invalid URL: " {
		t.Fatalf("err = %T %v, want the DownloadError Invalid URL", err, err)
	}
}

// TS convertAnthropicUsage passes the parsed usage as raw: a fractional count
// (z.number()) stays as sent in Usage.Raw, while the Go int counts truncate
// it (provb CR r97).
func TestR97provaFractionalUsageRawExact(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"type":"message","id":"m","model":"x","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":10.5,"output_tokens":3}}`))
	}))
	defer srv.Close()
	model := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), ClaudeSonnet4_5, nil)
	result, err := model.DoGenerate(context.Background(), r97provaPrompt())
	if err != nil {
		t.Fatal(err)
	}
	if v := result.Usage.Raw["input_tokens"]; v != 10.5 {
		t.Fatalf("Usage.Raw input_tokens = %v, want 10.5", v)
	}
	if result.Usage.InputDetails == nil || *result.Usage.InputDetails.NoCacheTokens != 10 {
		t.Fatalf("noCache tokens = %+v, want 10", result.Usage.InputDetails)
	}
}
