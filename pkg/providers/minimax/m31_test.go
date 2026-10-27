package minimax

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported from minimax-chat-language-model.test.ts (TS #23551): MiniMax M3.1
// and later models.

const (
	m31JSONBody   = `{"type":"message","content":[{"type":"text","text":"Hello!"}],"stop_reason":"end_turn","usage":{"input_tokens":4,"output_tokens":10}}`
	m31StreamBody = "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg\",\"model\":\"m\",\"usage\":{\"input_tokens\":4,\"output_tokens\":0}}}\n\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":0}}\n\n" +
		"data: {\"type\":\"message_stop\"}\n\n"
)

// m31Server records request bodies and headers.
type m31Server struct {
	srv     *httptest.Server
	mu      sync.Mutex
	bodies  []map[string]interface{}
	headers []http.Header
}

func newM31Server(t *testing.T, stream bool) *m31Server {
	t.Helper()
	s := &m31Server{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]interface{}
		_ = json.Unmarshal(raw, &body)
		s.mu.Lock()
		s.bodies = append(s.bodies, body)
		s.headers = append(s.headers, r.Header.Clone())
		s.mu.Unlock()
		if stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, m31StreamBody)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, m31JSONBody)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *m31Server) provider() *Provider {
	return New(Config{APIKey: "test-api-key", BaseURL: s.srv.URL})
}

func (s *m31Server) body(t *testing.T, i int) map[string]interface{} {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if i >= len(s.bodies) {
		t.Fatalf("no request #%d (have %d)", i, len(s.bodies))
	}
	return s.bodies[i]
}

func (s *m31Server) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.bodies)
}

func m31Prompt(parts ...types.ContentPart) types.Prompt {
	return types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: parts}}}
}

func m31Text(text string) types.ContentPart { return types.TextContent{Text: text} }

func m31Generate(t *testing.T, s *m31Server, modelID string, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	t.Helper()
	m, err := s.provider().LanguageModel(modelID)
	if err != nil {
		t.Fatal(err)
	}
	return m.DoGenerate(context.Background(), opts)
}

func m31Stream(t *testing.T, s *m31Server, modelID string, opts *provider.GenerateOptions) []*provider.StreamChunk {
	t.Helper()
	m, err := s.provider().LanguageModel(modelID)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := m.DoStream(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	var chunks []*provider.StreamChunk
	for {
		c, err := stream.Next()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				t.Fatalf("stream error: %v", err)
			}
			return chunks
		}
		chunks = append(chunks, c)
	}
}

func levelPtr(l types.ReasoningLevel) *types.ReasoningLevel { return &l }

func TestMiniMaxM31ReasoningMapsToEffort(t *testing.T) {
	for _, tc := range []struct {
		reasoning types.ReasoningLevel
		effort    string
		warns     bool
	}{
		{types.ReasoningMinimal, "low", true},
		{types.ReasoningLow, "low", false},
		{types.ReasoningMedium, "medium", false},
		{types.ReasoningHigh, "high", false},
		{types.ReasoningXHigh, "xhigh", false},
		{types.ReasoningMax, "max", false},
		{types.ReasoningNone, "low", true},
	} {
		t.Run(string(tc.reasoning), func(t *testing.T) {
			s := newM31Server(t, false)
			temp, topP := 1.0, 0.95
			maxTokens := 16384
			res, err := m31Generate(t, s, "minimax-m3.1-flash", &provider.GenerateOptions{
				Prompt: m31Prompt(m31Text("Hello")), Reasoning: levelPtr(tc.reasoning),
				MaxTokens: &maxTokens, Temperature: &temp, TopP: &topP,
			})
			if err != nil {
				t.Fatal(err)
			}
			body := s.body(t, 0)
			oc, _ := body["output_config"].(map[string]interface{})
			if oc["effort"] != tc.effort || body["max_tokens"] != float64(16384) || body["temperature"] != 1.0 || body["top_p"] != 0.95 {
				t.Fatalf("body = %#v", body)
			}
			if _, has := body["thinking"]; has {
				t.Errorf("thinking must be omitted, got %#v", body["thinking"])
			}
			if tc.warns {
				if len(res.Warnings) != 1 || res.Warnings[0].Type != "compatibility" || res.Warnings[0].Feature != "reasoning" || !strings.Contains(res.Warnings[0].Details, "low") {
					t.Fatalf("warnings = %#v", res.Warnings)
				}
			} else if len(res.Warnings) != 0 {
				t.Fatalf("warnings = %#v", res.Warnings)
			}
		})
	}
}

