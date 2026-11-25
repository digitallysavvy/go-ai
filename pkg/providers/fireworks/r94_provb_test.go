package fireworks

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR94provbAsyncEmptyRequestIDAndEmptyFile: TS asyncSubmitResponseSchema
// is z.string(), so an empty request_id is polled like any other, and
// convertImageModelFileToDataUri turns an empty inline file into a
// (non-empty) data URI that is sent as input_image.
func TestR94provbAsyncEmptyRequestIDAndEmptyFile(t *testing.T) {
	var mu sync.Mutex
	var submitted map[string]interface{}
	var polledID interface{}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]interface{}
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/get_result"):
			mu.Lock()
			polledID = body["id"]
			mu.Unlock()
			_, _ = w.Write([]byte(`{"id":"","status":"Ready","result":{"sample":"` + srv.URL + `/img.png"}}`))
		case r.Method == http.MethodPost:
			mu.Lock()
			submitted = body
			mu.Unlock()
			_, _ = w.Write([]byte(`{"request_id":""}`))
		default:
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{1, 2, 3})
		}
	}))
	defer srv.Close()
	m := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "accounts/fireworks/models/flux-kontext-pro")
	res, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "x",
		Files:  []provider.ImageFile{{Type: "file", MediaType: "image/jpeg", Data: []byte{}}},
	})
	if err != nil {
		t.Fatalf("err = %v, want success", err)
	}
	if string(res.Image) != "\x01\x02\x03" {
		t.Fatalf("image = %v", res.Image)
	}
	mu.Lock()
	defer mu.Unlock()
	if polledID != "" {
		t.Fatalf("polled id = %#v, want \"\"", polledID)
	}
	if submitted["input_image"] != "data:image/jpeg;base64," {
		t.Fatalf("input_image = %#v, want the empty data URI", submitted["input_image"])
	}
}
