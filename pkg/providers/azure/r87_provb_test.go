package azure

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS 7139593a3a (azure-openai-provider.test.ts "%s strict tools > should
// forward strict tools with companion strict=%s when endpoint support is
// unspecified"): Azure DeepSeek sends each tool's own strict flag, generate
// and stream, and the stream has no error part.
func TestR87provbAzureDeepSeekCompanionStrict(t *testing.T) {
	yes, no := true, false
	for _, method := range []string{"doGenerate", "doStream"} {
		for _, companion := range []*bool{&yes, &no, nil} {
			var body map[string]interface{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&body)
				if method == "doStream" {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, `data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n")
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`)
			}))
			p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
			if err != nil {
				t.Fatal(err)
			}
			m, _ := p.DeepSeek("deepseek-v4-pro")
			opts := &provider.GenerateOptions{
				Prompt: types.Prompt{Text: "x"},
				Tools: []types.Tool{
					{Name: "ready", Parameters: map[string]interface{}{"type": "object"}, Strict: &yes},
					{Name: "other", Parameters: map[string]interface{}{"type": "object"}, Strict: companion},
				},
			}
			if method == "doGenerate" {
				_, err = m.DoGenerate(context.Background(), opts)
			} else {
				var st provider.TextStream
				st, err = m.DoStream(context.Background(), opts)
				for err == nil {
					var c *provider.StreamChunk
					c, err = st.Next()
					if err == nil && c.Type == provider.ChunkTypeError {
						t.Fatalf("%s: error part %v", method, c.Err)
					}
				}
				if err == io.EOF {
					err = nil
				}
			}
			srv.Close()
			if err != nil {
				t.Fatalf("%s: %v", method, err)
			}
			tools, _ := body["tools"].([]interface{})
			if len(tools) != 2 {
				t.Fatalf("%s: tools = %v", method, body["tools"])
			}
			f0 := tools[0].(map[string]interface{})["function"].(map[string]interface{})
			f1 := tools[1].(map[string]interface{})["function"].(map[string]interface{})
			if f0["strict"] != true {
				t.Errorf("%s: tools[0].strict = %v", method, f0["strict"])
			}
			var want interface{}
			if companion != nil {
				want = *companion
			}
			if f1["strict"] != want {
				t.Errorf("%s companion %v: tools[1].strict = %v", method, want, f1["strict"])
			}
		}
	}
}
