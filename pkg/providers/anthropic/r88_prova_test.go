package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
)

// r88provaServer serves one canned response (JSON for generate, SSE events
// for stream) and records the request bodies.
type r88provaServer struct {
	mu      sync.Mutex
	bodies  []map[string]interface{}
	headers []http.Header
	json    string
	events  []string
	srv     *httptest.Server
	paths   []string
}

func r88provaNewServer(t *testing.T, jsonBody string, events []string) *r88provaServer {
	t.Helper()
	s := &r88provaServer{json: jsonBody, events: events}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]interface{}
		_ = json.Unmarshal(raw, &body)
		s.mu.Lock()
		s.bodies = append(s.bodies, body)
		s.headers = append(s.headers, r.Header.Clone())
		s.paths = append(s.paths, r.URL.Path)
		s.mu.Unlock()
		if body["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			for _, e := range s.events {
				_, _ = io.WriteString(w, "data: "+e+"\n\n")
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, s.json)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *r88provaServer) body(i int) map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bodies[i]
}

func r88provaModel(t *testing.T, s *r88provaServer, modelID string) provider.LanguageModel {
	t.Helper()
	m, err := New(Config{APIKey: "k", BaseURL: s.srv.URL}).LanguageModel(modelID)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// r88provaDrain reads a stream to its end and returns the parts and the
// terminal error (nil at EOF).
func r88provaDrain(stream provider.TextStream) ([]*provider.StreamChunk, error) {
	defer stream.Close() //nolint:errcheck
	var parts []*provider.StreamChunk
	for {
		c, err := stream.Next()
		if err == io.EOF {
			return parts, nil
		}
		if err != nil {
			return parts, err
		}
		parts = append(parts, c)
	}
}

func r88provaJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// r88provaMemberResponse is TS prepareMemberResponse (anthropic-language-
// model.test.ts, describe 'computer toolset').
func r88provaMemberResponse(t *testing.T, memberName, toolsetName string, input map[string]interface{}, partialJSON *string) *r88provaServer {
	if input == nil {
		input = map[string]interface{}{}
	}
	pj := r88provaJSON(input)
	if partialJSON != nil {
		pj = *partialJSON
	}
	member := func(streaming bool) map[string]interface{} {
		in := input
		if streaming {
			in = map[string]interface{}{}
		}
		return map[string]interface{}{"type": "tool_use", "id": "toolu_member", "name": memberName, "toolset_name": toolsetName, "input": in}
	}
	message := func(streaming bool) map[string]interface{} {
		m := map[string]interface{}{
			"id": "msg_toolset", "type": "message", "role": "assistant", "model": "claude-opus-5-5",
			"stop_sequence": nil, "usage": map[string]interface{}{"input_tokens": 4, "output_tokens": 30},
		}
		if streaming {
			m["content"] = []interface{}{}
			m["stop_reason"] = nil
		} else {
			m["content"] = []interface{}{member(false)}
			m["stop_reason"] = "tool_use"
		}
		return m
	}
	events := []string{
		r88provaJSON(map[string]interface{}{"type": "message_start", "message": message(true)}),
		r88provaJSON(map[string]interface{}{"type": "content_block_start", "index": 0, "content_block": member(true)}),
		r88provaJSON(map[string]interface{}{"type": "content_block_delta", "index": 0, "delta": map[string]interface{}{"type": "input_json_delta", "partial_json": pj}}),
		r88provaJSON(map[string]interface{}{"type": "content_block_stop", "index": 0}),
		r88provaJSON(map[string]interface{}{"type": "message_delta", "delta": map[string]interface{}{"stop_reason": "tool_use", "stop_sequence": nil}, "usage": map[string]interface{}{"output_tokens": 30}}),
		r88provaJSON(map[string]interface{}{"type": "message_stop"}),
	}
	return r88provaNewServer(t, r88provaJSON(message(false)), events)
}

func r88provaToolset(configs map[tools.ComputerToolset20260801Member]tools.ComputerToolset20260801MemberConfig) types.Tool {
	return tools.ComputerToolset20260801(tools.ComputerToolset20260801Config{Configs: configs})
}

func r88provaBool(b bool) *bool { return &b }

// r88provaToolParts counts the tool-input-start / -end parts of the last
// streamed r88provaCall.
var r88provaToolParts int

// r88provaCall runs the call and returns its tool calls, the tool-input
// deltas (streaming) and the error.
func r88provaCall(t *testing.T, s *r88provaServer, streaming bool, opts *provider.GenerateOptions) ([]types.ToolCall, []string, error) {
	t.Helper()
	m := r88provaModel(t, s, "claude-opus-5-5")
	r88provaToolParts = 0
	if !streaming {
		res, err := m.DoGenerate(context.Background(), opts)
		if err != nil {
			return nil, nil, err
		}
		return res.ToolCalls, nil, nil
	}
	stream, err := m.DoStream(context.Background(), opts)
	if err != nil {
		return nil, nil, err
	}
	parts, err := r88provaDrain(stream)
	r88provaToolParts = 0
	var calls []types.ToolCall
	var deltas []string
	for _, p := range parts {
		switch p.Type {
		case provider.ChunkTypeToolCall:
			calls = append(calls, *p.ToolCall)
		case provider.ChunkTypeToolInputDelta:
			deltas = append(deltas, p.Text)
		case provider.ChunkTypeToolInputStart, provider.ChunkTypeToolInputEnd:
			r88provaToolParts++
		}
	}
	return calls, deltas, err
}

// TS a161c0a665 'should preserve the returned member action'.
func TestR88provaToolsetMemberActionWins(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		s := r88provaMemberResponse(t, "screenshot", "computer", map[string]interface{}{"action": "wait", "duration": 0}, nil)
		calls, deltas, err := r88provaCall(t, s, streaming, &provider.GenerateOptions{
			Prompt: types.Prompt{Text: "hi"},
			Tools: []types.Tool{r88provaToolset(map[tools.ComputerToolset20260801Member]tools.ComputerToolset20260801MemberConfig{
				tools.ComputerToolsetWait: {Enabled: r88provaBool(false)},
			})},
		})
		if err != nil {
			t.Fatalf("streaming %v: %v", streaming, err)
		}
		if len(calls) != 1 {
			t.Fatalf("streaming %v: calls = %d", streaming, len(calls))
		}
		// TS JSON.stringify({...input, action}): an input `action` keeps its
		// position with the member value.
		if calls[0].RawArguments != `{"action":"screenshot","duration":0}` {
			t.Fatalf("streaming %v: input = %s", streaming, calls[0].RawArguments)
		}
		if calls[0].Arguments["action"] != "screenshot" {
			t.Fatalf("streaming %v: args = %v", streaming, calls[0].Arguments)
		}
		if streaming && (len(deltas) != 1 || deltas[0] != calls[0].RawArguments) {
			t.Fatalf("deltas = %v", deltas)
		}
	}
	// action comes last when the input has none (TS snapshot change).
	s := r88provaMemberResponse(t, "left_click", "computer", map[string]interface{}{"coordinate": []int{640, 60}}, nil)
	for _, streaming := range []bool{false, true} {
		calls, _, err := r88provaCall(t, s, streaming, &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Tools: []types.Tool{r88provaToolset(nil)}})
		if err != nil {
			t.Fatal(err)
		}
		if calls[0].RawArguments != `{"coordinate":[640,60],"action":"left_click"}` {
			t.Fatalf("streaming %v: input = %s", streaming, calls[0].RawArguments)
		}
	}
}

// TS a161c0a665 'should reject $scenario members'.
func TestR88provaToolsetRejectsUnavailableMembers(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		for _, scenario := range []string{"disabled", "unknown member", "unknown toolset", "undeclared"} {
			member, toolset := "screenshot", "computer"
			if scenario == "unknown member" {
				member = "unknown"
			}
			if scenario == "unknown toolset" {
				toolset = "other"
			}
			s := r88provaMemberResponse(t, member, toolset, nil, nil)
			var ts []types.Tool
			if scenario != "undeclared" {
				ts = []types.Tool{r88provaToolset(map[tools.ComputerToolset20260801Member]tools.ComputerToolset20260801MemberConfig{
					tools.ComputerToolsetScreenshot: {Enabled: r88provaBool(scenario != "disabled")},
				})}
			}
			calls, _, err := r88provaCall(t, s, streaming, &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Tools: ts})
			var ire *providererrors.InvalidResponseDataError
			if !errors.As(err, &ire) {
				t.Fatalf("%s streaming %v: err = %v, calls = %v", scenario, streaming, err, calls)
			}
			if want := "Received an unavailable toolset member: " + toolset + "." + member + "."; ire.Message != want {
				t.Fatalf("message = %q", ire.Message)
			}
			if len(calls) != 0 || r88provaToolParts != 0 {
				t.Fatalf("%s: tool parts emitted", scenario)
			}
		}
	}
}

// TS a161c0a665 'should reject invalid streamed member input'.
func TestR88provaToolsetRejectsInvalidStreamedInput(t *testing.T) {
	for _, pj := range []string{"{", "null", "[]", `"text"`, "1"} {
		pj := pj
		s := r88provaMemberResponse(t, "screenshot", "computer", nil, &pj)
		calls, _, err := r88provaCall(t, s, true, &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Tools: []types.Tool{r88provaToolset(nil)}})
		var ire *providererrors.InvalidResponseDataError
		if !errors.As(err, &ire) || len(calls) != 0 {
			t.Fatalf("%s: err = %v calls = %v", pj, err, calls)
		}
		want := "Invalid toolset member input: expected an object."
		if pj == "{" {
			want = "Invalid toolset member input JSON."
		}
		if ire.Message != want {
			t.Fatalf("%s: message = %q", pj, ire.Message)
		}
	}
}

