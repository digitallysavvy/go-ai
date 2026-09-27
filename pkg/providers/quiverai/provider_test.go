package quiverai

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func TestProviderFactories(t *testing.T) {
	p := New(Config{APIKey: "k"})
	if p.Name() != "quiverai" {
		t.Fatalf("Name = %q", p.Name())
	}
	model, err := p.ImageModel("")
	if err != nil {
		t.Fatalf("ImageModel: %v", err)
	}
	if model.Provider() != "quiverai.image" || model.ModelID() != ModelArrow11 {
		t.Fatalf("unexpected model metadata: provider=%q id=%q", model.Provider(), model.ModelID())
	}
	if _, err := p.LanguageModel("x"); err == nil {
		t.Fatal("LanguageModel should be unsupported")
	}
}

func TestImageModelGenerateRequestAndResponse(t *testing.T) {
	var seenBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/svgs/generations" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer k" {
			t.Fatalf("Authorization = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&seenBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_, _ = w.Write([]byte(`{"id":"svg-1","created":1700000000,"data":[{"svg":"<svg/>","mime_type":"image/svg+xml"}],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "k", BaseURL: server.URL})
	model := NewImageModel(p, ModelArrow11Max)
	n := 2
	result, err := model.DoGenerate(t.Context(), &provider.ImageGenerateOptions{
		Prompt: "logo",
		N:      &n,
		Files:  []provider.ImageFile{{Data: []byte("ref"), MediaType: "image/png"}},
		ProviderOptions: map[string]interface{}{
			"quiverai": map[string]interface{}{
				"instructions": "flat",
				"temperature":  0.3,
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if seenBody["model"] != ModelArrow11Max || seenBody["n"] != float64(2) || seenBody["prompt"] != "logo" {
		t.Fatalf("unexpected request body: %#v", seenBody)
	}
	if refs, ok := seenBody["references"].([]interface{}); !ok || len(refs) != 1 {
		t.Fatalf("references missing: %#v", seenBody)
	}
	if string(result.Image) != "<svg/>" || result.MimeType != "image/svg+xml" {
		t.Fatalf("unexpected image result: mime=%q image=%q", result.MimeType, string(result.Image))
	}
	if result.Response == nil || result.Response.ID != "svg-1" || result.Response.ModelID != ModelArrow11Max {
		t.Fatalf("response metadata missing: %#v", result.Response)
	}
	if result.Usage.InputTokens != 1 || result.Usage.OutputTokens != 2 || result.Usage.TotalTokens != 3 {
		t.Fatalf("usage = %#v", result.Usage)
	}
}

func TestImageModelVectorizeRequest(t *testing.T) {
	var seenBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/svgs/vectorizations" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&seenBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_, _ = w.Write([]byte(`{"id":"svg-2","created":1700000000,"data":[{"svg":"<svg/>","mime_type":"image/svg+xml"}]}`))
	}))
	defer server.Close()

	model := NewImageModel(New(Config{APIKey: "k", BaseURL: server.URL}), ModelArrow11)
	_, err := model.DoGenerate(t.Context(), &provider.ImageGenerateOptions{
		Files: []provider.ImageFile{{URL: "https://example.test/image.png", Type: "url"}},
		ProviderOptions: map[string]interface{}{
			"quiverai": map[string]interface{}{
				"operation":  "vectorize",
				"autoCrop":   true,
				"targetSize": 512,
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if seenBody["auto_crop"] != true || seenBody["target_size"] != float64(512) {
		t.Fatalf("vectorize options missing: %#v", seenBody)
	}
	if _, ok := seenBody["image"].(map[string]interface{}); !ok {
		t.Fatalf("image reference missing: %#v", seenBody)
	}
}

func TestImageModelGenerateRejectsWhitespacePrompt(t *testing.T) {
	model := NewImageModel(New(Config{APIKey: "k"}), ModelArrow11)
	_, err := model.DoGenerate(t.Context(), &provider.ImageGenerateOptions{Prompt: " \t\n "})
	if err == nil {
		t.Fatal("expected whitespace-only prompt to be rejected")
	}
}

func TestImageModelRetryableProviderErrors(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusBadGateway} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"code":"temporary","message":"try again"}`))
		}))

		model := NewImageModel(New(Config{APIKey: "k", BaseURL: server.URL}), ModelArrow11)
		_, err := model.DoGenerate(t.Context(), &provider.ImageGenerateOptions{Prompt: "logo"})
		server.Close()
		if err == nil {
			t.Fatalf("status %d: expected error", status)
		}
		var providerErr *providererrors.ProviderError
		if !errors.As(err, &providerErr) {
			t.Fatalf("status %d: error = %T, want ProviderError", status, err)
		}
		if !providerErr.IsRetryable() {
			t.Fatalf("status %d should be retryable", status)
		}
	}
}

func TestArrow2ModelIDConstants(t *testing.T) {
	if ModelArrow2 != "arrow-2" || ModelArrow2Telos != "arrow-2-telos" {
		t.Fatalf("unexpected arrow-2 model IDs: %q, %q", ModelArrow2, ModelArrow2Telos)
	}
}

func TestImageModelEditRequestAndResponse(t *testing.T) {
	var seenBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/svgs/edits" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&seenBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_, _ = w.Write([]byte(`{"id":"svg-edit-1","created":1713374520,"data":[{"svg":"<svg viewBox=\"0 0 10 10\"><rect width=\"10\" height=\"10\" fill=\"blue\"/></svg>","mime_type":"image/svg+xml"}],"usage":{"total_tokens":25,"input_tokens":14,"output_tokens":11},"credits":3}`))
	}))
	defer server.Close()

	model := NewImageModel(New(Config{APIKey: "k", BaseURL: server.URL}), ModelArrow2)
	sourceSVG := `<svg><rect width="10" height="10"/></svg>`
	result, err := model.DoGenerate(t.Context(), &provider.ImageGenerateOptions{
		Prompt: "Make the icon blue.",
		Files:  []provider.ImageFile{{Data: []byte(sourceSVG), MediaType: "image/svg+xml"}},
		ProviderOptions: map[string]interface{}{
			"quiverai": map[string]interface{}{
				"operation":       "edit",
				"reasoningEffort": "high",
				"maxReviewSteps":  2,
				"maxOutputTokens": 1000,
				"referenceImages": []interface{}{
					map[string]interface{}{"url": "https://example.test/ref.png"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}

	if seenBody["model"] != ModelArrow2 || seenBody["prompt"] != "Make the icon blue." {
		t.Fatalf("unexpected request body: %#v", seenBody)
	}
	if _, ok := seenBody["n"]; ok {
		t.Fatalf("edit request body must not include n: %#v", seenBody)
	}
	svgSource, ok := seenBody["svg_source"].(map[string]interface{})
	if !ok || svgSource["base64"] == nil {
		t.Fatalf("svg_source missing or malformed: %#v", seenBody["svg_source"])
	}
	if got := seenBody["reasoning_effort"]; got != "high" {
		t.Fatalf("reasoning_effort = %v, want high", got)
	}
	if got := seenBody["max_review_steps"]; got != float64(2) {
		t.Fatalf("max_review_steps = %v, want 2", got)
	}
	refImages, ok := seenBody["reference_images"].([]interface{})
	if !ok || len(refImages) != 1 {
		t.Fatalf("reference_images missing: %#v", seenBody["reference_images"])
	}
	settings, ok := seenBody["settings"].(map[string]interface{})
	if !ok || settings["max_output_tokens"] != float64(1000) {
		t.Fatalf("settings missing max_output_tokens: %#v", seenBody["settings"])
	}

	if len(result.Images) != 1 || string(result.Image) == "" {
		t.Fatalf("unexpected image result: %#v", result)
	}
	meta, ok := result.ProviderMetadata["quiverai"].(map[string]interface{})
	if !ok {
		t.Fatalf("providerMetadata.quiverai missing: %#v", result.ProviderMetadata)
	}
	if meta["credits"] != 3 {
		t.Fatalf("credits = %v, want 3 (fixed-credit billing metadata)", meta["credits"])
	}
}

func TestImageModelEditRejectsMalformedSVGBeforeNetworking(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer server.Close()

	model := NewImageModel(New(Config{APIKey: "k", BaseURL: server.URL}), ModelArrow2)
	_, err := model.DoGenerate(t.Context(), &provider.ImageGenerateOptions{
		Prompt: "Make the icon blue.",
		Files:  []provider.ImageFile{{Data: []byte("<svg><g></svg>"), MediaType: "image/svg+xml"}},
		ProviderOptions: map[string]interface{}{
			"quiverai": map[string]interface{}{"operation": "edit"},
		},
	})
	if err == nil {
		t.Fatal("expected malformed SVG to be rejected")
	}
	var argErr *providererrors.InvalidArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("error = %T, want InvalidArgumentError", err)
	}
	if called {
		t.Fatal("malformed SVG should be rejected before any network call")
	}
}

func TestImageModelEditRejectsNonArrow2Model(t *testing.T) {
	model := NewImageModel(New(Config{APIKey: "k"}), ModelArrow11)
	_, err := model.DoGenerate(t.Context(), &provider.ImageGenerateOptions{
		Prompt: "Make the icon blue.",
		Files:  []provider.ImageFile{{Data: []byte(`<svg><rect/></svg>`), MediaType: "image/svg+xml"}},
		ProviderOptions: map[string]interface{}{
			"quiverai": map[string]interface{}{"operation": "edit"},
		},
	})
	if err == nil {
		t.Fatal("expected edit on non-arrow-2 model to be rejected")
	}
}

func TestImageModelEditRejectsMultipleOutputs(t *testing.T) {
	n := 2
	model := NewImageModel(New(Config{APIKey: "k"}), ModelArrow2)
	_, err := model.DoGenerate(t.Context(), &provider.ImageGenerateOptions{
		Prompt: "Make the icon blue.",
		N:      &n,
		Files:  []provider.ImageFile{{Data: []byte(`<svg><rect/></svg>`), MediaType: "image/svg+xml"}},
		ProviderOptions: map[string]interface{}{
			"quiverai": map[string]interface{}{"operation": "edit"},
		},
	})
	if err == nil {
		t.Fatal("expected multi-output edit request to be rejected")
	}
}

func TestImageModelAnimateRequestAndResponse(t *testing.T) {
	var seenBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/svgs/animations" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&seenBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_, _ = w.Write([]byte(`{"id":"svg-animation-1","created":1713374520,"data":[{"svg":"<svg><circle r=\"4\"/></svg>","mime_type":"image/svg+xml","loop_period_ms":1200,"opening_animation_ms":null}],"usage":{"total_tokens":24,"input_tokens":13,"output_tokens":11}}`))
	}))
	defer server.Close()

	model := NewImageModel(New(Config{APIKey: "k", BaseURL: server.URL}), ModelArrow2Telos)
	sourceSVG := `<svg width="10" height="10"><circle r="4"/></svg>`
	result, err := model.DoGenerate(t.Context(), &provider.ImageGenerateOptions{
		Prompt: "Make it pulse.",
		Files:  []provider.ImageFile{{Data: []byte(sourceSVG), MediaType: "image/svg+xml"}},
		ProviderOptions: map[string]interface{}{
			"quiverai": map[string]interface{}{"operation": "animate"},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if seenBody["model"] != ModelArrow2Telos || seenBody["prompt"] != "Make it pulse." {
		t.Fatalf("unexpected request body: %#v", seenBody)
	}
	if _, ok := seenBody["n"]; ok {
		t.Fatalf("animate request body must not include n: %#v", seenBody)
	}
	svgSource, ok := seenBody["svg_source"].(map[string]interface{})
	if !ok || svgSource["base64"] == nil {
		t.Fatalf("svg_source missing or malformed: %#v", seenBody["svg_source"])
	}

	meta, ok := result.ProviderMetadata["quiverai"].(map[string]interface{})
	if !ok {
		t.Fatalf("providerMetadata.quiverai missing: %#v", result.ProviderMetadata)
	}
	images, ok := meta["images"].([]map[string]interface{})
	if !ok || len(images) != 1 {
		t.Fatalf("images metadata missing: %#v", meta["images"])
	}
	if images[0]["loopPeriodMs"] != 1200 {
		t.Fatalf("loopPeriodMs = %v, want 1200", images[0]["loopPeriodMs"])
	}
	if _, hasOpening := images[0]["openingAnimationMs"]; hasOpening {
		t.Fatalf("openingAnimationMs should be omitted when null, got %v", images[0]["openingAnimationMs"])
	}
}

func TestImageModelAnimateRejectsNonSVGData(t *testing.T) {
	model := NewImageModel(New(Config{APIKey: "k"}), ModelArrow2)
	_, err := model.DoGenerate(t.Context(), &provider.ImageGenerateOptions{
		Files: []provider.ImageFile{{Data: []byte("not an svg document"), MediaType: "image/svg+xml"}},
		ProviderOptions: map[string]interface{}{
			"quiverai": map[string]interface{}{"operation": "animate"},
		},
	})
	if err == nil {
		t.Fatal("expected non-SVG animate source to be rejected")
	}
}

func TestImageModelAnimateRejectsOnNonArrow2Model(t *testing.T) {
	model := NewImageModel(New(Config{APIKey: "k"}), ModelArrow11)
	_, err := model.DoGenerate(t.Context(), &provider.ImageGenerateOptions{
		Files: []provider.ImageFile{{Data: []byte(`<svg><rect/></svg>`), MediaType: "image/svg+xml"}},
		ProviderOptions: map[string]interface{}{
			"quiverai": map[string]interface{}{"operation": "animate"},
		},
	})
	if err == nil {
		t.Fatal("expected animate on non-arrow-2 model to be rejected")
	}
}

func TestImageModelArrow2MaxOutputTokensLimit(t *testing.T) {
	model := NewImageModel(New(Config{APIKey: "k"}), ModelArrow2)
	_, err := model.DoGenerate(t.Context(), &provider.ImageGenerateOptions{
		Prompt: "logo",
		ProviderOptions: map[string]interface{}{
			"quiverai": map[string]interface{}{"maxOutputTokens": 70000},
		},
	})
	if err == nil {
		t.Fatal("expected arrow-2 maxOutputTokens over 65536 to be rejected")
	}

	// Non-Arrow-2 models retain the legacy 131072 ceiling and are unaffected
	// by the tighter Arrow-2 check.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"svg-3","created":1700000000,"data":[{"svg":"<svg/>","mime_type":"image/svg+xml"}]}`))
	}))
	defer server.Close()
	legacyModel := NewImageModel(New(Config{APIKey: "k", BaseURL: server.URL}), ModelArrow11)
	_, err = legacyModel.DoGenerate(t.Context(), &provider.ImageGenerateOptions{
		Prompt: "logo",
		ProviderOptions: map[string]interface{}{
			"quiverai": map[string]interface{}{"maxOutputTokens": 70000},
		},
	})
	if err != nil {
		t.Fatalf("legacy model should accept maxOutputTokens=70000, got error: %v", err)
	}
}

