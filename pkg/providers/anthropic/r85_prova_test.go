package anthropic

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR85provaNativeSchemaDifferential replays TS hasCircularSchemaReference
// and supportsNativeStructuredOutputSchema (ai@30548ee7f9, TS f4c5f1e959 /
// b0d51fecc0) over the TS test schemas, pointer edge cases and 300 generated
// schemas, recorded by testdata/tscases/regen/native_schema_diff.mjs:
//
//	node native_schema_diff.mjs | gzip -9n > ../native_schema_diff.json.gz
func TestR85provaNativeSchemaDifferential(t *testing.T) {
	f, err := os.Open("testdata/tscases/native_schema_diff.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Schema   interface{} `json:"schema"`
		Circular bool        `json:"circular"`
		Native   bool        `json:"native"`
	}
	if err := json.NewDecoder(zr).Decode(&cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 300 {
		t.Fatalf("only %d cases", len(cases))
	}
	for i, c := range cases {
		if got := hasCircularSchemaReference(c.Schema); got != c.Circular {
			b, _ := json.Marshal(c.Schema)
			t.Errorf("case %d: hasCircularSchemaReference = %v, TS %v: %s", i, got, c.Circular, b)
		}
		if got := SupportsNativeStructuredOutputSchema(c.Schema); got != c.Native {
			b, _ := json.Marshal(c.Schema)
			t.Errorf("case %d: SupportsNativeStructuredOutputSchema = %v, TS %v: %s", i, got, c.Native, b)
		}
	}
}

// has-circular-schema-reference.test.ts and
// supports-native-structured-output-schema.test.ts (function-level cases).
func TestR85provaTSCasesNativeSchemaFns(t *testing.T) {
	for _, file := range []string{"testdata/tscases/has_circular_schema_reference.json.gz", "testdata/tscases/supports_native_structured_output_schema.json.gz"} {
		cases := tsfixture.LoadCases(t, file)
		tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
			if file == "testdata/tscases/has_circular_schema_reference.json.gz" {
				return hasCircularSchemaReference(args[0]), nil, nil
			}
			return SupportsNativeStructuredOutputSchema(args[0]), nil, nil
		}, nil)
	}
}

func r85provaForcedModel(modelID string, native bool) *LanguageModel {
	off := false
	return NewLanguageModel(New(Config{APIKey: "test-api-key", SupportsNativeStructuredOutput: &native, SupportsStrictTools: &off}), modelID, nil)
}

func r85provaBody(t *testing.T, m *LanguageModel, opts *provider.GenerateOptions, stream bool) (map[string]interface{}, *preparedRequest) {
	t.Helper()
	if opts.Prompt.Text == "" {
		opts.Prompt = types.Prompt{Text: "Hello"}
	}
	req, err := m.prepareRequest(opts, stream)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(req.body)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	return body, req
}

func r85provaToolNames(body map[string]interface{}) []string {
	var names []string
	tools, _ := body["tools"].([]interface{})
	for _, tl := range tools {
		names = append(names, tl.(map[string]interface{})["name"].(string))
	}
	return names
}

var r85provaSchema = map[string]interface{}{
	"type":                 "object",
	"properties":           map[string]interface{}{"answer": map[string]interface{}{"type": "string"}},
	"required":             []interface{}{"answer"},
	"additionalProperties": false,
}

var r85provaCircularSchema = map[string]interface{}{
	"type":                 "object",
	"properties":           map[string]interface{}{"root": map[string]interface{}{"$ref": "#/$defs/node"}},
	"required":             []interface{}{"root"},
	"additionalProperties": false,
	"$defs": map[string]interface{}{"node": map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name":     map[string]interface{}{"type": "string"},
			"children": map[string]interface{}{"type": "array", "items": map[string]interface{}{"$ref": "#/$defs/node"}},
		},
		"required":             []interface{}{"name", "children"},
		"additionalProperties": false,
	}},
}

