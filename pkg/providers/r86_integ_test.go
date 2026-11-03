package providers_test

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/bfl"
	"github.com/digitallysavvy/go-ai/pkg/providers/google"
	"github.com/digitallysavvy/go-ai/pkg/providers/groq"
	"github.com/digitallysavvy/go-ai/pkg/providers/hume"
	"github.com/digitallysavvy/go-ai/pkg/providers/moonshot"
)

// r86integCapture records the first request a provider sends, normalized so
// that a JSON body compares by value and a multipart body by its fields.
type r86integCapture struct {
	mu   sync.Mutex
	reqs []string
}

func (c *r86integCapture) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		norm := r.Method + " " + r.URL.Path + "?" + r.URL.RawQuery + "\n" + r86integNormalizeBody(r.Header.Get("Content-Type"), body)
		c.mu.Lock()
		c.reqs = append(c.reqs, norm)
		c.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"stop","type":"invalid_request_error"}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (c *r86integCapture) first() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.reqs) == 0 {
		return "", false
	}
	return c.reqs[0], true
}

func r86integNormalizeBody(contentType string, body []byte) string {
	mediaType, params, _ := mime.ParseMediaType(contentType)
	if strings.HasPrefix(mediaType, "multipart/") {
		mr := multipart.NewReader(strings.NewReader(string(body)), params["boundary"])
		var fields []string
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			v, _ := io.ReadAll(p)
			if p.FileName() != "" {
				v = []byte("<file>")
			}
			fields = append(fields, p.FormName()+"="+string(v))
		}
		sort.Strings(fields)
		return strings.Join(fields, "\n")
	}
	var v interface{}
	if json.Unmarshal(body, &v) == nil {
		b, _ := json.Marshal(v)
		return string(b)
	}
	return string(body)
}