func TestImageModelReasoningEffortValidation(t *testing.T) {
	model := NewImageModel(New(Config{APIKey: "k"}), ModelArrow11)
	_, err := model.DoGenerate(t.Context(), &provider.ImageGenerateOptions{
		Prompt: "logo",
		ProviderOptions: map[string]interface{}{
			"quiverai": map[string]interface{}{"reasoningEffort": "bogus"},
		},
	})
	if err == nil {
		t.Fatal("expected invalid reasoningEffort to be rejected")
	}

	var seenBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		_, _ = w.Write([]byte(`{"id":"svg-4","created":1700000000,"data":[{"svg":"<svg/>","mime_type":"image/svg+xml"}]}`))
	}))
	defer server.Close()
	model2 := NewImageModel(New(Config{APIKey: "k", BaseURL: server.URL}), ModelArrow11)
	_, err = model2.DoGenerate(t.Context(), &provider.ImageGenerateOptions{
		Prompt: "logo",
		ProviderOptions: map[string]interface{}{
			"quiverai": map[string]interface{}{"reasoningEffort": "xhigh"},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if seenBody["reasoning_effort"] != "xhigh" {
		t.Fatalf("reasoning_effort = %v, want xhigh", seenBody["reasoning_effort"])
	}
}

func TestImageModelAttributesViewBoxForwarded(t *testing.T) {
	var seenBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		_, _ = w.Write([]byte(`{"id":"svg-5","created":1700000000,"data":[{"svg":"<svg/>","mime_type":"image/svg+xml"}]}`))
	}))
	defer server.Close()

	model := NewImageModel(New(Config{APIKey: "k", BaseURL: server.URL}), ModelArrow11)
	_, err := model.DoGenerate(t.Context(), &provider.ImageGenerateOptions{
		Prompt: "logo",
		ProviderOptions: map[string]interface{}{
			"quiverai": map[string]interface{}{
				"attributes": map[string]interface{}{
					"viewBox": map[string]interface{}{
						"minX": 0, "minY": 0, "width": 100, "height": 50,
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	attrs, ok := seenBody["attributes"].(map[string]interface{})
	if !ok {
		t.Fatalf("attributes missing: %#v", seenBody["attributes"])
	}
	viewBox, ok := attrs["viewBox"].(map[string]interface{})
	if !ok || viewBox["width"] != float64(100) || viewBox["height"] != float64(50) {
		t.Fatalf("viewBox mismatch: %#v", attrs["viewBox"])
	}
}

func TestImageModelVectorizeRejectsMultipleOutputs(t *testing.T) {
	n := 2
	model := NewImageModel(New(Config{APIKey: "k"}), ModelArrow11)
	_, err := model.DoGenerate(t.Context(), &provider.ImageGenerateOptions{
		N:     &n,
		Files: []provider.ImageFile{{URL: "https://example.test/image.png", Type: "url"}},
		ProviderOptions: map[string]interface{}{
			"quiverai": map[string]interface{}{"operation": "vectorize"},
		},
	})
	if err == nil {
		t.Fatal("expected multi-output vectorize request to be rejected")
	}
}

func TestImageModelEditOnlyOptionsRejectedForOtherOperations(t *testing.T) {
	model := NewImageModel(New(Config{APIKey: "k"}), ModelArrow2)
	_, err := model.DoGenerate(t.Context(), &provider.ImageGenerateOptions{
		Prompt: "logo",
		ProviderOptions: map[string]interface{}{
			"quiverai": map[string]interface{}{
				"maxReviewSteps": 1,
			},
		},
	})
	if err == nil {
		t.Fatal("expected edit-only option maxReviewSteps to be rejected for generate")
	}
}
