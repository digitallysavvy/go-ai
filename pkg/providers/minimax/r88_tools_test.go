package minimax

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Post-merge port (after prova's TS 6b101ac076 / 8291ccf237 anthropic
// changes): minimax-chat-language-model.test.ts "MiniMax service tiers" and
// the MiniMax half of the message-thread provider gate.

const r88toolsMessage = `{"id":"msg_tier","type":"message","role":"assistant","model":"minimax-m3.1-flash","content":[],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`

type r88toolsCalls struct {
	mu     sync.Mutex
	bodies []map[string]any
}

func r88toolsServer(t *testing.T, stream bool) (*httptest.Server, *r88toolsCalls) {
	t.Helper()
	calls := &r88toolsCalls{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		calls.mu.Lock()
		calls.bodies = append(calls.bodies, body)
		calls.mu.Unlock()
		if !stream {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, r88toolsMessage)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, ev := range []string{
			`{"type":"message_start","message":` + r88toolsMessage + `}`,
			`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
			`{"type":"message_stop"}`,
		} {
			_, _ = io.WriteString(w, "data: "+ev+"\n\n")
		}
	}))
	t.Cleanup(srv.Close)
	return srv, calls
}

func r88toolsOptions(po map[string]interface{}) *provider.GenerateOptions {
	max := 16
	return &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}},
		}},
		MaxTokens:       &max,
		ProviderOptions: po,
	}
}

func r88toolsModel(srv *httptest.Server) provider.LanguageModel {
	return NewLanguageModel(New(Config{APIKey: "test-api-key", BaseURL: srv.URL}), "minimax-m3.1-flash")
}

// "sends priority admission when using generate / stream"
func TestR88toolsMiniMaxPriorityTier(t *testing.T) {
	for _, stream := range []bool{false, true} {
		srv, calls := r88toolsServer(t, stream)
		opts := r88toolsOptions(map[string]interface{}{"minimax": map[string]interface{}{"serviceTier": "priority"}})
		if stream {
			s, err := r88toolsModel(srv).DoStream(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			for {
				if c, err := s.Next(); err != nil || c == nil {
					break
				}
			}
			_ = s.Close()
		} else if _, err := r88toolsModel(srv).DoGenerate(context.Background(), opts); err != nil {
			t.Fatal(err)
		}
		if calls.bodies[0]["service_tier"] != "priority" {
			t.Fatalf("stream=%v: service_tier = %v", stream, calls.bodies[0]["service_tier"])
		}
	}
}

// "uses the MiniMax tier over a canonical namespace value" and "warns and
// omits an Anthropic-only tier"
func TestR88toolsMiniMaxTierNamespaces(t *testing.T) {
	srv, calls := r88toolsServer(t, false)
	if _, err := r88toolsModel(srv).DoGenerate(context.Background(), r88toolsOptions(map[string]interface{}{
		"anthropic": map[string]interface{}{"serviceTier": "priority"},
		"minimax":   map[string]interface{}{"serviceTier": "standard"},
	})); err != nil {
		t.Fatal(err)
	}
	if calls.bodies[0]["service_tier"] != "standard" {
		t.Fatalf("service_tier = %v", calls.bodies[0]["service_tier"])
	}
	res, err := r88toolsModel(srv).DoGenerate(context.Background(), r88toolsOptions(map[string]interface{}{
		"anthropic": map[string]interface{}{"serviceTier": "auto"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := calls.bodies[1]["service_tier"]; ok {
		t.Fatalf("Anthropic-only tier sent: %v", calls.bodies[1])
	}
	found := false
	for _, w := range res.Warnings {
		if w.Type == "unsupported" && w.Feature == "serviceTier" {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %+v", res.Warnings)
	}
}

// "rejects an invalid tier %s before sending a request"
func TestR88toolsMiniMaxInvalidTier(t *testing.T) {
	srv, calls := r88toolsServer(t, false)
	for _, tier := range []interface{}{nil, "invalid"} {
		if _, err := r88toolsModel(srv).DoGenerate(context.Background(), r88toolsOptions(map[string]interface{}{
			"minimax": map[string]interface{}{"serviceTier": tier},
		})); err == nil {
			t.Fatalf("%v: accepted", tier)
		}
	}
	if len(calls.bodies) != 0 {
		t.Fatalf("requests sent: %d", len(calls.bodies))
	}
}

// TS 6b101ac076 anthropic-message-threads.test.ts provider gate, MiniMax
// half: thread fails before any request with the TS provider string.
func TestR88toolsMiniMaxRejectsMessageThreads(t *testing.T) {
	srv, calls := r88toolsServer(t, false)
	for _, key := range []string{"minimax", "anthropic"} {
		opts := r88toolsOptions(map[string]interface{}{key: map[string]interface{}{"thread": map[string]interface{}{"type": "create"}}})
		_, err := r88toolsModel(srv).DoGenerate(context.Background(), opts)
		var unsupported *providererrors.UnsupportedFunctionalityError
		if !errors.As(err, &unsupported) || !strings.Contains(err.Error(), "minimax.messages does not support Anthropic message threads.") {
			t.Fatalf("%s: err = %v", key, err)
		}
		if _, err := r88toolsModel(srv).DoStream(context.Background(), opts); err == nil {
			t.Fatalf("%s: stream accepted thread", key)
		}
	}
	if len(calls.bodies) != 0 {
		t.Fatalf("requests sent: %d", len(calls.bodies))
	}
}
