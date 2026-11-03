package google

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestTSCasesInteractionsConvertInput replays
// interactions/convert-to-google-interactions-input.test.ts.
func TestTSCasesInteractionsConvertInput(t *testing.T) {
	m := NewInteractionsLanguageModel(New(Config{APIKey: "k"}), "gemini-2.5-flash")
	cases := tsfixture.LoadCases(t, "testdata/tscases/interactions_convert_input.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		msgs, why := tsfixture.BuildPromptMessages(in["prompt"])
		if why != "" {
			return nil, nil, fmt.Errorf("%w: %s", tsfixture.ErrUnsupported, why)
		}
		opts := GoogleInteractionsProviderOptions{}
		opts.PreviousInteractionID, _ = in["previousInteractionId"].(string)
		opts.MediaResolution, _ = in["mediaResolution"].(string)
		if v, ok := in["store"].(bool); ok {
			opts.Store = &v
		}
		steps, system, warnings, err := m.convertPrompt(types.Prompt{Messages: msgs}, opts)
		if err != nil {
			return nil, nil, err
		}
		res := map[string]interface{}{"input": steps, "warnings": tsfixture.WarningsToTS(warnings)}
		if system != "" {
			res["systemInstruction"] = system
		}
		return res, nil, nil
	}, nil)
}

// TestTSCasesInteractionsFinishReason replays
// interactions/map-google-interactions-finish-reason.test.ts.
func TestTSCasesInteractionsFinishReason(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/interactions_finish_reason.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		status, _ := in["status"].(string)
		hasFn, _ := in["hasFunctionCall"].(bool)
		return string(mapInteractionsFinishReason(status, hasFn)), nil, nil
	}, nil)
}

// TestTSCasesInteractionsPrepareTools replays
// interactions/prepare-google-interactions-tools.test.ts.
func TestTSCasesInteractionsPrepareTools(t *testing.T) {
	tsfixture.ProviderToolHook = func(id, name string, args map[string]interface{}) (types.Tool, bool) {
		return types.Tool{Name: name, Type: "provider", ProviderID: id, ProviderArgs: args}, true
	}
	defer func() { tsfixture.ProviderToolHook = nil }()
	cases := tsfixture.LoadCases(t, "testdata/tscases/interactions_prepare_tools.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		ts, why := tsfixture.BuildTools(in["tools"])
		if why != "" {
			return nil, nil, fmt.Errorf("%w: %s", tsfixture.ErrUnsupported, why)
		}
		var choice types.ToolChoice
		if tc, ok := in["toolChoice"].(map[string]interface{}); ok {
			choice = tsfixture.BuildToolChoice(tc)
		}
		tools, toolChoice, warnings := prepareInteractionsTools(ts, choice)
		return map[string]interface{}{
			"tools":        tools,
			"toolChoice":   toolChoice,
			"toolWarnings": tsfixture.WarningsToTS(warnings),
		}, nil, nil
	}, nil)
}

// r83provaGenerateID mirrors the TS test generators: parse-google-interactions-
// outputs.test.ts returns 'gen-id'; extract-google-interactions-sources.test.ts
// counts `id-${++nextId}` from 0 per test.
func r83provaGenerateID(v interface{}) func() string {
	m, _ := v.(map[string]interface{})
	src, _ := m["src"].(string)
	if strings.Contains(src, "'gen-id'") {
		return func() string { return "gen-id" }
	}
	n := 0
	return func() string { n++; return fmt.Sprintf("id-%d", n) }
}

func r83provaDecode(v interface{}, out interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func r83provaContentToTS(parts []types.ContentPart) []interface{} {
	got, _ := tsfixture.GenerateContentToTS(&types.GenerateResult{Content: parts}).([]interface{})
	if got == nil {
		got = []interface{}{}
	}
	return got
}

// TestTSCasesInteractionsParseOutputs replays
// interactions/parse-google-interactions-outputs.test.ts.
func TestTSCasesInteractionsParseOutputs(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/interactions_parse_outputs.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		var steps []interactionsContentBlock
		if err := r83provaDecode(in["steps"], &steps); err != nil {
			return nil, nil, err
		}
		m := NewInteractionsLanguageModelWithConfig(InteractionsConfig{GenerateID: r83provaGenerateID(in["generateId"])}, "gemini-2.5-flash")
		interactionID, _ := in["interactionId"].(string)
		content, _, hasFn := m.parseOutputs(steps, interactionID)
		return map[string]interface{}{"content": r83provaContentToTS(content), "hasFunctionCall": hasFn}, nil, nil
	}, nil)
}