func r85provaJSONCall(s interface{}) *provider.GenerateOptions {
	return &provider.GenerateOptions{ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: s}}
}

// anthropic-forced-tool-structured-output.test.ts (TS f4c5f1e959,
// b0d51fecc0, 2cd0a97424).
func TestR85provaForcedToolStructuredOutput(t *testing.T) {
	// uses native structured outputs for %s when the provider disables them
	for _, id := range []string{"claude-sonnet-5-5", "claude-opus-5-5", "claude-fable-5-1"} {
		body, _ := r85provaBody(t, r85provaForcedModel(id, false), r85provaJSONCall(r85provaSchema), false)
		oc, _ := body["output_config"].(map[string]interface{})
		format, _ := oc["format"].(map[string]interface{})
		if format["type"] != "json_schema" || format["schema"] == nil || body["tools"] != nil || body["tool_choice"] != nil {
			t.Errorf("%s: body = %v", id, body)
		}
	}

	// keeps the JSON response tool for self-referencing schemas
	for _, native := range []bool{false, true} {
		body, _ := r85provaBody(t, r85provaForcedModel("claude-sonnet-5-5", native), r85provaJSONCall(r85provaCircularSchema), false)
		tc, _ := body["tool_choice"].(map[string]interface{})
		if body["output_config"] != nil || strings.Join(r85provaToolNames(body), ",") != "json" || tc["type"] != "auto" {
			t.Errorf("circular native=%v: body = %v", native, body)
		}
	}

	mapSchema := map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{"scores": map[string]interface{}{"type": "object", "additionalProperties": map[string]interface{}{"type": "number"}}},
		"required":             []interface{}{"scores"},
		"additionalProperties": false,
	}
	// keeps map values when native structured outputs are enabled
	body, _ := r85provaBody(t, r85provaForcedModel("claude-sonnet-5-5", true), r85provaJSONCall(mapSchema), false)
	if body["output_config"] != nil || strings.Join(r85provaToolNames(body), ",") != "json" {
		t.Errorf("map schema: body = %v", body)
	}
	if tools, _ := body["tools"].([]interface{}); len(tools) == 1 {
		got, _ := json.Marshal(tools[0].(map[string]interface{})["input_schema"])
		want, _ := json.Marshal(mapSchema)
		if string(got) != string(want) {
			t.Errorf("map schema input_schema = %s, want %s", got, want)
		}
	}

	// keeps the JSON response tool for a schema with %s
	optional := map[string]interface{}{}
	for i := 0; i < 11; i++ {
		optional[fmt.Sprintf("f%d", i)] = map[string]interface{}{"type": "string"}
	}
	for name, s := range map[string]interface{}{
		"a map field":                      mapSchema,
		"more than 10 optional properties": map[string]interface{}{"type": "object", "properties": optional, "additionalProperties": false},
	} {
		body, _ := r85provaBody(t, r85provaForcedModel("claude-sonnet-5-5", false), r85provaJSONCall(s), false)
		if body["output_config"] != nil || strings.Join(r85provaToolNames(body), ",") != "json" {
			t.Errorf("%s: body = %v", name, body)
		}
	}

	// still uses the JSON response tool for models that accept forced tool use
	body, _ = r85provaBody(t, r85provaForcedModel("claude-sonnet-4-6", false), r85provaJSONCall(r85provaSchema), false)
	if tc, _ := body["tool_choice"].(map[string]interface{}); body["output_config"] != nil || tc["type"] != "any" {
		t.Errorf("sonnet-4-6: body = %v", body)
	}

	openSchema := map[string]interface{}{"type": "object", "additionalProperties": true}
	for _, stream := range []bool{false, true} {
		for _, id := range []string{"claude-sonnet-4-5", "claude-sonnet-5-5"} {
			// fallback tool restrictions: preserves caller tool choice
			for _, c := range []struct {
				choice types.ToolChoice
				want   string
			}{
				{types.ToolChoice{Type: types.ToolChoiceNone}, "json"},
				{types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "lookup"}, "lookup,json"},
				{types.ToolChoice{Type: types.ToolChoiceAuto}, "lookup,unused,web_search,json"},
				{types.ToolChoice{Type: types.ToolChoiceRequired}, "lookup,unused,web_search,json"},
			} {
				opts := r85provaJSONCall(openSchema)
				opts.ToolChoice = c.choice
				opts.Tools = []types.Tool{
					{Type: types.ToolTypeFunction, Name: "lookup", Parameters: r85provaSchema},
					{Type: types.ToolTypeFunction, Name: "unused", Parameters: r85provaSchema},
					{Type: types.ToolTypeProviderDefined, ProviderID: "anthropic.web_search_20250305", Name: "web_search", ProviderArgs: map[string]interface{}{}},
				}
				body, _ := r85provaBody(t, r85provaForcedModel(id, true), opts, stream)
				if body["output_config"] != nil || strings.Join(r85provaToolNames(body), ",") != c.want {
					t.Errorf("stream=%v %s %v: tools = %v, want %s", stream, id, c.choice, r85provaToolNames(body), c.want)
				}
			}
			// preserves an open schema using the JSON tool
			body, _ := r85provaBody(t, r85provaForcedModel(id, true), r85provaJSONCall(openSchema), stream)
			wantChoice := "auto"
			if id == "claude-sonnet-4-5" {
				wantChoice = "any"
			}
			if tc, _ := body["tool_choice"].(map[string]interface{}); body["output_config"] != nil || strings.Join(r85provaToolNames(body), ",") != "json" || tc["type"] != wantChoice {
				t.Errorf("stream=%v %s open schema: body = %v", stream, id, body)
			}
		}
		// preserves the beta header for tools with explicit outputFormat
		opts := r85provaJSONCall(openSchema)
		opts.Tools = []types.Tool{{Type: types.ToolTypeFunction, Name: "lookup", Parameters: r85provaSchema}}
		opts.ProviderOptions = map[string]interface{}{"anthropic": map[string]interface{}{"structuredOutputMode": "outputFormat"}}
		m := r85provaForcedModel("claude-sonnet-4-5", true)
		body, _ := r85provaBody(t, m, opts, stream)
		if oc, _ := body["output_config"].(map[string]interface{}); oc["format"] == nil {
			t.Errorf("stream=%v outputFormat: body = %v", stream, body)
		}
		if betas := m.combineBetaHeaders(opts, stream); !strings.Contains(betas, "structured-outputs-2025-11-13") {
			t.Errorf("stream=%v outputFormat: anthropic-beta = %q", stream, betas)
		}
	}
}

