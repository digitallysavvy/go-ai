package anthropic

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func strPtrR87e(s string) *string { return &s }

// TS anthropic-language-model.ts `stopSequence: response.stop_sequence ??
// null` (generate) and `value.delta.stop_sequence ?? null` (stream): a
// present "" is reported as "", null / absent as null
// (cr-r87e-provb-to-prova 1).
func TestR87eintegAnthropicStopSequenceEmptyString(t *testing.T) {
	for _, tc := range []struct {
		name string
		wire string
		want interface{}
	}{
		{"empty", `""`, ""},
		{"null", `null`, nil},
		{"value", `"END"`, "END"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := anthropicProviderMetadata(anthropicUsage{}, nil, nil, nil, nil)["anthropic"].(map[string]interface{})["stopSequence"]
			if got != nil {
				t.Fatalf("nil stop sequence = %#v", got)
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				if strings.Contains(string(body), `"stream":true`) {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"model\":\"claude\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n"+
						"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"stop_sequence\",\"stop_sequence\":"+tc.wire+"},\"usage\":{\"output_tokens\":1}}\n\n"+
						"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"m","type":"message","role":"assistant","model":"claude","content":[{"type":"text","text":"hi"}],"stop_reason":"stop_sequence","stop_sequence":`+tc.wire+`,"usage":{"input_tokens":1,"output_tokens":1}}`)
			}))
			defer srv.Close()
			model, err := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("claude-sonnet-4-5")
			if err != nil {
				t.Fatal(err)
			}
			opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}
			res, err := model.DoGenerate(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			if got := res.ProviderMetadata["anthropic"].(map[string]interface{})["stopSequence"]; got != tc.want {
				t.Fatalf("generate stopSequence = %#v, want %#v", got, tc.want)
			}
			stream, err := model.DoStream(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close() //nolint:errcheck
			var finish *provider.StreamChunk
			for {
				c, err := stream.Next()
				if err != nil {
					break
				}
				if c.Type == provider.ChunkTypeFinish {
					finish = c
				}
			}
			if finish == nil {
				t.Fatal("no finish part")
			}
			wantJSON := `"stopSequence":null`
			if s, ok := tc.want.(string); ok {
				wantJSON = `"stopSequence":"` + s + `"`
			}
			if !strings.Contains(string(finish.ProviderMetadata), wantJSON) {
				t.Fatalf("stream metadata %s lacks %s", finish.ProviderMetadata, wantJSON)
			}
		})
	}
}