func TestMiniMaxM31PreservesServerDefaultReasoning(t *testing.T) {
	for _, reasoning := range []*types.ReasoningLevel{nil, levelPtr(types.ReasoningDefault)} {
		s := newM31Server(t, false)
		maxTokens := 16384
		if _, err := m31Generate(t, s, "minimax-m3.1-flash", &provider.GenerateOptions{
			Prompt: m31Prompt(m31Text("Hello")), Reasoning: reasoning, MaxTokens: &maxTokens,
		}); err != nil {
			t.Fatal(err)
		}
		body := s.body(t, 0)
		if _, has := body["output_config"]; has {
			t.Errorf("output_config = %#v", body["output_config"])
		}
		if _, has := body["thinking"]; has {
			t.Errorf("thinking = %#v", body["thinking"])
		}
	}
}

func TestMiniMaxM31ExplicitEffortWinsOverReasoning(t *testing.T) {
	for _, ns := range []string{"minimax", "anthropic"} {
		s := newM31Server(t, false)
		maxTokens := 16384
		res, err := m31Generate(t, s, "minimax-m3.1-flash", &provider.GenerateOptions{
			Prompt: m31Prompt(m31Text("Hello")), Reasoning: levelPtr(types.ReasoningNone), MaxTokens: &maxTokens,
			ProviderOptions: map[string]interface{}{ns: map[string]interface{}{"effort": "max"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		body := s.body(t, 0)
		if !reflect.DeepEqual(body["output_config"], map[string]interface{}{"effort": "max"}) {
			t.Errorf("%s: output_config = %#v", ns, body["output_config"])
		}
		if _, has := body["thinking"]; has || len(res.Warnings) != 0 {
			t.Errorf("%s: thinking=%v warnings=%#v", ns, body["thinking"], res.Warnings)
		}
	}
}

func TestMiniMaxM31CanonicalMixedCaseModelID(t *testing.T) {
	s := newM31Server(t, false)
	if _, err := m31Generate(t, s, "MiniMax-M3.1-Flash", &provider.GenerateOptions{
		Prompt: m31Prompt(m31Text("Hello")), Reasoning: levelPtr(types.ReasoningHigh),
	}); err != nil {
		t.Fatal(err)
	}
	body := s.body(t, 0)
	oc, _ := body["output_config"].(map[string]interface{})
	if body["model"] != "MiniMax-M3.1-Flash" || oc["effort"] != "high" {
		t.Fatalf("body = %#v", body)
	}
}

func TestMiniMaxM3KeepsAnthropicReasoningHandling(t *testing.T) {
	s := newM31Server(t, false)
	if _, err := m31Generate(t, s, "minimax-m3", &provider.GenerateOptions{
		Prompt: m31Prompt(m31Text("Hello")), Reasoning: levelPtr(types.ReasoningNone),
	}); err != nil {
		t.Fatal(err)
	}
	body := s.body(t, 0)
	if !reflect.DeepEqual(body["thinking"], map[string]interface{}{"type": "disabled"}) {
		t.Fatalf("thinking = %#v", body["thinking"])
	}
	if _, has := body["output_config"]; has {
		t.Fatalf("output_config = %#v", body["output_config"])
	}
}

func TestMiniMaxM31StreamReasoningWarnings(t *testing.T) {
	s := newM31Server(t, true)
	maxTokens := 16384
	chunks := m31Stream(t, s, "minimax-m3.1-flash", &provider.GenerateOptions{
		Prompt: m31Prompt(m31Text("Hello")), Reasoning: levelPtr(types.ReasoningNone), MaxTokens: &maxTokens,
	})
	body := s.body(t, 0)
	if !reflect.DeepEqual(body["output_config"], map[string]interface{}{"effort": "low"}) {
		t.Fatalf("output_config = %#v", body["output_config"])
	}
	if _, has := body["thinking"]; has {
		t.Fatalf("thinking = %#v", body["thinking"])
	}
	var start *provider.StreamChunk
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeError {
			t.Fatalf("unexpected error chunk: %v", c.Err)
		}
		if c.Type == provider.ChunkTypeStreamStart {
			start = c
		}
	}
	if start == nil || len(start.Warnings) != 1 || start.Warnings[0].Type != "compatibility" || start.Warnings[0].Feature != "reasoning" {
		t.Fatalf("stream-start = %#v", start)
	}
}

func TestMiniMaxM31EffortThroughProviderOptions(t *testing.T) {
	for _, effort := range []string{"low", "medium", "high", "xhigh", "max"} {
		s := newM31Server(t, false)
		if _, err := m31Generate(t, s, "minimax-m3.1-flash", &provider.GenerateOptions{
			Prompt:          m31Prompt(m31Text("Hello")),
			ProviderOptions: map[string]interface{}{"minimax": map[string]interface{}{"effort": effort}},
		}); err != nil {
			t.Fatal(err)
		}
		oc, _ := s.body(t, 0)["output_config"].(map[string]interface{})
		if oc["effort"] != effort {
			t.Errorf("effort %s: output_config = %#v", effort, oc)
		}
	}
}

func m31File(mediaType string, data types.FileData, opts map[string]interface{}) types.ContentPart {
	return types.FileContent{MediaType: mediaType, FileData: data, ProviderOptions: opts}
}

func m31GenerateMedia(t *testing.T, s *m31Server, file types.ContentPart) error {
	t.Helper()
	maxTokens := 1000
	_, err := m31Generate(t, s, "minimax-m3.1-flash", &provider.GenerateOptions{
		Prompt: m31Prompt(m31Text("Describe."), file), MaxTokens: &maxTokens,
	})
	return err
}

func m31MediaBlock(t *testing.T, s *m31Server) map[string]interface{} {
	t.Helper()
	msgs := s.body(t, 0)["messages"].([]interface{})
	content := msgs[0].(map[string]interface{})["content"].([]interface{})
	return content[1].(map[string]interface{})
}

func TestMiniMaxM31ConvertsVideoSources(t *testing.T) {
	for name, tc := range map[string]struct {
		data   types.FileData
		source map[string]interface{}
	}{
		"bytes":        {types.FileData{Type: types.FileDataTypeData, Data: []byte{1, 2, 3}}, map[string]interface{}{"type": "base64", "media_type": "video/mp4", "data": "AQID"}},
		"base64":       {types.FileData{Type: types.FileDataTypeData, DataString: "AQID"}, map[string]interface{}{"type": "base64", "media_type": "video/mp4", "data": "AQID"}},
		"url":          {types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/video.mp4"}, map[string]interface{}{"type": "url", "url": "https://example.com/video.mp4"}},
		"reference":    {types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"minimax": "123"}}, map[string]interface{}{"type": "url", "url": "mm_file://123"}},
		"reference mm": {types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"minimax": "mm_file://123"}}, map[string]interface{}{"type": "url", "url": "mm_file://123"}},
	} {
		t.Run(name, func(t *testing.T) {
			s := newM31Server(t, false)
			if err := m31GenerateMedia(t, s, m31File("video/mp4", tc.data, map[string]interface{}{"minimax": map[string]interface{}{"fps": 1}})); err != nil {
				t.Fatal(err)
			}
			want := map[string]interface{}{"type": "video", "source": tc.source}
			want["source"].(map[string]interface{})["fps"] = float64(1)
			if got := m31MediaBlock(t, s); !reflect.DeepEqual(got, want) {
				t.Fatalf("block = %#v, want %#v", got, want)
			}
			s.mu.Lock()
			beta := s.headers[0].Get("anthropic-beta")
			s.mu.Unlock()
			if beta != "" {
				t.Errorf("anthropic-beta = %q, want none", beta)
			}
		})
	}
}

