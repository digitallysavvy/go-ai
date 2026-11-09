package mistral

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r90provgMockID is TS mockId(): id-0, id-1, ...
func r90provgMockID() func() string {
	n := 0
	return func() string {
		id := "id-" + strconv.Itoa(n)
		n++
		return id
	}
}

// TestR90provgChatStreamIDs ports the mistral-chat-language-model.test.ts
// doStream reasoning snapshots: createMistral({generateId}) gives each new
// reasoning block a generateId() id (a second block after text gets a new
// one) and a streamed tool call without an id a generated one.
func TestR90provgChatStreamIDs(t *testing.T) {
	sse := `data: {"id":"x","created":1,"model":"m","choices":[{"index":0,"delta":{"content":[{"type":"thinking","thinking":[{"type":"text","text":"a"}]}]},"finish_reason":null}]}

data: {"choices":[{"index":0,"delta":{"content":"t"},"finish_reason":null}]}

data: {"choices":[{"index":0,"delta":{"content":[{"type":"thinking","thinking":[{"type":"text","text":"b"}]}]},"finish_reason":null}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"f","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}

`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sse)
	}))
	defer server.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: server.URL, GenerateID: r90provgMockID()}).LanguageModel("magistral-medium-2507")
	stream, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	var reasoningIDs, reasoning []string
	toolID := ""
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		switch c.Type {
		case provider.ChunkTypeReasoningStart:
			reasoningIDs = append(reasoningIDs, c.ID)
		case provider.ChunkTypeReasoning:
			reasoning = append(reasoning, c.Reasoning)
		case provider.ChunkTypeToolCall:
			toolID = c.ToolCall.ID
		}
	}
	if len(reasoningIDs) != 2 || reasoningIDs[0] != "id-0" || reasoningIDs[1] != "id-1" {
		t.Fatalf("reasoning block ids = %v, want [id-0 id-1]", reasoningIDs)
	}
	if len(reasoning) != 2 || reasoning[0] != "a" || reasoning[1] != "b" {
		t.Fatalf("reasoning deltas = %q, want [a b]", reasoning)
	}
	if toolID != "id-2" {
		t.Fatalf("tool call id = %q, want id-2", toolID)
	}
}

// TestR90provgConversationSourceIDs: TS createMistral passes generateId to
// the conversation model, whose tool_reference sources take
// generateId() ids (mistral-conversation-language-model.ts
// createContentConverter).
func TestR90provgConversationSourceIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"conversation_id":"c","outputs":[{"type":"message.output","content":[{"type":"text","text":"hi"},{"type":"tool_reference","tool":"web_search","title":"T","url":"https://example.com/a"}]}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer server.Close()
	m := New(Config{APIKey: "k", BaseURL: server.URL, GenerateID: r90provgMockID()}).ConversationModel("mistral-medium-latest")
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	id := ""
	for _, c := range res.Content {
		if s, ok := c.(types.SourceContent); ok {
			id = s.ID
		}
	}
	if id != "id-0" {
		t.Fatalf("source id = %q, want id-0", id)
	}
}

