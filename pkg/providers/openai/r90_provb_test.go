package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r90provbCompatModel(t *testing.T, baseURL string, extractor MetadataExtractor) *LanguageModel {
	t.Helper()
	p := New(Config{APIKey: "k", BaseURL: baseURL, OpenAICompatible: true, Name: "test-provider",
		ChatProviderName: "test-provider.chat", IncludeUsage: true, MetadataExtractor: extractor})
	m, err := p.ChatModel("grok-3")
	if err != nil {
		t.Fatal(err)
	}
	return m.(*LanguageModel)
}

const r90provbTextResponse = `{"id":"chatcmpl-test","object":"chat.completion","created":1711115037,"model":"grok-3","choices":[{"index":0,"message":{"role":"assistant","content":"Hello!"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"total_tokens":34,"completion_tokens":30}}`

func r90provbJSONValue(t *testing.T, v interface{}) interface{} {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out interface{}
	_ = json.Unmarshal(b, &out)
	return out
}

// TestR90provbPrepareRequestMatchesDoGenerate ports "prepareRequest > should
// match the body and warnings of doGenerate" and "should not include stream
// fields" (openai-compatible-chat-language-model.test.ts, TS c9c6402cbd).
func TestR90provbPrepareRequestMatchesDoGenerate(t *testing.T) {
	var mu sync.Mutex
	var sent []map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]interface{}
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		sent = append(sent, body)
		mu.Unlock()
		_, _ = w.Write([]byte(r90provbTextResponse))
	}))
	defer srv.Close()
	m := r90provbCompatModel(t, srv.URL, nil)
	maxTokens, temp, topK, seed := 100, 0.5, 10, 42
	opts := &provider.GenerateOptions{
		Prompt:        chatTestPrompt(),
		MaxTokens:     &maxTokens,
		Temperature:   &temp,
		TopK:          &topK,
		StopSequences: []string{"\n\n"},
		Seed:          &seed,
		ResponseFormat: &provider.ResponseFormat{Type: "json", Name: "answer", Schema: map[string]interface{}{
			"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"},
		}},
		Tools: []types.Tool{{Name: "weather", Parameters: map[string]interface{}{
			"type": "object", "properties": map[string]interface{}{"location": map[string]interface{}{"type": "string"}}, "required": []interface{}{"location"},
		}}},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "weather"},
		ProviderOptions: map[string]interface{}{"testProvider": map[string]interface{}{
			"user": "test-user", "reasoningEffort": "medium", "custom_passthrough": "value",
		}},
	}
	body, warnings, err := m.PrepareRequest(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	n := len(sent)
	mu.Unlock()
	if n != 0 {
		t.Fatalf("PrepareRequest made %d network calls", n)
	}
	result, err := m.DoGenerate(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := r90provbJSONValue(t, body), r90provbJSONValue(t, sent[0]); !reflect.DeepEqual(got, want) {
		t.Fatalf("prepared body %v\nsent %v", got, want)
	}
	if !reflect.DeepEqual(r90provbJSONValue(t, warnings), r90provbJSONValue(t, result.Warnings)) {
		t.Fatalf("prepared warnings %v, DoGenerate warnings %v", warnings, result.Warnings)
	}
	foundTopK := false
	for _, w := range warnings {
		foundTopK = foundTopK || (w.Type == "unsupported" && w.Feature == "topK")
	}
	if !foundTopK {
		t.Fatalf("no topK warning: %v", warnings)
	}
	if body["user"] != "test-user" || body["reasoning_effort"] != "medium" || body["custom_passthrough"] != "value" {
		t.Fatalf("options not applied: %v", body)
	}
	if _, ok := body["stream"]; ok {
		t.Fatalf("stream field in prepared body: %v", body)
	}
	if _, ok := body["stream_options"]; ok {
		t.Fatalf("stream_options in prepared body: %v", body)
	}
}

// TestR90provbConvertResponse ports the convertResponse tests (TS
// c9c6402cbd): content, prediction tokens, response metadata, no warnings,
// request or response body; the metadata key follows the provider options.
func TestR90provbConvertResponse(t *testing.T) {
	m := r90provbCompatModel(t, "http://127.0.0.1:1", nil)
	body := `{"id":"chatcmpl-test","object":"chat.completion","created":1711115037,"model":"grok-3","choices":[{"index":0,"message":{"role":"assistant","content":"Hello!","reasoning_content":"Thinking...","tool_calls":[{"id":"call_1","type":"function","function":{"name":"weather","arguments":"{\"location\":\"SF\"}"},"extra_content":{"google":{"thought_signature":"sig-123"}}}]},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":30,"total_tokens":34,"completion_tokens_details":{"accepted_prediction_tokens":5,"rejected_prediction_tokens":2}}}`
	res, err := m.ConvertResponse(context.Background(), []byte(body), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "Hello!" || len(res.Content) == 0 || !reflect.DeepEqual(res.Content[0], types.ReasoningContent{Text: "Thinking..."}) {
		t.Fatalf("content = %q %#v", res.Text, res.Content)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].ID != "call_1" || res.ToolCalls[0].RawArguments != `{"location":"SF"}` {
		t.Fatalf("tool calls = %#v", res.ToolCalls)
	}
	if got := r90provbJSONValue(t, res.ToolCalls[0].ProviderMetadata); !reflect.DeepEqual(got, map[string]interface{}{"test-provider": map[string]interface{}{"thoughtSignature": "sig-123"}}) {
		t.Fatalf("tool call metadata = %v", got)
	}
	if got := r90provbJSONValue(t, res.ProviderMetadata); !reflect.DeepEqual(got, map[string]interface{}{"test-provider": map[string]interface{}{"acceptedPredictionTokens": float64(5), "rejectedPredictionTokens": float64(2)}}) {
		t.Fatalf("provider metadata = %v", got)
	}
	if res.FinishReason != types.FinishReasonStop || res.RawFinishReason != "stop" {
		t.Fatalf("finish = %v %q", res.FinishReason, res.RawFinishReason)
	}
	if res.Usage.InputTokens == nil || *res.Usage.InputTokens != 4 || res.Usage.OutputTokens == nil || *res.Usage.OutputTokens != 30 {
		t.Fatalf("usage = %#v", res.Usage)
	}
	rm := res.ResponseMetadata
	if rm == nil || rm.ID != "chatcmpl-test" || rm.ModelID != "grok-3" || !rm.Timestamp.Equal(time.Date(2024, 3, 22, 13, 43, 57, 0, time.UTC)) || rm.Headers != nil {
		t.Fatalf("response metadata = %#v", rm)
	}
	if res.Warnings == nil || len(res.Warnings) != 0 || res.RawRequest != nil || res.RawResponse != nil || res.ResponseHeaders != nil {
		t.Fatalf("warnings %v, request %v, response body %v, headers %v", res.Warnings, res.RawRequest, res.RawResponse, res.ResponseHeaders)
	}

	camel, err := m.ConvertResponse(context.Background(), []byte(body), map[string]interface{}{"testProvider": map[string]interface{}{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := camel.ProviderMetadata["testProvider"]; !ok || len(camel.ProviderMetadata) != 1 {
		t.Fatalf("camelCase key: %v", camel.ProviderMetadata)
	}
}

// TestR90provbConvertResponseMatchesDoGenerate: ConvertResponse gives the
// content, finish reason, usage, metadata and response ids of DoGenerate
// ("should match doGenerate for the fixture").
func TestR90provbConvertResponseMatchesDoGenerate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r90provbTextResponse))
	}))
	defer srv.Close()
	m := r90provbCompatModel(t, srv.URL, nil)
	gen, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: chatTestPrompt()})
	if err != nil {
		t.Fatal(err)
	}
	conv, err := m.ConvertResponse(context.Background(), []byte(r90provbTextResponse), nil)
	if err != nil {
		t.Fatal(err)
	}
	if conv.Text != gen.Text || conv.FinishReason != gen.FinishReason || !reflect.DeepEqual(conv.Usage, gen.Usage) ||
		!reflect.DeepEqual(conv.ProviderMetadata, gen.ProviderMetadata) || conv.ResponseMetadata.ID != gen.ResponseMetadata.ID ||
		conv.ResponseMetadata.ModelID != gen.ResponseMetadata.ModelID || !conv.ResponseMetadata.Timestamp.Equal(gen.ResponseMetadata.Timestamp) {
		t.Fatalf("converted %#v\ngenerated %#v", conv, gen)
	}
}

