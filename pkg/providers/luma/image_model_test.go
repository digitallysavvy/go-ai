package luma

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func newTestImageModel(t *testing.T, handler http.HandlerFunc) (*ImageModel, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	return NewImageModel(prov, ModelPhoton1), server
}

// TestImageModel_ProviderInfo mirrors TS "should expose correct provider and model information".
func TestImageModel_ProviderInfo(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	m := NewImageModel(prov, ModelPhoton1)
	if m.Provider() != "luma.image" {
		t.Errorf("Provider() = %q", m.Provider())
	}
	if m.ModelID() != ModelPhoton1 {
		t.Errorf("ModelID() = %q", m.ModelID())
	}
	if m.SpecificationVersion() != "v4" {
		t.Errorf("SpecificationVersion() = %q", m.SpecificationVersion())
	}
	if m.MaxImagesPerCall() != 1 {
		t.Errorf("MaxImagesPerCall() = %d, want 1", m.MaxImagesPerCall())
	}
}

// newLumaTestServer builds a mux that handles generation creation
// (POST /dream-machine/v1/generations/image), status polling
// (GET /dream-machine/v1/generations/{id}), and the image download itself,
// all rooted at the same test server so the download stays "same origin"
// (trusted) with the configured base URL.
func newLumaTestServer(t *testing.T, states []string, captured *map[string]interface{}) *httptest.Server {
	t.Helper()
	// serverURL is set right after the server starts; the handler only
	// reads it at request time, once the server (and thus its URL) exists.
	var serverURL string
	pollCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/dream-machine/v1/generations/image", func(w http.ResponseWriter, r *http.Request) {
		if captured != nil {
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			*captured = body
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "gen-1", "state": "queued"})
	})
	mux.HandleFunc("/dream-machine/v1/generations/gen-1", func(w http.ResponseWriter, r *http.Request) {
		state := states[pollCount]
		if pollCount < len(states)-1 {
			pollCount++
		}
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]interface{}{"id": "gen-1", "state": state}
		if state == "completed" {
			resp["assets"] = map[string]interface{}{"image": serverURL + "/image.jpg"}
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/image.jpg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte{0xFF, 0xD8, 0xFF})
	})
	server := httptest.NewServer(mux)
	serverURL = server.URL
	return server
}

// TestDoGenerate_T2I mirrors TS "should pass the correct parameters including aspect ratio"
// and "should call the correct urls in sequence".
func TestDoGenerate_T2I(t *testing.T) {
	var captured map[string]interface{}
	server := newLumaTestServer(t, []string{"queued", "dreaming", "completed"}, &captured)
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewImageModel(prov, ModelPhoton1)

	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt:      "a cat",
		AspectRatio: "16:9",
		ProviderOptions: map[string]interface{}{
			"luma": map[string]interface{}{"pollIntervalMillis": 5},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	if captured["prompt"] != "a cat" || captured["aspect_ratio"] != "16:9" || captured["model"] != ModelPhoton1 {
		t.Errorf("captured body = %#v", captured)
	}
	// providerOptions.{pollIntervalMillis,maxPollAttempts} must never leak
	// into the generation request body.
	if _, exists := captured["pollIntervalMillis"]; exists {
		t.Error("pollIntervalMillis leaked into request body")
	}
	if len(result.Image) == 0 || result.URL != server.URL+"/image.jpg" {
		t.Errorf("result = %+v", result)
	}
}

// TestDoGenerate_Warnings mirrors TS "should return warnings for unsupported parameters".
func TestDoGenerate_Warnings(t *testing.T) {
	server := newLumaTestServer(t, []string{"completed"}, nil)
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewImageModel(prov, ModelPhoton1)

	seed := 42
	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "x", Seed: &seed, Size: "512x512",
	})
	if err != nil {
		t.Fatalf("DoGenerate() error: %v", err)
	}
	features := map[string]bool{}
	for _, w := range result.Warnings {
		features[w.Feature] = true
	}
	if !features["seed"] || !features["size"] {
		t.Errorf("warnings = %+v", result.Warnings)
	}
}

