package quiverai

import (
	"context"
	"encoding/json"
	"errors"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS spreads loop_period_ms / opening_animation_ms when `!== undefined`, so a
// null in the response is metadata null (absent key is omitted), and a null
// presencePenalty (nullable) is sent as null.
func TestR33_NullMetadataAndNullPresencePenalty(t *testing.T) {
	var posted map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &posted)
		_, _ = w.Write([]byte(`{"id":"i","created":1,"data":[{"svg":"<svg/>","mime_type":"image/svg+xml","loop_period_ms":null,"opening_animation_ms":5},{"svg":"<svg/>","mime_type":"image/svg+xml"}]}`))
	}))
	defer srv.Close()
	m := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), ModelArrow2)
	n := 2
	res, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "p", N: &n,
		ProviderOptions: map[string]interface{}{"quiverai": map[string]interface{}{"presencePenalty": nil}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := posted["presence_penalty"]; !ok || v != nil {
		t.Errorf("presence_penalty = %v (present %v)", v, ok)
	}
	imgs := res.ProviderMetadata["quiverai"].(map[string]interface{})["images"].([]map[string]interface{})
	if v, ok := imgs[0]["loopPeriodMs"]; !ok || v != nil {
		t.Errorf("loopPeriodMs = %v (present %v)", v, ok)
	}
	if imgs[0]["openingAnimationMs"] != 5 {
		t.Errorf("openingAnimationMs = %v", imgs[0]["openingAnimationMs"])
	}
	if _, ok := imgs[1]["loopPeriodMs"]; ok {
		t.Errorf("absent loop_period_ms reported: %v", imgs[1])
	}
}

// JS String.prototype.trim strips U+FEFF but not U+0085, unlike Go's TrimSpace.
func TestR33_PromptTrimIsJSTrim(t *testing.T) {
	m := NewImageModel(New(Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"}), ModelArrow2)
	_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "\ufeff \u00a0"})
	var invalid *providererrors.InvalidArgumentError
	if !errors.As(err, &invalid) || invalid.Field != "prompt" {
		t.Fatalf("BOM/NBSP-only prompt: got %v", err)
	}
	// U+0085 is not JS whitespace, so the prompt is non-empty and the request is attempted.
	_, err = m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "\u0085"})
	if errors.As(err, &invalid) {
		t.Fatalf("NEL-only prompt was rejected as empty: %v", err)
	}
}
