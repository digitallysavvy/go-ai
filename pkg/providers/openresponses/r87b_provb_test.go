package openresponses

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// V061 35 (TS 3fab50aa9b open-responses-language-model.ts doStream:
// createEventSourceResponseHandler(z.any()) then plain JS field reads).
// Expected parts recorded by running the TS doStream on these events (bun,
// TS source at 3fab50aa9b):
//
//	text-start m1; text-delta m1 5; text-delta m1 [1,[2,null],{"a":1},true];
//	tool-call {toolCallId: 99, toolName: 42, input: '{"a":1}'};
//	tool-call {toolCallId: "c2", toolName: "g", input: "undefinednull"};
//	error {type: "error", code: undefined, message: undefined};
//	finish {unified: "error"}.
//
// A string sequence_number / output_index / content_index, a non-string
// delta, call_id or name, a numeric type, a non-object item and a non-object
// error no longer fail the event decode; non-string values read as JS
// String(value) and an absent / null delta appends "undefined" / "null".
func TestR87bprovbStreamEventLenientFieldTypes(t *testing.T) {
	chunks, err := r87provbStream(t,
		`{"type":"response.output_item.added","sequence_number":"1","output_index":"0","item":{"type":"message","id":"m1","status":1}}`,
		`{"type":"response.output_text.delta","sequence_number":2.5,"item_id":"m1","content_index":"0","delta":5}`,
		`{"type":"response.output_text.delta","item_id":"m1","delta":[1,[2,null],{"a":1},true]}`,
		`{"type":7,"delta":"x"}`,
		`{"type":"response.output_item.added","item":"oops"}`,
		`{"type":"response.output_item.added","item":{"type":"function_call","id":"fc1","call_id":99,"name":42,"arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc1","delta":"{\"a\":"}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc1","delta":1}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc1","delta":"}"}`,
		`{"type":"response.output_item.done","item":{"type":"function_call","id":"fc1","call_id":"c-done","name":"n-done","arguments":""}}`,
		`{"type":"response.output_item.added","item":{"type":"function_call","id":"fc2","call_id":"c2","name":"g","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc2"}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc2","delta":null}`,
		`{"type":"response.output_item.done","item":{"type":"function_call","id":"fc2","arguments":""}}`,
		`{"type":"error","error":"boom"}`,
	)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range chunks {
		switch c.Type {
		case provider.ChunkTypeTextStart:
			got = append(got, "text-start "+c.ID)
		case provider.ChunkTypeText:
			got = append(got, "text "+c.ID+" "+c.Text)
		case provider.ChunkTypeToolCall:
			got = append(got, "tool-call "+c.ToolCall.ID+" "+c.ToolCall.ToolName+" "+c.ToolCall.RawArguments)
		case provider.ChunkTypeError:
			var spe *providererrors.StreamProviderError
			if !errors.As(c.Err, &spe) {
				t.Fatalf("error chunk err = %T %v", c.Err, c.Err)
			}
			got = append(got, "error "+spe.Type+" code="+fmt.Sprint(spe.Code)+" message="+c.Text)
		case provider.ChunkTypeFinish:
			got = append(got, "finish "+string(c.FinishReason))
		}
	}
	want := []string{
		"text-start m1",
		"text m1 5",
		"text m1 1,2,,[object Object],true",
		`tool-call 99 42 {"a":1}`,
		"tool-call c2 g undefinednull",
		"error error code=<nil> message=",
		"finish error",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("parts:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// V061 35, generate side (TS doGenerate applies jsonSchema<...> types without
// validating the body). Recorded with the TS doGenerate at 3fab50aa9b: content
// [text "hi", tool-call {toolCallId: 7, toolName: "f", input: "{}"}],
// finish tool-calls, usage input 3 / output 2. A numeric item status, a
// numeric call_id and a string total_tokens no longer fail the response.
func TestR87bprovbGenerateLenientFieldTypes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"r1","object":"response","created_at":1,"status":"completed","model":"m","output":[{"type":"message","id":"m1","status":1,"role":"assistant","content":[{"type":"output_text","text":"hi","annotations":[]}]},{"type":"function_call","id":"fc1","call_id":7,"name":"f","arguments":"{}","status":"completed"}],"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":"5"}}`))
	}))
	defer srv.Close()
	m := NewLanguageModel(New(Config{BaseURL: srv.URL, Name: "open-responses"}), "m")
	res, err := m.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "hi" || len(res.ToolCalls) != 1 || res.ToolCalls[0].ID != "7" || res.ToolCalls[0].ToolName != "f" {
		t.Fatalf("text %q tool calls %+v", res.Text, res.ToolCalls)
	}
	if res.FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("finish = %v", res.FinishReason)
	}
	if res.Usage.InputTokens == nil || *res.Usage.InputTokens != 3 || res.Usage.OutputTokens == nil || *res.Usage.OutputTokens != 2 {
		t.Fatalf("usage = %+v", res.Usage)
	}
}

// TS 3fab50aa9b createOpenResponsesStreamError passes `code: error.code`
// as is: absent or null is no code, "" is kept.
func TestR87bprovbStreamErrorCodePresence(t *testing.T) {
	for _, tc := range []struct {
		name, event string
		want        interface{}
	}{
		{"error empty", `{"type":"error","error":{"code":"","message":"m"}}`, ""},
		{"error absent", `{"type":"error","error":{"message":"m"}}`, nil},
		{"error null", `{"type":"error","error":{"code":null,"message":"m"}}`, nil},
		{"failed empty", `{"type":"response.failed","response":{"status":"failed","error":{"code":"","message":"m"}}}`, ""},
		{"failed absent", `{"type":"response.failed","response":{"status":"failed","error":{"message":"m"}}}`, nil},
		{"failed value", `{"type":"response.failed","response":{"status":"failed","error":{"code":"x","message":"m"}}}`, "x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chunks, err := r87provbStream(t, tc.event)
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range chunks {
				if c.Type != provider.ChunkTypeError {
					continue
				}
				var spe *providererrors.StreamProviderError
				if !errors.As(c.Err, &spe) {
					t.Fatalf("chunk.Err = %T", c.Err)
				}
				if spe.Code != tc.want {
					t.Fatalf("code = %#v, want %#v", spe.Code, tc.want)
				}
				return
			}
			t.Fatal("no error chunk")
		})
	}
}

// V061 37: TS openResponsesLanguageModelOptions declares reasoningSummary as
// z.enum(['concise', 'detailed', 'auto']).nullish(), so a present empty
// summary is an InvalidArgumentError in TS (never sent); Go rejects it too.
func TestR87bprovbEmptyReasoningSummaryRejected(t *testing.T) {
	m := NewLanguageModel(New(Config{BaseURL: "http://127.0.0.1:0", Name: "open-responses"}), "m")
	_, _, err := m.buildRequestBody(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"open-responses": map[string]interface{}{"reasoningSummary": ""}},
	}, false)
	var iae *providererrors.InvalidArgumentError
	if !errors.As(err, &iae) {
		t.Fatalf("err = %v (%T), want InvalidArgumentError", err, err)
	}
}

// Class sweep of V061 37: TS 3fab50aa9b open-responses-language-model.ts
// sends a custom tool's `description` when `typeof tool.args.description ===
// 'string'`, so a present "" is written; an absent or non-string one is not.
func TestR87bprovbCustomToolEmptyDescription(t *testing.T) {
	for _, tc := range []struct {
		name string
		args map[string]interface{}
		want string
	}{
		{"empty", map[string]interface{}{"description": ""}, `{"type":"custom","name":"render","description":""}`},
		{"absent", map[string]interface{}{}, `{"type":"custom","name":"render"}`},
		{"non-string", map[string]interface{}{"description": 5}, `{"type":"custom","name":"render"}`},
		{"value", map[string]interface{}{"description": "d"}, `{"type":"custom","name":"render","description":"d"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			converted, _, _ := convertToolsToOpenResponses([]types.Tool{{Type: "provider", ProviderID: "acme.custom", Name: "render", ProviderArgs: tc.args}}, nil, "acme.custom")
			b, err := json.Marshal(converted[0])
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != tc.want {
				t.Fatalf("tool = %s, want %s", b, tc.want)
			}
		})
	}
}
