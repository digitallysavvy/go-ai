package xai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to create a test server with image download support
func createImageTestServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	var serverURL string
	mux := http.NewServeMux()

	// Handle API requests
	mux.HandleFunc("/v1/images/generations", handler)
	mux.HandleFunc("/v1/images/edits", handler)

	// Handle image downloads with generic pattern
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" && r.URL.Path != "/v1/images/edits" {
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("fake-image"))
		} else {
			handler(w, r)
		}
	})

	server := httptest.NewServer(mux)
	serverURL = server.URL

	// Update handler to use serverURL (closures)
	_ = serverURL
	return server
}

func TestImageModel_Metadata(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewImageModel(prov, "grok-image-1")

	assert.Equal(t, "v3", model.SpecificationVersion())
	assert.Equal(t, "xai", model.Provider())
	assert.Equal(t, "grok-image-1", model.ModelID())
}

func TestImageModel_TextToImage(t *testing.T) {
	// Create a single server with both routes
	var serverURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/images/generations", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)

		var body map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)

		assert.Equal(t, "grok-image-1", body["model"])
		assert.Equal(t, "A beautiful sunset", body["prompt"])
		assert.Equal(t, "url", body["response_format"])

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{
					"url": serverURL + "/test-image.png",
				},
			},
		})
	})
	mux.HandleFunc("/test-image.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("fake-image-data"))
	})

	server := httptest.NewServer(mux)
	defer server.Close()
	serverURL = server.URL

	prov := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	model := NewImageModel(prov, "grok-image-1")

	opts := &provider.ImageGenerateOptions{
		Prompt: "A beautiful sunset",
	}

	ctx := context.Background()
	result, err := model.DoGenerate(ctx, opts)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, []byte("fake-image-data"), result.Image)
	assert.Equal(t, "image/png", result.MimeType)
}

func TestImageModel_WithN(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/images/generations" {
			var body map[string]interface{}
			err := json.NewDecoder(r.Body).Decode(&body)
			require.NoError(t, err)

			assert.Equal(t, float64(3), body["n"])

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{"url": server.URL + "/image1.png"},
					{"url": server.URL + "/image2.png"},
					{"url": server.URL + "/image3.png"},
				},
			})
		} else {
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("fake-image"))
		}
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	model := NewImageModel(prov, "grok-image-1")

	n := 3
	opts := &provider.ImageGenerateOptions{
		Prompt: "A sunset",
		N:      &n,
	}

	ctx := context.Background()
	result, err := model.DoGenerate(ctx, opts)

	require.NoError(t, err)
	require.NotNil(t, result)
	// ImageResult only supports single image, but Usage.ImageCount shows total
	assert.NotNil(t, result.Image)
	assert.Equal(t, 3, result.Usage.ImageCount)
}

func TestImageModel_WithAspectRatio(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/images/generations" {
			var body map[string]interface{}
			err := json.NewDecoder(r.Body).Decode(&body)
			require.NoError(t, err)

			assert.Equal(t, "16:9", body["aspect_ratio"])

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{"url": server.URL + "/image.png"},
				},
			})
		} else {
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("fake-image"))
		}
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	model := NewImageModel(prov, "grok-image-1")

	opts := &provider.ImageGenerateOptions{
		Prompt:      "A sunset",
		AspectRatio: "16:9",
	}

	ctx := context.Background()
	result, err := model.DoGenerate(ctx, opts)

	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestImageModel_WithProviderAspectRatio(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/images/generations" {
			var body map[string]interface{}
			err := json.NewDecoder(r.Body).Decode(&body)
			require.NoError(t, err)

			assert.Equal(t, "1:1", body["aspect_ratio"])

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{"url": server.URL + "/image.png"},
				},
			})
		} else {
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("fake-image"))
		}
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	model := NewImageModel(prov, "grok-image-1")

	aspectRatio := "1:1"
	opts := &provider.ImageGenerateOptions{
		Prompt: "A sunset",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"aspect_ratio": aspectRatio,
			},
		},
	}

	ctx := context.Background()
	result, err := model.DoGenerate(ctx, opts)

	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestImageModel_ImageEditing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/images/edits" {
			assert.Equal(t, http.MethodPost, r.Method)

			var body map[string]interface{}
			err := json.NewDecoder(r.Body).Decode(&body)
			require.NoError(t, err)

			assert.Contains(t, body, "image")
			image := body["image"].(map[string]interface{})
			assert.Contains(t, image, "url")
			assert.Equal(t, "https://example.com/source.png", image["url"])

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{"url": server.URL + "/edited.png"},
				},
			})
		} else {
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("edited-image"))
		}
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	model := NewImageModel(prov, "grok-image-1")

	opts := &provider.ImageGenerateOptions{
		Prompt: "Change the sky to sunset colors",
		Files: []provider.ImageFile{
			{
				Type: "url",
				URL:  "https://example.com/source.png",
			},
		},
	}

	ctx := context.Background()
	result, err := model.DoGenerate(ctx, opts)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotNil(t, result.Image)
}

