package google

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
)

const (
	r87integSig     = "b3BhcXVlLXN5bnRoZXRpYy1zaWduYXR1cmUvKz0="
	r87integCallSig = "Y2FsbC1zaWduYXR1cmU="
)

// r87integSummary is TS summarize(part) of google-streamed-tool-order.test.ts.
type r87integSummary struct {
	Type, Text, ID, Name, Signature string
	Input                           interface{}
}

func r87integSummarize(t *testing.T, parts []types.ContentPart) []r87integSummary {
	t.Helper()
	var out []r87integSummary
	for _, p := range parts {
		b, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]interface{}
		_ = json.Unmarshal(b, &m)
		sig := ""
		for _, key := range []string{"providerMetadata", "providerOptions"} {
			if md, ok := m[key].(map[string]interface{}); ok {
				if g, ok := md["google"].(map[string]interface{}); ok {
					if s, ok := g["thoughtSignature"].(string); ok && sig == "" {
						sig = s
					}
				}
			}
		}
		if ts, ok := m["thoughtSignature"].(string); ok && sig == "" {
			// Go keeps a tool call's signature on ToolCallContent.ThoughtSignature.
			sig = ts
		}
		s := r87integSummary{Type: fmt.Sprint(m["type"]), Signature: sig}
		if s.Type == "tool-call" {
			s.ID, s.Name = fmt.Sprint(m["toolCallId"]), fmt.Sprint(m["toolName"])
			if in, ok := m["input"].(string); ok {
				_ = json.Unmarshal([]byte(in), &s.Input)
			} else {
				s.Input = m["input"]
			}
		} else {
			s.Text, _ = m["text"].(string)
		}
		out = append(out, s)
	}
	return out
}