type r90provbExtractor struct {
	calls  int
	parsed interface{}
}

func (e *r90provbExtractor) ExtractMetadata(_ context.Context, parsed interface{}) (map[string]interface{}, error) {
	e.calls++
	e.parsed = parsed
	return map[string]interface{}{"test-provider": map[string]interface{}{"value": "extracted"}}, nil
}

func (e *r90provbExtractor) CreateStreamExtractor() StreamMetadataExtractor { return nil }

// TestR90provbConvertResponseExtractorAndErrors ports "should call
// metadataExtractor with the unvalidated body", "should throw
// InvalidResponseDataError when there are no choices" and "should throw
// TypeValidationError for an invalid body" (TS c9c6402cbd).
func TestR90provbConvertResponseExtractorAndErrors(t *testing.T) {
	ex := &r90provbExtractor{}
	m := r90provbCompatModel(t, "http://127.0.0.1:1", ex)
	body := `{"id":"chatcmpl-test","object":"chat.completion","created":1711115037,"model":"grok-3","choices":[{"index":0,"message":{"role":"assistant","content":"Hello!"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"total_tokens":34,"completion_tokens":30},"test_field":"unknown-field"}`
	res, err := m.ConvertResponse(context.Background(), []byte(body), nil)
	if err != nil {
		t.Fatal(err)
	}
	if ex.calls != 1 || ex.parsed.(map[string]interface{})["test_field"] != "unknown-field" {
		t.Fatalf("extractor calls %d, parsed %v", ex.calls, ex.parsed)
	}
	if got := r90provbJSONValue(t, res.ProviderMetadata); !reflect.DeepEqual(got, map[string]interface{}{"test-provider": map[string]interface{}{"value": "extracted"}}) {
		t.Fatalf("provider metadata = %v", got)
	}

	m = r90provbCompatModel(t, "http://127.0.0.1:1", nil)
	noChoices := `{"id":"chatcmpl-test","object":"chat.completion","created":1711115037,"model":"grok-3","choices":[],"usage":{"prompt_tokens":4,"total_tokens":34,"completion_tokens":30}}`
	_, err = m.ConvertResponse(context.Background(), []byte(noChoices), nil)
	var ide *providererrors.InvalidResponseDataError
	if !errors.As(err, &ide) || ide.Message != "Response did not contain any choices." {
		t.Fatalf("no choices: %v", err)
	}
	var want interface{}
	_ = json.Unmarshal([]byte(noChoices), &want)
	if !reflect.DeepEqual(ide.Data, want) {
		t.Fatalf("error data = %#v", ide.Data)
	}
	for _, bad := range []string{`{"id":"x","choices":"nope"}`, `null`} {
		_, err := m.ConvertResponse(context.Background(), []byte(bad), nil)
		var ve *providererrors.ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("%s: want a validation error, got %T %v", bad, err, err)
		}
	}
}

