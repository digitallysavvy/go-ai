package tencent

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR91provcTypedOptionLiterals (typed-option ruling): Go-typed literals
// (a typed options struct, []map[string]string, int64) read like the decoded
// JSON.
func TestR91provcTypedOptionLiterals(t *testing.T) {
	s := newVideoServer(t)
	priority := 3
	cases := []map[string]interface{}{
		{"tencent": VideoModelOptions{NegativePrompt: strp("dogs"), TasksPriority: &priority}},
		{"tencent": &VideoModelOptions{NegativePrompt: strp("dogs"), TasksPriority: &priority}},
		{"tencent": map[string]interface{}{"negativePrompt": "dogs", "tasksPriority": int64(3)}},
	}
	for i, po := range cases {
		opts := defaultVideoOptions()
		opts.ProviderOptions = po
		start(t, s.model(t, "vod/VS/2.0"), opts)
		if body := s.call(i).body; !contains(body, `"NegativePrompt":"dogs"`) || !contains(body, `"TasksPriority":3`) {
			t.Errorf("case %d: body = %s", i, body)
		}
	}
	opts := defaultVideoOptions()
	opts.ProviderOptions = map[string]interface{}{"tencent": map[string]interface{}{
		"elements": []map[string]string{{"elementId": "element-1"}},
	}}
	opts.Image = &provider.VideoModelV3File{Type: "url", URL: "https://example.com/first.png"}
	start(t, s.model(t, "kling-video-v3"), opts)
	if body := s.call(len(cases)).body; !contains(body, `{"type":"element","element_id":"element-1"}`) {
		t.Errorf("elements body = %s", body)
	}
}

func strp(s string) *string { return &s }

func contains(s, sub string) bool { return strings.Contains(s, sub) }
