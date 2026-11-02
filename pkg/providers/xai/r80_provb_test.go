package xai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r80provbJSONServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TS z.number() (files bytes, image usage.cost_in_usd_ticks) and the
// unvalidated realtime client secret accept a fractional number; Go decoded
// an integer field with encoding/json and failed the call. Go now truncates
// it (known difference "Fractional counts").
func TestR80provbXAIFractionalNumbersInResponses(t *testing.T) {
	t.Run("files metadata bytes", func(t *testing.T) {
		srv := r80provbJSONServer(t, `{"id":"file-1","object":"file","bytes":2048.75,"created_at":1700000000,"filename":"a.jsonl","purpose":"batch"}`)
		res, err := New(Config{APIKey: "k", BaseURL: srv.URL}).Files().(provider.FileMetadataGetter).GetFileMetadata(t.Context(), provider.FileMetadataOptions{File: types.ProviderReference{"xai": "file-1"}})
		if err != nil {
			t.Fatal(err)
		}
		if res.ByteSize == nil || *res.ByteSize != 2048 {
			t.Fatalf("ByteSize = %v", res.ByteSize)
		}
	})
	t.Run("image cost ticks", func(t *testing.T) {
		srv := r80provbJSONServer(t, `{"data":[{"b64_json":"aGVsbG8="}],"usage":{"cost_in_usd_ticks":42.5}}`)
		res, err := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), ModelGrokImagineImage).DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
		if err != nil {
			t.Fatal(err)
		}
		meta, _ := res.ProviderMetadata["xai"].(XAIImageMetadata)
		if meta.CostInUsdTicks == nil || *meta.CostInUsdTicks != 42 {
			t.Fatalf("metadata = %+v", res.ProviderMetadata)
		}
	})
	t.Run("realtime client secret expires_at", func(t *testing.T) {
		srv := r80provbJSONServer(t, `{"value":"tok","expires_at":1700000000.5}`)
		res, err := NewRealtimeModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "grok-realtime").DoCreateClientSecret(t.Context(), provider.ClientSecretOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if res.ExpiresAt == nil || *res.ExpiresAt != 1700000000 {
			t.Fatalf("result = %+v", res)
		}
	})
}
