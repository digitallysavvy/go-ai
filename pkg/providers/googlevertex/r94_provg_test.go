package googlevertex

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Cloud TTS is TS postJsonToApi (`!response.ok` runs the failed-response
// handler): an unfollowed 3xx is the Vertex API error, not a success.
func TestR94provgCloudTTSNon2xxFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(300)
		_, _ = w.Write([]byte(`{"error":{"code":300,"message":"moved","status":"X"},"audioContent":"AAAA"}`))
	}))
	defer srv.Close()
	model, err := newChirpProvider(t, srv.URL).SpeechModel("chirp-3-hd")
	if err != nil {
		t.Fatal(err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi"})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.StatusCode != 300 || pe.Message != "moved" {
		t.Fatalf("err = %T %v, want the 300 API error", err, err)
	}
}

// cr-r94-carry-to-provg class 1 sweep: TS sends the Live setup frame with
// JSON.stringify and the MaaS body transform re-serializes with
// JSON.stringify, so <, > and & stay literal.
func TestR94provgJSONStringifyText(t *testing.T) {
	got := make(chan string, 1)
	srv := httptest.NewServer(websocket.Server{
		Handler: func(conn *websocket.Conn) {
			var raw string
			if websocket.Message.Receive(conn, &raw) == nil {
				got <- raw
			}
			_ = websocket.Message.Receive(conn, &raw)
		},
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
	})
	defer srv.Close()
	m := newLiveTestModel(t, srv.URL)
	res, err := m.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            &r94provgAudio{block: make(chan struct{})},
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: r94provgIntPtr(16000)},
		ProviderOptions:  map[string]interface{}{"vertex": map[string]interface{}{"customVocabulary": []interface{}{"a<b&c"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Stream.Close() //nolint:errcheck
	go func() {
		for {
			if _, err := res.Stream.Next(); err != nil {
				return
			}
		}
	}()
	select {
	case f := <-got:
		if !strings.Contains(f, `"a<b&c"`) {
			t.Errorf("setup = %s", f)
		}
	case <-time.After(5 * time.Second):
		t.Error("no setup frame")
	}

	out := applyMaasMaxTokensDefault([]byte(`{"model":"meta/llama-4-scout-17b-16e-instruct-maas","messages":[{"role":"user","content":"a<b&c"}]}`))
	if !strings.Contains(string(out), `"a<b&c"`) || !strings.Contains(string(out), `"max_tokens"`) {
		t.Errorf("maas body = %s", out)
	}
}

type r94provgAudio struct{ block chan struct{} }

func (a *r94provgAudio) Next(ctx context.Context) ([]byte, error) {
	select {
	case <-a.block:
	case <-ctx.Done():
	}
	return nil, io.EOF
}
func (a *r94provgAudio) Cancel(error) {}

func r94provgIntPtr(v int) *int { return &v }

// cr-r94-prova-to-provg 2 sweep: a cancelled call fails the Live stream with
// the cancel cause (TS onAbort(abortSignal.reason)).
func TestR94provgAbortCause(t *testing.T) {
	srv := httptest.NewServer(websocket.Server{
		Handler: func(conn *websocket.Conn) {
			var raw string
			for websocket.Message.Receive(conn, &raw) == nil {
			}
		},
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
	})
	defer srv.Close()
	ctx, cancel := context.WithCancelCause(context.Background())
	res, err := newLiveTestModel(t, srv.URL).DoStream(ctx, &provider.TranscriptionStreamOptions{
		Audio: &r94provgAudio{block: make(chan struct{})}, InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: r94provgIntPtr(16000)},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Stream.Close() //nolint:errcheck
	time.Sleep(50 * time.Millisecond)
	reason := errors.New("r94provg user stop")
	cancel(reason)
	for {
		if _, err := res.Stream.Next(); err != nil {
			if !errors.Is(err, reason) {
				t.Fatalf("err = %v, want the cancel cause", err)
			}
			return
		}
	}
}

// A present empty title is sent (TS title !== undefined); an explicit null
// option is treated as absent, as in v0.5.2 (known difference).
func TestR94provgEmbeddingOptionNullAndEmptyTitle(t *testing.T) {
	var bodies []map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&b)
		bodies = append(bodies, b)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, ":embedContent") {
			_, _ = w.Write([]byte(`{"embedding":{"values":[1]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"predictions":[{"embeddings":{"values":[1],"statistics":{"token_count":1}}}]}`))
	}))
	defer srv.Close()
	p := newVertexTestProvider(t, srv.URL)
	for _, key := range []string{"outputDimensionality", "taskType", "title", "autoTruncate"} {
		if _, err := NewEmbeddingModel(p, "text-embedding-005").DoEmbed(context.Background(), "x", &provider.EmbedModelOptions{
			ProviderOptions: map[string]interface{}{"googleVertex": map[string]interface{}{key: nil}},
		}); err != nil {
			t.Errorf("%s: null rejected: %v", key, err)
		}
	}
	bodies = nil
	for _, model := range []string{"text-embedding-005", EmbeddingModelGeminiEmbedding2} {
		if _, err := NewEmbeddingModel(p, model).DoEmbed(context.Background(), "x", &provider.EmbedModelOptions{
			ProviderOptions: map[string]interface{}{"googleVertex": map[string]interface{}{"title": ""}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if len(bodies) != 2 {
		t.Fatalf("bodies = %v", bodies)
	}
	inst := bodies[0]["instances"].([]interface{})[0].(map[string]interface{})
	cfg := bodies[1]["embedContentConfig"].(map[string]interface{})
	if v, ok := inst["title"]; !ok || v != "" {
		t.Errorf("predict title = %#v (present %v)", v, ok)
	}
	if v, ok := cfg["title"]; !ok || v != "" {
		t.Errorf("embedContent title = %#v (present %v)", v, ok)
	}
}

// The "No videos in response" error text is TS JSON.stringify of the
// googleVertexOperationSchema parse result.
func TestR94provgVideoNoVideosErrorText(t *testing.T) {
	m := &VideoModel{prov: newR93provgProvider(t), modelID: "veo-3.0-generate-001"}
	body := []byte(`{"extra":1,"response":{"videos":[],"raiMediaFilteredCount":1.0,"x":2},"error":null,"done":true,"name":""}`)
	var op vertexVideoOperationWire
	if err := json.Unmarshal(body, &op); err != nil {
		t.Fatal(err)
	}
	_, err := m.buildCompletedResult(&op, body, provider.VideoModelV3ResponseInfo{})
	want := `No videos in response. Response: {"name":"","done":true,"error":null,"response":{"videos":[],"raiMediaFilteredCount":1}}`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v", err)
	}
}

// googleVertexOperationSchema reads code and raiMediaFilteredCount with
// z.number(): a fractional value is valid, so the status call succeeds.
func TestR94provgVideoFractionalCounts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"op","done":true,"response":{"videos":[{"gcsUri":"gs://b/v.mp4"}],"raiMediaFilteredCount":1.5}}`))
	}))
	defer srv.Close()
	m := NewVideoModel(newVertexTestProvider(t, srv.URL), "veo-3.0-generate-001")
	res, err := m.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: []byte(`{"operationName":"op"}`)})
	if err != nil || res.Status != provider.VideoOperationStatusCompleted {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}
