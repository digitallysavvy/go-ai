package openresponses

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r91provbBody(t *testing.T, cfg Config, opts *provider.GenerateOptions) (string, http.Header) {
	t.Helper()
	var body string
	var headers http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body, headers = string(raw), r.Header.Clone()
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()
	cfg.BaseURL = srv.URL
	_, _ = New(cfg).languageModelForTest("m").DoGenerate(context.Background(), opts)
	return body, headers
}

func (p *Provider) languageModelForTest(id string) provider.LanguageModel {
	m, _ := p.LanguageModel(id)
	return m
}

func r91provbUser(text string) types.Prompt {
	return types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: text}}}}}
}

// PB-07 (provb2b-02): the provider's own key wins over the openai alias
// (TS providerOptions[providerOptionsName]).
func TestR91provbProviderKeyWins(t *testing.T) {
	body, _ := r91provbBody(t, Config{Name: "lmstudio"}, &provider.GenerateOptions{Prompt: r91provbUser("hi"), ProviderOptions: map[string]interface{}{
		"openai":   map[string]interface{}{"reasoningEffort": "low", "store": false},
		"lmstudio": map[string]interface{}{"reasoningEffort": "high"},
	}})
	if !strings.Contains(body, `"effort":"high"`) || strings.Contains(body, `"store"`) {
		t.Fatalf("body = %s", body)
	}
}

// PB-28 (provb2b-06): the default User-Agent suffix is
// ai-sdk-open-responses/<version> whatever the name.
func TestR91provbUserAgentSuffix(t *testing.T) {
	_, h := r91provbBody(t, Config{Name: "lmstudio"}, &provider.GenerateOptions{Prompt: r91provbUser("hi")})
	if ua := h.Get("User-Agent"); !strings.Contains(ua, "ai-sdk-open-responses/") || strings.Contains(ua, "ai-sdk-lmstudio/") {
		t.Fatalf("User-Agent = %q", ua)
	}
}

// PB-33 (provb2b-14): input items keep <, > and & literal (JSON.stringify).
func TestR91provbInputNotHTMLEscaped(t *testing.T) {
	body, _ := r91provbBody(t, Config{Name: "acme"}, &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "u<1&"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "a>2"}, types.ToolCallContent{ToolCallID: "c", ToolName: "f", Input: `{"x":"<y>"}`}}},
	}}})
	if !strings.Contains(body, "u<1&") || !strings.Contains(body, "a>2") || !strings.Contains(body, `<y>`) || strings.Contains(body, "\\u003c") {
		t.Fatalf("body = %s", body)
	}
}

// PB-34 (provb2b-15): encrypted_content "" is kept in the reasoning
// provider metadata (TS encrypted_content != null).
func TestR91provbEmptyEncryptedContent(t *testing.T) {
	var item OutputItem
	if err := json.Unmarshal([]byte(`{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":""}`), &item); err != nil {
		t.Fatal(err)
	}
	meta := openResponsesReasoningProviderMetadata("acme", &item)
	if !strings.Contains(string(meta), `"reasoningEncryptedContent":""`) {
		t.Fatalf("metadata = %s", meta)
	}
}

// PB-31 (provb2b-11): an extension DecodeEvent failure sets the finish
// reason to error (open-responses-language-model.ts:716).
func TestR91provbExtensionDecodeEventFinishError(t *testing.T) {
	ext := Extension{
		ID: "acme.events", EventTypes: []string{"acme:progress"},
		DecodeEvent: func(ExtensionEvent, map[string]interface{}) ([]*provider.StreamChunk, error) {
			return nil, errors.New("bad event")
		},
	}
	p := New(Config{BaseURL: "http://localhost:1/v1", Name: "acme", Extensions: []Extension{ext}})
	if p.extensionRegistry == nil {
		t.Skip("extension registry not built")
	}
	sse := "data: {\"type\":\"acme:progress\",\"sequence_number\":1}\n\n"
	s := newOpenResponsesStream(io.NopCloser(strings.NewReader(sse)), nil, "acme")
	s.extensionRegistry = p.extensionRegistry
	var finish *provider.StreamChunk
	sawError := false
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeError {
			sawError = true
		}
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
	}
	if !sawError || finish == nil || finish.FinishReason != types.FinishReasonError {
		t.Fatalf("error=%v finish=%+v", sawError, finish)
	}
}

// PB-35 (provb2b-16): a tool message's tool-result carrying an extension
// replay item replays the item once instead of a function_call_output.
func TestR91provbToolMessageExtensionReplay(t *testing.T) {
	reg, err := NewExtensionRegistry([]Extension{codeExecutionExtension()})
	if err != nil {
		t.Fatal(err)
	}
	item := map[string]interface{}{"type": "lmstudio:code_execution", "id": "ce_1", "status": "completed", "code": "x"}
	po := map[string]interface{}{"acme": map[string]interface{}{"openResponsesExtension": map[string]interface{}{"id": "lmstudio.code_execution", "item": item}}}
	msgs := []types.Message{
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.CustomContent{Kind: extensionReplayKind, ProviderOptions: po}}},
		{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{ToolCallID: "ce_1", ToolName: "run_code", Result: "1", ProviderOptions: po}}},
	}
	input, _, _, err := ConvertToOpenResponsesInputForProviderStrict(msgs, "", "acme", false, openResponsesExtensionOptions{Registry: reg})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(input)
	if strings.Count(string(b), `"lmstudio:code_execution"`) != 1 || strings.Contains(string(b), "function_call_output") {
		t.Fatalf("input = %s", b)
	}
}