// TS ai/src/generate-text/google-streamed-tool-order.test.ts (fd7e8a75d7,
// coupled gen e1112026 + prova 621b7452): a Google stream's text, reasoning
// and tool calls keep part order, a signed empty text / reasoning part is its
// own block, survives into the response messages and is replayed on the next
// request exactly as the model sent it; an unsigned empty text is removed.
func TestR87integGoogleSignedEmptyStreamRoundTrip(t *testing.T) {
	args := map[string]interface{}{"query": "example", "options": map[string]interface{}{"limit": 0.0, "enabled": false}}
	call := map[string]interface{}{"functionCall": map[string]interface{}{"id": "call-1", "name": "lookup", "args": args}, "thoughtSignature": r87integCallSig}
	noArgs := map[string]interface{}{"functionCall": map[string]interface{}{"id": "call-2", "name": "read_theme"}}
	a := map[string]interface{}{"text": "A"}
	b := map[string]interface{}{"text": "B"}
	r1 := map[string]interface{}{"text": "R1", "thought": true}
	r2 := map[string]interface{}{"text": "R2", "thought": true}
	signedText := map[string]interface{}{"text": "", "thoughtSignature": r87integSig}
	signedReasoning := map[string]interface{}{"text": "", "thought": true, "thoughtSignature": r87integSig}
	text := func(v, sig string) r87integSummary { return r87integSummary{Type: "text", Text: v, Signature: sig} }
	reasoning := func(v, sig string) r87integSummary {
		return r87integSummary{Type: "reasoning", Text: v, Signature: sig}
	}
	lookup := r87integSummary{Type: "tool-call", ID: "call-1", Name: "lookup", Input: args, Signature: r87integCallSig}
	readTheme := r87integSummary{Type: "tool-call", ID: "call-2", Name: "read_theme", Input: map[string]interface{}{}}
	type m = map[string]interface{}
	cases := []struct {
		name     string
		groups   [][]m
		expected []r87integSummary
		wire     []m
	}{
		{"signed empty text after completed call", [][]m{{a, call, signedText}}, []r87integSummary{text("A", ""), lookup, text("", r87integSig)}, []m{a, call, signedText}},
		{"separate events and no-args call", [][]m{{a}, {noArgs}, {signedText}, {b}}, []r87integSummary{text("A", ""), readTheme, text("", r87integSig), text("B", "")}, []m{a, noArgs, signedText, b}},
		{"signed empty reasoning after completed call", [][]m{{a, noArgs, signedReasoning, b}}, []r87integSummary{text("A", ""), readTheme, reasoning("", r87integSig), text("B", "")}, []m{a, noArgs, signedReasoning, b}},
		{"empty text between reasoning parts", [][]m{{r1}, {signedText}, {r2}}, []r87integSummary{reasoning("R1", ""), text("", r87integSig), reasoning("R2", "")}, []m{r1, signedText, r2}},
		{"empty reasoning between reasoning parts", [][]m{{r1, signedReasoning, r2}}, []r87integSummary{reasoning("R1", ""), reasoning("", r87integSig), reasoning("R2", "")}, []m{r1, signedReasoning, r2}},
		{"text-only trailing signature normalization", [][]m{{a}, {signedText}}, []r87integSummary{text("A", r87integSig)}, []m{{"text": "A", "thoughtSignature": r87integSig}}},
		{"unsigned empty text remains removed", [][]m{{a, noArgs, {"text": ""}, b}}, []r87integSummary{text("A", ""), readTheme, text("B", "")}, []m{a, noArgs, b}},
	}
	tools := []types.Tool{
		{Name: "lookup", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{
			"query":   map[string]interface{}{"type": "string"},
			"options": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"limit": map[string]interface{}{"type": "number"}, "enabled": map[string]interface{}{"type": "boolean"}}, "required": []interface{}{"limit", "enabled"}},
		}, "required": []interface{}{"query", "options"}}},
		{Name: "read_theme", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var requests []map[string]interface{}
			var sse strings.Builder
			for i, parts := range tc.groups {
				cand := m{"content": m{"role": "model", "parts": parts}, "index": 0}
				if i == len(tc.groups)-1 {
					cand["finishReason"] = "STOP"
				}
				line, _ := json.Marshal(m{"candidates": []m{cand}})
				sse.WriteString("data: " + string(line) + "\n\n")
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				var req map[string]interface{}
				_ = json.Unmarshal(body, &req)
				mu.Lock()
				requests = append(requests, req)
				n := len(requests)
				mu.Unlock()
				if n == 1 {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = w.Write([]byte(sse.String()))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"Done"}]},"finishReason":"STOP","index":0}]}`))
			}))
			defer srv.Close()
			model, err := New(Config{APIKey: "dummy-key", BaseURL: srv.URL}).LanguageModel("gemini-2.5-flash")
			if err != nil {
				t.Fatal(err)
			}
			res, err := ai.StreamText(context.Background(), ai.StreamTextOptions{Model: model, Prompt: "Synthetic preservation check", Tools: tools})
			if err != nil {
				t.Fatal(err)
			}
			var signedTypes []provider.ChunkType
			for c := range res.Chunks() {
				if c.Type == provider.ChunkTypeError {
					t.Fatalf("error part: %v", c.Err)
				}
				if c.ProviderMetadata != nil {
					var md map[string]map[string]interface{}
					if json.Unmarshal(c.ProviderMetadata, &md) == nil && md["google"]["thoughtSignature"] == r87integSig {
						signedTypes = append(signedTypes, c.Type)
					}
				}
			}
			if err := res.Err(); err != nil {
				t.Fatal(err)
			}
			step := res.FinalStep()
			if got := r87integSummarize(t, step.Content); !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("step content = %+v\nwant %+v", got, tc.expected)
			}
			var assistant *types.Message
			for i := range step.Response.Messages {
				if step.Response.Messages[i].Role == types.RoleAssistant {
					assistant = &step.Response.Messages[i]
				}
			}
			if assistant == nil {
				t.Fatal("no assistant response message")
			}
			if got := r87integSummarize(t, assistant.Content); !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("response message = %+v\nwant %+v", got, tc.expected)
			}
			var wantType string
			for _, e := range tc.expected {
				if e.Signature == r87integSig {
					wantType = e.Type
					break
				}
			}
			if wantType != "" {
				if len(signedTypes) == 0 {
					t.Error("no stream part carries the signature")
				}
				for _, ct := range signedTypes {
					// Go's delta chunk types are "text" / "reasoning".
					if string(ct) != wantType && !strings.HasPrefix(string(ct), wantType+"-") {
						t.Errorf("signed part %q is not a %s part", ct, wantType)
					}
				}
			}

			msgs := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Synthetic preservation check"}}}}
			msgs = append(msgs, step.Response.Messages...)
			if len(step.ToolCalls) > 0 {
				var results []types.ContentPart
				for _, c := range step.ToolCalls {
					results = append(results, types.ToolResultContent{ToolCallID: c.ID, ToolName: c.ToolName,
						Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"done": true}}})
				}
				msgs = append(msgs, types.Message{Role: types.RoleTool, Content: results})
			}
			msgs = append(msgs, types.Message{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Continue"}}})
			if _, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{Model: model, Tools: tools, Messages: msgs}); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(requests) != 2 {
				t.Fatalf("requests = %d", len(requests))
			}
			wantWire := make([]interface{}, len(tc.wire))
			for i, p := range tc.wire {
				q := m{}
				for k, v := range p {
					q[k] = v
				}
				if fc, ok := p["functionCall"].(m); ok {
					nfc := m{"id": fc["id"], "name": fc["name"], "args": m{}}
					if v, ok := fc["args"]; ok {
						nfc["args"] = v
					}
					q["functionCall"] = nfc
				}
				wantWire[i] = q
			}
			wantJSON, _ := json.Marshal(wantWire)
			contents, _ := requests[1]["contents"].([]interface{})
			var gotParts interface{}
			for _, c := range contents {
				if cm, _ := c.(map[string]interface{}); cm["role"] == "model" {
					gotParts = cm["parts"]
					break
				}
			}
			gotJSON, _ := json.Marshal(gotParts)
			var gw, ww interface{}
			_ = json.Unmarshal(gotJSON, &gw)
			_ = json.Unmarshal(wantJSON, &ww)
			if !reflect.DeepEqual(gw, ww) {
				t.Errorf("replayed model parts = %s\nwant %s", gotJSON, wantJSON)
			}
			last, _ := json.Marshal(contents[len(contents)-1])
			if string(last) != `{"parts":[{"text":"Continue"}],"role":"user"}` {
				t.Errorf("last content = %s", last)
			}

			// Cross-provider replay (triage §6 item 7): the signed empty text
			// part kept above reaches the Anthropic and Bedrock converters only
			// with Google provider options, and both drop it.
			for _, msg := range step.Response.Messages {
				if msg.Role != types.RoleAssistant {
					continue
				}
				blocks := prompt.ToAnthropicMessages([]types.Message{msg})
				bj, _ := json.Marshal(blocks)
				if strings.Contains(string(bj), `"text":""`) {
					t.Errorf("anthropic replay sends an empty text block: %s", bj)
				}
			}
		})
	}
}