// anthropic-json-response-tool-fallback.test.ts (TS e10aebbad6), extracted at
// 30548ee7f9 with --asserts plus capture.mjs. The TS model disables native
// structured outputs and strict tools (the Bedrock / Vertex adapters).
func TestR85provaTSCasesJSONResponseToolFallback(t *testing.T) {
	tsfixture.DropResponseTimestamp = true
	defer func() { tsfixture.DropResponseTimestamp = false }()
	cases := tsfixture.LoadCases(t, "testdata/tscases/json_response_tool_fallback.json.gz")
	if len(cases) != 14 {
		t.Fatalf("want 14 cases, got %d", len(cases))
	}
	tsfixture.EvalPathSnapshots(t)
	tsfixture.EvalExtendedAsserts(t)
	tsfixture.Run(t, cases, func(base, id string) provider.LanguageModel {
		off := false
		m, err := New(Config{APIKey: "test-api-key", BaseURL: base, SupportsNativeStructuredOutput: &off, SupportsStrictTools: &off}).LanguageModel(id)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}, nil)
}

// TestR85provaJSONResponseToolFallbackStreamText evaluates the stream
// assertions of anthropic-json-response-tool-fallback.test.ts that tsfixture
// leaves unevaluated: the joined text-delta text (`text`, `textOf(...)`) and
// the reasoning/text part type sequence.
func TestR85provaJSONResponseToolFallbackStreamText(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/json_response_tool_fallback.json.gz")
	checked := 0
	for _, c := range cases {
		if c.CallKind != "stream" || c.Response == nil {
			continue
		}
		c := c
		t.Run(c.Test, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				for _, ch := range c.Response.Chunks {
					_, _ = w.Write([]byte(ch.(string)))
				}
			}))
			defer srv.Close()
			off := false
			m := NewLanguageModel(New(Config{APIKey: "test-api-key", BaseURL: srv.URL, SupportsNativeStructuredOutput: &off, SupportsStrictTools: &off}), c.ModelID, nil)
			opts, why := tsfixture.BuildOptions(c.Call)
			if why != "" {
				t.Fatal(why)
			}
			stream, err := m.DoStream(t.Context(), opts)
			if err != nil {
				t.Fatal(err)
			}
			var chunks []*provider.StreamChunk
			for {
				ch, err := stream.Next()
				if err != nil {
					break
				}
				chunks = append(chunks, ch)
			}
			_ = stream.Close()
			parts := tsfixture.StreamToTS(chunks)
			var text strings.Builder
			var kinds []interface{}
			for _, p := range parts {
				pm, _ := p.(map[string]interface{})
				typ, _ := pm["type"].(string)
				if typ == "text-delta" {
					d, _ := pm["delta"].(string)
					text.WriteString(d)
				}
				if strings.HasPrefix(typ, "reasoning-") || strings.HasPrefix(typ, "text-") {
					kinds = append(kinds, typ)
				}
			}
			for _, a := range c.Asserts {
				switch {
				case a.Expr == "text" || strings.HasPrefix(a.Expr, "textOf("):
					checked++
					if text.String() != a.Arg {
						t.Errorf("text = %q, TS %v", text.String(), a.Arg)
					}
				case strings.Contains(a.Expr, "part.type.startsWith('reasoning-') ||"):
					checked++
					if d := tsfixture.Diff("types", a.Arg, kinds); d != "" {
						t.Errorf("part types: %s", d)
					}
				}
			}
		})
	}
	if checked != 7 {
		t.Fatalf("checked %d stream assertions, want 7", checked)
	}
}