func TestMiniMaxM31ImageDetailAndURLs(t *testing.T) {
	for _, detail := range []string{"low", "default", "high"} {
		s := newM31Server(t, false)
		if err := m31GenerateMedia(t, s, m31File("image/png", types.FileData{Type: types.FileDataTypeData, DataString: "AQID"}, map[string]interface{}{"minimax": map[string]interface{}{"detail": detail}})); err != nil {
			t.Fatal(err)
		}
		want := map[string]interface{}{"type": "image", "source": map[string]interface{}{"type": "base64", "media_type": "image/png", "data": "AQID", "detail": detail}}
		if got := m31MediaBlock(t, s); !reflect.DeepEqual(got, want) {
			t.Errorf("detail %s: block = %#v", detail, got)
		}
	}

	s := newM31Server(t, false)
	if err := m31GenerateMedia(t, s, m31File("image/png", types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/image.png"}, nil)); err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{"type": "image", "source": map[string]interface{}{"type": "url", "url": "https://example.com/image.png"}}
	if got := m31MediaBlock(t, s); !reflect.DeepEqual(got, want) {
		t.Errorf("url block = %#v", got)
	}
}

func TestMiniMaxM31FPS(t *testing.T) {
	data := types.FileData{Type: types.FileDataTypeData, DataString: "AQID"}
	s := newM31Server(t, false)
	if err := m31GenerateMedia(t, s, m31File("video/mp4", data, nil)); err != nil {
		t.Fatal(err)
	}
	if _, has := m31MediaBlock(t, s)["source"].(map[string]interface{})["fps"]; has {
		t.Error("fps must be omitted by default")
	}
	for _, fps := range []float64{0.2, 5} {
		s := newM31Server(t, false)
		if err := m31GenerateMedia(t, s, m31File("video/mp4", data, map[string]interface{}{"minimax": map[string]interface{}{"fps": fps}})); err != nil {
			t.Fatal(err)
		}
		if got := m31MediaBlock(t, s)["source"].(map[string]interface{})["fps"]; got != fps {
			t.Errorf("fps = %v, want %v", got, fps)
		}
	}
	for _, fps := range []interface{}{0, 0.1, 5.1, "1"} {
		s := newM31Server(t, false)
		if err := m31GenerateMedia(t, s, m31File("video/mp4", data, map[string]interface{}{"minimax": map[string]interface{}{"fps": fps}})); err == nil {
			t.Errorf("fps %v: want error", fps)
		}
		if s.calls() != 0 {
			t.Errorf("fps %v: request was sent", fps)
		}
	}
}

func TestMiniMaxM31MediaErrors(t *testing.T) {
	s := newM31Server(t, false)
	if err := m31GenerateMedia(t, s, m31File("image/png", types.FileData{Type: types.FileDataTypeData, DataString: "AQID"}, map[string]interface{}{"minimax": map[string]interface{}{"detail": "auto"}})); err == nil || s.calls() != 0 {
		t.Errorf("unsupported detail: err=%v calls=%d", err, s.calls())
	}

	err := m31GenerateMedia(t, s, m31File("video/mp4", types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"anthropic": "file_123"}}, nil))
	var nsp *providererrors.NoSuchProviderReferenceError
	if !errors.As(err, &nsp) || s.calls() != 0 {
		t.Errorf("foreign reference: err=%v calls=%d", err, s.calls())
	}

	err = m31GenerateMedia(t, s, m31File("image/png", types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"minimax": "1"}}, nil))
	var unsupported *providererrors.UnsupportedFunctionalityError
	if !errors.As(err, &unsupported) || s.calls() != 0 {
		t.Errorf("image reference: err=%v calls=%d", err, s.calls())
	}
}

