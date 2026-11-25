package together

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

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

const r90provbChatResponse = `{"id":"chatcmpl-test","object":"chat.completion","created":1711115037,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"Hello!","reasoning_content":"Thinking...","tool_calls":[{"id":"call_1","type":"function","function":{"name":"weather","arguments":"{\"location\":\"SF\"}"}}]},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":30,"total_tokens":34,"completion_tokens_details":{"accepted_prediction_tokens":5}}}`

func r90provbPrompt() types.Prompt {
	return types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}}}}
}

// TestR90provbPrepareRequestMatchesDoGenerate: PrepareRequest gives the body
// and warnings DoGenerate sends, without a network call or stream fields
// (TS OpenAICompatibleChatLanguageModel.prepareRequest, c9c6402cbd).
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
		_, _ = w.Write([]byte(r90provbChatResponse))
	}))
	defer srv.Close()
	lm, err := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
	if err != nil {
		t.Fatal(err)
	}
	m := lm.(*LanguageModel)
	topK := 10
	opts := &provider.GenerateOptions{Prompt: r90provbPrompt(), TopK: &topK, ProviderOptions: map[string]interface{}{"togetherai": map[string]interface{}{"user": "test-user", "custom_passthrough": "value"}}}
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
	if _, ok := body["stream"]; ok {
		t.Fatalf("stream field in prepared body: %v", body)
	}
	if _, ok := body["stream_options"]; ok {
		t.Fatalf("stream_options in prepared body: %v", body)
	}
	result, err := m.DoGenerate(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(body)
	var prepared map[string]interface{}
	_ = json.Unmarshal(b, &prepared)
	if !reflect.DeepEqual(prepared, sent[0]) {
		t.Fatalf("prepared %v\nsent %v", prepared, sent[0])
	}
	if !reflect.DeepEqual(warnings, result.Warnings) {
		t.Fatalf("prepared warnings %v, DoGenerate warnings %v", warnings, result.Warnings)
	}

	conv, err := m.ConvertResponse(context.Background(), []byte(r90provbChatResponse), nil)
	if err != nil {
		t.Fatal(err)
	}
	if conv.Text != result.Text || conv.FinishReason != result.FinishReason || !reflect.DeepEqual(conv.Usage, result.Usage) ||
		!reflect.DeepEqual(conv.ProviderMetadata, result.ProviderMetadata) || !reflect.DeepEqual(conv.Content, result.Content) ||
		!reflect.DeepEqual(conv.ToolCalls, result.ToolCalls) || conv.ResponseMetadata.ID != "chatcmpl-test" ||
		!conv.ResponseMetadata.Timestamp.Equal(result.ResponseMetadata.Timestamp) {
		t.Fatalf("converted %#v\ngenerated %#v", conv, result)
	}
	if conv.Warnings == nil || len(conv.Warnings) != 0 || conv.RawRequest != nil || conv.RawResponse != nil || conv.ResponseHeaders != nil || conv.ResponseMetadata.Headers != nil {
		t.Fatalf("converted extras: %#v", conv)
	}
}

// TestR90provbConvertResponseErrors: no choices is InvalidResponseDataError,
// a body failing the schema a TypeValidationError (TS c9c6402cbd).
func TestR90provbConvertResponseErrors(t *testing.T) {
	lm, _ := New(Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"}).LanguageModel("m")
	m := lm.(*LanguageModel)
	_, err := m.ConvertResponse(context.Background(), []byte(`{"id":"x","choices":[]}`), nil)
	var ide *providererrors.InvalidResponseDataError
	if !errors.As(err, &ide) || ide.Message != "Response did not contain any choices." {
		t.Fatalf("no choices: %v", err)
	}
	for _, bad := range []string{`{"id":"x","choices":"nope"}`, `null`} {
		_, err := m.ConvertResponse(context.Background(), []byte(bad), nil)
		var ve *providererrors.ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("%s: want a validation error, got %T %v", bad, err, err)
		}
	}
}
