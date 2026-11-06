package openresponses

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS doGenerate does not validate the body (jsonSchema only applies types) and
// sets timestamp new Date(response.created_at * 1000): a fractional
// created_at keeps its milliseconds, null is the epoch (null * 1000 is 0) and
// a non-numeric one is an Invalid Date; none of them fails the call.
func TestR79provbOpenResponsesCreatedAtTolerated(t *testing.T) {
	for _, tc := range []struct {
		name, createdAt string
		want            time.Time
		wantField       int64
	}{
		{"fractional", `"created_at":1700000000.75,`, time.UnixMilli(1700000000750), 1700000000},
		{"null", `"created_at":null,`, time.UnixMilli(0), 0},
		{"string", `"created_at":"soon",`, time.Time{}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"resp_1",` + tc.createdAt + `"completed_at":1700000001.5,"model":"m","output":[{"type":"message","content":[{"type":"output_text","text":"done"}]}],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`))
			}))
			defer srv.Close()
			m := NewLanguageModel(New(Config{BaseURL: srv.URL, Name: "open-responses"}), "m")
			res, err := m.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
			if err != nil {
				t.Fatal(err)
			}
			ts := res.ResponseMetadata.Timestamp
			if ts.IsZero() != tc.want.IsZero() || !ts.Equal(tc.want) {
				t.Fatalf("timestamp = %v, want %v", ts, tc.want)
			}
			if len(res.Content) == 0 {
				t.Fatal("no content decoded")
			}
		})
	}
}

// openResponsesResponseWire truncates fractional unix seconds and keeps every
// other field.
func TestR79provbOpenResponsesResponseWireFractional(t *testing.T) {
	var w openResponsesResponseWire
	if err := json.Unmarshal([]byte(`{"id":"r","created_at":12.9,"completed_at":-3.5,"status":"completed","model":"m","output":[]}`), &w); err != nil {
		t.Fatal(err)
	}
	r := w.response()
	if r.ID != "r" || r.Status != "completed" || r.Model != "m" || r.Output == nil {
		t.Fatalf("fields lost: %+v", r)
	}
	if r.CreatedAt != 12 || r.CompletedAt == nil || *r.CompletedAt != -3 {
		t.Fatalf("created_at %d completed_at %v", r.CreatedAt, r.CompletedAt)
	}
	var absent openResponsesResponseWire
	if err := json.Unmarshal([]byte(`{"id":"r","completed_at":null}`), &absent); err != nil || absent.response().CompletedAt != nil || absent.response().CreatedAt != 0 {
		t.Fatalf("absent: %+v %v", absent, err)
	}
}

// TS doStream parses events with z.any(): a response.completed event whose
// response has a fractional created_at still finishes with its usage.
func TestR79provbOpenResponsesStreamFractionalCreatedAt(t *testing.T) {
	body := `data: {"type":"response.created","response":{"id":"r","created_at":1700000000.5,"model":"m"}}` + "\n\n" +
		`data: {"type":"response.output_text.delta","delta":"Hi"}` + "\n\n" +
		`data: {"type":"response.completed","response":{"id":"r","created_at":1700000000.5,"status":"completed","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}` + "\n\n"
	s := newOpenResponsesStream(io.NopCloser(strings.NewReader(body)), nil)
	defer func() { _ = s.Close() }()
	var finish *provider.StreamChunk
	for i := 0; i < 100; i++ {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeError {
			t.Fatalf("unexpected error part: %+v", c)
		}
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
	}
	if finish == nil || finish.FinishReason != types.FinishReasonStop || finish.Usage == nil || finish.Usage.OutputTokens == nil || *finish.Usage.OutputTokens != 2 {
		t.Fatalf("finish = %+v", finish)
	}
}

// TS custom_tool_call input: JSON.stringify(input), which keeps <, > and &
// literal (Go's json.Marshal escapes them as < etc.).
func TestR79provbOpenResponsesCustomToolInputNotHTMLEscaped(t *testing.T) {
	const want = `"a<b && c>d"`
	t.Run("generate", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"resp_1","created_at":1,"model":"m","output":[{"type":"custom_tool_call","id":"ct_1","call_id":"call_1","name":"grammar","input":"a<b && c>d"}],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`))
		}))
		defer srv.Close()
		m := NewLanguageModel(New(Config{BaseURL: srv.URL, Name: "open-responses"}), "m")
		res, err := m.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.ToolCalls) != 1 || res.ToolCalls[0].RawArguments != want {
			t.Fatalf("tool calls = %+v", res.ToolCalls)
		}
	})
	t.Run("stream", func(t *testing.T) {
		body := `data: {"type":"response.output_item.added","item":{"type":"custom_tool_call","id":"ct_1","call_id":"call_1","name":"grammar","input":""}}` + "\n\n" +
			`data: {"type":"response.output_item.done","item":{"type":"custom_tool_call","id":"ct_1","call_id":"call_1","name":"grammar","input":"a<b && c>d"}}` + "\n\n"
		s := newOpenResponsesStream(io.NopCloser(strings.NewReader(body)), nil)
		defer func() { _ = s.Close() }()
		var call *types.ToolCall
		for i := 0; i < 100; i++ {
			c, err := s.Next()
			if err != nil {
				break
			}
			if c.Type == provider.ChunkTypeToolCall {
				call = c.ToolCall
			}
		}
		if call == nil || call.RawArguments != want {
			t.Fatalf("tool call = %+v", call)
		}
	})
}

// r79provbProviderToolEntry matches one `['name', { type: 'provider', id:
// '...', name: '...' }]` entry of a TS providerToolsByName Map literal (a Map
// extracts as {}).
var r79provbProviderToolEntry = regexp.MustCompile(`\[\s*'([^']+)',\s*\{\s*type:\s*'provider',\s*id:\s*'([^']+)',\s*name:\s*'([^']+)'`)

// TestR79provbTSCasesConvertInput replays convert-to-open-responses-input.test.ts
// (TS convertToOpenResponsesInput, extracted at e64ed82f27 with --expand-each
// --fn=convertToOpenResponsesInput) through ConvertToOpenResponsesInputForProviderStrict.
func TestR79provbTSCasesConvertInput(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/convert_input.json.gz")
	srcByTest := map[string]string{}
	for _, c := range cases {
		if len(c.FnArgSrc) > 0 {
			srcByTest[c.Test] = c.FnArgSrc[0]
		}
	}
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		msgs, why := tsfixture.BuildPromptMessages(in["prompt"])
		if why != "" {
			return nil, nil, fmt.Errorf("%w: %s", tsfixture.ErrUnsupported, why)
		}
		providerName := "open-responses" // TS providerOptionsName default
		if name, ok := in["providerOptionsName"].(string); ok {
			providerName = name
		}
		strict, _ := in["strictResponseInput"].(bool)
		var ext openResponsesExtensionOptions
		ext.CustomToolID, _ = in["customToolId"].(string)
		if _, ok := in["providerToolsByName"]; ok {
			found := false
			for _, src := range srcByTest {
				for _, m := range r79provbProviderToolEntry.FindAllStringSubmatch(src, -1) {
					if strings.Contains(src, `customToolId: '`+ext.CustomToolID+`'`) {
						ext.Tools = append(ext.Tools, types.Tool{Type: "provider", ProviderID: m[2], Name: m[3]})
						found = true
					}
				}
				if found {
					break
				}
			}
			if !found {
				return nil, nil, fmt.Errorf("%w: providerToolsByName", tsfixture.ErrUnsupported)
			}
		}
		input, instructions, warnings, err := ConvertToOpenResponsesInputForProviderStrict(msgs, "", providerName, strict, ext)
		if err != nil {
			return nil, nil, err
		}
		b, _ := json.Marshal(input)
		var out interface{}
		_ = json.Unmarshal(b, &out)
		if out == nil {
			out = []interface{}{}
		}
		result := map[string]interface{}{"input": out, "warnings": tsfixture.WarningsToTS(warnings)}
		if instructions != "" {
			result["instructions"] = instructions
		}
		return result, nil, nil
	}, map[string]string{
		"should convert tool message with json output":                     "tool output JSON text keeps TS object key order, which a Go map cannot",
		"should convert user - assistant (tool call) - tool message chain": "tool output JSON text keeps TS object key order, which a Go map cannot",
		"should convert a tool roundtrip with follow-up assistant message": "tool output JSON text keeps TS object key order, which a Go map cannot",
	})
}

// The TS case "should preserve interleaved assistant content order" asserts
// with a conditional expression the replay cannot evaluate: the item types
// are reasoning, function_call, message, in content order.
func TestR79provbOpenResponsesInterleavedAssistantOrder(t *testing.T) {
	msgs := []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
		types.ReasoningContent{Text: "Analyzing the problem"},
		types.ToolCallContent{ToolCallID: "call_123", ToolName: "search", Input: "{}"},
		types.TextContent{Text: "Answer after the call"},
	}}}
	input, _, _, err := ConvertToOpenResponsesInputForProviderStrict(msgs, "", "open-responses", false)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(input)
	var items []map[string]interface{}
	if err := json.Unmarshal(b, &items); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, it := range items {
		got = append(got, fmt.Sprint(it["type"]))
	}
	if strings.Join(got, ",") != "reasoning,function_call,message" {
		t.Fatalf("item types = %v", got)
	}
}

// TestR79provbTSCasesFinishReason replays map-open-responses-finish-reason.test.ts
// (extracted at e64ed82f27 with --expand-each --fn=mapOpenResponsesFinishReason).
func TestR79provbTSCasesFinishReason(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/finish_reason.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		reason, _ := in["finishReason"].(string) // undefined and null are no reason
		hasToolCalls, _ := in["hasToolCalls"].(bool)
		return string(MapOpenResponsesFinishReason(reason, hasToolCalls)), nil, nil
	}, nil)
}