func TestImageModel_ImageEditing_Base64(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/images/edits" {
			var body map[string]interface{}
			err := json.NewDecoder(r.Body).Decode(&body)
			require.NoError(t, err)

			assert.Contains(t, body, "image")
			image := body["image"].(map[string]interface{})
			assert.Contains(t, image, "url")
			// Check it's a data URL
			assert.Contains(t, image["url"], "data:image/png;base64,")

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{"url": server.URL + "/edited.png"},
				},
			})
		} else {
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("edited-image"))
		}
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	model := NewImageModel(prov, "grok-image-1")

	opts := &provider.ImageGenerateOptions{
		Prompt: "Change colors",
		Files: []provider.ImageFile{
			{
				Type:      "file",
				Data:      []byte("fake-image-data"),
				MediaType: "image/png",
			},
		},
	}

	ctx := context.Background()
	result, err := model.DoGenerate(ctx, opts)

	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestImageModel_Inpainting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/images/edits" {
			var body map[string]interface{}
			err := json.NewDecoder(r.Body).Decode(&body)
			require.NoError(t, err)

			assert.Contains(t, body, "image")
			assert.Contains(t, body, "mask")

			mask := body["mask"].(map[string]interface{})
			assert.Contains(t, mask, "url")

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{"url": server.URL + "/inpainted.png"},
				},
			})
		} else {
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("inpainted-image"))
		}
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	model := NewImageModel(prov, "grok-image-1")

	opts := &provider.ImageGenerateOptions{
		Prompt: "Add a rainbow in the masked area",
		Files: []provider.ImageFile{
			{
				Type: "url",
				URL:  "https://example.com/source.png",
			},
		},
		Mask: &provider.ImageFile{
			Type: "url",
			URL:  "https://example.com/mask.png",
		},
	}

	ctx := context.Background()
	result, err := model.DoGenerate(ctx, opts)

	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestImageModel_Variations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/images/edits" {
			var body map[string]interface{}
			err := json.NewDecoder(r.Body).Decode(&body)
			require.NoError(t, err)

			// Variations would typically use the edits endpoint with a generic prompt
			assert.Contains(t, body, "image")
			assert.Equal(t, float64(4), body["n"])

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{"url": server.URL + "/var1.png"},
					{"url": server.URL + "/var2.png"},
					{"url": server.URL + "/var3.png"},
					{"url": server.URL + "/var4.png"},
				},
			})
		} else {
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("variation"))
		}
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	model := NewImageModel(prov, "grok-image-1")

	n := 4
	opts := &provider.ImageGenerateOptions{
		Prompt: "Generate variations",
		N:      &n,
		Files: []provider.ImageFile{
			{
				Type: "url",
				URL:  "https://example.com/source.png",
			},
		},
	}

	ctx := context.Background()
	result, err := model.DoGenerate(ctx, opts)

	require.NoError(t, err)
	require.NotNil(t, result)
	// ImageResult only supports single image
	assert.NotNil(t, result.Image)
	assert.Equal(t, 4, result.Usage.ImageCount)
}

func TestImageModel_WithOutputFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/images/generations" {
			var body map[string]interface{}
			err := json.NewDecoder(r.Body).Decode(&body)
			require.NoError(t, err)

			assert.Equal(t, "jpeg", body["output_format"])

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{"url": server.URL + "/image.jpg"},
				},
			})
		} else {
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write([]byte("jpeg-image"))
		}
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	model := NewImageModel(prov, "grok-image-1")

	opts := &provider.ImageGenerateOptions{
		Prompt: "A sunset",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"output_format": "jpeg",
			},
		},
	}

	ctx := context.Background()
	result, err := model.DoGenerate(ctx, opts)

	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestImageModel_UnsupportedOptions_Warnings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/images/generations" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{"url": server.URL + "/image.png"},
				},
			})
		} else {
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("image"))
		}
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	model := NewImageModel(prov, "grok-image-1")

	seed := 12345
	opts := &provider.ImageGenerateOptions{
		Prompt: "A sunset",
		Size:   "1024x1024",
		Seed:   &seed,
	}

	ctx := context.Background()
	result, err := model.DoGenerate(ctx, opts)

	require.NoError(t, err)
	require.NotNil(t, result)

	// Should have warnings for size and seed
	assert.GreaterOrEqual(t, len(result.Warnings), 2)
}

func TestImageModel_MultipleFiles_Warning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/images/edits" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{"url": server.URL + "/image.png"},
				},
			})
		} else {
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("image"))
		}
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	model := NewImageModel(prov, "grok-image-1")

	opts := &provider.ImageGenerateOptions{
		Prompt: "Edit",
		Files: []provider.ImageFile{
			{Type: "url", URL: "https://example.com/1.png"},
			{Type: "url", URL: "https://example.com/2.png"},
		},
	}

	ctx := context.Background()
	result, err := model.DoGenerate(ctx, opts)

	require.NoError(t, err)
	require.NotNil(t, result)

	// Should have warning about multiple files
	assert.Contains(t, result.Warnings[0].Message, "single input image")
}

func TestImageModel_RevisedPrompt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/images/generations" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{
						"url":            server.URL + "/image.png",
						"revised_prompt": "A beautiful sunset over the ocean with waves",
					},
				},
			})
		} else {
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("image"))
		}
	}))
	defer server.Close()

	prov := New(Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	model := NewImageModel(prov, "grok-image-1")

	opts := &provider.ImageGenerateOptions{
		Prompt: "A sunset",
	}

	ctx := context.Background()
	result, err := model.DoGenerate(ctx, opts)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.ProviderMetadata)

	xaiMeta := result.ProviderMetadata["xai"].(map[string]interface{})
	revisedPrompts := xaiMeta["revisedPrompts"].([]string)
	assert.Len(t, revisedPrompts, 1)
	assert.Equal(t, "A beautiful sunset over the ocean with waves", revisedPrompts[0])
}