// Ruling: a non-object member input in DoGenerate raises the TS error
// (decoded raw for toolset members).
func TestR88provaToolsetRejectsNonObjectGenerateInput(t *testing.T) {
	body := `{"id":"m","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"tool_use","id":"t","name":"screenshot","toolset_name":"computer","input":[1]}],"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1}}`
	s := r88provaNewServer(t, body, nil)
	_, _, err := r88provaCall(t, s, false, &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Tools: []types.Tool{r88provaToolset(nil)}})
	var ire *providererrors.InvalidResponseDataError
	if !errors.As(err, &ire) || ire.Message != "Invalid toolset member input: expected an object." {
		t.Fatalf("err = %v", err)
	}
	// A plain tool_use with a non-object input still decodes (TS
	// z.unknown()) and reports JSON.stringify(input).
	body = strings.Replace(body, `,"toolset_name":"computer"`, "", 1)
	s = r88provaNewServer(t, body, nil)
	calls, _, err := r88provaCall(t, s, false, &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil || len(calls) != 1 || calls[0].RawArguments != "[1]" {
		t.Fatalf("plain tool_use: %v %v", err, calls)
	}
}

// TS computer-toolset_20260801.test.ts: the local input schema's action enum
// drops disabled members.
func TestR88provaToolsetSchemaEnum(t *testing.T) {
	enumOf := func(tool types.Tool) []string {
		return tool.Parameters.(map[string]interface{})["properties"].(map[string]interface{})["action"].(map[string]interface{})["enum"].([]string)
	}
	if got := enumOf(r88provaToolset(nil)); len(got) != 17 {
		t.Fatalf("default enum = %v", got)
	}
	got := enumOf(r88provaToolset(map[tools.ComputerToolset20260801Member]tools.ComputerToolset20260801MemberConfig{
		tools.ComputerToolsetWait:       {Enabled: r88provaBool(false)},
		tools.ComputerToolsetScreenshot: {Enabled: r88provaBool(true), DeferLoading: r88provaBool(true)},
	}))
	if len(got) != 16 || strings.Contains(strings.Join(got, ","), "wait") || got[0] != "screenshot" {
		t.Fatalf("enum = %v", got)
	}
	all := map[tools.ComputerToolset20260801Member]tools.ComputerToolset20260801MemberConfig{}
	for _, m := range computerToolset20260801Members {
		all[tools.ComputerToolset20260801Member(m)] = tools.ComputerToolset20260801MemberConfig{Enabled: r88provaBool(false)}
	}
	if got := enumOf(r88provaToolset(all)); len(got) != 0 {
		t.Fatalf("all disabled enum = %v", got)
	}
}

// r88provaPending lists the language.json.gz cases (re-extracted at TS
// 0f3a85f92d) whose TS change a later round 88 commit ports.
var r88provaPending = map[string]string{}

// r88provaReplayed lists language.json.gz cases a hand port replays
// because the extractor cannot see the provider the TS test builds.
var r88provaReplayed = map[string]string{
	"should pass sanitized zod output schema as output_config.format": "capture.mjs fills the call from the TS options, where the zod schema of responseFormat does not serialize (the call has no schema); the sanitizer is replayed by sanitize_schema.json.gz",
	`reads the legacy "vertex" key when "googleVertex" is absent`:     "createGoogleVertexNamedModel() builds createAnthropic({name: 'googleVertex'}) in a helper; replayed by TestR88provaLegacyVertexKey",
	`ignores the legacy "vertex" key when "googleVertex" is present`:  "createGoogleVertexNamedModel() builds createAnthropic({name: 'googleVertex'}) in a helper; replayed by TestR88provaLegacyVertexKey",
}

// r88provaWithPending adds r88provaReplayed and r88provaPending to a
// suite's skip list.
func r88provaWithPending(skips map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range skips {
		out[k] = v
	}
	for k, v := range r88provaReplayed {
		out[k] = v
	}
	if os.Getenv("R88PROVA_NO_PENDING") != "" {
		return out
	}
	for k, v := range r88provaPending {
		if _, ok := out[k]; !ok {
			out[k] = v
		}
	}
	return out
}

// TS 9754014925 'only emits typed pings when enabled' (includePings x raw
// chunks x provider key) and 'ignores typed ping opt-in for non-streaming
// requests'.
func TestR88provaIncludePings(t *testing.T) {
	events := []string{
		`{"type":"message_start","message":{"id":"msg_01KfpJoAEabmH2iHRRFjQMAG","type":"message","role":"assistant","content":[],"model":"claude-3-haiku-20240307","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":17,"output_tokens":1}}}`,
		`{"type":"ping"}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":2}}`,
		`{"type":"message_stop"}`,
	}
	for _, includePings := range []interface{}{nil, false, true} {
		for _, raw := range []bool{false, true} {
			for _, key := range []string{"anthropic", "vertex"} {
				s := r88provaNewServer(t, "", events)
				name := "anthropic"
				if key == "vertex" {
					name = "vertex.anthropic"
				}
				m, err := New(Config{APIKey: "k", BaseURL: s.srv.URL, Name: name}).LanguageModel("claude-3-haiku-20240307")
				if err != nil {
					t.Fatal(err)
				}
				po := map[string]interface{}{}
				if includePings != nil {
					po["includePings"] = includePings
				}
				stream, err := m.DoStream(context.Background(), &provider.GenerateOptions{
					Prompt: types.Prompt{Text: "hi"}, IncludeRawChunks: raw,
					ProviderOptions: map[string]interface{}{key: po},
				})
				if err != nil {
					t.Fatal(err)
				}
				parts, err := r88provaDrain(stream)
				if err != nil {
					t.Fatal(err)
				}
				pings, rawPings := 0, 0
				for _, p := range parts {
					if p.Type == provider.ChunkTypePing {
						pings++
					}
					if p.Type == provider.ChunkTypeRaw && strings.Contains(r88provaJSON(p.Raw), `"type":"ping"`) {
						rawPings++
					}
				}
				wantPings, wantRaw := 0, 0
				if includePings == true {
					wantPings = 1
				}
				if raw {
					wantRaw = 1
				}
				if pings != wantPings || rawPings != wantRaw {
					t.Fatalf("%v raw=%v %s: pings %d raw pings %d", includePings, raw, key, pings, rawPings)
				}
				if _, ok := s.body(0)["includePings"]; ok {
					t.Fatal("includePings sent on the wire")
				}
			}
		}
	}
	s := r88provaNewServer(t, `{"id":"m","type":"message","role":"assistant","model":"claude-3-haiku-20240307","content":[{"type":"text","text":"Hello"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`, nil)
	res, err := r88provaModel(t, s, "claude-3-haiku-20240307").DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"}, ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"includePings": true}},
	})
	if err != nil || res.Text != "Hello" {
		t.Fatalf("generate: %v %v", err, res)
	}
	if _, ok := s.body(0)["includePings"]; ok {
		t.Fatal("includePings sent on the wire")
	}
}

// TS 9754014925 'preserves startup error handling after pings': pings
// before a first-event error do not hide it from DoStream.
func TestR88provaPingsBeforeStartupError(t *testing.T) {
	for _, raw := range []bool{false, true} {
		s := r88provaNewServer(t, "", []string{`{"type":"ping"}`, `{"type":"ping"}`, `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`})
		_, err := r88provaModel(t, s, "claude-3-haiku-20240307").DoStream(context.Background(), &provider.GenerateOptions{
			Prompt: types.Prompt{Text: "hi"}, IncludeRawChunks: raw,
			ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"includePings": true}},
		})
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || !pe.IsRetryable() || pe.Message != "Overloaded" {
			t.Fatalf("raw=%v: err = %#v", raw, err)
		}
	}
}

// TS 6400cf8b88 'legacy custom provider options names': a provider named
// googleVertex reads providerOptions.vertex when googleVertex is absent
// (first present wins, metadata only under anthropic), ignores it when
// googleVertex is present, and never sends Vertex-only keys such as labels.
func TestR88provaLegacyVertexKey(t *testing.T) {
	body := `{"id":"msg_legacy_test","type":"message","role":"assistant","content":[{"type":"text","text":"Hello, World!"}],"model":"claude-3-haiku-20240307","stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":20}}`
	tool := []types.Tool{{Name: "testTool", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}}}
	run := func(po map[string]interface{}, tools []types.Tool) (map[string]interface{}, *types.GenerateResult) {
		s := r88provaNewServer(t, body, nil)
		m, err := New(Config{APIKey: "test-api-key", BaseURL: s.srv.URL, Name: "googleVertex"}).LanguageModel("claude-3-haiku-20240307")
		if err != nil {
			t.Fatal(err)
		}
		res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello"}, ProviderOptions: po, Tools: tools})
		if err != nil {
			t.Fatal(err)
		}
		return s.body(0), res
	}
	b, res := run(map[string]interface{}{"vertex": map[string]interface{}{"disableParallelToolUse": true}}, tool)
	if tc, _ := b["tool_choice"].(map[string]interface{}); tc["disable_parallel_tool_use"] != true || tc["type"] != "auto" {
		t.Fatalf("tool_choice = %v", b["tool_choice"])
	}
	if len(res.ProviderMetadata) != 1 || res.ProviderMetadata["anthropic"] == nil {
		t.Fatalf("metadata keys = %v", res.ProviderMetadata)
	}
	b, _ = run(map[string]interface{}{
		"googleVertex": map[string]interface{}{"disableParallelToolUse": false},
		"vertex":       map[string]interface{}{"disableParallelToolUse": true},
	}, tool)
	if tc, _ := b["tool_choice"].(map[string]interface{}); tc != nil && tc["disable_parallel_tool_use"] == true {
		t.Fatalf("tool_choice = %v", b["tool_choice"])
	}
	b, _ = run(map[string]interface{}{"vertex": map[string]interface{}{"labels": map[string]interface{}{"team": "ai"}}}, nil)
	if _, ok := b["labels"]; ok {
		t.Fatal("labels sent")
	}
}

// r88provaTierEvents is TS anthropic-service-tier.test.ts events(value).
func r88provaTierEvents(message map[string]interface{}) []string {
	return []string{
		r88provaJSON(map[string]interface{}{"type": "message_start", "message": message}),
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
		`{"type":"message_stop"}`,
	}
}