// TestTSCasesInteractionsParseOutputsEmptyBuiltinID ports "generates an ID
// when a built-in tool call ID is empty" (the steps are an inline cast the
// extractor does not evaluate).
func TestTSCasesInteractionsParseOutputsEmptyBuiltinID(t *testing.T) {
	m := NewInteractionsLanguageModelWithConfig(InteractionsConfig{GenerateID: func() string { return "gen-id" }}, "gemini-2.5-flash")
	var steps []interactionsContentBlock
	if err := json.Unmarshal([]byte(`[{"type":"google_search_call","id":"","arguments":{"query":"weather"}}]`), &steps); err != nil {
		t.Fatal(err)
	}
	content, _, _ := m.parseOutputs(steps, "")
	got := r83provaContentToTS(content)
	want := map[string]interface{}{"type": "tool-call", "toolCallId": "gen-id", "toolName": "google_search", "providerExecuted": true}
	if len(got) == 0 {
		t.Fatal("no content")
	}
	part, _ := got[0].(map[string]interface{})
	for k, v := range want {
		if part[k] != v {
			t.Errorf("content[0].%s = %v, TS %v", k, part[k], v)
		}
	}
}

// TestTSCasesInteractionsSources replays
// interactions/extract-google-interactions-sources.test.ts (annotationToSource,
// annotationsToSources, builtinToolResultToSources).
func TestTSCasesInteractionsSources(t *testing.T) {
	all := tsfixture.LoadCases(t, "testdata/tscases/interactions_sources.json.gz")
	byFn := map[string][]tsfixture.Case{}
	for _, c := range all {
		byFn[c.TopDescribe] = append(byFn[c.TopDescribe], c)
	}
	if len(byFn) != 3 {
		t.Fatalf("describes = %d, want 3", len(byFn))
	}
	sourcesFns := map[string]func(in map[string]interface{}) ([]interface{}, error){
		"annotationsToSources": func(in map[string]interface{}) ([]interface{}, error) {
			var anns []interactionsAnnotation
			err := r83provaDecode(in["annotations"], &anns)
			return r83provaContentToTS(annotationsToSources(anns, r83provaGenerateID(in["generateId"]))), err
		},
		"builtinToolResultToSources": func(in map[string]interface{}) ([]interface{}, error) {
			var block interactionsContentBlock
			err := r83provaDecode(in["block"], &block)
			return r83provaContentToTS(builtinToolResultSources(block, r83provaGenerateID(in["generateId"]))), err
		},
	}
	// The harness does not evaluate the ternary map these tests assert
	// (`sources.map(s => (s.sourceType === 'url' ? s.url : null))`); check it
	// here.
	mapped := 0
	for _, c := range all {
		for _, a := range c.Asserts {
			if a.Expr != "sources.map(s => (s.sourceType === 'url' ? s.url : null))" {
				continue
			}
			mapped++
			in, _ := c.FnArgs[0].(map[string]interface{})
			got, err := sourcesFns[c.TopDescribe](in)
			if err != nil {
				t.Fatalf("%s: %v", c.Test, err)
			}
			urls := make([]interface{}, 0, len(got))
			for _, g := range got {
				s, _ := g.(map[string]interface{})
				if s["sourceType"] == "url" {
					urls = append(urls, s["url"])
				} else {
					urls = append(urls, nil)
				}
			}
			if d := tsfixture.DiffValues(a.Arg, urls); d != "" {
				t.Errorf("%s: %s", c.Test, d)
			}
		}
	}
	if mapped != 2 {
		t.Errorf("ternary map assertions = %d, want 2", mapped)
	}
	t.Run("annotationToSource", func(t *testing.T) {
		tsfixture.RunFn(t, byFn["annotationToSource"], func(args []interface{}) (interface{}, map[string]interface{}, error) {
			in, _ := args[0].(map[string]interface{})
			var ann interactionsAnnotation
			if err := r83provaDecode(in["annotation"], &ann); err != nil {
				return nil, nil, err
			}
			got := r83provaContentToTS(annotationsToSources([]interactionsAnnotation{ann}, r83provaGenerateID(in["generateId"])))
			if len(got) == 0 {
				return nil, nil, nil
			}
			return got[0], nil, nil
		}, nil)
	})
	t.Run("annotationsToSources", func(t *testing.T) {
		tsfixture.RunFn(t, byFn["annotationsToSources"], func(args []interface{}) (interface{}, map[string]interface{}, error) {
			in, _ := args[0].(map[string]interface{})
			var anns []interactionsAnnotation
			if u, _ := in["annotations"].(map[string]interface{}); u["__undef"] == true {
				in["annotations"] = nil
			}
			if err := r83provaDecode(in["annotations"], &anns); err != nil {
				return nil, nil, err
			}
			return r83provaContentToTS(annotationsToSources(anns, r83provaGenerateID(in["generateId"]))), nil, nil
		}, nil)
	})
	t.Run("builtinToolResultToSources", func(t *testing.T) {
		tsfixture.RunFn(t, byFn["builtinToolResultToSources"], func(args []interface{}) (interface{}, map[string]interface{}, error) {
			in, _ := args[0].(map[string]interface{})
			var block interactionsContentBlock
			if err := r83provaDecode(in["block"], &block); err != nil {
				return nil, nil, err
			}
			return r83provaContentToTS(builtinToolResultSources(block, r83provaGenerateID(in["generateId"]))), nil, nil
		}, nil)
	})
}