func TestMiniMaxM31KeepsMediaOrderAcrossMergedUserMessages(t *testing.T) {
	s := newM31Server(t, false)
	maxTokens := 1000
	_, err := m31Generate(t, s, "minimax-m3.1-flash", &provider.GenerateOptions{
		Prompt: types.Prompt{System: "Analyze the media.", Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{
				m31Text("First"),
				m31File("image/png", types.FileData{Type: types.FileDataTypeData, DataString: "AQID"}, map[string]interface{}{"minimax": map[string]interface{}{"detail": "high"}}),
			}},
			{Role: types.RoleUser, Content: []types.ContentPart{
				m31File("video/mp4", types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"minimax": "456"}}, map[string]interface{}{"minimax": map[string]interface{}{"fps": 2}}),
				m31Text("Last"),
			}},
		}},
		Reasoning: levelPtr(types.ReasoningHigh), MaxTokens: &maxTokens,
	})
	if err != nil {
		t.Fatal(err)
	}
	body := s.body(t, 0)
	content := body["messages"].([]interface{})[0].(map[string]interface{})["content"]
	want := []interface{}{
		map[string]interface{}{"type": "text", "text": "First"},
		map[string]interface{}{"type": "image", "source": map[string]interface{}{"type": "base64", "media_type": "image/png", "data": "AQID", "detail": "high"}},
		map[string]interface{}{"type": "video", "source": map[string]interface{}{"type": "url", "url": "mm_file://456", "fps": float64(2)}},
		map[string]interface{}{"type": "text", "text": "Last"},
	}
	if !reflect.DeepEqual(content, want) {
		t.Fatalf("content = %#v", content)
	}
	if !reflect.DeepEqual(body["output_config"], map[string]interface{}{"effort": "high"}) {
		t.Errorf("output_config = %#v", body["output_config"])
	}
	if !reflect.DeepEqual(body["system"], []interface{}{map[string]interface{}{"type": "text", "text": "Analyze the media."}}) {
		t.Errorf("system = %#v", body["system"])
	}
}