// TestR90provgEmbedFractionalTokens: TS MistralTextEmbeddingResponseSchema
// usage.prompt_tokens is z.number(); usage.tokens is that number.
func TestR90provgEmbedFractionalTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","data":[{"object":"embedding","embedding":[0.1],"index":0}],"model":"mistral-embed","usage":{"prompt_tokens":2.5,"total_tokens":2.5}}`)
	}))
	defer server.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: server.URL}).EmbeddingModel("mistral-embed")
	res, err := m.DoEmbedMany(context.Background(), []string{"a"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Usage.Tokens != 2.5 {
		t.Fatalf("tokens = %v, want 2.5", res.Usage.Tokens)
	}
}

// TestR90provgConversationStreamErrorAndExecutionInput: TS enqueues a
// conversation.response.error event object as the error, which core
// normalizes (type, numeric code as status code, retryable), and a built-in
// execution's input is JSON.stringify({arguments}) with HTML characters kept
// literal (mistral-conversation-language-model.ts).
func TestR90provgConversationStreamErrorAndExecutionInput(t *testing.T) {
	sse := `data: {"type":"conversation.response.started","conversation_id":"c"}

data: {"type":"tool.execution.started","id":"e1","name":"web_search","arguments":"{\"query\":\"Tom & Jerry <3>\"}"}

data: {"type":"tool.execution.done","id":"e1","name":"web_search"}

data: {"type":"conversation.response.error","message":"Too many requests","code":429}

`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sse)
	}))
	defer server.Close()
	m := New(Config{APIKey: "k", BaseURL: server.URL}).ConversationModel("mistral-medium-latest")
	stream, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	var input string
	var streamErr error
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		switch c.Type {
		case provider.ChunkTypeToolCall:
			input = c.ToolCall.RawArguments
		case provider.ChunkTypeError:
			streamErr = c.Err
		}
	}
	if want := `{"arguments":"{\"query\":\"Tom & Jerry <3>\"}"}`; input != want {
		t.Fatalf("execution input = %s, want %s", input, want)
	}
	var spe *providererrors.StreamProviderError
	if !errors.As(streamErr, &spe) {
		t.Fatalf("stream error = %#v, want a StreamProviderError", streamErr)
	}
	if spe.Message != "Too many requests" || spe.Type != "conversation.response.error" || spe.StatusCode == nil || *spe.StatusCode != 429 || !spe.IsRetryable {
		t.Fatalf("stream error = %+v", spe)
	}
}

// TestR90provgConversationMissingExecutionStart: TS enqueues new
// Error(`Missing tool execution start for ${id}`), an Error instance core
// passes on unchanged (not a StreamProviderError).
func TestR90provgConversationMissingExecutionStart(t *testing.T) {
	sse := `data: {"type":"tool.execution.done","id":"e9","name":"web_search"}

`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sse)
	}))
	defer server.Close()
	m := New(Config{APIKey: "k", BaseURL: server.URL}).ConversationModel("mistral-medium-latest")
	stream, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	for {
		c, err := stream.Next()
		if err != nil {
			t.Fatal("no error part")
		}
		if c.Type == provider.ChunkTypeError {
			var spe *providererrors.StreamProviderError
			if c.Err == nil || errors.As(c.Err, &spe) || c.Err.Error() != "Missing tool execution start for e9" {
				t.Fatalf("error part Err = %#v", c.Err)
			}
			return
		}
	}
}

// TestR90provgDocumentLimitNumberKinds: documentImageLimit is z.number(), so
// every Go integer kind the validation accepts reaches the wire.
func TestR90provgDocumentLimitNumberKinds(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "mistral-small-latest")
	for _, v := range []interface{}{uint16(3), int8(3), uint(3)} {
		body := m.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, ProviderOptions: map[string]interface{}{"mistral": map[string]interface{}{"documentImageLimit": v}}}, false)
		if body["document_image_limit"] != 3.0 {
			t.Errorf("%T: document_image_limit = %#v, want 3", v, body["document_image_limit"])
		}
	}
}

// TestR90provgSpeechRequestBodyText: TS request.body is
// JSON.stringify(requestBodyValues), which keeps HTML characters literal and
// the literal's key order (mistral-speech-model.ts).
func TestR90provgSpeechRequestBodyText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"audio_data":"AAEC"}`)
	}))
	defer server.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: server.URL}).SpeechModel("voxtral-mini-tts-latest")
	res, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "a < b & c", Voice: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"model":"voxtral-mini-tts-latest","input":"a < b & c","voice_id":"v1","response_format":"mp3","stream":false}`
	if res.Request == nil || res.Request.Body != want {
		t.Fatalf("request body = %v, want %s", res.Request, want)
	}
}

// TestR90provgTranscriptionTemperatureText: TS appendFormValue sends
// String(temperature), JS number text (1e-7, not 0.0000001).
func TestR90provgTranscriptionTemperatureText(t *testing.T) {
	m := NewTranscriptionModel(New(Config{APIKey: "k"}), "voxtral-mini-latest")
	body, _, err := m.buildMultipartBody(&provider.TranscriptionOptions{Audio: []byte{1}, MimeType: "audio/wav", ProviderOptions: map[string]interface{}{"mistral": map[string]interface{}{"temperature": 1e-7}}})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(body)
	if !bytes.Contains(b, []byte("\r\n\r\n1e-7\r\n")) {
		t.Fatalf("temperature field not 1e-7: %q", b)
	}
}
