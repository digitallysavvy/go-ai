package mistral

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Hand ports of the mistral TS assertions the replay harness does not
// evaluate (R87D_TSFIXTURE closure rule), and their marks.

var r87dprovrestPorted = map[string]string{
	tsfixture.PortedKey("should preserve the complete raw usage object without changing normalized usage", "usage", "toStrictEqual"):                                 "TestR87dprovrestMistralRawUsagePreserved",
	tsfixture.PortedKey("uses Conversations even when no search tools are supplied", "model.provider", "toBe"):                                                       "TestR87dprovrestMistralConversationProvider",
	tsfixture.PortedKey("replays search executions and assistant answers on a follow-up turn", "(await server.calls[1].requestBodyJson).inputs", "toEqual"):          "TestR87dprovrestMistralFollowUpReplaysSearch",
	tsfixture.PortedKey("preserves the executed function and raw search result for replay", "(await server.calls[1].requestBodyJson).inputs.slice(1, 3)", "toEqual"): "TestR87dprovrestMistralReplayKeepsExecutedFunction",
	tsfixture.PortedKey("can replay search history after the tool is removed", "(await server.calls[1].requestBodyJson).inputs[1]", "toMatchObject"):                 "TestR87dprovrestMistralReplayWithoutTool",
	tsfixture.PortedKey("does not retain history on a shared model instance", "(await server.calls[1].requestBodyJson).inputs", "toEqual"):                           "TestR87dprovrestMistralNoSharedHistory",
}

const r87dprovrestConvFile = "testdata/tscases/conversation.json.gz"

// mistral-chat-language-model.test.ts 285-289: const { usage } = await model.doGenerate(...); expect(usage).toStrictEqual({...})
func TestR87dprovrestMistralRawUsagePreserved(t *testing.T) {
	const test = "should preserve the complete raw usage object without changing normalized usage"
	run := r87dprovrestReplay(t, "testdata/tscases/chat_asserts.json.gz", test, 0, nil, func(base, id string) provider.LanguageModel {
		if id == "" {
			id = "mistral-small-latest"
		}
		m, err := New(Config{APIKey: "k", BaseURL: base}).LanguageModel(id)
		if err != nil {
			t.Fatal(err)
		}
		return m
	})
	if run.gen == nil {
		t.Fatalf("no generate result (err %v)", run.genErr)
	}
	r87dprovrestEqual(t, "usage", r87dprovrestArg(t, "testdata/tscases/chat_asserts.json.gz", test, 0, "usage"), tsfixture.UsageToTS(run.gen.Usage))
}

// mistral-conversation-language-model.test.ts 165-168: expect(model.provider).toBe('mistral.conversation')
func TestR87dprovrestMistralConversationProvider(t *testing.T) {
	const test = "uses Conversations even when no search tools are supplied"
	run := r87dprovrestReplay(t, r87dprovrestConvFile, test, 0, nil, func(base, id string) provider.LanguageModel {
		return New(Config{APIKey: "test-api-key", BaseURL: base}).ConversationModel("mistral-small-latest")
	})
	if run.gen == nil {
		t.Fatalf("no generate result (err %v)", run.genErr)
	}
	m := New(Config{APIKey: "test-api-key"}).ConversationModel("mistral-small-latest")
	if got, want := m.Provider(), r87dprovrestArg(t, r87dprovrestConvFile, test, 0, "model.provider"); got != want {
		t.Errorf("model.provider = %q, TS %q", got, want)
	}
}

// r87dprovrestSecondCall runs the TS test's two doGenerate calls against the
// case's canned response (served to every request, as the TS beforeEach /
// test response is) and returns the second request body.
func r87dprovrestSecondCall(t *testing.T, response string, first *provider.GenerateOptions, second func(first *types.GenerateResult) *provider.GenerateOptions) map[string]interface{} {
	t.Helper()
	rec, p := newConvRecorder(t, "application/json", response)
	res := generateWith(t, p, first)
	generateWith(t, p, second(res))
	return rec.body(1)
}

