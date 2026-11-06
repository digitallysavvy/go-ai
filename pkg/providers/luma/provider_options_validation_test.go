package luma

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func TestLumaImageOptionsAreValidated(t *testing.T) {
	bad := []map[string]interface{}{
		{"referenceType": "pose"},
		{"referenceType": 3},
		{"images": "x"},
		{"images": []interface{}{map[string]interface{}{"weight": 2}}},
		{"images": []interface{}{map[string]interface{}{"id": 5}}},
		{"pollIntervalMillis": "fast"},
		{"maxPollAttempts": true},
	}
	for i, opts := range bad {
		err := validateLumaImageOptions(map[string]interface{}{"luma": opts})
		var ia *providererrors.InvalidArgumentError
		if !errors.As(err, &ia) {
			t.Fatalf("case %d (%v): err = %v, want InvalidArgumentError", i, opts, err)
		}
	}
	good := map[string]interface{}{
		"referenceType": nil, "images": []interface{}{map[string]interface{}{"weight": 0.5, "id": nil}, map[string]interface{}{}},
		"pollIntervalMillis": 10, "maxPollAttempts": nil, "extra": map[string]interface{}{"any": 1},
	}
	if err := validateLumaImageOptions(map[string]interface{}{"luma": good}); err != nil {
		t.Fatalf("valid options rejected: %v", err)
	}
}

func TestLumaGenerationResponseIsValidated(t *testing.T) {
	for _, body := range []string{`{"state":"queued"}`, `{"id":"g","state":"weird"}`, `{"id":"g","state":"completed","assets":{"image":1}}`, `<html>`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		p := New(Config{APIKey: "k", BaseURL: srv.URL})
		_, err := NewImageModel(p, "photon-1").DoGenerate(t.Context(), &provider.ImageGenerateOptions{Prompt: "x"})
		srv.Close()
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.Message != "Invalid JSON response" {
			t.Fatalf("body %q: err = %v, want ProviderError \"Invalid JSON response\"", body, err)
		}
	}
}