func r88provaTierMessage(extra map[string]interface{}, usageExtra map[string]interface{}) map[string]interface{} {
	usage := map[string]interface{}{"input_tokens": 1, "output_tokens": 1}
	for k, v := range usageExtra {
		usage[k] = v
	}
	m := map[string]interface{}{"id": "msg_tier", "type": "message", "role": "assistant", "model": "claude-haiku-4-5", "content": []interface{}{}, "stop_reason": "end_turn", "usage": usage}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

// r88provaTierFinishes runs the call and returns providerMetadata.anthropic
// .serviceTier of the result (generate) or of every finish part (stream).
func r88provaTierFinishes(t *testing.T, s *r88provaServer, mode string, po map[string]interface{}) ([]interface{}, []types.Warning) {
	t.Helper()
	m := r88provaModel(t, s, "claude-haiku-4-5")
	max := 16
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hi"}}}}}, MaxTokens: &max, ProviderOptions: po}
	tier := func(meta map[string]interface{}) interface{} {
		a, _ := meta["anthropic"].(map[string]interface{})
		return a["serviceTier"]
	}
	if mode == "generate" {
		res, err := m.DoGenerate(context.Background(), opts)
		if err != nil {
			t.Fatal(err)
		}
		return []interface{}{tier(res.ProviderMetadata)}, res.Warnings
	}
	stream, err := m.DoStream(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	parts, err := r88provaDrain(stream)
	if err != nil {
		t.Fatal(err)
	}
	var out []interface{}
	var warnings []types.Warning
	for _, p := range parts {
		if p.Type == provider.ChunkTypeStreamStart {
			warnings = p.Warnings
		}
		if p.Type == provider.ChunkTypeFinish {
			var meta map[string]interface{}
			_ = json.Unmarshal(p.ProviderMetadata, &meta)
			out = append(out, tier(meta))
		}
	}
	return out, warnings
}

// TS 8291ccf237 anthropic-service-tier.test.ts.
func TestR88provaServiceTier(t *testing.T) {
	for _, mode := range []string{"generate", "stream"} {
		msg := r88provaTierMessage(nil, nil)
		s := r88provaNewServer(t, r88provaJSON(msg), r88provaTierEvents(msg))
		r88provaTierFinishes(t, s, mode, map[string]interface{}{"anthropic": map[string]interface{}{"serviceTier": "standard_only"}})
		if s.body(0)["service_tier"] != "standard_only" {
			t.Fatalf("%s: service_tier = %v", mode, s.body(0)["service_tier"])
		}

		// reads the applied tier from usage
		msg = r88provaTierMessage(map[string]interface{}{"service_tier": "top-level"}, map[string]interface{}{"service_tier": "priority"})
		s = r88provaNewServer(t, r88provaJSON(msg), r88provaTierEvents(msg))
		if got, _ := r88provaTierFinishes(t, s, mode, nil); len(got) != 1 || got[0] != "priority" {
			t.Fatalf("%s: usage tier = %v", mode, got)
		}
		// retains top-level compatible-provider metadata
		msg = r88provaTierMessage(map[string]interface{}{"service_tier": "standard"}, nil)
		s = r88provaNewServer(t, r88provaJSON(msg), r88provaTierEvents(msg))
		if got, _ := r88provaTierFinishes(t, s, mode, nil); len(got) != 1 || got[0] != "standard" {
			t.Fatalf("%s: top-level tier = %v", mode, got)
		}
		// absent: no serviceTier key
		msg = r88provaTierMessage(nil, nil)
		s = r88provaNewServer(t, r88provaJSON(msg), r88provaTierEvents(msg))
		if got, _ := r88provaTierFinishes(t, s, mode, nil); len(got) != 1 || got[0] != nil {
			t.Fatalf("%s: absent tier = %v", mode, got)
		}
	}

	// warns and omits a MiniMax-only tier
	msg := r88provaTierMessage(nil, nil)
	s := r88provaNewServer(t, r88provaJSON(msg), nil)
	_, warnings := r88provaTierFinishes(t, s, "generate", map[string]interface{}{"anthropic": map[string]interface{}{"serviceTier": "priority"}})
	if _, ok := s.body(0)["service_tier"]; ok {
		t.Fatal("service_tier sent")
	}
	found := false
	for _, w := range warnings {
		if w.Type == "unsupported" && w.Feature == "serviceTier" && w.Details == `serviceTier "priority" is not supported by anthropic. Use "auto" or "standard_only".` {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %v", warnings)
	}

	// does not retain a previous message tier when the next message omits it
	first := r88provaTierMessage(nil, map[string]interface{}{"service_tier": "standard"})
	second := r88provaTierMessage(map[string]interface{}{"id": "msg_second"}, nil)
	s = r88provaNewServer(t, "", append(r88provaTierEvents(first), r88provaTierEvents(second)...))
	if got, _ := r88provaTierFinishes(t, s, "stream", nil); len(got) != 2 || got[0] != "standard" || got[1] != nil {
		t.Fatalf("two messages: %v", got)
	}

	// uses a final usage update when the service tier arrives in a delta
	s = r88provaNewServer(t, "", []string{
		r88provaJSON(map[string]interface{}{"type": "message_start", "message": r88provaTierMessage(nil, nil)}),
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1,"service_tier":"priority"}}`,
		`{"type":"message_stop"}`,
	})
	if got, _ := r88provaTierFinishes(t, s, "stream", nil); len(got) != 1 || got[0] != "priority" {
		t.Fatalf("delta tier = %v", got)
	}

	// a non-string tier fails the response schema (TS z.string().nullish())
	s = r88provaNewServer(t, r88provaJSON(r88provaTierMessage(map[string]interface{}{"service_tier": 1}, nil)), nil)
	if _, err := r88provaModel(t, s, "claude-haiku-4-5").DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err == nil || !strings.Contains(err.Error(), "Invalid JSON response") || !strings.Contains(err.Error(), "service_tier") {
		t.Fatalf("non-string service_tier: %v", err)
	}
}

// TS 8291ccf237: a minimax-named provider accepts standard / priority and
// warns for the Anthropic-only tiers; another custom name forwards all.
func TestR88provaServiceTierPerProvider(t *testing.T) {
	msg := r88provaTierMessage(nil, nil)
	for _, tc := range []struct {
		name, tier string
		sent       bool
		warning    string
	}{
		{"minimax", "priority", true, ""},
		{"minimax", "standard", true, ""},
		{"minimax", "auto", false, `serviceTier "auto" is not supported by minimax. Use "standard" or "priority".`},
		{"other.messages", "priority", true, ""},
		{"other.messages", "auto", true, ""},
	} {
		s := r88provaNewServer(t, r88provaJSON(msg), nil)
		m, err := New(Config{APIKey: "k", BaseURL: s.srv.URL, Name: tc.name}).LanguageModel("claude-haiku-4-5")
		if err != nil {
			t.Fatal(err)
		}
		key := strings.SplitN(tc.name, ".", 2)[0]
		res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, ProviderOptions: map[string]interface{}{key: map[string]interface{}{"serviceTier": tc.tier}}})
		if err != nil {
			t.Fatal(err)
		}
		_, sent := s.body(0)["service_tier"]
		if sent != tc.sent {
			t.Fatalf("%s %s: sent = %v", tc.name, tc.tier, sent)
		}
		got := ""
		for _, w := range res.Warnings {
			if w.Feature == "serviceTier" {
				got = w.Details
			}
		}
		if got != tc.warning {
			t.Fatalf("%s %s: warning %q", tc.name, tc.tier, got)
		}
	}
}

const r88provaTextResponse = `{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`

var r88provaTextEvents = []string{
	`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[],"stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`,
	`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
	`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
	`{"type":"content_block_stop","index":0}`,
	`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
	`{"type":"message_stop"}`,
}

// r88provaRequestBody runs a generate or stream call against a text
// response and returns the request body (and the call error).
func r88provaRequestBody(t *testing.T, modelID, method string, opts *provider.GenerateOptions) (map[string]interface{}, error) {
	t.Helper()
	s := r88provaNewServer(t, r88provaTextResponse, r88provaTextEvents)
	m := r88provaModel(t, s, modelID)
	var err error
	if method == "generate" {
		_, err = m.DoGenerate(context.Background(), opts)
	} else {
		var stream provider.TextStream
		stream, err = m.DoStream(context.Background(), opts)
		if err == nil {
			_, err = r88provaDrain(stream)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.bodies) == 0 {
		return nil, err
	}
	return s.bodies[0], err
}

func r88provaToolNames(body map[string]interface{}) []string {
	var out []string
	for _, t := range body["tools"].([]interface{}) {
		out = append(out, t.(map[string]interface{})["name"].(string))
	}
	return out
}

// TS 5ab7ff14ad: MCP connectors are disabled when a named tool choice falls
// back to auto or the JSON response tool replaces a named / none choice.
func TestR88provaMCPRestrictedToolChoice(t *testing.T) {
	mcpServers := []interface{}{
		map[string]interface{}{"type": "url", "name": "default", "url": "https://default.invalid/mcp"},
		map[string]interface{}{"type": "url", "name": "enabled", "url": "https://enabled.invalid/mcp", "toolConfiguration": map[string]interface{}{"enabled": true, "allowedTools": []interface{}{"selected"}}},
		map[string]interface{}{"type": "url", "name": "disabled", "url": "https://disabled.invalid/mcp", "toolConfiguration": map[string]interface{}{"enabled": false, "allowedTools": []interface{}{"other"}}},
		map[string]interface{}{"type": "url", "name": "allowlist", "url": "https://allowlist.invalid/mcp", "toolConfiguration": map[string]interface{}{"allowedTools": []interface{}{"selected"}}},
	}
	obj := map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
	tools := []types.Tool{{Name: "selected", Parameters: obj}, {Name: "other", Parameters: obj}}
	disabled := `[{"name":"default","tool_configuration":{"enabled":false},"type":"url","url":"https://default.invalid/mcp"},{"name":"enabled","tool_configuration":{"allowed_tools":["selected"],"enabled":false},"type":"url","url":"https://enabled.invalid/mcp"},{"name":"disabled","tool_configuration":{"allowed_tools":["other"],"enabled":false},"type":"url","url":"https://disabled.invalid/mcp"},{"name":"allowlist","tool_configuration":{"allowed_tools":["selected"],"enabled":false},"type":"url","url":"https://allowlist.invalid/mcp"}]`
	preserved := `[{"name":"default","type":"url","url":"https://default.invalid/mcp"},{"name":"enabled","tool_configuration":{"allowed_tools":["selected"],"enabled":true},"type":"url","url":"https://enabled.invalid/mcp"},{"name":"disabled","tool_configuration":{"allowed_tools":["other"],"enabled":false},"type":"url","url":"https://disabled.invalid/mcp"},{"name":"allowlist","tool_configuration":{"allowed_tools":["selected"]},"type":"url","url":"https://allowlist.invalid/mcp"}]`
	jsonFormat := &provider.ResponseFormat{Type: "json", Schema: map[string]interface{}{"type": "object", "additionalProperties": true}}
	for _, method := range []string{"generate", "stream"} {
		for _, mode := range []string{"auto", "jsonTool"} {
			for _, choice := range []types.ToolChoice{{Type: types.ToolChoiceNone}, {Type: types.ToolChoiceTool, ToolName: "selected"}} {
				body, err := r88provaRequestBody(t, "claude-sonnet-4-5", method, &provider.GenerateOptions{
					Prompt: types.Prompt{Text: "hi"}, Tools: tools, ToolChoice: choice, ResponseFormat: jsonFormat,
					ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"mcpServers": mcpServers, "structuredOutputMode": mode}},
				})
				if err != nil {
					t.Fatal(err)
				}
				if got := r88provaJSON(body["mcp_servers"]); got != disabled {
					t.Fatalf("%s %s %s: mcp_servers = %s", method, mode, choice.Type, got)
				}
				want := "selected,json"
				if choice.Type == types.ToolChoiceNone {
					want = "json"
				}
				if got := strings.Join(r88provaToolNames(body), ","); got != want {
					t.Fatalf("tools = %s", got)
				}
			}
		}
		for _, choice := range []types.ToolChoice{{}, {Type: types.ToolChoiceAuto}, {Type: types.ToolChoiceRequired}} {
			body, err := r88provaRequestBody(t, "claude-sonnet-4-5", method, &provider.GenerateOptions{
				Prompt: types.Prompt{Text: "hi"}, Tools: tools, ToolChoice: choice, ResponseFormat: jsonFormat,
				ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"mcpServers": mcpServers}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := r88provaJSON(body["mcp_servers"]); got != preserved {
				t.Fatalf("%s %q: mcp_servers = %s", method, choice.Type, got)
			}
		}
		// named tool choice falling back to auto
		for _, tc := range []struct {
			model    string
			thinking interface{}
		}{{"claude-opus-5-5", nil}, {"claude-sonnet-4-5", map[string]interface{}{"type": "enabled", "budgetTokens": 1024}}} {
			po := map[string]interface{}{"mcpServers": []interface{}{map[string]interface{}{"type": "url", "name": "echo", "url": "https://echo.invalid/mcp"}}}
			if tc.thinking != nil {
				po["thinking"] = tc.thinking
			}
			body, err := r88provaRequestBody(t, tc.model, method, &provider.GenerateOptions{
				Prompt: types.Prompt{Text: "hi"}, Tools: []types.Tool{{Name: "selected", Parameters: map[string]interface{}{"type": "object"}}, {Name: "other", Parameters: map[string]interface{}{"type": "object"}}},
				ToolChoice: types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "selected"}, ProviderOptions: map[string]interface{}{"anthropic": po},
			})
			if err != nil {
				t.Fatal(err)
			}
			if r88provaJSON(body["tool_choice"]) != `{"type":"auto"}` || strings.Join(r88provaToolNames(body), ",") != "selected" {
				t.Fatalf("%s: tool_choice %v tools %v", tc.model, body["tool_choice"], body["tools"])
			}
			if got := r88provaJSON(body["mcp_servers"]); got != `[{"name":"echo","tool_configuration":{"enabled":false},"type":"url","url":"https://echo.invalid/mcp"}]` {
				t.Fatalf("%s: mcp_servers = %s", tc.model, got)
			}
		}
		// provider-enforced choices keep the configuration
		for _, structured := range []bool{false, true} {
			for _, choice := range []types.ToolChoice{{Type: types.ToolChoiceNone}, {Type: types.ToolChoiceTool, ToolName: "selected"}} {
				opts := &provider.GenerateOptions{
					Prompt: types.Prompt{Text: "hi"}, Tools: []types.Tool{{Name: "selected", Parameters: map[string]interface{}{"type": "object"}}}, ToolChoice: choice,
					ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"mcpServers": []interface{}{map[string]interface{}{"type": "url", "name": "echo", "url": "https://echo.invalid/mcp", "toolConfiguration": map[string]interface{}{"enabled": true, "allowedTools": []interface{}{"selected"}}}}}},
				}
				if structured {
					opts.ResponseFormat = &provider.ResponseFormat{Type: "json", Schema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}}
				}
				body, err := r88provaRequestBody(t, "claude-sonnet-4-5", method, opts)
				if err != nil {
					t.Fatal(err)
				}
				if got := r88provaJSON(body["mcp_servers"]); got != `[{"name":"echo","tool_configuration":{"allowed_tools":["selected"],"enabled":true},"type":"url","url":"https://echo.invalid/mcp"}]` {
					t.Fatalf("enforced %v %s: mcp_servers = %s", structured, choice.Type, got)
				}
			}
		}
	}
}

// r88provaConvertInlineTool is the tsfixture adapter's convertInlineTool:
// prepareTools on the one tool.
func r88provaConvertInlineTool(tool types.Tool) (map[string]interface{}, []types.Warning, []string, error) {
	p := prepareTools(prepareToolsOptions{tools: []types.Tool{tool}})
	if p.err != nil || len(p.tools) == 0 {
		return nil, p.warnings, p.betas, p.err
	}
	return p.tools[0], p.warnings, p.betas, nil
}

func r88provaInlineChange(tool map[string]interface{}) types.Message {
	return types.Message{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: ""}},
		ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"toolChanges": []interface{}{
			map[string]interface{}{"type": "tool_addition", "tool": tool},
		}}}}
}

func r88provaUserHello() types.Message {
	return types.Message{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}}
}

func r88provaCopyMap(m map[string]interface{}, over map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range m {
		out[k] = v
	}
	for k, v := range over {
		out[k] = v
	}
	return out
}

// TS ba302a14fc / b3088a77b0 'named tool choice with inline tools'.
func TestR88provaInlineToolChoiceRestrictions(t *testing.T) {
	selectedMap := map[string]interface{}{"type": "function", "name": "selected", "inputSchema": map[string]interface{}{"type": "object"}}
	selectedTool := types.Tool{Name: "selected", Parameters: map[string]interface{}{"type": "object"}}
	thinking := map[string]interface{}{"type": "enabled", "budgetTokens": 1024}
	var argErr *providererrors.InvalidArgumentError
	for _, method := range []string{"generate", "stream"} {
		// should keep ignoring initial system tool additions
		for _, useJSON := range []bool{false, true} {
			sys := r88provaInlineChange(r88provaCopyMap(selectedMap, map[string]interface{}{"name": "other"}))
			sys.Content = []types.ContentPart{types.TextContent{Text: "Initial instructions"}}
			opts := &provider.GenerateOptions{
				Prompt: types.Prompt{Messages: []types.Message{sys, r88provaUserHello()}}, Tools: []types.Tool{selectedTool},
				ToolChoice:      types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "selected"},
				ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"thinking": thinking, "structuredOutputMode": "jsonTool"}},
			}
			if useJSON {
				opts.ResponseFormat = &provider.ResponseFormat{Type: "json", Schema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}}
			}
			body, err := r88provaRequestBody(t, "claude-haiku-4-5", method, opts)
			if err != nil {
				t.Fatalf("initial system json=%v: %v", useJSON, err)
			}
			if r88provaJSON(body["system"]) != `[{"text":"Initial instructions","type":"text"}]` || len(body["messages"].([]interface{})) != 1 {
				t.Fatalf("initial system body = %v", body)
			}
		}
		// conflicting additions are rejected before the request
		inlineTools := []map[string]interface{}{
			r88provaCopyMap(selectedMap, map[string]interface{}{"name": "other"}),
			{"type": "provider", "id": "anthropic.web_search_20250305", "name": "web_search", "args": map[string]interface{}{}},
			{"type": "provider", "id": "anthropic.web_fetch_20250910", "name": "web_fetch", "args": map[string]interface{}{}},
			{"type": "provider", "id": "anthropic.web_search_20250305", "name": "selected", "args": map[string]interface{}{}},
		}
		for _, inline := range inlineTools {
			for _, tc := range []struct{ model, source string }{
				{"claude-haiku-4-5", "providerOptions"}, {"claude-haiku-4-5", "reasoning"}, {"claude-opus-5-5", "model"}, {"claude-haiku-4-5", "inlineOnly"},
			} {
				msgs := []types.Message{r88provaUserHello(), r88provaInlineChange(inline)}
				if tc.source != "providerOptions" {
					msgs = append(msgs, types.Message{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "OK."}}}, r88provaUserHello())
				}
				opts := &provider.GenerateOptions{Prompt: types.Prompt{Messages: msgs}, ToolChoice: types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "selected"}}
				if tc.source != "inlineOnly" {
					opts.Tools = []types.Tool{selectedTool}
				}
				switch tc.source {
				case "reasoning":
					low := types.ReasoningLow
					opts.Reasoning = &low
				case "model":
				default:
					opts.ProviderOptions = map[string]interface{}{"anthropic": map[string]interface{}{"thinking": thinking}}
				}
				body, err := r88provaRequestBody(t, tc.model, method, opts)
				if !errors.As(err, &argErr) || body != nil {
					t.Fatalf("%s %v via %s: err = %v, sent = %v", method, inline["name"], tc.source, err, body != nil)
				}
				if !strings.Contains(err.Error(), "' conflicts with named tool choice 'selected'. Inline definitions must respect the tool choice when forced tool use falls back or the JSON response tool is used.") {
					t.Fatalf("message = %v", err)
				}
			}
		}
		// allowed inline tools are kept
		for _, tc := range []struct {
			model    string
			thinking map[string]interface{}
			toolName string
			choice   string
			want     string
		}{
			{"claude-haiku-4-5", thinking, "selected", "tool", `{"type":"auto"}`},
			{"claude-haiku-4-5", thinking, "other", "auto", `{"type":"auto"}`},
			{"claude-haiku-4-5", thinking, "other", "required", `{"type":"auto"}`},
			{"claude-sonnet-4-6", map[string]interface{}{"type": "adaptive"}, "other", "tool", `{"name":"selected","type":"tool"}`},
			{"claude-haiku-5-5", thinking, "other", "tool", `{"name":"selected","type":"tool"}`},
		} {
			choice := types.ToolChoice{Type: types.ToolChoiceType(tc.choice)}
			if tc.choice == "tool" {
				choice.ToolName = "selected"
			}
			body, err := r88provaRequestBody(t, tc.model, method, &provider.GenerateOptions{
				Prompt:     types.Prompt{Messages: []types.Message{r88provaUserHello(), r88provaInlineChange(r88provaCopyMap(selectedMap, map[string]interface{}{"name": tc.toolName}))}},
				ToolChoice: choice, ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"thinking": tc.thinking}},
			})
			if err != nil {
				t.Fatalf("%s %s %s: %v", tc.model, tc.toolName, tc.choice, err)
			}
			if _, ok := body["tools"]; ok {
				t.Fatalf("tools sent: %v", body["tools"])
			}
			part := body["messages"].([]interface{})[1].(map[string]interface{})["content"].([]interface{})[0].(map[string]interface{})
			if part["type"] != "tool_addition" || part["tool"].(map[string]interface{})["definition"].(map[string]interface{})["name"] != tc.toolName {
				t.Fatalf("addition = %v", part)
			}
			if got := r88provaJSON(body["tool_choice"]); got != tc.want {
				t.Fatalf("%s %s %s: tool_choice = %s", tc.model, tc.toolName, tc.choice, got)
			}
		}
		// JSON response tool
		for _, useThinking := range []bool{false, true} {
			jsonFormat := &provider.ResponseFormat{Type: "json", Schema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}}
			po := map[string]interface{}{"structuredOutputMode": "jsonTool"}
			if useThinking {
				po["thinking"] = thinking
			}
			providerTool := map[string]interface{}{"type": "provider", "id": "anthropic.web_fetch_20250910", "name": "selected", "args": map[string]interface{}{}}
			for _, tc := range []struct {
				choice                   string
				providerAlias, reuseName bool
			}{{"tool", false, false}, {"none", false, false}, {"tool", true, false}, {"tool", false, true}, {"tool", true, true}} {
				inline := r88provaCopyMap(providerTool, nil)
				switch {
				case tc.reuseName:
					inline["id"], inline["name"] = "anthropic.web_search_20250305", "selected"
				case tc.providerAlias:
					inline["name"] = "other"
				default:
					inline["name"] = "web_fetch"
				}
				declared := selectedTool
				if tc.providerAlias {
					declared = types.Tool{Type: types.ToolTypeProviderDefined, ProviderID: "anthropic.web_fetch_20250910", Name: "selected", ProviderArgs: map[string]interface{}{}}
				}
				choice := types.ToolChoice{Type: types.ToolChoiceType(tc.choice)}
				if tc.choice == "tool" {
					choice.ToolName = "selected"
				}
				body, err := r88provaRequestBody(t, "claude-opus-4-8", method, &provider.GenerateOptions{
					Prompt: types.Prompt{Messages: []types.Message{r88provaUserHello(), r88provaInlineChange(inline)}},
					Tools:  []types.Tool{declared}, ToolChoice: choice, ResponseFormat: jsonFormat,
					ProviderOptions: map[string]interface{}{"anthropic": po},
				})
				if !errors.As(err, &argErr) || body != nil {
					t.Fatalf("json reject %+v thinking=%v: err = %v", tc, useThinking, err)
				}
			}
			for _, tc := range []struct {
				choice                    string
				providerAlias, inlineOnly bool
			}{{"tool", false, false}, {"auto", false, false}, {"required", false, false}, {"tool", true, false}, {"tool", false, true}, {"tool", true, true}} {
				var inline map[string]interface{}
				var declared types.Tool
				if tc.providerAlias {
					inline = map[string]interface{}{"type": "provider", "id": "anthropic.web_search_20250305", "name": "selected", "args": map[string]interface{}{}}
					declared = types.Tool{Type: types.ToolTypeProviderDefined, ProviderID: "anthropic.web_search_20250305", Name: "selected", ProviderArgs: map[string]interface{}{}}
				} else {
					name := "other"
					if tc.choice == "tool" {
						name = "selected"
					}
					inline = r88provaCopyMap(selectedMap, map[string]interface{}{"name": name})
					declared = selectedTool
				}
				var ts []types.Tool
				if !tc.inlineOnly {
					ts = []types.Tool{declared}
				}
				choice := types.ToolChoice{Type: types.ToolChoiceType(tc.choice)}
				if tc.choice == "tool" {
					choice.ToolName = "selected"
				}
				body, err := r88provaRequestBody(t, "claude-opus-4-8", method, &provider.GenerateOptions{
					Prompt: types.Prompt{Messages: []types.Message{r88provaUserHello(), r88provaInlineChange(inline)}},
					Tools:  ts, ToolChoice: choice, ResponseFormat: jsonFormat,
					ProviderOptions: map[string]interface{}{"anthropic": po},
				})
				if err != nil {
					t.Fatalf("json allow %+v thinking=%v: %v", tc, useThinking, err)
				}
				wantChoice := `{"disable_parallel_tool_use":true,"type":"any"}`
				if useThinking {
					wantChoice = `{"disable_parallel_tool_use":true,"type":"auto"}`
				}
				if got := r88provaJSON(body["tool_choice"]); got != wantChoice {
					t.Fatalf("json allow %+v: tool_choice %s", tc, got)
				}
				wantTools := "json"
				if !tc.inlineOnly {
					wantTools = "selected,json"
					if tc.providerAlias {
						wantTools = "web_search,json"
					}
				}
				if got := strings.Join(r88provaToolNames(body), ","); got != wantTools {
					t.Fatalf("json allow %+v: tools %s", tc, got)
				}
				def := body["messages"].([]interface{})[1].(map[string]interface{})["content"].([]interface{})[0].(map[string]interface{})["tool"].(map[string]interface{})["definition"].(map[string]interface{})
				wantName := inline["name"]
				if tc.providerAlias {
					wantName = "web_search"
				}
				if def["name"] != wantName {
					t.Fatalf("json allow %+v: definition %v", tc, def)
				}
			}
		}
	}
}

// r88provaPorted marks the TS 457dc40d06 assertions the evaluator cannot
// read (two calls compared, a Set) as hand-ported.
var r88provaPorted = map[string]string{
	tsfixture.PortedKey("should send the same definition for streaming and non-streaming requests", "JSON.stringify(streamBody.messages)", "toContain"):                    "TestR88provaInlineToolsByValue",
	tsfixture.PortedKey("should send the tool choice and disabled parallel tool use when all tools are added by value", "bodies.map(body => body.tools)", "toEqual"):       "TestR88provaInlineToolsByValue",
	tsfixture.PortedKey("should send the tool choice and disabled parallel tool use when all tools are added by value", "bodies.map(body => body.tool_choice)", "toEqual"): "TestR88provaInlineToolsByValue",
	tsfixture.PortedKey("should emit tool definitions for tools added by value and prefer the inline tools beta", "result.betas.has('inline-tools-2026-09-15')", "toBe"):   "TestR88provaInlineToolsByValue",
}

func init() { tsfixture.RegisterPorted(r88provaPorted) }

// TS 457dc40d06: tools added by value.
func TestR88provaInlineToolsByValue(t *testing.T) {
	forecast := map[string]interface{}{"type": "function", "name": "get_forecast", "inputSchema": map[string]interface{}{"type": "object"}}
	sayOK := types.Message{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Say OK."}}}
	msgs := []types.Message{sayOK, r88provaInlineChange(forecast)}

	// the same definition for streaming and non-streaming requests
	gen, err := r88provaRequestBody(t, "claude-opus-4-8", "generate", &provider.GenerateOptions{Prompt: types.Prompt{Messages: msgs}})
	if err != nil {
		t.Fatal(err)
	}
	str, err := r88provaRequestBody(t, "claude-opus-4-8", "stream", &provider.GenerateOptions{Prompt: types.Prompt{Messages: msgs}})
	if err != nil {
		t.Fatal(err)
	}
	if r88provaJSON(gen["messages"]) != r88provaJSON(str["messages"]) || strings.Contains(r88provaJSON(str["messages"]), "eager_input_streaming") {
		t.Fatalf("generate %s\nstream %s", r88provaJSON(gen["messages"]), r88provaJSON(str["messages"]))
	}

	// tool choice and parallel tool use with only inline tools
	for _, tc := range []struct {
		opts *provider.GenerateOptions
		want string
	}{
		{&provider.GenerateOptions{ToolChoice: types.ToolChoice{Type: types.ToolChoiceNone}}, `{"type":"none"}`},
		{&provider.GenerateOptions{ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"disableParallelToolUse": true}}}, `{"disable_parallel_tool_use":true,"type":"auto"}`},
		{&provider.GenerateOptions{ToolChoice: types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "get_forecast"}}, `{"name":"get_forecast","type":"tool"}`},
	} {
		tc.opts.Prompt = types.Prompt{Messages: msgs}
		body, err := r88provaRequestBody(t, "claude-opus-4-8", "generate", tc.opts)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := body["tools"]; ok || r88provaJSON(body["tool_choice"]) != tc.want {
			t.Fatalf("tools %v tool_choice %s, want %s", body["tools"], r88provaJSON(body["tool_choice"]), tc.want)
		}
	}
	// no tools at all: no tool choice
	body, err := r88provaRequestBody(t, "claude-opus-4-8", "generate", &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{sayOK}}, ToolChoice: types.ToolChoice{Type: types.ToolChoiceNone}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := body["tool_choice"]; ok {
		t.Fatalf("tool_choice = %v", body["tool_choice"])
	}

	// convertToAnthropicPrompt: the inline tools beta replaces the tool
	// changes beta; without a converter a by-value addition is unsupported.
	sys := r88provaInlineChange(map[string]interface{}{"type": "function", "name": "get_forecast", "description": "Get forecast", "inputSchema": map[string]interface{}{"type": "object"}})
	sys.ProviderOptions["anthropic"].(map[string]interface{})["toolChanges"] = append(sys.ProviderOptions["anthropic"].(map[string]interface{})["toolChanges"].([]interface{}),
		map[string]interface{}{"type": "tool_removal", "toolName": "get_weather"})
	convMsgs := []types.Message{{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "initial"}}}, {Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}, {Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "hello"}}}, sys}
	res, err := prompt.ConvertToAnthropicPrompt(convMsgs, prompt.AnthropicPromptOptions{ConvertInlineTool: r88provaConvertInlineTool})
	if err != nil {
		t.Fatal(err)
	}
	betas := strings.Join(res.Betas, ",")
	if !strings.Contains(betas, "inline-tools-2026-09-15") || strings.Contains(betas, "mid-conversation-tool-changes-2026-07-01") {
		t.Fatalf("betas = %s", betas)
	}
	if got := r88provaJSON(res.Messages[len(res.Messages)-1]["content"]); got != `[{"tool":{"definition":{"description":"Get forecast","input_schema":{"type":"object"},"name":"get_forecast"},"type":"tool_definition"},"type":"tool_addition"},{"tool":{"name":"get_weather","type":"tool_reference"},"type":"tool_removal"}]` {
		t.Fatalf("content = %s", got)
	}
	var uf *providererrors.UnsupportedFunctionalityError
	if _, err := prompt.ConvertToAnthropicPrompt(convMsgs, prompt.AnthropicPromptOptions{}); !errors.As(err, &uf) || uf.Functionality != "tool additions by value in mid-conversation tool changes" {
		t.Fatalf("no converter: %v", err)
	}
}

// r88provaThreadCall runs a call against a text response and returns the
// request body, the anthropic-beta header, the warnings and the error.
func r88provaThreadCall(t *testing.T, method string, m provider.LanguageModel, s *r88provaServer, opts *provider.GenerateOptions) (map[string]interface{}, string, []types.Warning, error) {
	t.Helper()
	var warnings []types.Warning
	var err error
	if method == "generate" {
		var res *types.GenerateResult
		res, err = m.DoGenerate(context.Background(), opts)
		if res != nil {
			warnings = res.Warnings
		}
	} else {
		var stream provider.TextStream
		stream, err = m.DoStream(context.Background(), opts)
		if err == nil {
			var parts []*provider.StreamChunk
			parts, err = r88provaDrain(stream)
			for _, p := range parts {
				if p.Type == provider.ChunkTypeStreamStart {
					warnings = p.Warnings
				}
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.bodies) == 0 {
		return nil, "", warnings, err
	}
	n := len(s.bodies) - 1
	return s.bodies[n], s.headers[n].Get("anthropic-beta"), warnings, err
}

func r88provaThreadOptions(po map[string]interface{}) *provider.GenerateOptions {
	return &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "Be concise."}}},
			r88provaUserHello(),
		}},
		Tools:           []types.Tool{{Name: "weather", Parameters: map[string]interface{}{"type": "object"}}},
		ProviderOptions: map[string]interface{}{"anthropic": po},
	}
}

func r88provaHasWarning(ws []types.Warning, substr string) bool {
	for _, w := range ws {
		if strings.Contains(w.Details, substr) {
			return true
		}
	}
	return false
}

// TS 6b101ac076 anthropic-message-threads.test.ts.
func TestR88provaMessageThreads(t *testing.T) {
	for _, method := range []string{"generate", "stream"} {
		s := r88provaNewServer(t, r88provaTextResponse, r88provaTextEvents)
		m := r88provaModel(t, s, "claude-sonnet-4-6")
		call := func(opts *provider.GenerateOptions) (map[string]interface{}, string, []types.Warning, error) {
			return r88provaThreadCall(t, method, m, s, opts)
		}
		// creates a thread with system and tools and merges beta headers
		opts := r88provaThreadOptions(map[string]interface{}{"thread": map[string]interface{}{"type": "create"}})
		opts.Headers = map[string]string{"anthropic-beta": "custom-beta"}
		body, beta, _, err := call(opts)
		if err != nil {
			t.Fatal(err)
		}
		if r88provaJSON(body["thread"]) != `{"type":"create"}` || r88provaJSON(body["system"]) != `[{"text":"Be concise.","type":"text"}]` || r88provaToolNames(body)[0] != "weather" {
			t.Fatalf("create body = %v", body)
		}
		if !strings.HasPrefix(beta, "message-threads-2026-08-12,") || !strings.Contains(beta, "custom-beta") {
			t.Fatalf("create beta = %s", beta)
		}

		// continues with only supplied messages, omitting system and tools
		opts = r88provaThreadOptions(map[string]interface{}{"thread": map[string]interface{}{"type": "continue", "previousMessageId": "msg_previous"}, "cacheControl": map[string]interface{}{"type": "ephemeral"}})
		opts.ToolChoice = types.ToolChoice{Type: types.ToolChoiceAuto}
		body, _, warnings, err := call(opts)
		if err != nil {
			t.Fatal(err)
		}
		_, hasSystem := body["system"]
		_, hasTools := body["tools"]
		if r88provaJSON(body["thread"]) != `{"previous_message_id":"msg_previous","type":"continue"}` || hasSystem || hasTools ||
			r88provaJSON(body["messages"]) != `[{"content":[{"text":"Hello","type":"text"}],"role":"user"}]` ||
			r88provaJSON(body["tool_choice"]) != `{"type":"auto"}` || r88provaJSON(body["cache_control"]) != `{"type":"ephemeral"}` {
			t.Fatalf("continue body = %v", body)
		}
		// warns rather than silently dropping new leading system instructions
		if !r88provaHasWarning(warnings, "To change system instructions or tools, start a new thread") {
			t.Fatalf("continue warnings = %v", warnings)
		}

		// keeps pinned PDF beta headers across a text-only continuation
		pdf := &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
			types.FileContent{Data: []byte{1}, MediaType: "application/pdf"},
		}}}}, ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"thread": map[string]interface{}{"type": "create"}, "anthropicBeta": []interface{}{"pdfs-2024-09-25"}}}}
		_, beta1, warnings, err := call(pdf)
		if err != nil {
			t.Fatal(err)
		}
		if len(warnings) != 0 {
			t.Fatalf("pinned warnings = %v", warnings)
		}
		_, beta2, _, err := call(&provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{r88provaUserHello()}},
			ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"thread": map[string]interface{}{"type": "continue", "previousMessageId": "msg_pdf"}, "anthropicBeta": []interface{}{"pdfs-2024-09-25"}}}})
		if err != nil || beta1 != beta2 {
			t.Fatalf("pinned betas %q vs %q (%v)", beta1, beta2, err)
		}
		// warns when message content enables an unpinned beta
		delete(pdf.ProviderOptions["anthropic"].(map[string]interface{}), "anthropicBeta")
		_, _, warnings, err = call(pdf)
		if err != nil || !r88provaHasWarning(warnings, "Message content enabled beta headers: pdfs-2024-09-25. Thread requests require the same beta set on every turn.") {
			t.Fatalf("unpinned warnings = %v (%v)", warnings, err)
		}

		// retains JSON-tool choice while omitting its synthetic definition
		body, _, _, err = call(&provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{r88provaUserHello()}},
			ResponseFormat:  &provider.ResponseFormat{Type: "json", Schema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
			ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"structuredOutputMode": "jsonTool", "thread": map[string]interface{}{"type": "continue", "previousMessageId": "msg_json"}}}})
		if _, ok := body["tools"]; err != nil || ok || body["tool_choice"].(map[string]interface{})["type"] != "any" {
			t.Fatalf("json tool body = %v (%v)", body, err)
		}

		// counts top-level cache control toward the four-breakpoint limit
		var parts []types.ContentPart
		for i := 0; i < 4; i++ {
			parts = append(parts, types.TextContent{Text: "Block", ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"cacheControl": map[string]interface{}{"type": "ephemeral"}}}})
		}
		body, _, warnings, err = call(&provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: parts}}},
			ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"thread": map[string]interface{}{"type": "create"}, "cacheControl": map[string]interface{}{"type": "ephemeral"}}}})
		if err != nil {
			t.Fatal(err)
		}
		cached := 0
		for _, p := range body["messages"].([]interface{})[0].(map[string]interface{})["content"].([]interface{}) {
			if p.(map[string]interface{})["cache_control"] != nil {
				cached++
			}
		}
		limit := false
		for _, w := range warnings {
			limit = limit || w.Feature == "cacheControl breakpoint limit"
		}
		if cached != 3 || !limit {
			t.Fatalf("cached = %d, warnings = %v", cached, warnings)
		}

		// allows empty continuation messages for pause_turn
		body, _, _, err = call(&provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{}},
			ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"thread": map[string]interface{}{"type": "continue", "previousMessageId": "msg_paused"}}}})
		if err != nil || r88provaJSON(body["messages"]) != "[]" {
			t.Fatalf("empty continuation: %v (%v)", body["messages"], err)
		}

		// leaves requests unchanged when threads are not enabled
		body, beta, _, err = call(r88provaThreadOptions(nil))
		if _, ok := body["thread"]; err != nil || ok || strings.Contains(beta, "message-threads") {
			t.Fatalf("no thread: %v %s", body["thread"], beta)
		}

		// allows keeping all thinking blocks
		body, _, _, err = call(r88provaThreadOptions(map[string]interface{}{"thread": map[string]interface{}{"type": "create"},
			"contextManagement": map[string]interface{}{"edits": []interface{}{map[string]interface{}{"type": "clear_thinking_20251015", "keep": "all"}}}}))
		if err != nil || r88provaJSON(body["context_management"]) != `{"edits":[{"keep":"all","type":"clear_thinking_20251015"}]}` {
			t.Fatalf("keep all: %v (%v)", body["context_management"], err)
		}
	}

	for _, method := range []string{"generate", "stream"} {
		// rejects unsupported context management
		for _, incompatible := range []map[string]interface{}{
			{"compaction": map[string]interface{}{"type": "summarize"}},
			{"contextManagement": map[string]interface{}{"edits": []interface{}{map[string]interface{}{"type": "clear_tool_uses_20250919"}}}},
			{"contextManagement": map[string]interface{}{"edits": []interface{}{map[string]interface{}{"type": "clear_thinking_20251015"}}}},
			{"contextManagement": map[string]interface{}{"edits": []interface{}{map[string]interface{}{"type": "compact_20260112"}}}},
		} {
			s := r88provaNewServer(t, r88provaTextResponse, r88provaTextEvents)
			po := map[string]interface{}{"thread": map[string]interface{}{"type": "create"}}
			for k, v := range incompatible {
				po[k] = v
			}
			body, _, _, err := r88provaThreadCall(t, method, r88provaModel(t, s, "claude-sonnet-4-6"), s, r88provaThreadOptions(po))
			if err == nil || !strings.Contains(err.Error(), "message threads do not support") || body != nil {
				t.Fatalf("%v: %v", incompatible, err)
			}
		}
		// rejects invalid thread options
		for _, thread := range []map[string]interface{}{{"type": "continue"}, {"type": "continue", "previousMessageId": ""}, {"type": "invalid"}} {
			s := r88provaNewServer(t, r88provaTextResponse, r88provaTextEvents)
			body, _, _, err := r88provaThreadCall(t, method, r88provaModel(t, s, "claude-sonnet-4-6"), s, r88provaThreadOptions(map[string]interface{}{"thread": thread}))
			if err == nil || body != nil {
				t.Fatalf("%v accepted", thread)
			}
		}
		// supports a custom provider-options namespace
		s := r88provaNewServer(t, r88provaTextResponse, r88provaTextEvents)
		m, _ := New(Config{APIKey: "k", BaseURL: s.srv.URL, Name: "custom.messages"}).LanguageModel("claude-sonnet-4-6")
		body, _, _, err := r88provaThreadCall(t, method, m, s, &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{r88provaUserHello()}},
			ProviderOptions: map[string]interface{}{"custom": map[string]interface{}{"thread": map[string]interface{}{"type": "continue", "previousMessageId": "msg_custom"}}}})
		if err != nil || r88provaJSON(body["thread"]) != `{"previous_message_id":"msg_custom","type":"continue"}` {
			t.Fatalf("custom namespace: %v (%v)", body["thread"], err)
		}
		// unsupported thread platforms
		for _, name := range []string{"bedrock.anthropic.messages", "googleVertex.anthropic.messages", "minimax"} {
			no := false
			s := r88provaNewServer(t, r88provaTextResponse, r88provaTextEvents)
			m, _ := New(Config{APIKey: "k", BaseURL: s.srv.URL, Name: name, SupportsMessageThreads: &no}).LanguageModel("claude-sonnet-4-6")
			body, _, _, err := r88provaThreadCall(t, method, m, s, r88provaThreadOptions(map[string]interface{}{"thread": map[string]interface{}{"type": "create"}}))
			var uf *providererrors.UnsupportedFunctionalityError
			want := name + " does not support Anthropic message threads."
			if name == "minimax" {
				want = "minimax.messages does not support Anthropic message threads."
			}
			if !errors.As(err, &uf) || uf.Error() != want || body != nil {
				t.Fatalf("%s: %v", name, err)
			}
		}
	}
}

// TS 6b101ac076 'thread errors': the parsed error object (with details)
// is the ProviderError data; anthropic-error.test.ts details shapes.
func TestR88provaThreadErrorData(t *testing.T) {
	for _, tc := range []struct{ details, want string }{
		{`{"error_code":"thread_not_found"}`, `{"error":{"details":{"error_code":"thread_not_found"},"message":"Thread not found","type":"not_found_error"},"type":"error"}`},
		{`{"error_code":"thread_not_found","unsupported":[{"future":true}]}`, `{"error":{"details":{"error_code":"thread_not_found"},"message":"Thread not found","type":"not_found_error"},"type":"error"}`},
		{`{"error_code":"thread_not_found","unknown_field":"future"}`, `{"error":{"details":{"error_code":"thread_not_found"},"message":"Thread not found","type":"not_found_error"},"type":"error"}`},
		{`"future details shape"`, `{"error":{"message":"Thread not found","type":"not_found_error"},"type":"error"}`},
		{`null`, `{"error":{"details":null,"message":"Thread not found","type":"not_found_error"},"type":"error"}`},
		{`{"unsupported":["a"],"error_code":null}`, `{"error":{"details":{"error_code":null,"unsupported":["a"]},"message":"Thread not found","type":"not_found_error"},"type":"error"}`},
	} {
		for _, method := range []string{"generate", "stream"} {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(404)
				_, _ = io.WriteString(w, `{"type":"error","error":{"type":"not_found_error","message":"Thread not found","details":`+tc.details+`},"extra":1}`)
			}))
			m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("claude-sonnet-4-6")
			opts := r88provaThreadOptions(map[string]interface{}{"thread": map[string]interface{}{"type": "continue", "previousMessageId": "msg_expired"}})
			var err error
			if method == "generate" {
				_, err = m.DoGenerate(context.Background(), opts)
			} else {
				_, err = m.DoStream(context.Background(), opts)
			}
			srv.Close()
			var pe *providererrors.ProviderError
			if !errors.As(err, &pe) || pe.StatusCode != 404 || pe.Message != "Thread not found" {
				t.Fatalf("%s: err = %v", method, err)
			}
			if got := r88provaJSON(pe.Data); got != tc.want {
				t.Fatalf("%s %s: data = %s", method, tc.details, got)
			}
		}
	}
}

// TS 6b101ac076 anthropic-batch.test.ts: batches reject message threads
// before any request.
func TestR88provaBatchRejectsThreads(t *testing.T) {
	s := r88provaNewServer(t, "{}", nil)
	req := textBatchRequest("thread-request", ClaudeSonnet4_5, "Hello")
	req.Text.Options.ProviderOptions = map[string]interface{}{"anthropic": map[string]interface{}{"thread": map[string]interface{}{"type": "create"}}}
	_, err := New(Config{APIKey: "k", BaseURL: s.srv.URL}).ExperimentalBatch().DoStartBatch(t.Context(), provider.BatchV4StartOptions{Requests: []provider.BatchV4Request{req}})
	var uf *providererrors.UnsupportedFunctionalityError
	if !errors.As(err, &uf) || uf.Error() != `Anthropic Message Batches do not support message threads (request "thread-request").` || len(s.bodies) != 0 {
		t.Fatalf("err = %v", err)
	}
}

// TS 6b101ac076 / a161c0a665: on a continuation the prepared tools still
// drive implicit code execution, custom tool names and toolset checks,
// although tools is not sent.
func TestR88provaThreadContinuationKeepsPreparedTools(t *testing.T) {
	cont := map[string]interface{}{"anthropic": map[string]interface{}{"thread": map[string]interface{}{"type": "continue", "previousMessageId": "msg_previous"}}}
	m, _ := New(Config{APIKey: "k"}).LanguageModel("claude-sonnet-4-6")
	req, err := m.(*LanguageModel).prepareRequest(&provider.GenerateOptions{
		Prompt:          types.Prompt{Messages: []types.Message{r88provaUserHello()}},
		Tools:           []types.Tool{{Type: types.ToolTypeProviderDefined, ProviderID: "anthropic.web_search_20260209", Name: "research", ProviderArgs: map[string]interface{}{}}},
		ProviderOptions: cont,
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := req.body["tools"]; ok || !req.markCodeExecutionDynamic || len(req.preparedTools) != 1 {
		t.Fatalf("tools sent %v, dynamic %v, prepared %v", req.body["tools"], req.markCodeExecutionDynamic, req.preparedTools)
	}
	for _, streaming := range []bool{false, true} {
		desktop := r88provaToolset(nil)
		desktop.Name = "desktop"
		s := r88provaMemberResponse(t, "screenshot", "computer", nil, nil)
		calls, _, err := r88provaCall(t, s, streaming, &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Tools: []types.Tool{desktop}, ProviderOptions: cont})
		if err != nil || len(calls) != 1 || calls[0].ToolName != "desktop" || calls[0].RawArguments != `{"action":"screenshot"}` {
			t.Fatalf("streaming %v: %v %v", streaming, calls, err)
		}
		if _, ok := s.body(0)["tools"]; ok {
			t.Fatal("tools sent on continuation")
		}
		s = r88provaMemberResponse(t, "screenshot", "computer", nil, nil)
		_, _, err = r88provaCall(t, s, streaming, &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, ProviderOptions: cont,
			Tools: []types.Tool{r88provaToolset(map[tools.ComputerToolset20260801Member]tools.ComputerToolset20260801MemberConfig{tools.ComputerToolsetScreenshot: {Enabled: r88provaBool(false)}})}})
		var ire *providererrors.InvalidResponseDataError
		if !errors.As(err, &ire) {
			t.Fatalf("disabled member on continuation: %v", err)
		}
	}
}

// cr-r88-tools-to-prova 1 (TS 8291ccf237, z.enum().optional()): a null
// serviceTier (and the other enum options) fails before the request under
// the anthropic and the minimax keys.
func TestR88provaNullEnumOptionsRejected(t *testing.T) {
	for _, name := range []string{"anthropic", "minimax"} {
		for _, key := range []string{"serviceTier", "effort", "speed", "inferenceGeo", "structuredOutputMode"} {
			s := r88provaNewServer(t, r88provaTextResponse, nil)
			m, _ := New(Config{APIKey: "k", BaseURL: s.srv.URL, Name: name}).LanguageModel("claude-haiku-4-5")
			_, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"},
				ProviderOptions: map[string]interface{}{name: map[string]interface{}{key: nil}}})
			var ae *providererrors.InvalidArgumentError
			if !errors.As(err, &ae) || len(s.bodies) != 0 {
				t.Fatalf("%s %s: err = %v, requests = %d", name, key, err, len(s.bodies))
			}
		}
	}
}

// r88provaFilesServer answers the files API: the handler gets the method
// and path.
func r88provaFilesServer(t *testing.T, h func(w http.ResponseWriter, r *http.Request)) (*FilesAPI, *[]*http.Request) {
	t.Helper()
	var mu sync.Mutex
	var reqs []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		reqs = append(reqs, r.Clone(context.Background()))
		mu.Unlock()
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return New(Config{APIKey: "test-api-key", BaseURL: srv.URL}).Files().(*FilesAPI), &reqs
}

// TS 8b11ff6eb6 files/anthropic-files.test.ts.
func TestR88provaFilesV4(t *testing.T) {
	full := `{"id":"file-abc123","type":"file","filename":"test.pdf","mime_type":"application/pdf","size_bytes":12345,"created_at":"2025-04-14T12:00:00Z","downloadable":true}`
	jsonBody := func(body string) func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, body)
		}
	}
	ref := types.ProviderReference{"anthropic": "file-abc123"}
	ctx := context.Background()

	// upload: metadata only for present fields, result fields filled
	files, _ := r88provaFilesServer(t, jsonBody(`{"id":"file-abc123","type":"file","filename":"test.pdf","mime_type":"application/pdf","size_bytes":12345,"created_at":"2025-04-14T12:00:00Z","downloadable":null,"expires_at":"2025-04-15T12:00:00Z"}`))
	up, err := files.UploadFile(ctx, types.UploadFileOptions{Data: types.FileData{Type: types.FileDataTypeData, Data: []byte{1, 2, 3}}, MediaType: "application/octet-stream"})
	if err != nil {
		t.Fatal(err)
	}
	if r88provaJSON(up.ProviderMetadata) != `{"anthropic":{"createdAt":"2025-04-14T12:00:00Z","expiresAt":"2025-04-15T12:00:00Z","filename":"test.pdf","mimeType":"application/pdf","sizeBytes":12345}}` ||
		up.ByteSize == nil || *up.ByteSize != 12345 || up.CreatedAt == nil || up.ExpiresAt == nil || !up.ExpiresAt.Equal(up.CreatedAt.Add(24*time.Hour)) {
		t.Fatalf("upload = %+v", up)
	}
	// upload with only an id: fallback media type, no metadata keys
	files, _ = r88provaFilesServer(t, jsonBody(`{"id":"file-abc123"}`))
	up, err = files.UploadFile(ctx, types.UploadFileOptions{Data: types.FileData{Type: types.FileDataTypeData, Data: []byte{1}}, MediaType: "text/plain", Filename: "a.txt"})
	if err != nil || up.MediaType != "text/plain" || up.Filename != "a.txt" || r88provaJSON(up.ProviderMetadata) != `{"anthropic":{}}` || up.ByteSize != nil {
		t.Fatalf("id-only upload = %+v (%v)", up, err)
	}

	// getFileMetadata
	files, reqs := r88provaFilesServer(t, jsonBody(full))
	md, err := files.GetFileMetadata(ctx, provider.FileMetadataOptions{File: ref})
	if err != nil {
		t.Fatal(err)
	}
	if (*reqs)[0].Method != "GET" || (*reqs)[0].URL.Path != "/files/file-abc123" || (*reqs)[0].Header.Get("anthropic-beta") != "files-api-2025-04-14" {
		t.Fatalf("request = %s %s %v", (*reqs)[0].Method, (*reqs)[0].URL.Path, (*reqs)[0].Header)
	}
	if md.Filename != "test.pdf" || md.MediaType != "application/pdf" || *md.ByteSize != 12345 || !md.CreatedAt.Equal(time.Date(2025, 4, 14, 12, 0, 0, 0, time.UTC)) ||
		r88provaJSON(md.ProviderMetadata) != `{"anthropic":{"createdAt":"2025-04-14T12:00:00Z","downloadable":true,"filename":"test.pdf","mimeType":"application/pdf","sizeBytes":12345}}` {
		t.Fatalf("metadata = %+v", md)
	}
	for _, body := range []string{`{"id":"file-abc123"}`, `{"id":"file-abc123","type":null,"filename":null,"mime_type":null,"size_bytes":null,"created_at":null,"downloadable":null}`} {
		files, _ = r88provaFilesServer(t, jsonBody(body))
		md, err = files.GetFileMetadata(ctx, provider.FileMetadataOptions{File: ref})
		if err != nil || md.Filename != "" || md.ByteSize != nil || md.CreatedAt != nil || md.ProviderReference["anthropic"] != "file-abc123" {
			t.Fatalf("%s: %+v (%v)", body, md, err)
		}
	}

	// deleteFile
	for _, body := range []string{`{"id":"file-abc123","type":"file_deleted"}`, `{"id":"file-abc123"}`, `{"id":"file-abc123","type":null}`} {
		files, reqs = r88provaFilesServer(t, jsonBody(body))
		del, err := files.DeleteFile(ctx, provider.DeleteFileOptions{File: ref})
		if err != nil || !del.Deleted || del.ProviderReference["anthropic"] != "file-abc123" || (*reqs)[0].Method != "DELETE" {
			t.Fatalf("%s: %+v (%v)", body, del, err)
		}
	}

	// downloadFile: streamed content, media type without parameters
	for _, tc := range []struct{ ct, want string }{{"application/pdf; charset=utf-8", "application/pdf"}, {"", ""}, {"; charset=utf-8", ""}} {
		files, reqs = r88provaFilesServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header()["Content-Type"] = []string{tc.ct}
			_, _ = io.WriteString(w, "file content")
		})
		dl, err := files.DownloadFile(ctx, provider.DownloadFileOptions{File: ref})
		if err != nil {
			t.Fatal(err)
		}
		content, _ := io.ReadAll(dl.Content)
		_ = dl.Content.Close()
		if string(content) != "file content" || dl.MediaType != tc.want || (*reqs)[0].URL.Path != "/files/file-abc123/content" {
			t.Fatalf("download %q: %q %q", tc.ct, content, dl.MediaType)
		}
	}

	// errors and call headers for every operation
	for _, op := range []string{"getFileMetadata", "downloadFile", "deleteFile"} {
		files, reqs = r88provaFilesServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(404)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"not_found_error","message":"File not found"}}`)
		})
		h := map[string]string{"x-request-id": "request-1"}
		var err error
		switch op {
		case "getFileMetadata":
			_, err = files.GetFileMetadata(ctx, provider.FileMetadataOptions{File: ref, Headers: h})
		case "downloadFile":
			_, err = files.DownloadFile(ctx, provider.DownloadFileOptions{File: ref, Headers: h})
		default:
			_, err = files.DeleteFile(ctx, provider.DeleteFileOptions{File: ref, Headers: h})
		}
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.StatusCode != 404 || pe.Message != "File not found" {
			t.Fatalf("%s: %v", op, err)
		}
		if got := (*reqs)[0].Header; got.Get("x-request-id") != "request-1" || got.Get("anthropic-beta") != "files-api-2025-04-14" || got.Get("x-api-key") != "test-api-key" {
			t.Fatalf("%s headers = %v", op, got)
		}
		// blank, whitespace-only and missing ids
		for _, bad := range []types.ProviderReference{{"anthropic": ""}, {"anthropic": " \t"}, {"openai": "x"}} {
			var err error
			switch op {
			case "getFileMetadata":
				_, err = files.GetFileMetadata(ctx, provider.FileMetadataOptions{File: bad})
			case "downloadFile":
				_, err = files.DownloadFile(ctx, provider.DownloadFileOptions{File: bad})
			default:
				_, err = files.DeleteFile(ctx, provider.DeleteFileOptions{File: bad})
			}
			var ae *providererrors.InvalidArgumentError
			if !errors.As(err, &ae) || !strings.Contains(err.Error(), "file reference is missing an 'anthropic' file id.") {
				t.Fatalf("%s %v: %v", op, bad, err)
			}
		}
	}
}

