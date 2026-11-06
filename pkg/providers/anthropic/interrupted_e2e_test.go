package anthropic_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

const (
	e2eMsgStart  = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude\",\"usage\":{\"input_tokens\":10,\"output_tokens\":1}}}\n\n"
	e2eTextStart = "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"
	e2eTextDelta = "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n"
)

// TS #23816 end to end: an Anthropic stream cut before any output rejects with
// NoOutputGeneratedError (and fires OnStepError); a cut after output resolves
// with the partial text and finish reason interrupted.
func TestStreamText_EmptyAnthropicStreamRejectsNoOutput(t *testing.T) {
	cases := map[string]struct {
		sse         string
		wantNoOut   bool
		wantText    string
		wantFinish  types.FinishReason
		wantStepErr bool
	}{
		"empty body":    {sse: "", wantNoOut: true, wantStepErr: true},
		"metadata only": {sse: e2eMsgStart, wantNoOut: true, wantStepErr: true},
		"partial text":  {sse: e2eMsgStart + e2eTextStart + e2eTextDelta, wantText: "Hello", wantFinish: types.FinishReasonInterrupted},
	}
	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, tc.sse)
			}))
			defer srv.Close()
			model, err := anthropic.New(anthropic.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("claude-sonnet-4-5")
			if err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			stepErr := false
			result, err := ai.StreamText(context.Background(), ai.StreamTextOptions{
				Model:  model,
				Prompt: "Hello",
				OnStepError: func(ctx context.Context, e ai.OnStepErrorEvent) {
					mu.Lock()
					stepErr = true
					mu.Unlock()
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			_, readErr := result.ReadAll()
			if tc.wantNoOut {
				if !ai.IsNoOutputGeneratedError(readErr) {
					t.Fatalf("err = %v, want NoOutputGeneratedError", readErr)
				}
			} else if readErr != nil || result.Text() != tc.wantText || result.FinishReason() != tc.wantFinish {
				t.Fatalf("err=%v text=%q finish=%q", readErr, result.Text(), result.FinishReason())
			}
			mu.Lock()
			defer mu.Unlock()
			if stepErr != tc.wantStepErr {
				t.Fatalf("OnStepError = %v, want %v", stepErr, tc.wantStepErr)
			}
		})
	}
}
