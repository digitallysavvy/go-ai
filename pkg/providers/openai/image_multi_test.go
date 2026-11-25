package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestImageGenerateReturnsAllImagesUsageAndMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Req", "r1")
		_, _ = w.Write([]byte(`{"created":5,"size":"1024x1024","data":[{"b64_json":"YQ==","revised_prompt":"p1"},{"b64_json":"Yg=="},{"b64_json":"Yw=="}],"usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30,"input_tokens_details":{"image_tokens":10,"text_tokens":4}}}`))
	}))
	defer srv.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).ImageModel("gpt-image-1")
	n := 3
	res, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x", N: &n})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Images) != 3 || string(res.Images[2]) != "c" || len(res.Base64Images) != 3 {
		t.Fatalf("images = %d", len(res.Images))
	}
	if res.Usage.InputTokens != 10 || res.Usage.TotalTokens != 30 {
		t.Fatalf("usage = %+v", res.Usage)
	}
	if res.Response == nil || res.Response.ModelID != "gpt-image-1" || (res.Response.Headers["X-Req"] == "" && res.Response.Headers["x-req"] == "") {
		t.Fatalf("response = %+v", res.Response)
	}
	items := res.ProviderMetadata["openai"].(map[string]interface{})["images"].([]map[string]interface{})
	if items[0]["revisedPrompt"] != "p1" || items[0]["imageTokens"] != 3 || items[2]["imageTokens"] != 4 || items[2]["textTokens"] != 2 {
		t.Fatalf("metadata = %v", items)
	}
}