func TestMiniMaxM31SendsVideoWhenStreaming(t *testing.T) {
	s := newM31Server(t, true)
	maxTokens := 1000
	chunks := m31Stream(t, s, "minimax-m3.1-flash", &provider.GenerateOptions{
		Prompt:    m31Prompt(m31File("video/mp4", types.FileData{Type: types.FileDataTypeData, DataString: "AQID"}, map[string]interface{}{"minimax": map[string]interface{}{"fps": 1}})),
		MaxTokens: &maxTokens,
	})
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeError {
			t.Fatalf("error chunk: %v", c.Err)
		}
	}
	body := s.body(t, 0)
	content := body["messages"].([]interface{})[0].(map[string]interface{})["content"]
	want := []interface{}{map[string]interface{}{"type": "video", "source": map[string]interface{}{"type": "base64", "media_type": "video/mp4", "data": "AQID", "fps": float64(1)}}}
	if body["stream"] != true || !reflect.DeepEqual(content, want) {
		t.Fatalf("body = %#v", body)
	}
}

func TestMiniMaxSupportedURLsByModelVersion(t *testing.T) {
	p := New(Config{APIKey: "k"})
	supported := func(id string) map[string][]string {
		m, err := p.LanguageModel(id)
		if err != nil {
			t.Fatal(err)
		}
		return m.(*LanguageModel).SupportedURLs()
	}
	want := map[string][]string{"image/*": {`^https?://`}, "video/*": {`^https?://`}}
	for _, id := range []string{"minimax-m3.1-flash", "MiniMax-M3.1-Flash"} {
		if got := supported(id); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %v", id, got)
		}
	}
	if got := supported("minimax-m3"); len(got) != 0 {
		t.Errorf("minimax-m3: %v", got)
	}
}

func TestMiniMaxM3DoesNotEnableVideoInput(t *testing.T) {
	s := newM31Server(t, false)
	_, err := m31Generate(t, s, "minimax-m3", &provider.GenerateOptions{
		Prompt: m31Prompt(m31File("video/mp4", types.FileData{Type: types.FileDataTypeData, DataString: "AQID"}, nil)),
	})
	var unsupported *providererrors.UnsupportedFunctionalityError
	if !errors.As(err, &unsupported) || s.calls() != 0 {
		t.Fatalf("err=%v calls=%d", err, s.calls())
	}
}

func m31Call(t *testing.T, mode, modelID string, opts *provider.GenerateOptions) (map[string]interface{}, []types.Warning) {
	t.Helper()
	s := newM31Server(t, mode == "stream")
	if opts.MaxTokens == nil {
		maxTokens := 1000
		opts.MaxTokens = &maxTokens
	}
	if mode == "generate" {
		res, err := m31Generate(t, s, modelID, opts)
		if err != nil {
			t.Fatal(err)
		}
		return s.body(t, 0), res.Warnings
	}
	var warnings []types.Warning
	for _, c := range m31Stream(t, s, modelID, opts) {
		if c.Type == provider.ChunkTypeError {
			t.Fatalf("error chunk: %v", c.Err)
		}
		if c.Type == provider.ChunkTypeStreamStart {
			warnings = c.Warnings
		}
	}
	return s.body(t, 0), warnings
}

func features(warnings []types.Warning) []string {
	var out []string
	for _, w := range warnings {
		out = append(out, w.Feature)
	}
	return out
}

