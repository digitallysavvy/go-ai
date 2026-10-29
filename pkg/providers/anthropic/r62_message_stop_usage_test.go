package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r62: a message_stop with no message_start or message_delta before it has
// no rawUsage in TS, so providerMetadata.anthropic.usage is null and
// usage.raw is the stream's initial usage object (`raw: rawUsage ?? usage`).
func TestR62BareMessageStopUsageRaw(t *testing.T) {
	model, closeFn := fixtureModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"message_stop\"}\n\n")
	})
	defer closeFn()
	s, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	var finish *provider.StreamChunk
	for _, c := range drain(t, s) {
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
	}
	if finish == nil || finish.Usage == nil {
		t.Fatalf("finish = %+v", finish)
	}
	wantRaw := map[string]interface{}{
		"input_tokens":                0,
		"output_tokens":               0,
		"cache_creation_input_tokens": 0,
		"cache_read_input_tokens":     0,
		"iterations":                  nil,
	}
	if !reflect.DeepEqual(finish.Usage.Raw, wantRaw) {
		t.Fatalf("usage.raw = %#v, want %#v", finish.Usage.Raw, wantRaw)
	}
	var meta map[string]map[string]interface{}
	if err := json.Unmarshal(finish.ProviderMetadata, &meta); err != nil {
		t.Fatal(err)
	}
	if u, ok := meta["anthropic"]["usage"]; !ok || u != nil {
		t.Fatalf("providerMetadata.anthropic.usage = %#v (present %v), want null", u, ok)
	}
}