// r83provaRunTransform is the TS test helper runTransform: the events go
// through the interactions stream (SSE data frames) with generateId
// 'test-id'; it returns the raw chunks and the TS-shaped stream parts.
func r83provaRunTransform(events interface{}) ([]*provider.StreamChunk, []interface{}, error) {
	list, _ := events.([]interface{})
	var b strings.Builder
	for _, e := range list {
		raw, err := json.Marshal(e)
		if err != nil {
			return nil, nil, err
		}
		b.WriteString("data: " + string(raw) + "\n\n")
	}
	stream := newInteractionsEventStream(context.Background(), New(Config{APIKey: "k"}).client, io.NopCloser(strings.NewReader(b.String())), "", nil, []types.Warning{}, nil, 0)
	stream.(*interactionsStream).generateID = func() string { return "test-id" }
	defer func() { _ = stream.Close() }()
	var chunks []*provider.StreamChunk
	for {
		c, err := stream.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		chunks = append(chunks, c)
	}
	return chunks, tsfixture.StreamToTS(chunks), nil
}

func r83provaPartsOf(parts []interface{}, keep func(typ string) bool) []interface{} {
	out := []interface{}{}
	for _, p := range parts {
		m, _ := p.(map[string]interface{})
		if typ, _ := m["type"].(string); keep(typ) {
			out = append(out, m)
		}
	}
	return out
}

