package deepgram

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// JSON.stringify leaves HTML characters literal in request.body.
func TestSpeechRequestBodyKeepsHTMLCharacters(t *testing.T) {
	srv := speechTestServer(t, nil, nil)
	defer srv.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).SpeechModel("aura-2-thalia-en")
	res, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "a<b & c>"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"text":"a<b & c>"}`; res.Request.Body != want {
		t.Fatalf("request.body = %s, want %s", res.Request.Body, want)
	}
}

// TS `if (deepgramOptions.tag)`: an empty string is falsy, an empty array is not.
func TestSpeechEmptyStringTagIsSkipped(t *testing.T) {
	query := map[string]string{}
	applyDeepgramSpeechOptions(query, &SpeechModelOptions{Tag: ""}, new([]types.Warning))
	if _, ok := query["tag"]; ok {
		t.Fatalf("query = %v", query)
	}
	applyDeepgramSpeechOptions(query, &SpeechModelOptions{Tag: []interface{}{}}, new([]types.Warning))
	if v, ok := query["tag"]; !ok || v != "" {
		t.Fatalf("query = %v", query)
	}
}
