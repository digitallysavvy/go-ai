package huggingface

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestHuggingFaceEmbeddingModelParseAndDoEmbedMany(t *testing.T) {
	var calls int
	p := newHFProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/models/e5" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		input := body["inputs"].(string)
		switch input {
		case "alpha":
			return hfJSONResponse(200, `[1,2,3]`), nil
		case "bravo":
			return hfJSONResponse(200, `[[4,5,6]]`), nil
		default:
			return hfJSONResponse(200, `{"embedding":[7,8,9]}`), nil
		}
	})
	m := NewEmbeddingModel(p, "e5")
	out, err := m.DoEmbedMany(context.Background(), []string{"alpha", "bravo"}, &provider.EmbedModelOptions{Headers: map[string]string{"X-Test": "1"}})
	if err != nil {
		t.Fatalf("DoEmbedMany error = %v", err)
	}
	if calls != 2 || len(out.Embeddings) != 2 || out.Embeddings[0][0] != 1 || out.Embeddings[1][0] != 4 {
		t.Fatalf("embeddings mismatch: %#v calls=%d", out.Embeddings, calls)
	}
	if out.Usage.TotalTokens == 0 {
		t.Fatalf("usage not set: %#v", out.Usage)
	}

	one, err := m.DoEmbed(context.Background(), "c", nil)
	if err != nil || len(one.Embedding) != 3 || one.Embedding[0] != 7 {
		t.Fatalf("DoEmbed mismatch result=%#v err=%v", one, err)
	}
}

func TestHuggingFaceEmbeddingModelErrorAndOptsHeaders(t *testing.T) {
	m := NewEmbeddingModel(New(Config{APIKey: "k"}), "e5")
	if _, err := m.parseEmbeddingResponse([]byte(`{"error":"bad"}`)); err == nil || !strings.Contains(err.Error(), "hugging face API error") {
		t.Fatalf("expected API error, got %v", err)
	}
	if _, err := m.parseEmbeddingResponse([]byte(`{"x":1}`)); err == nil || !strings.Contains(err.Error(), "unexpected response format") {
		t.Fatalf("expected unexpected format error, got %v", err)
	}
	if h := optsHeaders(nil); h != nil {
		t.Fatalf("optsHeaders(nil) = %#v", h)
	}
}

func TestHuggingFaceImageModelBuildAndConvert(t *testing.T) {
	m := NewImageModel(New(Config{APIKey: "k"}), "sd")
	n := 2
	body := m.buildRequestBody(&provider.ImageGenerateOptions{Prompt: "cat", N: &n, Size: "640x480"})
	if body["inputs"] != "cat" {
		t.Fatalf("inputs = %#v", body["inputs"])
	}
	params, ok := body["parameters"].(map[string]interface{})
	if !ok || params["num_images"] != 2 || params["width"] != 640 || params["height"] != 480 {
		t.Fatalf("parameters mismatch: %#v", body["parameters"])
	}

	png, err := m.convertResponse([]byte{0x89, 0x50, 0x4E, 0x47, 0x00})
	if err != nil || png.MimeType != "image/png" {
		t.Fatalf("png convert mismatch: result=%#v err=%v", png, err)
	}
	jpg, err := m.convertResponse([]byte{0xFF, 0xD8, 0x00})
	if err != nil || jpg.MimeType != "image/jpeg" {
		t.Fatalf("jpeg convert mismatch: result=%#v err=%v", jpg, err)
	}
	if _, err := m.convertResponse([]byte(`{"error":"bad"}`)); err == nil {
		t.Fatal("expected API error")
	}
	if _, err := m.convertResponse(nil); err == nil {
		t.Fatal("expected empty response error")
	}
}

func TestHuggingFaceLanguageModelBuildConvertAndStream(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "llama")
	temp := float64(0.2)
	maxTokens := 10
	body := m.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}},
			System:   "sys",
		},
		Temperature: &temp,
		MaxTokens:   &maxTokens,
	})
	params, ok := body["parameters"].(map[string]interface{})
	if !ok || params["temperature"] != temp || params["max_new_tokens"] != maxTokens {
		t.Fatalf("parameters mismatch: %#v", body["parameters"])
	}
	inputText, ok := body["inputs"].(string)
	if !ok || !strings.Contains(inputText, "User: hi") || !strings.Contains(inputText, "Assistant: ") {
		t.Fatalf("inputs prompt mismatch: %#v", body["inputs"])
	}

	out, err := m.convertResponse([]byte(`[{"generated_text":"hello world"}]`))
	if err != nil || out.Text != "hello world" || out.FinishReason != types.FinishReasonStop {
		t.Fatalf("array convert mismatch: result=%#v err=%v", out, err)
	}
	out2, err := m.convertResponse([]byte(`{"generated_text":"hi"}`))
	if err != nil || out2.Text != "hi" {
		t.Fatalf("object convert mismatch: result=%#v err=%v", out2, err)
	}
	if _, err := m.convertResponse([]byte(`{"error":"bad"}`)); err == nil {
		t.Fatal("expected API error")
	}

	stream := &huggingfaceStream{result: &types.GenerateResult{Text: "abcdefghijk", FinishReason: types.FinishReasonStop}}
	chunk1, err := stream.Next()
	if err != nil || chunk1.Type != provider.ChunkTypeText || chunk1.Text != "abcdefghij" {
		t.Fatalf("first chunk mismatch: chunk=%#v err=%v", chunk1, err)
	}
	chunk2, err := stream.Next()
	if err != nil || chunk2.Text != "k" {
		t.Fatalf("second chunk mismatch: chunk=%#v err=%v", chunk2, err)
	}
	finish, err := stream.Next()
	if err != nil || finish.Type != provider.ChunkTypeFinish {
		t.Fatalf("finish chunk mismatch: chunk=%#v err=%v", finish, err)
	}
	if _, err := stream.Next(); err == nil {
		t.Fatal("expected exhausted stream error")
	}
}

func TestHuggingFaceEmbeddingDoEmbedTransportError(t *testing.T) {
	p := newHFProviderWithTransport(t, func(_ *http.Request) (*http.Response, error) {
		return nil, errors.New("boom")
	})
	if _, err := NewEmbeddingModel(p, "e5").DoEmbed(context.Background(), "x", nil); err == nil {
		t.Fatal("expected transport error")
	}
}

type hfRoundTripper func(*http.Request) (*http.Response, error)

func (f hfRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newHFProviderWithTransport(t *testing.T, rt hfRoundTripper) *Provider {
	t.Helper()
	p := New(Config{APIKey: "k", BaseURL: "https://hf.example"})
	p.client = internalhttp.NewClient(internalhttp.Config{
		BaseURL: "https://hf.example",
		Headers: map[string]string{
			"Authorization": "Bearer k",
			"Content-Type":  "application/json",
		},
		HTTPClient: &http.Client{Transport: rt},
	})
	return p
}

func hfJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "X-Hf": []string{"1"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