// TS e10aebbad6: a paused message's text is not the answer (generate), and
// held-back stream text keeps its citations on text-end.
func TestR85provaJSONResponseToolFallbackEdges(t *testing.T) {
	m := r85provaForcedModel("claude-sonnet-5-5", false)
	res := m.convertResponse(anthropicResponse{
		Content:    []anthropicContent{{Type: "text", Text: `{"root":{}}`}},
		StopReason: "pause_turn",
	}, true)
	if res.Text != "" {
		t.Fatalf("pause_turn: text = %q, want none", res.Text)
	}
	for _, p := range res.Content {
		if _, ok := p.(types.TextContent); ok {
			t.Fatalf("pause_turn: content = %#v, want no text", res.Content)
		}
	}

	events := []string{
		`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-5-5","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":1}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"{\"answer\":\"42\"}"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"citations_delta","citation":{"type":"web_search_result_location","url":"https://example.com","title":"Example","encrypted_index":"x","cited_text":"42"}}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":20}}`,
		`{"type":"message_stop"}`,
	}
	var sse strings.Builder
	for _, e := range events {
		sse.WriteString("data: " + e + "\n\n")
	}
	s := newAnthropicStream(io.NopCloser(strings.NewReader(sse.String())), true)
	var textEnd *provider.StreamChunk
	for {
		ch, err := s.Next()
		if err != nil {
			break
		}
		if ch.Type == provider.ChunkTypeTextEnd {
			textEnd = ch
		}
	}
	if textEnd == nil || len(textEnd.Citations) != 1 || !strings.Contains(string(textEnd.ProviderMetadata), "web_search_result_location") {
		t.Fatalf("text-end = %#v", textEnd)
	}
}
