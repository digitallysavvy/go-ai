package luma

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS loops `i < maxPollAttempts`, so a fractional 2.5 allows three attempts.
func TestR35_FractionalMaxPollAttempts(t *testing.T) {
	var captured map[string]interface{}
	server := newLumaTestServer(t, []string{"queued", "dreaming", "completed"}, &captured)
	defer server.Close()
	model := NewImageModel(New(Config{APIKey: "k", BaseURL: server.URL}), ModelPhoton1)
	_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "a cat",
		ProviderOptions: map[string]interface{}{
			"luma": map[string]interface{}{"pollIntervalMillis": 1.5, "maxPollAttempts": 2.5},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
}