// TS 317-349: the follow-up turn replays the search execution and answer.
func TestR87dprovrestMistralFollowUpReplaysSearch(t *testing.T) {
	const test = "replays search executions and assistant answers on a follow-up turn"
	tools := []types.Tool{searchTool("search")}
	body := r87dprovrestSecondCall(t, convOutputs(convExecution, convAnswer), &provider.GenerateOptions{Prompt: whoWon(), Tools: tools}, func(first *types.GenerateResult) *provider.GenerateOptions {
		call := first.Content[0].(types.ToolCallContent)
		return &provider.GenerateOptions{Prompt: types.Prompt{Messages: followupMessages(call, map[string]interface{}{"status": "success"})}, Tools: tools}
	})
	r87dprovrestEqual(t, "server.calls[1] inputs", r87dprovrestArg(t, r87dprovrestConvFile, test, 0, "(await server.calls[1].requestBodyJson).inputs"), body["inputs"])
}

// TS 351-394: the executed function and the raw search result replay.
func TestR87dprovrestMistralReplayKeepsExecutedFunction(t *testing.T) {
	const test = "preserves the executed function and raw search result for replay"
	exec := `{"type":"tool.execution","id":"search-1","name":"web_search","arguments":"{\"url\":\"https://example.com\"}","info":{"result":"[{\"text\":\"Retrieved page\"}]"},"function":"web_fetch"}`
	tools := []types.Tool{searchTool("search")}
	body := r87dprovrestSecondCall(t, convOutputs(exec, convAnswer), &provider.GenerateOptions{Prompt: whoWon(), Tools: tools}, func(first *types.GenerateResult) *provider.GenerateOptions {
		call := first.Content[0].(types.ToolCallContent)
		return &provider.GenerateOptions{Prompt: types.Prompt{Messages: followupMessages(call, map[string]interface{}{"result": `[{"text":"Retrieved page"}]`})}, Tools: tools}
	})
	inputs, _ := body["inputs"].([]interface{})
	if len(inputs) < 3 {
		t.Fatalf("inputs = %v", inputs)
	}
	r87dprovrestEqual(t, "server.calls[1] inputs.slice(1, 3)", r87dprovrestArg(t, r87dprovrestConvFile, test, 0, "(await server.calls[1].requestBodyJson).inputs.slice(1, 3)"), inputs[1:3])
}

// TS 396-405: the search history replays after the tool is removed.
func TestR87dprovrestMistralReplayWithoutTool(t *testing.T) {
	const test = "can replay search history after the tool is removed"
	tools := []types.Tool{searchTool("search")}
	body := r87dprovrestSecondCall(t, convOutputs(convExecution, convAnswer), &provider.GenerateOptions{Prompt: whoWon(), Tools: tools}, func(first *types.GenerateResult) *provider.GenerateOptions {
		call := first.Content[0].(types.ToolCallContent)
		return &provider.GenerateOptions{Prompt: types.Prompt{Messages: followupMessages(call, map[string]interface{}{"status": "success"})}}
	})
	inputs, _ := body["inputs"].([]interface{})
	if len(inputs) < 2 {
		t.Fatalf("inputs = %v", inputs)
	}
	r87dprovrestMatchObject(t, "server.calls[1] inputs[1]", r87dprovrestArg(t, r87dprovrestConvFile, test, 0, "(await server.calls[1].requestBodyJson).inputs[1]"), inputs[1])
}

// TS 407-425: a shared model instance keeps no history between calls.
func TestR87dprovrestMistralNoSharedHistory(t *testing.T) {
	const test = "does not retain history on a shared model instance"
	tools := []types.Tool{searchTool("search")}
	body := r87dprovrestSecondCall(t, convOutputs(convExecution, convAnswer), &provider.GenerateOptions{Prompt: whoWon(), Tools: tools}, func(*types.GenerateResult) *provider.GenerateOptions {
		return &provider.GenerateOptions{Prompt: userPrompt("Independent search"), Tools: tools}
	})
	r87dprovrestEqual(t, "server.calls[1] inputs", r87dprovrestArg(t, r87dprovrestConvFile, test, 0, "(await server.calls[1].requestBodyJson).inputs"), body["inputs"])
}