func r83provaJSON(t *testing.T, s string) interface{} {
	t.Helper()
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// TestTSCasesInteractionsStreamTransform replays
// interactions/build-google-interactions-stream-transform.test.ts: the event
// lists of the six extracted cases come from the TS test file, the
// assertions (filters the harness does not evaluate) are written out here.
func TestTSCasesInteractionsStreamTransform(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/interactions_stream_transform.json.gz")
	byName := map[string]interface{}{}
	for _, c := range cases {
		byName[c.Test] = c.FnArgs[0]
	}
	run := func(t *testing.T, name string) ([]*provider.StreamChunk, []interface{}) {
		t.Helper()
		events, ok := byName[name]
		if !ok {
			t.Fatalf("case %q not extracted", name)
		}
		chunks, parts, err := r83provaRunTransform(events)
		if err != nil {
			t.Fatal(err)
		}
		return chunks, parts
	}
	isType := func(want string) func(string) bool { return func(typ string) bool { return typ == want } }
	check := func(t *testing.T, want string, got interface{}) {
		t.Helper()
		if d := tsfixture.DiffValues(r83provaJSON(t, want), got); d != "" {
			t.Error(d)
		}
	}
	files := func(t *testing.T, name string) []interface{} {
		_, parts := run(t, name)
		files := r83provaPartsOf(parts, isType("file"))
		for _, f := range files {
			delete(f.(map[string]interface{}), "providerMetadata") // expect.objectContaining
		}
		return files
	}
	t.Run("video inline data", func(t *testing.T) {
		check(t, `[{"type":"file","mediaType":"video/mp4","data":{"type":"data","data":"AAAAIGZ0eXBpc29t"}}]`,
			files(t, "emits a file stream part for a video delta carrying inline data"))
	})
	t.Run("video uri", func(t *testing.T) {
		check(t, `[{"type":"file","mediaType":"video/mp4","data":{"type":"url","url":"https://example.test/clip.mp4"}}]`,
			files(t, "emits a file stream part for a video delta carrying a uri"))
	})
	t.Run("usage modality", func(t *testing.T) {
		_, parts := run(t, "surfaces output_tokens_by_modality on the finish part providerMetadata")
		finish := r83provaPartsOf(parts, isType("finish"))
		if len(finish) != 1 {
			t.Fatalf("finish parts = %v", finish)
		}
		meta, _ := finish[0].(map[string]interface{})["providerMetadata"].(map[string]interface{})
		google, _ := meta["google"].(map[string]interface{})
		check(t, `{"video":57920,"text":19}`, google["outputTokensByModality"])
	})
	tools := func(typ string) bool { return strings.HasPrefix(typ, "tool-") }
	t.Run("empty function call id", func(t *testing.T) {
		_, parts := run(t, "uses the generated block ID when a function call ID is empty")
		check(t, `[
			{"type":"tool-input-start","id":"interaction-1:0","toolName":"get_weather"},
			{"type":"tool-input-delta","id":"interaction-1:0","delta":"{}"},
			{"type":"tool-input-end","id":"interaction-1:0"},
			{"type":"tool-call","toolCallId":"interaction-1:0","toolName":"get_weather","input":"{}","providerMetadata":{"google":{"interactionId":"interaction-1"}}}
		]`, r83provaPartsOf(parts, tools))
	})
	t.Run("empty built-in ids", func(t *testing.T) {
		_, parts := run(t, "preserves generated block IDs when built-in tool deltas contain empty IDs")
		check(t, `[
			{"type":"tool-call","toolCallId":"interaction-1:0","toolName":"google_search","input":"{\"queries\":[\"weather\"]}","providerExecuted":true},
			{"type":"tool-result","toolCallId":"interaction-1:1","toolName":"google_search","result":[]}
		]`, r83provaPartsOf(parts, tools))
	})
	t.Run("error event", func(t *testing.T) {
		chunks, parts := run(t, "preserves the event type, code, and raw error event")
		var errChunk *provider.StreamChunk
		for _, c := range chunks {
			if c.Type == provider.ChunkTypeError {
				errChunk = c
				break
			}
		}
		if errChunk == nil {
			t.Fatal("no error part")
		}
		var spe *providererrors.StreamProviderError
		if !errors.As(errChunk.Err, &spe) {
			t.Fatalf("error = %T, TS isProviderStreamError", errChunk.Err)
		}
		if spe.Message != "Rate limit reached" || spe.Type != "error" || spe.Code != "429" {
			t.Errorf("error = %+v, TS message %q type %q code %q", spe, "Rate limit reached", "error", "429")
		}
		check(t, `{"event_type":"error","event_id":"event-error","error":{"code":"429","message":"Rate limit reached"}}`, tsfixture.WNorm(spe.Data))
		last, _ := parts[len(parts)-1].(map[string]interface{})
		reason, _ := last["finishReason"].(map[string]interface{})
		if last["type"] != "finish" || reason["unified"] != "error" {
			t.Errorf("last part = %v, TS finish with unified error", last)
		}
	})
	t.Run("processing steps", func(t *testing.T) {
		// Not extracted (the event list has no cast); events from the TS test.
		_, parts, err := r83provaRunTransform(r83provaJSON(t, `[
			{"event_type":"interaction.created","interaction":{"id":"interaction-1","status":"in_progress"}},
			{"event_type":"step.start","index":0,"step":{"type":"processing_call","id":"processing-1","signature":"call-signature"}},
			{"event_type":"step.stop","index":0},
			{"event_type":"step.start","index":1,"step":{"type":"processing_result","call_id":"processing-1","signature":"result-signature"}},
			{"event_type":"step.stop","index":1},
			{"event_type":"interaction.completed","interaction":{"id":"interaction-1","status":"completed"}}
		]`))
		if err != nil {
			t.Fatal(err)
		}
		check(t, `[
			{"type":"custom","kind":"google.processing_call","providerMetadata":{"google":{"signature":"call-signature","interactionId":"interaction-1","processingId":"processing-1"}}},
			{"type":"custom","kind":"google.processing_result","providerMetadata":{"google":{"signature":"result-signature","interactionId":"interaction-1","processingCallId":"processing-1"}}}
		]`, r83provaPartsOf(parts, isType("custom")))
	})
	// it.each 'handles the last status %s as %s' (not extracted: a spread
	// conditional) and 'preserves an explicit completion event without a
	// status' (the events are an inline cast).
	for _, tc := range []struct{ status, unified string }{
		{"", "interrupted"}, {"in_progress", "interrupted"}, {"requires_action", "tool-calls"},
		{"completed", "stop"}, {"cancelled", "other"}, {"unknown_status", "other"},
	} {
		t.Run("last status "+tc.status, func(t *testing.T) {
			events := []interface{}{
				r83provaJSON(t, `{"event_type":"interaction.created","interaction":{"id":"i1","status":"in_progress"}}`),
				r83provaJSON(t, `{"event_type":"step.start","index":0,"step":{"type":"model_output"}}`),
				r83provaJSON(t, `{"event_type":"step.delta","index":0,"delta":{"type":"text","text":"Partial answer"}}`),
			}
			if tc.status != "" {
				events = append(events, map[string]interface{}{"event_type": "interaction.status_update", "status": tc.status})
			}
			_, parts, err := r83provaRunTransform(events)
			if err != nil {
				t.Fatal(err)
			}
			if deltas := r83provaPartsOf(parts, isType("text-delta")); len(deltas) != 1 || deltas[0].(map[string]interface{})["delta"] != "Partial answer" {
				t.Errorf("text deltas = %v", deltas)
			}
			if errs := r83provaPartsOf(parts, isType("error")); len(errs) != 0 {
				t.Errorf("error parts = %v", errs)
			}
			finish := r83provaPartsOf(parts, isType("finish"))
			if len(finish) != 1 {
				t.Fatalf("finish parts = %v", finish)
			}
			var raw interface{}
			if tc.unified != "interrupted" {
				raw = tc.status
			}
			check(t, fmt.Sprintf(`{"unified":%q,"raw":%s}`, tc.unified, r83provaJSONText(raw)), finish[0].(map[string]interface{})["finishReason"])
		})
	}
	t.Run("completion without status", func(t *testing.T) {
		_, parts, err := r83provaRunTransform([]interface{}{r83provaJSON(t, `{"event_type":"interaction.completed","interaction":{"id":"i1"}}`)})
		if err != nil {
			t.Fatal(err)
		}
		finish := r83provaPartsOf(parts, isType("finish"))
		if len(finish) != 1 {
			t.Fatalf("finish parts = %v", finish)
		}
		check(t, `{"unified":"other","raw":null}`, finish[0].(map[string]interface{})["finishReason"])
	})
}

func r83provaJSONText(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