// TestDoGenerate_FailedState mirrors TS "should handle failed generation state".
func TestDoGenerate_FailedState(t *testing.T) {
	server := newLumaTestServer(t, []string{"failed"}, nil)
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewImageModel(prov, ModelPhoton1)

	_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x"})
	if err == nil {
		t.Fatal("expected an error for a failed generation")
	}
	if !strings.Contains(err.Error(), "failed") {
		t.Errorf("expected 'failed' in error, got: %v", err)
	}
}

// TestDoGenerate_APIError mirrors TS "should handle API errors".
func TestDoGenerate_APIError(t *testing.T) {
	model, server := newTestImageModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"detail": []map[string]interface{}{{"type": "value_error", "loc": []string{"prompt"}, "msg": "prompt is required"}},
		})
	})
	defer server.Close()

	_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{})
	if err == nil {
		t.Fatal("expected an API error")
	}
	if !strings.Contains(err.Error(), "prompt is required") {
		t.Errorf("expected the Luma error detail message, got: %v", err)
	}
}

// TestGetEditingOptions_ImageReference mirrors TS "should send image by default when URL file is provided".
func TestGetEditingOptions_ImageReference(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewImageModel(prov, ModelPhoton1)

	opts, err := model.getEditingOptions([]provider.ImageFile{{Type: "url", URL: "https://example.com/ref.jpg"}}, nil, nil)
	if err != nil {
		t.Fatalf("getEditingOptions() error: %v", err)
	}
	images, ok := opts["image"].([]map[string]interface{})
	if !ok || len(images) != 1 || images[0]["url"] != "https://example.com/ref.jpg" || images[0]["weight"] != 0.85 {
		t.Errorf("image = %#v", opts["image"])
	}
}

// TestGetEditingOptions_ModifyImage mirrors TS "should send modify_image when referenceType is set".
func TestGetEditingOptions_ModifyImage(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewImageModel(prov, ModelPhoton1)
	rt := ReferenceTypeModifyImage

	opts, err := model.getEditingOptions([]provider.ImageFile{{Type: "url", URL: "https://example.com/src.jpg"}}, nil, &ImageModelOptions{ReferenceType: &rt})
	if err != nil {
		t.Fatalf("getEditingOptions() error: %v", err)
	}
	modify, ok := opts["modify_image"].(map[string]interface{})
	if !ok || modify["url"] != "https://example.com/src.jpg" || modify["weight"] != 1.0 {
		t.Errorf("modify_image = %#v", opts["modify_image"])
	}
}

// TestGetEditingOptions_Style mirrors TS "should send style when referenceType is style".
func TestGetEditingOptions_Style(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewImageModel(prov, ModelPhoton1)
	rt := ReferenceTypeStyle

	opts, err := model.getEditingOptions([]provider.ImageFile{{Type: "url", URL: "https://example.com/style.jpg"}}, nil, &ImageModelOptions{ReferenceType: &rt})
	if err != nil {
		t.Fatalf("getEditingOptions() error: %v", err)
	}
	styles, ok := opts["style"].([]map[string]interface{})
	if !ok || len(styles) != 1 || styles[0]["weight"] != 0.8 {
		t.Errorf("style = %#v", opts["style"])
	}
}

// TestGetEditingOptions_Character mirrors TS "should send character when referenceType is character"
// and "should send character with multiple identities from images config".
func TestGetEditingOptions_Character(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewImageModel(prov, ModelPhoton1)
	rt := ReferenceTypeCharacter
	id1 := "identity1"

	files := []provider.ImageFile{
		{Type: "url", URL: "https://example.com/a.jpg"},
		{Type: "url", URL: "https://example.com/b.jpg"},
	}
	opts, err := model.getEditingOptions(files, nil, &ImageModelOptions{
		ReferenceType: &rt,
		Images:        []ImageConfig{{}, {ID: &id1}},
	})
	if err != nil {
		t.Fatalf("getEditingOptions() error: %v", err)
	}
	character, ok := opts["character"].(map[string]interface{})
	if !ok {
		t.Fatalf("character = %#v", opts["character"])
	}
	identity0 := character["identity0"].(map[string]interface{})
	identity1 := character["identity1"].(map[string]interface{})
	if len(identity0["images"].([]string)) != 1 || len(identity1["images"].([]string)) != 1 {
		t.Errorf("character groups = %#v", character)
	}
}

