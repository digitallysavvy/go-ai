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

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
)

// r88provaServer serves one canned response (JSON for generate, SSE events
// for stream) and records the request bodies.
type r88provaServer struct {
	mu       sync.Mutex
	bodies   []map[string]interface{}
	headers  []http.Header
	json     string
	events   []string
	srv      *httptest.Server
	paths    []string
	statuses []int
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

func (s *r88provaServer) header(i int) http.Header {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.headers[i]
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
var r88provaPending = map[string]string{
	"should pass sanitized zod output schema as output_config.format":                                                   "r88 pending",
	"should expose container information as provider metadata":                                                          "r88 pending: 8291ccf237 service tier",
	"should stream the tool call":                                                                                       "r88 pending: 8291ccf237 / 9754014925",
	"should support tools with empty parameters in streaming":                                                           "r88 pending: 8291ccf237 / 9754014925",
	"should stream programmatic tool calling with multiple message_start/stop sequences":                                "r88 pending: 8291ccf237 / 9754014925",
	"should stream web search tool results":                                                                             "r88 pending: 8291ccf237 / 9754014925",
	"should stream tool search bm25 results":                                                                            "r88 pending: 8291ccf237 / 9754014925",
	`reads the legacy "vertex" key when "googleVertex" is absent`:                                                       "r88 pending: 6400cf8b88",
	"should send tools added by value as tool definitions with the inline tools beta":                                   "r88 pending: 457dc40d06",
	"should send the same definition for streaming and non-streaming requests":                                          "r88 pending: 457dc40d06",
	"should warn and ignore computer tools added by value":                                                              "r88 pending: 457dc40d06",
	"should keep the response name of a declared computer toolset when an inline computer tool is ignored":              "r88 pending: 457dc40d06",
	"should keep the response name of a declared provider tool when an inline provider tool has the same provider name": "r88 pending: 457dc40d06",
	"should send the tool choice and disabled parallel tool use when all tools are added by value":                      "r88 pending: 457dc40d06",
}

// r88provaWithPending adds r88provaPending to a suite's skip list.
func r88provaWithPending(skips map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range skips {
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