func TestMiniMaxM31Versions(t *testing.T) {
	for _, mode := range []string{"generate", "stream"} {
		t.Run(mode, func(t *testing.T) {
			for _, id := range []string{
				"minimax-m3.1", "MiniMax-M3.1-Flash-Preview", "minimax-m3.1.1", "minimax-m3.2", "minimax-m3.10-flash",
				"minimax-m3.123-flash", "minimax-m4", "MiniMax-M4.0-Highspeed", "minimax-m10",
			} {
				temp, topP := 2.0, 0.9
				body, warnings := m31Call(t, mode, id, &provider.GenerateOptions{
					Prompt: m31Prompt(
						types.TextContent{Text: "Reference.", ProviderOptions: map[string]interface{}{"minimax": map[string]interface{}{"cacheControl": map[string]interface{}{"type": "ephemeral"}}}},
						m31File("image/png", types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/image.png"}, map[string]interface{}{"minimax": map[string]interface{}{"detail": "high"}}),
						m31File("video/mp4", types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"minimax": "123"}}, map[string]interface{}{"minimax": map[string]interface{}{"fps": 1}}),
					),
					Reasoning: levelPtr(types.ReasoningHigh), Temperature: &temp, TopP: &topP,
					ProviderOptions: map[string]interface{}{"minimax": map[string]interface{}{"thinking": map[string]interface{}{"type": "adaptive"}}},
				})
				if body["model"] != id || body["temperature"] != 2.0 || body["top_p"] != 0.9 ||
					!reflect.DeepEqual(body["thinking"], map[string]interface{}{"type": "adaptive"}) ||
					!reflect.DeepEqual(body["output_config"], map[string]interface{}{"effort": "high"}) {
					t.Errorf("%s: body = %#v", id, body)
				}
				want := []interface{}{
					map[string]interface{}{"type": "text", "text": "Reference.", "cache_control": map[string]interface{}{"type": "ephemeral"}},
					map[string]interface{}{"type": "image", "source": map[string]interface{}{"type": "url", "url": "https://example.com/image.png", "detail": "high"}},
					map[string]interface{}{"type": "video", "source": map[string]interface{}{"type": "url", "url": "mm_file://123", "fps": float64(1)}},
				}
				if got := body["messages"].([]interface{})[0].(map[string]interface{})["content"]; !reflect.DeepEqual(got, want) {
					t.Errorf("%s: content = %#v", id, got)
				}
				if len(warnings) != 0 {
					t.Errorf("%s: warnings = %#v", id, warnings)
				}
			}

			for _, id := range []string{"minimax-m3.2", "minimax-m4"} {
				body, warnings := m31Call(t, mode, id, &provider.GenerateOptions{Prompt: m31Prompt(m31Text("Hello")), Reasoning: levelPtr(types.ReasoningNone)})
				if !reflect.DeepEqual(body["output_config"], map[string]interface{}{"effort": "low"}) {
					t.Errorf("%s: output_config = %#v", id, body["output_config"])
				}
				if _, has := body["thinking"]; has || len(warnings) != 1 || warnings[0].Feature != "reasoning" {
					t.Errorf("%s: thinking=%v warnings=%#v", id, body["thinking"], warnings)
				}
			}

			for _, id := range []string{"minimax-m3", "minimax-m3.0-flash", "minimax-m2.99", "custom-model", "other-minimax-m4", "minimax-m3.1oops", "minimax-m4oops"} {
				temp := 1.5
				body, warnings := m31Call(t, mode, id, &provider.GenerateOptions{
					Prompt:      m31Prompt(types.TextContent{Text: "Reference.", ProviderOptions: map[string]interface{}{"minimax": map[string]interface{}{"cacheControl": map[string]interface{}{"type": "ephemeral"}}}}),
					Temperature: &temp,
				})
				first := body["messages"].([]interface{})[0].(map[string]interface{})["content"].([]interface{})[0].(map[string]interface{})
				if _, has := first["cache_control"]; has || body["temperature"] != 1.0 || !reflect.DeepEqual(features(warnings), []string{"temperature"}) {
					t.Errorf("%s: first=%#v temperature=%v warnings=%#v", id, first, body["temperature"], warnings)
				}
				m, _ := New(Config{APIKey: "k"}).LanguageModel(id)
				if got := m.(*LanguageModel).SupportedURLs(); len(got) != 0 {
					t.Errorf("%s: supported URLs = %v", id, got)
				}
			}
		})
	}
}