// TestGetEditingOptions_MaskUnsupported mirrors TS "should throw error when mask is provided".
func TestGetEditingOptions_MaskUnsupported(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewImageModel(prov, ModelPhoton1)

	_, err := model.getEditingOptions(nil, &provider.ImageFile{Type: "url", URL: "https://example.com/mask.png"}, nil)
	if err == nil {
		t.Fatal("expected an error when a mask is provided")
	}
}

// TestGetEditingOptions_NonURLFileRejected mirrors TS "should throw error when base64 file data is provided".
func TestGetEditingOptions_NonURLFileRejected(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewImageModel(prov, ModelPhoton1)

	_, err := model.getEditingOptions([]provider.ImageFile{{Type: "file", Data: []byte{1, 2, 3}, MediaType: "image/png"}}, nil, nil)
	if err == nil {
		t.Fatal("expected an error for a non-URL file")
	}
}

// TestGetEditingOptions_TooManyImages mirrors TS "should throw error when more than 4 images for image".
func TestGetEditingOptions_TooManyImages(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewImageModel(prov, ModelPhoton1)

	files := make([]provider.ImageFile, 5)
	for i := range files {
		files[i] = provider.ImageFile{Type: "url", URL: "https://example.com/img.jpg"}
	}
	_, err := model.getEditingOptions(files, nil, nil)
	if err == nil {
		t.Fatal("expected an error for more than 4 reference images")
	}
}

// TestGetEditingOptions_ModifyImageRejectsMultiple mirrors TS "should throw error when multiple files for modify_image".
func TestGetEditingOptions_ModifyImageRejectsMultiple(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewImageModel(prov, ModelPhoton1)
	rt := ReferenceTypeModifyImage

	files := []provider.ImageFile{
		{Type: "url", URL: "https://example.com/a.jpg"},
		{Type: "url", URL: "https://example.com/b.jpg"},
	}
	_, err := model.getEditingOptions(files, nil, &ImageModelOptions{ReferenceType: &rt})
	if err == nil {
		t.Fatal("expected an error for multiple modify_image files")
	}
}

// TestDoGenerate_TimesOut verifies polling gives up after maxPollAttempts.
func TestDoGenerate_TimesOut(t *testing.T) {
	server := newLumaTestServer(t, []string{"dreaming"}, nil)
	defer server.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewImageModel(prov, ModelPhoton1)

	_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "x",
		ProviderOptions: map[string]interface{}{
			"luma": map[string]interface{}{"pollIntervalMillis": 1, "maxPollAttempts": 2},
		},
	})
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected 'timed out' in error, got: %v", err)
	}
}

// TestDownloadImage_RejectsPrivateIPRedirect verifies that a generated-image
// URL redirecting to a private/link-local address is rejected instead of
// followed (the 4be82c1-equivalent SSRF fix for Luma's response-supplied
// asset URL).
func TestDownloadImage_RejectsPrivateIPRedirect(t *testing.T) {
	var captured map[string]interface{}
	var serverURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/dream-machine/v1/generations/image", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "gen-1", "state": "queued"})
	})
	mux.HandleFunc("/dream-machine/v1/generations/gen-1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "gen-1", "state": "completed",
			"assets": map[string]interface{}{"image": serverURL + "/redirect"},
		})
	})
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	serverURL = server.URL

	prov := New(Config{APIKey: "test-key", BaseURL: server.URL})
	model := NewImageModel(prov, ModelPhoton1)

	_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "x",
		ProviderOptions: map[string]interface{}{
			"luma": map[string]interface{}{"pollIntervalMillis": 1},
		},
	})
	if err == nil {
		t.Fatal("expected an error rejecting the private-IP redirect target")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("expected an SSRF validation error, got: %v", err)
	}
}
