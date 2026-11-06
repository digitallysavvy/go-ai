package bytedance

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS `error?.message ?? error?.code ?? JSON.stringify(...)` only falls through
// on null/absent, and `last_frame_url != null` keeps an empty string.
func TestR33_EmptyStringNullishHandling(t *testing.T) {
	ts := newTestServer(t, "t", []string{`{"id":"t","status":"failed","error":{"message":"","code":"E1"}}`})
	defer ts.server.Close()
	model := newVideoModel(providerForServer(t, ts.server.URL), "seedance-1-0-pro-250528")
	op, _ := json.Marshal(bytedanceOperation{TaskID: "t"})
	status, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err != nil {
		t.Fatal(err)
	}
	if status.Error != "Video generation failed. Task ID: t. " {
		t.Errorf("error = %q", status.Error)
	}

	ts2 := newTestServer(t, "t", []string{`{"id":"t","status":"succeeded","content":{"video_url":"https://cdn.example.com/v.mp4","last_frame_url":""}}`})
	defer ts2.server.Close()
	model = newVideoModel(providerForServer(t, ts2.server.URL), "seedance-1-0-pro-250528")
	status, err = model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err != nil {
		t.Fatal(err)
	}
	meta := status.ProviderMetadata["bytedance"].(map[string]interface{})
	if v, ok := meta["lastFrameUrl"]; !ok || v != "" {
		t.Errorf("lastFrameUrl = %v (present %v)", v, ok)
	}
}

func TestR33_EmptyPromptIsSentWhenSet(t *testing.T) {
	model := newVideoModel(newTestProvider(t), "seedance-1-0-pro-250528")
	body, err := model.buildRequestBody(&provider.VideoModelV3CallOptions{Prompt: "", PromptSet: true}, &ProviderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(body["content"])
	if !strings.Contains(string(b), `"text":""`) {
		t.Errorf("content = %s", b)
	}
	if !bytedanceIsVideoFile(&provider.VideoModelV3File{MediaType: "video"}) {
		t.Error("bare 'video' media type is a video (getTopLevelMediaType)")
	}
}
