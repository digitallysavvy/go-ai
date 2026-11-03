package mistral

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR84provaConversationUnknownToolChoiceType: TS
// mistral-conversation-prepare-tools.ts throws UnsupportedFunctionalityError
// "tool choice type: X" for an unknown type (the chat model already did);
// without tools it returns before the switch.
func TestR84provaConversationUnknownToolChoiceType(t *testing.T) {
	var sent atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent.Add(1)
		w.WriteHeader(400)
	}))
	defer srv.Close()
	m := New(Config{APIKey: "k", BaseURL: srv.URL}).ConversationModel("mistral-medium-latest")
	tools := []types.Tool{{Name: "t", Parameters: map[string]interface{}{"type": "object"}}}
	const want = "'tool choice type: unrecognized' functionality not supported."
	for _, stream := range []bool{false, true} {
		opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Tools: tools,
			ToolChoice: types.ToolChoice{Type: "unrecognized"}}
		var err error
		if stream {
			var s provider.TextStream
			s, err = m.DoStream(context.Background(), opts)
			if s != nil {
				_ = s.Close()
			}
		} else {
			_, err = m.DoGenerate(context.Background(), opts)
		}
		var uf *providererrors.UnsupportedFunctionalityError
		if err == nil || err.Error() != want || !errors.As(err, &uf) {
			t.Errorf("stream=%v: err = %v, want %q", stream, err, want)
		}
	}
	if n := sent.Load(); n != 0 {
		t.Errorf("%d requests sent", n)
	}
	_, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"},
		ToolChoice: types.ToolChoice{Type: "unrecognized"}})
	if err != nil && err.Error() == want {
		t.Error("no tools: tool choice still checked")
	}
}
