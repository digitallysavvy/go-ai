package google

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// provc CR (TS generate-video.ts useStartStatus): TS GoogleVideoModel has no
// doGenerate, so the model marks itself start/status only for
// ai.GenerateVideo, returning the v0.5.2 poll provider options (0 = core default).
func TestR89provgVideoStartStatusPollDefaults(t *testing.T) {
	var model interface {
		VideoStartStatusPollDefaults(map[string]interface{}) (int, int)
	} = NewVideoModel(New(Config{APIKey: "k"}), "veo-3.0-generate-001")
	if i, to := model.VideoStartStatusPollDefaults(map[string]interface{}{}); i != 0 || to != 0 {
		t.Fatalf("defaults = %d, %d; want 0, 0", i, to)
	}
	i, to := model.VideoStartStatusPollDefaults(map[string]interface{}{"google": map[string]interface{}{"pollIntervalMs": float64(250), "pollTimeoutMs": float64(90000)}})
	if i != 250 || to != 90000 {
		t.Fatalf("defaults = %d, %d; want 250, 90000", i, to)
	}
}

// TS google-language-model.ts doGenerate: response.body is the rawValue of the
// last request (the parsed JSON with every key), like the other providers.
func TestR89provgGoogleRawResponseBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":"hi"}]},"finishReason":"STOP"}],"modelVersion":"gemini-2.5-flash-001","responseId":"r1","extraKey":{"a":1}}`)
	}))
	defer srv.Close()
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("gemini-2.5-flash")
	if err != nil {
		t.Fatal(err)
	}
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	body, ok := res.RawResponse.(map[string]interface{})
	if !ok || body["modelVersion"] != "gemini-2.5-flash-001" || body["extraKey"] == nil {
		t.Fatalf("RawResponse = %#v", res.RawResponse)
	}
}
