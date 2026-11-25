package bfl

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestR92provcImageBodyKeyOrder (r92 provc-11): the submit body follows the
// TS getArgs literal order (JSON.stringify), not Go's sorted map keys.
func TestR92provcImageBodyKeyOrder(t *testing.T) {
	for _, tc := range []struct {
		modelID string
		opts    *provider.ImageGenerateOptions
		want    string
	}{
		{"flux-pro-1.1", &provider.ImageGenerateOptions{Prompt: "p", Size: "1024x768", ProviderOptions: map[string]interface{}{
			"blackForestLabs": map[string]interface{}{"steps": 20, "outputFormat": "png", "guidance": 3},
		}}, `{"prompt":"p","aspect_ratio":"4:3","width":1024,"height":768,"steps":20,"guidance":3,"output_format":"png"}`},
		{"flux-3-image-klein", &provider.ImageGenerateOptions{Prompt: "p", ProviderOptions: map[string]interface{}{
			"blackForestLabs": map[string]interface{}{"steps": 4, "outputFormat": "png", "safetyTolerance": 2},
		}}, `{"prompt":"p","safety_tolerance":2,"steps":4,"output_format":"png"}`},
	} {
		var mu sync.Mutex
		var raw string
		var serverURL string
		mux := http.NewServeMux()
		mux.HandleFunc("/"+tc.modelID, func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			mu.Lock()
			raw = string(b)
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "req-1", "polling_url": serverURL + "/get_result"})
		})
		mux.HandleFunc("/get_result", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"status":"Error"}`)
		})
		server := httptest.NewServer(mux)
		serverURL = server.URL
		_, _ = NewImageModel(New(Config{APIKey: "k", BaseURL: server.URL}), tc.modelID).DoGenerate(context.Background(), tc.opts)
		server.Close()
		mu.Lock()
		if raw != tc.want {
			t.Errorf("%s body = %s\nwant %s", tc.modelID, raw, tc.want)
		}
		mu.Unlock()
	}
}

// TestR92provcVideoDurationNonFinite (r92 provc-12): TS rounds any
// non-integer duration (NaN and Infinity included, with a warning); Infinity
// is then clamped to 20 and NaN is sent as JSON null.
func TestR92provcVideoDurationNonFinite(t *testing.T) {
	m := NewVideoModel(New(Config{APIKey: "k"}), VideoModelFlux3Video)
	inf, nan := math.Inf(1), math.NaN()
	body, warnings, err := m.buildVideoArgs(&provider.VideoModelV3CallOptions{Prompt: "p", Duration: &inf})
	if err != nil || body["duration"] != float64(20) || len(warnings) != 2 || warnings[0].Details != "FLUX 3 video requires a whole number of seconds. The requested duration of Infinity was rounded to Infinity." {
		t.Errorf("infinity: body=%v warnings=%+v err=%v", body, warnings, err)
	}
	body, warnings, err = m.buildVideoArgs(&provider.VideoModelV3CallOptions{Prompt: "p", Duration: &nan})
	if err != nil || len(warnings) != 1 {
		t.Fatalf("nan: warnings=%+v err=%v", warnings, err)
	}
	if v, ok := body["duration"]; !ok || v != nil {
		t.Errorf("nan duration = %#v (present %v), want JSON null", v, ok)
	}
	if got, err := json.Marshal(orderedVideoBody(body)); err != nil || !strings.Contains(string(got), `"duration":null`) {
		t.Errorf("nan body = %s err=%v", got, err)
	}
}