// Class sweep, round 86 (provb #1-#5, prova #8 and the provb CRs): TS parses
// a provider option as its JSON value, so a Go-typed value ([]string,
// [][]interface{}, []map[string]interface{}, map[string]bool) must send the
// same request as its JSON form ([]interface{}, map[string]interface{}).
// Every case sends one request with each form; both must reach the server
// and be equal.
func TestR86integTypedProviderOptionsMatchJSONForm(t *testing.T) {
	ctx := context.Background()
	schema := map[string]interface{}{"type": "object", "properties": map[string]interface{}{"a": map[string]interface{}{"type": "string"}}}
	cases := []struct {
		name        string
		jsonForm    interface{}
		typed       interface{}
		mustContain string
		call        func(base string, opt interface{}) error
	}{
		{
			name: "groq transcription timestampGranularities", mustContain: "timestamp_granularities[]=word",
			jsonForm: []interface{}{"word"}, typed: []string{"word"},
			call: func(base string, opt interface{}) error {
				m, _ := groq.New(groq.Config{APIKey: "k", BaseURL: base}).TranscriptionModel("whisper-large-v3")
				_, err := m.DoTranscribe(ctx, &provider.TranscriptionOptions{Audio: []byte("RIFF"), MimeType: "audio/wav",
					ProviderOptions: map[string]interface{}{"groq": map[string]interface{}{"timestampGranularities": opt}}})
				return err
			},
		},
		{
			name: "bfl video keyframes", mustContain: `"keyframes":["https://a.test/1.png","https://a.test/2.png"]`,
			jsonForm: []interface{}{"https://a.test/1.png", "https://a.test/2.png"}, typed: []string{"https://a.test/1.png", "https://a.test/2.png"},
			call: func(base string, opt interface{}) error {
				m, _ := bfl.New(bfl.Config{APIKey: "k", BaseURL: base}).VideoModel(bfl.VideoModelFlux3Video)
				_, err := m.DoGenerate(ctx, &provider.VideoModelV3CallOptions{Prompt: "p", PromptSet: true,
					ProviderOptions: map[string]interface{}{"blackForestLabs": map[string]interface{}{"keyframes": opt}}})
				return err
			},
		},
		{
			name: "bfl video timed keyframes", mustContain: `"keyframes":[[0,"https://a.test/1.png"],[2,"https://a.test/2.png"]]`,
			jsonForm: []interface{}{[]interface{}{0.0, "https://a.test/1.png"}, []interface{}{2.0, "https://a.test/2.png"}},
			typed:    [][]interface{}{{0, "https://a.test/1.png"}, {2, "https://a.test/2.png"}},
			call: func(base string, opt interface{}) error {
				m, _ := bfl.New(bfl.Config{APIKey: "k", BaseURL: base}).VideoModel(bfl.VideoModelFlux3Video)
				_, err := m.DoGenerate(ctx, &provider.VideoModelV3CallOptions{Prompt: "p", PromptSet: true,
					ProviderOptions: map[string]interface{}{"blackForestLabs": map[string]interface{}{"keyframes": opt}}})
				return err
			},
		},
		{
			name: "hume speech context utterances", mustContain: `"context":{"utterances":[{"description":"","text":"a"}]}`,
			jsonForm: []interface{}{map[string]interface{}{"text": "a", "description": ""}},
			typed:    []map[string]interface{}{{"text": "a", "description": ""}},
			call: func(base string, opt interface{}) error {
				m, _ := hume.New(hume.Config{APIKey: "k", BaseURL: base}).SpeechModel("")
				_, err := m.DoGenerate(ctx, &provider.SpeechGenerateOptions{Text: "hi",
					ProviderOptions: map[string]interface{}{"hume": map[string]interface{}{"context": map[string]interface{}{"utterances": opt}}}})
				return err
			},
		},
		{
			name: "google embedding content", mustContain: `{"text":"x"}`,
			jsonForm: []interface{}{[]interface{}{map[string]interface{}{"text": "x"}}},
			typed:    [][]map[string]interface{}{{{"text": "x"}}},
			call: func(base string, opt interface{}) error {
				m, _ := google.New(google.Config{APIKey: "k", BaseURL: base}).EmbeddingModel("gemini-embedding-2")
				_, err := m.DoEmbed(ctx, "hi", &provider.EmbedModelOptions{
					ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"content": opt}}})
				return err
			},
		},
		{
			name: "anthropic tool allowedCallers", mustContain: `"allowed_callers":["code_execution_20260120"]`,
			jsonForm: []interface{}{"code_execution_20260120"}, typed: []string{"code_execution_20260120"},
			call: func(base string, opt interface{}) error {
				m, _ := anthropic.New(anthropic.Config{APIKey: "k", BaseURL: base}).LanguageModel("claude-sonnet-4-5")
				_, err := m.DoGenerate(ctx, &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Tools: []types.Tool{{
					Name: "f", Parameters: schema,
					ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"allowedCallers": opt}},
				}}})
				return err
			},
		},
		{
			name: "google interactions environment sources", mustContain: `"sources":[{"content":"x","target":"/a","type":"inline"}]`,
			jsonForm: []interface{}{map[string]interface{}{"type": "inline", "content": "x", "target": "/a"}},
			typed:    []map[string]interface{}{{"type": "inline", "content": "x", "target": "/a"}},
			call: func(base string, opt interface{}) error {
				m, _ := google.New(google.Config{APIKey: "k", BaseURL: base}).InteractionsAgent("deep-research-pro-preview-12-2025")
				_, err := m.DoGenerate(ctx, &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"},
					ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"environment": map[string]interface{}{"type": "remote", "sources": opt}}}})
				return err
			},
		},
		{
			name: "moonshot system message tools", mustContain: `"name":"calc"`,
			jsonForm: []interface{}{map[string]interface{}{"type": "function", "name": "calc", "description": "d", "inputSchema": schema}},
			typed:    []map[string]interface{}{{"type": "function", "name": "calc", "description": "d", "inputSchema": schema}},
			call: func(base string, opt interface{}) error {
				m, _ := moonshot.New(moonshot.Config{APIKey: "k", BaseURL: base}).LanguageModel("kimi-k3")
				_, err := m.DoGenerate(ctx, &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{
					{Role: types.RoleSystem, ProviderOptions: map[string]interface{}{"moonshotai": map[string]interface{}{"tools": opt}}},
					{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
				}}})
				return err
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var wantCap, gotCap r86integCapture
			jsonErr := tc.call(wantCap.server(t).URL, tc.jsonForm)
			want, ok := wantCap.first()
			if !ok {
				t.Fatalf("JSON form sent no request: %v", jsonErr)
			}
			if !strings.Contains(want, tc.mustContain) {
				t.Fatalf("JSON form request lacks %s:\n%s", tc.mustContain, want)
			}
			typedErr := tc.call(gotCap.server(t).URL, tc.typed)
			got, ok := gotCap.first()
			if !ok {
				t.Fatalf("typed form sent no request: %v", typedErr)
			}
			if got != want {
				t.Fatalf("typed form request:\n%s\nJSON form request:\n%s", got, want)
			}
		})
	}
}
