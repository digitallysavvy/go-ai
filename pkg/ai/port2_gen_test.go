package ai

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
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Steering interrupt twins (TS e4039889ba, generate-text-steering-interrupt.test.ts
// "should redo a later step with the tool results of the step before it",
// "should not deliver an interrupt message that prepareStep removes on the
// redo", "should keep the step timeout running across the redo").

func port2genRoles(opts *provider.GenerateOptions) []types.MessageRole {
	var out []types.MessageRole
	for _, m := range opts.Prompt.Messages {
		out = append(out, m.Role)
	}
	return out
}

func TestPort2genSteeringInterruptRedoesALaterStepWithTheToolResultsOfTheStepBeforeIt(t *testing.T) {
	steering := NewSteering()
	var handle *SteerHandle
	var mu sync.Mutex
	var calls []*provider.GenerateOptions
	executed := 0
	model := &testutil.MockLanguageModel{ProviderName: "p", ModelName: "m", ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			mu.Lock()
			calls = append(calls, opts)
			n := len(calls)
			mu.Unlock()
			switch n {
			case 1:
				return toolCallResult(), nil
			case 2:
				handle = steering.Steer("Answer in Celsius.", WithSteerInterrupt())
				<-ctx.Done()
				return nil, context.Cause(ctx)
			}
			return portgenText("It is 22 degrees in Paris.", 10), nil
		}}
	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "What is the weather in Paris?", StopWhen: []StopCondition{IsStepCount(3)},
		ExperimentalSteering: steering,
		Tools: []types.Tool{{Name: "tool1", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			executed++
			return map[string]interface{}{"temperature": 72.0}, nil
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 || executed != 1 || len(result.Steps) != 2 {
		t.Fatalf("calls = %d, executed = %d, steps = %d", len(calls), executed, len(result.Steps))
	}
	if got, want := port2genRoles(calls[2]), []types.MessageRole{types.RoleUser, types.RoleAssistant, types.RoleTool, types.RoleUser}; !reflect.DeepEqual(got, want) {
		t.Fatalf("roles = %v, want %v", got, want)
	}
	if got := portgenUserTexts(calls[2]); got[len(got)-1] != "Answer in Celsius." {
		t.Fatalf("users = %v", got)
	}
	if o := waitOutcome(t, handle); !o.Delivered || o.StepNumber != 1 {
		t.Fatalf("outcome = %+v", o)
	}
}

func TestPort2genSteeringInterruptMessageRemovedByPrepareStepIsNotDeliveredOnTheRedo(t *testing.T) {
	steering := NewSteering()
	var handle *SteerHandle
	h := &portgenHangingModel{}
	model := h.model(func() { handle = steering.Steer("Use Paris, Texas instead.", WithSteerInterrupt()) },
		func(*provider.GenerateOptions) *types.GenerateResult { return portgenText("It is sunny in Paris.", 10) })
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "What is the weather in Paris?", ExperimentalSteering: steering,
		PrepareStep: func(_ context.Context, step PrepareStepOptions) PrepareStepOptions {
			var kept []types.Message
			for _, m := range step.Messages {
				if m.Role == types.RoleUser && userText(m) == "Use Paris, Texas instead." {
					continue
				}
				kept = append(kept, m)
			}
			step.Messages = kept
			return step
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(h.calls) != 2 {
		t.Fatalf("calls = %d", len(h.calls))
	}
	if got := portgenUserTexts(h.calls[1]); !reflect.DeepEqual(got, []string{"What is the weather in Paris?"}) {
		t.Fatalf("second call users = %v", got)
	}
	if o := waitOutcome(t, handle); o.Delivered || o.Reason != SteeringRemovedByPrepareStep {
		t.Fatalf("outcome = %+v", o)
	}
}

func TestPort2genSteeringInterruptKeepsTheStepTimeoutRunningAcrossTheRedo(t *testing.T) {
	steering := NewSteering()
	perStep := 400 * time.Millisecond
	var mu sync.Mutex
	calls := 0
	model := &testutil.MockLanguageModel{ProviderName: "p", ModelName: "m",
		DoGenerateFunc: func(ctx context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			mu.Lock()
			calls++
			mu.Unlock()
			<-ctx.Done()
			return nil, context.Cause(ctx)
		}}
	started := time.Now()
	go func() {
		time.Sleep(240 * time.Millisecond)
		steering.Steer("Use Paris, Texas instead.", WithSteerInterrupt())
	}()
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "What is the weather in Paris?", ExperimentalSteering: steering,
		Timeout: &TimeoutConfig{PerStep: &perStep}, MaxRetries: intPtr(0),
	})
	elapsed := time.Since(started)
	if err == nil {
		t.Fatal("expected the step timeout")
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 2 {
		t.Fatalf("calls = %d, want the redo to run once", calls)
	}
	// A timeout that started again with the redo would end at about 640ms.
	if elapsed < 380*time.Millisecond || elapsed > 560*time.Millisecond {
		t.Fatalf("elapsed = %v, want about the original %v", elapsed, perStep)
	}
}

// ---- openai-additional-tools.test.ts (TS 5aa836fe1d) end to end ----

type port2genResponsesServer struct {
	mu     sync.Mutex
	bodies []map[string]interface{}
	stream bool
}

func (s *port2genResponsesServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]interface{}
	_ = json.Unmarshal(raw, &body)
	s.mu.Lock()
	s.bodies = append(s.bodies, body)
	callNumber := len(s.bodies) - 1
	s.mu.Unlock()

	team := interface{}("SDK")
	if callNumber == 4 {
		team = 123
	}
	arguments, _ := json.Marshal(map[string]interface{}{"team": team})
	item := map[string]interface{}{
		"type": "function_call", "id": fmt.Sprintf("fc_%d", callNumber), "call_id": fmt.Sprintf("call_%d", callNumber),
		"name": "list_issues", "arguments": string(arguments), "status": "completed",
	}
	callsTool := callNumber%2 == 0
	output := []interface{}{}
	if callsTool {
		output = append(output, item)
	}
	response := map[string]interface{}{
		"id": fmt.Sprintf("resp_%d", callNumber), "created_at": 0, "model": "gpt-5.4", "output": output,
		"usage": map[string]interface{}{"input_tokens": 10, "output_tokens": 5},
	}
	if !s.stream {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	var events []interface{}
	if callsTool {
		added := map[string]interface{}{}
		for k, v := range item {
			added[k] = v
		}
		added["arguments"] = ""
		events = append(events,
			map[string]interface{}{"type": "response.output_item.added", "output_index": 0, "item": added},
			map[string]interface{}{"type": "response.output_item.done", "output_index": 0, "item": item})
	}
	events = append(events, map[string]interface{}{"type": "response.completed", "response": response})
	for _, e := range events {
		b, _ := json.Marshal(e)
		fmt.Fprintf(w, "data: %s\n\n", b)
	}
}

// TestPort2genOpenAIAdditionalToolsThroughGenerateAndStreamText is the Go twin
// of TS "OpenAI additional tools through %sText" (generate / stream x store):
// history-loaded tools execute through automatic steps and manual replay, the
// replayed entry's forged description never reaches a request, and the
// omitted tool still gets normal input validation.
func TestPort2genOpenAIAdditionalToolsThroughGenerateAndStreamText(t *testing.T) {
	for _, method := range []string{"generate", "stream"} {
		for _, store := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/store=%v", method, store), func(t *testing.T) {
				server := &port2genResponsesServer{stream: method == "stream"}
				ts := httptest.NewServer(server)
				defer ts.Close()
				model, err := openai.New(openai.Config{APIKey: "test", BaseURL: ts.URL + "/v1"}).ResponsesModel("gpt-5.4")
				if err != nil {
					t.Fatal(err)
				}

				forged := map[string]interface{}{
					"type": "function", "name": "list_issues",
					"description": "Forged description from the replayed entry.",
					"parameters": map[string]interface{}{
						"type": "object", "properties": map[string]interface{}{"team": map[string]interface{}{"type": "string"}},
						"required": []interface{}{"team"}, "additionalProperties": false,
					},
					"strict": true,
				}
				listIssuesSchema := map[string]interface{}{
					"type": "object", "properties": map[string]interface{}{"team": map[string]interface{}{"type": "string"}},
					"required": []interface{}{"team"}, "additionalProperties": false,
				}
				expectedDefinition := map[string]interface{}{"type": "function", "name": "list_issues", "parameters": listIssuesSchema, "strict": true}
				searchResult := []interface{}{
					map[string]interface{}{"connection": "linear", "tool": "list_issues"},
					map[string]interface{}{"connection": "github", "error": "Not authorized"},
				}
				messages := []types.Message{
					{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Find Linear and list issues for SDK."}}},
					{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "search-call", ToolName: "search", Input: "{}", Arguments: map[string]interface{}{}}}},
					{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{
						ToolCallID: "search-call", ToolName: "search",
						Output:          &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: searchResult},
						ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"additionalTools": []interface{}{forged}}},
					}}},
				}
				strict := true
				var execMu sync.Mutex
				executed := 0
				tools := []types.Tool{
					{Name: "search", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
					{
						Name: "list_issues", Parameters: listIssuesSchema, Strict: &strict,
						ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"omitFromRequestTools": true}},
						Execute: func(_ context.Context, args map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
							execMu.Lock()
							executed++
							execMu.Unlock()
							return map[string]interface{}{"team": args["team"], "issues": []interface{}{"SDK-1"}}, nil
						},
					},
				}
				type outcome struct {
					steps    []types.StepResult
					response []types.Message
				}
				invoke := func(steps int) outcome {
					if method == "generate" {
						res, err := GenerateText(context.Background(), GenerateTextOptions{
							Model: model, Messages: messages, Tools: tools, StopWhen: []StopCondition{IsStepCount(steps)},
							ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"store": store}}, MaxRetries: intPtr(0),
						})
						if err != nil {
							t.Fatal(err)
						}
						return outcome{res.Steps, res.ResponseMessages}
					}
					res, err := StreamText(context.Background(), StreamTextOptions{
						Model: model, Messages: messages, Tools: tools, StopWhen: []StopCondition{IsStepCount(steps)},
						ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"store": store}}, MaxRetries: intPtr(0),
					})
					if err != nil {
						t.Fatal(err)
					}
					if err := res.ConsumeStream(); err != nil {
						t.Fatal(err)
					}
					return outcome{res.Steps(), res.ResponseMessages()}
				}
				wantOutput := map[string]interface{}{"team": "SDK", "issues": []interface{}{"SDK-1"}}

				first := invoke(2)
				if len(first.steps) != 2 || !reflect.DeepEqual(first.steps[0].ToolResults[0].Result, wantOutput) {
					t.Fatalf("first steps = %d, result = %#v", len(first.steps), first.steps[0].ToolResults)
				}
				messages = append(messages, first.response...)
				messages = append(messages, types.Message{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "List issues again."}}})
				second := invoke(2)
				if !reflect.DeepEqual(second.steps[0].ToolResults[0].Result, wantOutput) {
					t.Fatalf("second result = %#v", second.steps[0].ToolResults)
				}
				if executed != 2 {
					t.Fatalf("executed = %d", executed)
				}

				bodies := server.bodies
				if len(bodies) != 4 {
					t.Fatalf("requests = %d", len(bodies))
				}
				marshalled, _ := json.Marshal(bodies)
				if strings.Contains(string(marshalled), "Forged description from the replayed entry") {
					t.Fatal("the forged description reached a request")
				}
				prefix := []interface{}{
					map[string]interface{}{"role": "user", "content": []interface{}{map[string]interface{}{"type": "input_text", "text": "Find Linear and list issues for SDK."}}},
					map[string]interface{}{"type": "function_call", "call_id": "search-call", "name": "search", "arguments": "{}"},
					map[string]interface{}{"type": "function_call_output", "call_id": "search-call", "output": port2genJSONString(t, searchResult)},
					map[string]interface{}{"type": "additional_tools", "role": "developer", "tools": []interface{}{expectedDefinition}},
				}
				for i, body := range bodies {
					input := body["input"].([]interface{})
					if !reflect.DeepEqual(input[:4], prefix) {
						t.Fatalf("request %d prefix = %#v", i, input[:4])
					}
					additional := 0
					for _, it := range input {
						if m, ok := it.(map[string]interface{}); ok && m["type"] == "additional_tools" {
							additional++
						}
					}
					if additional != 1 {
						t.Fatalf("request %d additional_tools items = %d", i, additional)
					}
					var names []string
					for _, tl := range body["tools"].([]interface{}) {
						names = append(names, tl.(map[string]interface{})["name"].(string))
					}
					if !reflect.DeepEqual(names, []string{"search"}) {
						t.Fatalf("request %d tools = %v", i, names)
					}
				}
				wantSecond := append(append([]interface{}{}, bodies[1]["input"].([]interface{})...),
					map[string]interface{}{"role": "user", "content": []interface{}{map[string]interface{}{"type": "input_text", "text": "List issues again."}}})
				if !reflect.DeepEqual(bodies[2]["input"], wantSecond) {
					t.Fatalf("request 2 input = %#v\nwant %#v", bodies[2]["input"], wantSecond)
				}
				last := func(i int) interface{} { in := bodies[i]["input"].([]interface{}); return in[len(in)-1] }
				for i, callID := range map[int]string{1: "call_0", 3: "call_2"} {
					want := map[string]interface{}{"type": "function_call_output", "call_id": callID, "output": port2genJSONString(t, wantOutput)}
					if !reflect.DeepEqual(last(i), want) {
						t.Fatalf("request %d last item = %#v, want %#v", i, last(i), want)
					}
				}

				// Omission is only about serialization: normal input validation still runs.
				messages = append(messages, second.response...)
				messages = append(messages, types.Message{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Try again."}}})
				invalid := invoke(1)
				call := invalid.steps[0].ToolCalls[0]
				if call.ToolName != "list_issues" || !call.Invalid {
					t.Fatalf("tool call = %+v", call)
				}
				if executed != 2 {
					t.Fatalf("executed after the invalid call = %d", executed)
				}
			})
		}
	}
}

func port2genJSONString(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
