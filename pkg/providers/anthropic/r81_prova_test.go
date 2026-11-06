package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports anthropic-language-model.test.ts "transformRequestBody > should
// transform request body in doGenerate / doStream when transformRequestBody is
// provided": the transform is called once with the request args (model,
// messages) and the transformed body is what is sent. The wire replay skips
// them because the TS transform is test code; Go has Config.TransformRequestBody.
func TestR81provaAnthropicTransformRequestBody(t *testing.T) {
	const jsonBody = `{"id":"msg_test","type":"message","role":"assistant","content":[{"type":"text","text":"Hello!"}],"model":"claude-3-haiku-20240307","stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":4,"output_tokens":30}}`
	streamChunks := []string{
		`{"type":"message_start","message":{"id":"msg_test","type":"message","role":"assistant","content":[],"model":"claude-3-haiku-20240307","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":4,"output_tokens":1}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello!"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":2}}`,
		`{"type":"message_stop"}`,
	}
	for _, stream := range []bool{false, true} {
		var (
			mu       sync.Mutex
			sent     map[string]interface{}
			calls    int
			seenArgs map[string]interface{}
			seenFlag bool
		)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			mu.Lock()
			_ = json.Unmarshal(b, &sent)
			mu.Unlock()
			if stream {
				w.Header().Set("Content-Type", "text/event-stream")
				for _, c := range streamChunks {
					_, _ = io.WriteString(w, "data: "+c+"\n\n")
				}
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, jsonBody)
		}))
		p := New(Config{
			APIKey: "k", BaseURL: srv.URL,
			TransformRequestBody: func(body map[string]interface{}, isStream bool) map[string]interface{} {
				mu.Lock()
				calls++
				seenArgs, seenFlag = body, isStream
				mu.Unlock()
				out := map[string]interface{}{"custom_field": "added-by-transform"}
				for k, v := range body {
					out[k] = v
				}
				return out
			},
		})
		model := NewLanguageModel(p, "claude-3-haiku-20240307", nil)
		opts := &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}}}}}
		if stream {
			s, err := model.DoStream(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			for {
				if _, err := s.Next(); err != nil {
					break
				}
			}
			_ = s.Close()
		} else if _, err := model.DoGenerate(context.Background(), opts); err != nil {
			t.Fatal(err)
		}
		srv.Close()
		mu.Lock()
		if calls != 1 || seenFlag != stream {
			t.Fatalf("stream=%v: transform calls = %d (stream flag %v), want 1", stream, calls, seenFlag)
		}
		if seenArgs["model"] != "claude-3-haiku-20240307" {
			t.Fatalf("stream=%v: transform args model = %v", stream, seenArgs["model"])
		}
		msgs, _ := json.Marshal(seenArgs["messages"])
		var gotMsgs interface{}
		_ = json.Unmarshal(msgs, &gotMsgs)
		var wantMsgs interface{}
		_ = json.Unmarshal([]byte(`[{"role":"user","content":[{"type":"text","text":"Hello"}]}]`), &wantMsgs)
		if !reflect.DeepEqual(gotMsgs, wantMsgs) {
			t.Fatalf("stream=%v: transform args messages = %s", stream, msgs)
		}
		if sent["custom_field"] != "added-by-transform" {
			t.Fatalf("stream=%v: sent body = %v, want custom_field", stream, sent)
		}
		mu.Unlock()
	}
}