func TestMiniMaxM31PromptCaching(t *testing.T) {
	ephemeral := map[string]interface{}{"type": "ephemeral"}
	cache := map[string]interface{}{"minimax": map[string]interface{}{"cacheControl": ephemeral}}
	for _, mode := range []string{"generate", "stream"} {
		t.Run(mode, func(t *testing.T) {
			t.Run("system and text blocks", func(t *testing.T) {
				body, warnings := m31Call(t, mode, "minimax-m3.1-flash", &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{
					{Role: types.RoleSystem, Content: []types.ContentPart{m31Text("Stable instructions.")}, ProviderOptions: cache},
					{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Stable reference.", ProviderOptions: cache}, m31Text("Changing question.")}},
				}}})
				if !reflect.DeepEqual(body["system"], []interface{}{map[string]interface{}{"type": "text", "text": "Stable instructions.", "cache_control": ephemeral}}) {
					t.Errorf("system = %#v", body["system"])
				}
				want := []interface{}{
					map[string]interface{}{"type": "text", "text": "Stable reference.", "cache_control": ephemeral},
					map[string]interface{}{"type": "text", "text": "Changing question."},
				}
				if got := body["messages"].([]interface{})[0].(map[string]interface{})["content"]; !reflect.DeepEqual(got, want) || len(warnings) != 0 {
					t.Errorf("content = %#v warnings = %#v", got, warnings)
				}
			})

			t.Run("message cache control applies to the final block", func(t *testing.T) {
				body, _ := m31Call(t, mode, "minimax-m3.1-flash", &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{
					{Role: types.RoleUser, ProviderOptions: cache, Content: []types.ContentPart{m31Text("First."), m31Text("Last.")}},
				}}})
				want := []interface{}{
					map[string]interface{}{"type": "text", "text": "First."},
					map[string]interface{}{"type": "text", "text": "Last.", "cache_control": ephemeral},
				}
				if got := body["messages"].([]interface{})[0].(map[string]interface{})["content"]; !reflect.DeepEqual(got, want) {
					t.Errorf("content = %#v", got)
				}
			})

			for _, key := range []string{"cacheControl", "cache_control"} {
				t.Run("prefers minimax."+key, func(t *testing.T) {
					body, warnings := m31Call(t, mode, "minimax-m3.1-flash", &provider.GenerateOptions{Prompt: m31Prompt(types.TextContent{Text: "Reference.", ProviderOptions: map[string]interface{}{
						"minimax":   map[string]interface{}{key: ephemeral},
						"anthropic": map[string]interface{}{"cacheControl": map[string]interface{}{"type": "ephemeral", "ttl": "1h"}},
					}})})
					got := body["messages"].([]interface{})[0].(map[string]interface{})["content"].([]interface{})[0].(map[string]interface{})["cache_control"]
					if !reflect.DeepEqual(got, ephemeral) || len(warnings) != 0 {
						t.Errorf("cache_control = %#v warnings = %#v", got, warnings)
					}
				})
				t.Run("falls back to anthropic."+key, func(t *testing.T) {
					body, warnings := m31Call(t, mode, "minimax-m3.1-flash", &provider.GenerateOptions{Prompt: m31Prompt(types.TextContent{Text: "Reference.", ProviderOptions: map[string]interface{}{
						"minimax":   map[string]interface{}{},
						"anthropic": map[string]interface{}{key: ephemeral},
					}})})
					got := body["messages"].([]interface{})[0].(map[string]interface{})["content"].([]interface{})[0].(map[string]interface{})["cache_control"]
					if !reflect.DeepEqual(got, ephemeral) || len(warnings) != 0 {
						t.Errorf("cache_control = %#v warnings = %#v", got, warnings)
					}
				})
			}

			t.Run("one breakpoint per block across both namespaces", func(t *testing.T) {
				var parts []types.ContentPart
				for i := 0; i < 5; i++ {
					parts = append(parts, types.TextContent{Text: fmt.Sprintf("Reference %d.", i), ProviderOptions: map[string]interface{}{
						"minimax": map[string]interface{}{"cacheControl": ephemeral}, "anthropic": map[string]interface{}{"cacheControl": ephemeral},
					}})
				}
				body, warnings := m31Call(t, mode, "minimax-m3.1-flash", &provider.GenerateOptions{Prompt: m31Prompt(parts...)})
				content := body["messages"].([]interface{})[0].(map[string]interface{})["content"].([]interface{})
				for i, c := range content {
					_, has := c.(map[string]interface{})["cache_control"]
					if has != (i < 4) {
						t.Errorf("block %d cache_control present = %v", i, has)
					}
				}
				if !reflect.DeepEqual(features(warnings), []string{"cacheControl breakpoint limit"}) {
					t.Errorf("warnings = %#v", warnings)
				}
			})

			t.Run("no marker", func(t *testing.T) {
				body, warnings := m31Call(t, mode, "minimax-m3.1-flash", &provider.GenerateOptions{Prompt: m31Prompt(m31Text("Hello"))})
				first := body["messages"].([]interface{})[0].(map[string]interface{})["content"].([]interface{})[0].(map[string]interface{})
				if _, has := first["cache_control"]; has || len(warnings) != 0 {
					t.Errorf("first = %#v warnings = %#v", first, warnings)
				}
			})

			for _, id := range []string{"minimax-m3", "MiniMax-M2.7"} {
				t.Run("existing namespace behavior "+id, func(t *testing.T) {
					body, warnings := m31Call(t, mode, id, &provider.GenerateOptions{Prompt: m31Prompt(
						types.TextContent{Text: "MiniMax marker.", ProviderOptions: cache},
						types.TextContent{Text: "Anthropic marker.", ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"cacheControl": ephemeral}}},
					)})
					want := []interface{}{
						map[string]interface{}{"type": "text", "text": "MiniMax marker."},
						map[string]interface{}{"type": "text", "text": "Anthropic marker.", "cache_control": ephemeral},
					}
					if got := body["messages"].([]interface{})[0].(map[string]interface{})["content"]; !reflect.DeepEqual(got, want) || len(warnings) != 0 {
						t.Errorf("content = %#v warnings = %#v", got, warnings)
					}
				})
			}
		})
	}
}