var (
	_ provider.FileMetadataGetter = (*FilesAPI)(nil)
	_ provider.FileDownloader     = (*FilesAPI)(nil)
	_ provider.FileDeleter        = (*FilesAPI)(nil)
)

// TS 4b8f00efff anthropic-prepare-tools.test.ts: no strict-tool beta for
// providers that only support native final output.
func TestR88provaPrepareToolsNativeOutputOnly(t *testing.T) {
	strict := true
	p := prepareTools(prepareToolsOptions{
		tools:                    []types.Tool{{Name: "report", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}, Strict: &strict}},
		supportsStructuredOutput: true,
	})
	if len(p.betas) != 0 || len(p.warnings) != 1 || p.warnings[0].Feature != "strict" || p.tools[0]["strict"] != nil {
		t.Fatalf("betas %v warnings %v tool %v", p.betas, p.warnings, p.tools[0])
	}
}

// TS convertAnthropicBatchResponse has no toolset mapping: a toolset member
// tool_use is a plain tool call named by the member (no computer tool call
// that skipped the member checks).
func TestR88provaBatchToolsetMemberIsPlainToolCall(t *testing.T) {
	s := &anthropicBatchResultsStream{provider: New(Config{APIKey: "k"})}
	got := s.convertResult(anthropicBatchResultLineWire{CustomID: "r1", Result: []byte(`{"type":"succeeded","message":{"id":"m","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"tool_use","id":"t1","name":"screenshot","toolset_name":"computer","input":{"action":"type","text":"x"}}],"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1}}}`)})
	if got.TextResult == nil || len(got.TextResult.ToolCalls) != 1 {
		t.Fatalf("result = %+v", got)
	}
	tc := got.TextResult.ToolCalls[0]
	if tc.ToolName != "screenshot" || tc.RawArguments != `{"action":"type","text":"x"}` || tc.ProviderMetadata != nil {
		t.Fatalf("tool call = %+v", tc)
	}
}

// TS createJsonErrorResponseHandler parses with safeParseJSON
// (secureJsonParse): an error body with a __proto__ key gives the status
// text and no data.
func TestR88provaErrorBodySecureParse(t *testing.T) {
	perr := anthropicHTTPError("anthropic", 400, nil, []byte(`{"type":"error","error":{"type":"x","message":"m","__proto__":{}}}`), nil)
	if perr.Message != "Bad Request" || perr.Data != nil {
		t.Fatalf("message %q data %v", perr.Message, perr.Data)
	}
}
