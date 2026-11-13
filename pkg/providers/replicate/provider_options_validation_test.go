package replicate

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func TestReplicateOptionsAreValidatedLikeTheTSSchemas(t *testing.T) {
	badImage := []map[string]interface{}{
		{"maxWaitTimeInSeconds": 0},
		{"maxWaitTimeInSeconds": "5"},
		{"pollIntervalMillis": 1.5},
		{"pollIntervalMillis": -1},
		{"maxPollAttempts": 0},
		{"output_format": "gif"},
		{"output_quality": 0},
		{"output_quality": 101},
		{"strength": 2},
		{"negative_prompt": 3},
		{"guidance_scale": "high"},
	}
	for i, opts := range badImage {
		err := validateImageOptions(map[string]interface{}{"replicate": opts})
		var ia *providererrors.InvalidArgumentError
		if !errors.As(err, &ia) {
			t.Fatalf("image case %d (%v): err = %v, want InvalidArgumentError", i, opts, err)
		}
	}
	if err := validateImageOptions(map[string]interface{}{"replicate": map[string]interface{}{
		"maxWaitTimeInSeconds": 10, "pollIntervalMillis": 100, "output_format": nil, "strength": 0, "custom_model_key": []interface{}{1},
	}}); err != nil {
		t.Fatalf("valid image options rejected: %v", err)
	}

	badVideo := []map[string]interface{}{
		{"pollIntervalMs": 0}, {"pollTimeoutMs": "x"}, {"video_length": 3}, {"prompt_optimizer": "yes"}, {"frames_per_second": "24"},
	}
	for i, opts := range badVideo {
		if err := validateVideoOptions(map[string]interface{}{"replicate": opts}); err == nil {
			t.Fatalf("video case %d (%v) accepted", i, opts)
		}
	}
}

func TestReplicatePredictionResponsesAreValidated(t *testing.T) {
	for _, body := range []string{`{"id":"p","status":"starting"}`, `{"id":"p","status":"weird","urls":{"get":"x"}}`, `<html>`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		p := New(Config{APIKey: "k", BaseURL: srv.URL})
		_, err := NewImageModel(p, "owner/model").DoGenerate(t.Context(), &provider.ImageGenerateOptions{Prompt: "x"})
		srv.Close()
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.Message != "Invalid JSON response" {
			t.Fatalf("body %q: err = %v, want ProviderError \"Invalid JSON response\"", body, err)
		}
	}
}
