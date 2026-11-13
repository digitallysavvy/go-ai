package alibaba

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// textType / outputType are z.enum(...).optional(): an empty string is not a
// member, so the options fail validation like TS.
func TestR33_EmptyEnumEmbeddingOptionsAreRejected(t *testing.T) {
	m := NewEmbeddingModel(New(Config{APIKey: "k"}), "text-embedding-v4")
	for _, key := range []string{"textType", "outputType"} {
		_, err := m.DoEmbedMany(context.Background(), []string{"x"}, &provider.EmbedModelOptions{
			ProviderOptions: map[string]interface{}{"alibaba": map[string]interface{}{key: ""}},
		})
		if err == nil {
			t.Errorf("%s: empty string accepted", key)
		}
	}
}

func TestR33_VideoStatusNullUsageAndTrimAndRatio(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
	defer server.Close()
	model := NewVideoModel(New(Config{APIKey: "k", VideoBaseURL: server.URL}), "wan2.6-t2v")
	status := func() *provider.VideoModelV3OperationStatusResult {
		res, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: []byte(`{"taskId":"t"}`)})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	body = `{"output":{"task_id":"t","task_status":"SUCCEEDED","video_url":"https://x/v.mp4"},"usage":{"duration":null,"size":"","ratio":"","fps":null,"extra":1}}`
	usage := status().ProviderMetadata["alibaba"].(map[string]interface{})["usage"].(map[string]interface{})
	if v, ok := usage["duration"]; !ok || v != nil {
		t.Errorf("duration = %v (present %v)", v, ok)
	}
	if v, ok := usage["size"]; !ok || v != "" {
		t.Errorf("size = %v (present %v)", v, ok)
	}
	if v, ok := usage["ratio"]; !ok || v != "" {
		t.Errorf("ratio (non-null) = %v (present %v)", v, ok)
	}
	if _, ok := usage["fps"]; ok {
		t.Errorf("null fps must be omitted: %v", usage)
	}
	if _, ok := usage["extra"]; ok {
		t.Errorf("unknown usage key kept: %v", usage)
	}

	// JS trim does not strip U+0085, Go TrimSpace does.
	body = "{\"output\":{\"task_id\":\"t\",\"task_status\":\"FAILED\",\"message\":\"\u0085\"}}"
	if got := status().Error; got != "Video generation failed. Task ID: t. \u0085" {
		t.Errorf("error = %q", got)
	}

	// Only the first two parts of resolution.split('x') count, as Numbers.
	if r, ok := deriveAlibabaRatioFromResolution("1280x720x9"); !ok || r != "16:9" {
		t.Errorf("ratio = %q %v", r, ok)
	}
	if r, ok := deriveAlibabaRatioFromResolution("1280.0x720"); !ok || r != "16:9" {
		t.Errorf("ratio = %q %v", r, ok)
	}
}
