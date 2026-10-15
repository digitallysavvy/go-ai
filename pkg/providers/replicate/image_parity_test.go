package replicate

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestReplicateImageUnversionedMultiOutputFilesAndOptions(t *testing.T) {
	var gotPath, prefer string
	var body map[string]interface{}
	p := newReplicateProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodPost:
			gotPath, prefer = r.URL.Path, r.Header.Get("Prefer")
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			return replicateJSONResponse(201, `{"id":"p","status":"succeeded","output":["https://replicate.example/a.png","https://replicate.example/b.png"],"urls":{"get":"https://replicate.example/predictions/p"}}`), nil
		case r.URL.Path == "/a.png":
			return replicateJSONResponse(200, "AAA"), nil
		case r.URL.Path == "/b.png":
			return replicateJSONResponse(200, "BBB"), nil
		}
		return nil, errors.New("unexpected " + r.URL.Path)
	})
	m := NewImageModel(p, "black-forest-labs/flux-fill-pro")
	if m.MaxImagesPerCall() != 1 || NewImageModel(p, "black-forest-labs/flux-2-pro").MaxImagesPerCall() != 8 {
		t.Fatal("MaxImagesPerCall mismatch")
	}
	res, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "x",
		Files:  []provider.ImageFile{{Type: "file", Data: []byte("hi"), MediaType: "image/png"}, {Type: "url", URL: "https://e.com/2.png"}},
		Mask:   &provider.ImageFile{Type: "file", Data: []byte("m"), MediaType: "image/png"},
		ProviderOptions: map[string]interface{}{"replicate": map[string]interface{}{
			"maxWaitTimeInSeconds": 30, "guidance": 3.5, "pollIntervalMillis": 1,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/models/black-forest-labs/flux-fill-pro/predictions" || prefer != "wait=30" {
		t.Fatalf("path=%q prefer=%q", gotPath, prefer)
	}
	in := body["input"].(map[string]interface{})
	if in["image"] != "data:image/png;base64,aGk=" || in["mask"] != "data:image/png;base64,bQ==" || in["guidance"] != 3.5 {
		t.Fatalf("input = %v", in)
	}
	if _, ok := in["maxWaitTimeInSeconds"]; ok {
		t.Fatalf("handled option leaked into input: %v", in)
	}
	if len(res.Images) != 2 || string(res.Images[1]) != "BBB" || len(res.Warnings) != 1 || res.Response == nil {
		t.Fatalf("result = %+v", res)
	}
}

func TestReplicateCredentialedHeadersOnlySameOrigin(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "https://replicate.example/v1"})
	if h := p.credentialedHeaders("https://replicate.example/v1/predictions/1", map[string]string{"X": "1"}); h["authorization"] != "Token k" || h["X"] != "1" {
		t.Fatalf("same-origin headers = %v", h)
	}
	if h := p.credentialedHeaders("https://evil.example.net/predictions/1", map[string]string{"X": "1"}); h != nil {
		t.Fatalf("foreign origin must get no headers, got %v", h)
	}
	// default base URL
	d := New(Config{APIKey: "k"})
	if d.credentialedHeaders("https://attacker.test/x", nil) != nil || d.credentialedHeaders("https://api.replicate.com/v1/predictions/1", nil) == nil {
		t.Fatal("default base URL origin handling wrong")
	}
}