// TestR90provbDecisionErrorSchema: openaiFailedResponseHandler matches
// openaiErrorDataSchema, whose type is a nullish string and code a nullish
// string or number; any other body gives the status text (openai-error.ts).
func TestR90provbDecisionErrorSchema(t *testing.T) {
	for body, want := range map[string]string{
		`{"error":{"message":"m","type":5}}`:            "Forbidden",
		`{"error":{"message":"m","code":true}}`:         "Forbidden",
		`{"error":{"message":"m","code":4,"type":"x"}}`: "m",
	} {
		p, _ := newDecisionProvider(t, body, 403)
		_, err := decide(t, p, decisionTestOptions())
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.Message != want {
			t.Errorf("%s: err = %v, want message %q", body, err, want)
		}
	}
}

// TestR90provbDiarizedEmptySpeaker: TS keeps a diarized segment whose
// speaker is "" (`'speaker' in segment` after the transcript.text.segment
// union member) and never treats a whisper segment as diarized
// (openai-transcription-model.ts, openai-transcription-api.ts).
func TestR90provbDiarizedEmptySpeaker(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"hi","segments":[{"type":"transcript.text.segment","id":"s1","text":"hi","start":0,"end":1,"speaker":""}]}`))
	}))
	defer srv.Close()
	m := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "gpt-4o-transcribe-diarize")
	result, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/mpeg"})
	if err != nil {
		t.Fatal(err)
	}
	got := r90provbJSONValue(t, result.ProviderMetadata)
	want := map[string]interface{}{"openai": map[string]interface{}{"segments": []interface{}{
		map[string]interface{}{"text": "hi", "startSecond": float64(0), "endSecond": float64(1), "speaker": ""},
	}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata = %v", got)
	}
}