func TestMiniMaxM31Sampling(t *testing.T) {
	adaptive := map[string]interface{}{"thinking": map[string]interface{}{"type": "adaptive"}}
	for _, mode := range []string{"generate", "stream"} {
		t.Run(mode, func(t *testing.T) {
			for _, temp := range []float64{0, 0.5, 1.5, 2} {
				topP := 0.9
				body, warnings := m31Call(t, mode, "minimax-m3.1-flash", &provider.GenerateOptions{Prompt: m31Prompt(m31Text("Hello")), Temperature: &temp, TopP: &topP})
				if body["temperature"] != temp || body["top_p"] != 0.9 || len(warnings) != 0 {
					t.Errorf("temperature %v: body=%#v warnings=%#v", temp, body, warnings)
				}
			}

			for _, ns := range []string{"minimax", "anthropic"} {
				temp, topP := 1.5, 0.9
				body, warnings := m31Call(t, mode, "minimax-m3.1-flash", &provider.GenerateOptions{
					Prompt: m31Prompt(m31Text("Hello")), Temperature: &temp, TopP: &topP,
					ProviderOptions: map[string]interface{}{ns: adaptive},
				})
				if body["temperature"] != 1.5 || body["top_p"] != 0.9 || !reflect.DeepEqual(body["thinking"], map[string]interface{}{"type": "adaptive"}) || len(warnings) != 0 {
					t.Errorf("%s: body=%#v warnings=%#v", ns, body, warnings)
				}
			}

			temp, topP := 2.0, 0.8
			body, warnings := m31Call(t, mode, "MiniMax-M3.1-Flash", &provider.GenerateOptions{Prompt: m31Prompt(m31Text("Hello")), Temperature: &temp, TopP: &topP, Reasoning: levelPtr(types.ReasoningHigh)})
			if body["temperature"] != 2.0 || body["top_p"] != 0.8 || len(warnings) != 0 {
				t.Errorf("canonical: body=%#v warnings=%#v", body, warnings)
			}

			body, warnings = m31Call(t, mode, "minimax-m3.1-flash", &provider.GenerateOptions{Prompt: m31Prompt(m31Text("Hello")), ProviderOptions: map[string]interface{}{"minimax": adaptive}})
			if _, has := body["temperature"]; has || len(warnings) != 0 {
				t.Errorf("omitted: body=%#v warnings=%#v", body, warnings)
			}
			if _, has := body["top_p"]; has {
				t.Errorf("omitted top_p: %#v", body)
			}

			for _, tc := range []struct{ in, want float64 }{{-0.5, 0}, {2.5, 2}} {
				temp, topP := tc.in, 0.9
				body, warnings := m31Call(t, mode, "minimax-m3.1-flash", &provider.GenerateOptions{Prompt: m31Prompt(m31Text("Hello")), Temperature: &temp, TopP: &topP, ProviderOptions: map[string]interface{}{"minimax": adaptive}})
				if body["temperature"] != tc.want || body["top_p"] != 0.9 || len(warnings) != 1 ||
					warnings[0].Type != "unsupported" || warnings[0].Feature != "temperature" || !strings.Contains(warnings[0].Details, "minimax") {
					t.Errorf("clamp %v: body=%#v warnings=%#v", tc.in, body, warnings)
				}
			}

			for _, id := range []string{"minimax-m3", "MiniMax-M2.7"} {
				temp := 1.5
				body, warnings := m31Call(t, mode, id, &provider.GenerateOptions{Prompt: m31Prompt(m31Text("Hello")), Temperature: &temp})
				if body["temperature"] != 1.0 || !reflect.DeepEqual(features(warnings), []string{"temperature"}) {
					t.Errorf("%s: body=%#v warnings=%#v", id, body, warnings)
				}

				temp, topP := 0.5, 0.9
				body, warnings = m31Call(t, mode, id, &provider.GenerateOptions{Prompt: m31Prompt(m31Text("Hello")), Temperature: &temp, TopP: &topP, ProviderOptions: map[string]interface{}{"minimax": adaptive}})
				_, hasTemp := body["temperature"]
				_, hasTopP := body["top_p"]
				if hasTemp || hasTopP || !reflect.DeepEqual(features(warnings), []string{"temperature", "topP"}) {
					t.Errorf("%s adaptive: body=%#v warnings=%#v", id, body, warnings)
				}
			}
		})
	}
}
