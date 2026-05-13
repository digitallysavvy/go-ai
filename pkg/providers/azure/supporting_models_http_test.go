package azure

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestAzureEmbeddingModelDoEmbedAndDoEmbedManyHTTP(t *testing.T) {
	var seenPath string
	var seenBody map[string]interface{}
	serverURL, closeServer := newAzureIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.String()
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("X-Req", "r1")
		w.Header().Set("Content-Type", "application/json")
		if _, ok := seenBody["input"].([]interface{}); ok {
			_, _ = w.Write([]byte(`{"object":"list","data":[{"index":0,"embedding":[1,2]},{"index":1,"embedding":[3,4]}],"usage":{"prompt_tokens":7,"total_tokens":9}}`))
			return
		}
		_, _ = w.Write([]byte(`{"object":"list","data":[{"index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":3,"total_tokens":4}}`))
	}))
	defer closeServer()

	p := New(Config{APIKey: "k", BaseURL: serverURL, DeploymentID: "dep", APIVersion: "2024-10-21"})
	m := NewEmbeddingModel(p, "dep")
	one, err := m.DoEmbed(context.Background(), "hello", &provider.EmbedModelOptions{Headers: map[string]string{"X-Test": "yes"}})
	if err != nil {
		t.Fatalf("DoEmbed error = %v", err)
	}
	if !strings.HasPrefix(seenPath, "/openai/deployments/dep/embeddings?api-version=2024-10-21") || seenBody["input"] != "hello" {
		t.Fatalf("request mismatch path=%q body=%#v", seenPath, seenBody)
	}
	if len(one.Embedding) != 2 || one.Response.Headers["X-Req"][0] != "r1" {
		t.Fatalf("result mismatch: %#v", one)
	}

	many, err := m.DoEmbedMany(context.Background(), []string{"a", "b"}, nil)
	if err != nil {
		t.Fatalf("DoEmbedMany error = %v", err)
	}
	if len(many.Embeddings) != 2 || many.Responses[0].Headers["X-Req"][0] != "r1" {
		t.Fatalf("many result mismatch: %#v", many)
	}
}

func TestAzureLanguageModelDoGenerateHTTP(t *testing.T) {
	var seenPath string
	var seenBody map[string]interface{}
	serverURL, closeServer := newAzureIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.String()
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok","tool_calls":[]}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer closeServer()

	p := New(Config{APIKey: "k", BaseURL: serverURL, DeploymentID: "dep", APIVersion: "2024-10-21"})
	m := NewLanguageModel(p, "dep")
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hello"}})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if !strings.HasPrefix(seenPath, "/openai/deployments/dep/chat/completions?api-version=2024-10-21") {
		t.Fatalf("path = %q", seenPath)
	}
	if seenBody["stream"] != false {
		t.Fatalf("stream flag mismatch: %#v", seenBody)
	}
	if res.Text != "ok" {
		t.Fatalf("result text = %q", res.Text)
	}
}

func TestAzureImageAndSpeechDoGenerateHTTP(t *testing.T) {
	imageServerURL, closeImage := newAzureIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.String(), "/openai/deployments/img/images/generations?api-version=2024-10-21") {
			t.Fatalf("image path = %q", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"data":[{"url":"https://example.com/image.png"}]}`))
	}))
	defer closeImage()
	pImage := New(Config{APIKey: "k", BaseURL: imageServerURL, DeploymentID: "img", APIVersion: "2024-10-21"})
	im := NewImageModel(pImage, "img")
	img, err := im.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "cat"})
	if err != nil {
		t.Fatalf("image DoGenerate error = %v", err)
	}
	if img.URL == "" {
		t.Fatalf("expected URL image result: %#v", img)
	}

	speechServerURL, closeSpeech := newAzureIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.String(), "/openai/deployments/tts/audio/speech?api-version=2024-10-21") {
			t.Fatalf("speech path = %q", r.URL.String())
		}
		_, _ = w.Write([]byte("AUDIO"))
	}))
	defer closeSpeech()
	pSpeech := New(Config{APIKey: "k", BaseURL: speechServerURL, DeploymentID: "tts", APIVersion: "2024-10-21"})
	sm := NewSpeechModel(pSpeech, "tts")
	speech, err := sm.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hello"})
	if err != nil {
		t.Fatalf("speech DoGenerate error = %v", err)
	}
	if string(speech.Audio) != "AUDIO" || speech.MimeType != "audio/mpeg" {
		t.Fatalf("speech result mismatch: %#v", speech)
	}
}

func TestAzureTranscriptionDoTranscribeHTTPAndSerialization(t *testing.T) {
	var seenBody string
	serverURL, closeServer := newAzureIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.String(), "/openai/deployments/whisper/audio/transcriptions?api-version=2024-10-21") {
			t.Fatalf("transcription path = %q", r.URL.String())
		}
		body, _ := io.ReadAll(r.Body)
		seenBody = string(body)
		_, _ = w.Write([]byte(`{"text":"hello","duration":1.5,"segments":[{"text":"hello","start":0.0,"end":1.5}]}`))
	}))
	defer closeServer()

	p := New(Config{APIKey: "k", BaseURL: serverURL, DeploymentID: "whisper", APIVersion: "2024-10-21"})
	tm := NewTranscriptionModel(p, "whisper")
	res, err := tm.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio:      []byte("audio"),
		MimeType:   "audio/mpeg",
		Language:   "en",
		Timestamps: true,
	})
	if err != nil {
		t.Fatalf("DoTranscribe error = %v", err)
	}
	if !strings.Contains(seenBody, `name="response_format"`) || !strings.Contains(seenBody, "verbose_json") {
		t.Fatalf("missing verbose response format in multipart body")
	}
	if res.Text != "hello" || len(res.Timestamps) != 1 {
		t.Fatalf("transcription result mismatch: %#v", res)
	}

	modelAny, err := p.LanguageModel("whisper")
	if err != nil {
		t.Fatalf("LanguageModel error = %v", err)
	}
	serialized := modelAny.(*LanguageModel).Serialize()
	restored, err := deserializeModel(serialized)
	if err != nil {
		t.Fatalf("deserializeModel error = %v", err)
	}
	if restored.Provider() != "azure-openai" || restored.ModelID() != "whisper" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}
}

func newAzureIPv4TestServer(t *testing.T, handler http.Handler) (string, func()) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen tcp4 error = %v", err)
	}
	srv := &http.Server{Handler: handler}
	go func() { _ = srv.Serve(listener) }()
	baseURL := "http://127.0.0.1:" + strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	closeFn := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
	return baseURL, closeFn
}
