package replicate

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

func TestReplicateProviderSurfaceAndClient(t *testing.T) {
	p := New(Config{APIKey: "k"})
	if p.Name() != "replicate" {
		t.Fatalf("name mismatch: %s", p.Name())
	}
	if p.Client() == nil {
		t.Fatal("expected client")
	}
	if _, err := p.LanguageModel(""); err == nil {
		t.Fatal("expected language model ID error")
	}
	if _, err := p.EmbeddingModel("x"); err == nil {
		t.Fatal("expected unsupported embeddings")
	}
	if _, err := p.VideoModel(""); err == nil {
		t.Fatal("expected video model ID error")
	}
	if _, err := p.SpeechModel("x"); err == nil {
		t.Fatal("expected unsupported speech")
	}
	if _, err := p.TranscriptionModel("x"); err == nil {
		t.Fatal("expected unsupported transcription")
	}
	if _, err := p.RerankingModel("x"); err == nil {
		t.Fatal("expected unsupported reranking")
	}
}

func TestReplicateLanguageDoGenerateWithPolling(t *testing.T) {
	var postBody map[string]interface{}
	p := newReplicateProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost && r.URL.Path == "/predictions" {
			_ = json.NewDecoder(r.Body).Decode(&postBody)
			return replicateJSONResponse(200, `{"id":"pred1","status":"starting"}`), nil
		}
		if r.Method == http.MethodGet && r.URL.Path == "/predictions/pred1" {
			return replicateJSONResponse(200, `{"id":"pred1","status":"succeeded","output":["hello ","world"]}`), nil
		}
		return nil, errors.New("unexpected request")
	})

	lm := NewLanguageModel(p, "ver-1")
	if lm.SpecificationVersion() != "v3" || lm.Provider() != "replicate" || lm.ModelID() != "ver-1" {
		t.Fatalf("metadata mismatch")
	}
	if lm.SupportsTools() || lm.SupportsStructuredOutput() || lm.SupportsImageInput() {
		t.Fatalf("capability mismatch")
	}
	out, err := lm.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if postBody["version"] != "ver-1" || out.Text != "hello world" || out.FinishReason != types.FinishReasonStop {
		t.Fatalf("result mismatch postBody=%#v out=%#v", postBody, out)
	}
}

func TestReplicateImageDoGeneratePollingAndNoURLError(t *testing.T) {
	p := newReplicateProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost && r.URL.Path == "/predictions" {
			return replicateJSONResponse(200, `{"id":"pred2","status":"processing"}`), nil
		}
		if r.Method == http.MethodGet && r.URL.Path == "/predictions/pred2" {
			return replicateJSONResponse(200, `{"id":"pred2","status":"succeeded","output":[]}`), nil
		}
		return nil, errors.New("unexpected request")
	})
	im := NewImageModel(p, "img-ver")
	if im.SpecificationVersion() != "v3" || im.Provider() != "replicate" || im.ModelID() != "img-ver" {
		t.Fatalf("metadata mismatch")
	}
	_, err := im.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "cat"})
	if err == nil || !strings.Contains(err.Error(), "no image URL in prediction output") {
		t.Fatalf("expected no-image-url error, got %v", err)
	}
}

func TestReplicateVideoDoGeneratePollingNoVideo(t *testing.T) {
	p := newReplicateProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost && r.URL.Path == "/predictions" {
			return replicateJSONResponse(200, `{"id":"pred3","status":"starting"}`), nil
		}
		if r.Method == http.MethodGet && r.URL.Path == "/predictions/pred3" {
			return replicateJSONResponse(200, `{"id":"pred3","status":"succeeded","output":[]}`), nil
		}
		return nil, errors.New("unexpected request")
	})
	vm := NewVideoModel(p, "vid-ver")
	if vm.SpecificationVersion() != "v3" || vm.Provider() != "replicate" || vm.ModelID() != "vid-ver" || vm.MaxVideosPerCall() != nil {
		t.Fatalf("metadata mismatch")
	}
	_, err := vm.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{Prompt: "run"})
	if err == nil || !strings.Contains(err.Error(), "no video") {
		t.Fatalf("expected no video error, got %v", err)
	}
}

func TestReplicatePollingFailuresAndStreamHelpers(t *testing.T) {
	pFail := newReplicateProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost && r.URL.Path == "/predictions" {
			return replicateJSONResponse(200, `{"id":"pred4","status":"starting"}`), nil
		}
		if r.Method == http.MethodGet && r.URL.Path == "/predictions/pred4" {
			return replicateJSONResponse(200, `{"id":"pred4","status":"failed","error":"bad"}`), nil
		}
		return nil, errors.New("unexpected request")
	})
	if _, err := NewLanguageModel(pFail, "ver").DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}}); err == nil || !strings.Contains(err.Error(), "prediction failed") {
		t.Fatalf("expected prediction failed error, got %v", err)
	}

	pCancel := newReplicateProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost && r.URL.Path == "/predictions" {
			return replicateJSONResponse(200, `{"id":"pred5","status":"starting"}`), nil
		}
		if r.Method == http.MethodGet && r.URL.Path == "/predictions/pred5" {
			return replicateJSONResponse(200, `{"id":"pred5","status":"canceled","error":"canceled"}`), nil
		}
		return nil, errors.New("unexpected request")
	})
	if _, err := NewImageModel(pCancel, "img").DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x"}); err == nil || !strings.Contains(err.Error(), "prediction canceled") {
		t.Fatalf("expected prediction canceled error, got %v", err)
	}

	s := &replicateStream{result: &types.GenerateResult{Text: "abc", FinishReason: types.FinishReasonStop}}
	if _, err := s.Next(); err != nil {
		t.Fatalf("stream Next error = %v", err)
	}
	_ = s.Close()
	if _, err := s.Next(); err == nil {
		t.Fatal("expected exhausted stream")
	}
	if s.Err() != nil {
		t.Fatal("replicate stream Err should be nil")
	}
}

type replicateRoundTripper func(*http.Request) (*http.Response, error)

func (f replicateRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newReplicateProviderWithTransport(t *testing.T, rt replicateRoundTripper) *Provider {
	t.Helper()
	p := New(Config{APIKey: "k", BaseURL: "https://replicate.example"})
	p.client = internalhttp.NewClient(internalhttp.Config{
		BaseURL: "https://replicate.example",
		Headers: map[string]string{
			"Authorization": "Token k",
			"Content-Type":  "application/json",
		},
		HTTPClient: &http.Client{Transport: rt},
	})
	return p
}

func replicateJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
