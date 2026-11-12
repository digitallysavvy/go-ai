package bfl

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// r92provcSubmitBody runs one latest-API image call and returns the submit
// request body (the poll answers Error so the call ends there).
func r92provcSubmitBody(t *testing.T, opts *provider.ImageGenerateOptions) map[string]interface{} {
	t.Helper()
	var mu sync.Mutex
	var body map[string]interface{}
	var serverURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/flux-3-image-klein", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		_ = json.Unmarshal(raw, &body)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "req-1", "polling_url": serverURL + "/get_result"})
	})
	mux.HandleFunc("/get_result", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"Error"}`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	serverURL = server.URL
	model := NewImageModel(New(Config{APIKey: "k", BaseURL: server.URL}), "flux-3-image-klein")
	_, _ = model.DoGenerate(context.Background(), opts)
	mu.Lock()
	defer mu.Unlock()
	return body
}

// TestR92provcLatestSizeWithTypedOptions (r92 provc-05, typed-option
// ruling): TS spreads `{width, height}` from size under the caller's
// blackForestLabs options; a Go-typed options literal (map[string]string)
// must get the same width and height as the decoded JSON form.
func TestR92provcLatestSizeWithTypedOptions(t *testing.T) {
	for name, options := range map[string]interface{}{
		"map[string]interface{}": map[string]interface{}{"outputFormat": "png"},
		"map[string]string":      map[string]string{"outputFormat": "png"},
	} {
		body := r92provcSubmitBody(t, &provider.ImageGenerateOptions{
			Prompt:          "p",
			Size:            "1024x768",
			ProviderOptions: map[string]interface{}{"blackForestLabs": options},
		})
		if body["width"] != float64(1024) || body["height"] != float64(768) || body["output_format"] != "png" || body["aspect_ratio"] != nil {
			t.Errorf("%s: body = %v", name, body)
		}
	}
}

// TestR92provcSizeAspectRatioJSText (r92 provc-10): the derived aspect
// ratio is a template literal of Math.round results (JS number text).
func TestR92provcSizeAspectRatioJSText(t *testing.T) {
	if got := bflSizeAspectRatio("1e21x1"); got != "1e+21:1" {
		t.Errorf("aspect ratio = %q", got)
	}
	if got := bflSizeAspectRatio("1920x1080"); got != "16:9" {
		t.Errorf("aspect ratio = %q", got)
	}
}
