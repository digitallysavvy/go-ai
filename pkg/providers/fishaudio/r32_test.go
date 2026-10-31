package fishaudio

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS `features != null`: an empty features array is still sent.
func TestSpeechEmptyFeaturesArrayIsSent(t *testing.T) {
	p, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte{1}) })
	model := mustSpeechModel(t, p, "s2-pro")
	res, err := model.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{
		Text:            "hi",
		ProviderOptions: map[string]interface{}{"fishAudio": map[string]interface{}{"features": []interface{}{}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(fmt.Sprint(res.Request.Body), `,"features":[]}`) {
		t.Fatalf("request.body = %s", res.Request.Body)
	}
}
