package anthropic

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r62 integrator: TS puts the same anthropicMetadata (usage: null when no
// message_start or message_delta came first) under the custom provider key.
func TestR62IntegratorBareMessageStopCustomKeyUsageNull(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()
	model := newCustomProviderMetadataModel(t, server.URL)
	s, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			customProviderMetadataName: map[string]interface{}{"sendReasoning": true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	finish := drainForFinish(t, s)
	meta := decodeStreamFinishMetadata(t, finish.ProviderMetadata)
	for _, key := range []string{"anthropic", customProviderMetadataName} {
		m, ok := meta[key].(map[string]interface{})
		if !ok {
			t.Fatalf("providerMetadata[%q] missing: %#v", key, meta)
		}
		if u, present := m["usage"]; !present || u != nil {
			t.Errorf("providerMetadata[%q].usage = %#v (present %v), want null", key, u, present)
		}
	}
}
