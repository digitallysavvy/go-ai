package cohere

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
)

var r87dprovrestPorted = map[string]string{
	tsfixture.PortedKey("should not include mediaType in the outgoing payload (category D)", "JSON.stringify(requestBody)", "toContain"): "TestR87dprovrestCohereRequestOmitsMediaType",
	tsfixture.PortedKey("should not read mediaType (document payload carries only text + title)", "payload", "toContain"):                "TestR87dprovrestCohereConvertedPromptOmitsMediaType",
	tsfixture.PortedKey("should handle auto tool choice", "result.toolChoice", "toBe"):                                                   "TestR87dprovrestCoherePrepareToolsAutoChoice",
}

// Hand ports of the cohere TS assertions the replay harness does not
// evaluate (R87D_TSFIXTURE). Each test runs the committed case's input and
// checks the TS expression against the TS expected value.

func r87dprovrestCohereModel(t *testing.T) func(base, id string) provider.LanguageModel {
	return func(base, id string) provider.LanguageModel {
		if id == "" {
			id = "command-r-plus"
		}
		m, err := New(Config{APIKey: "test-api-key", BaseURL: base}).LanguageModel(id)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
}

// cohere-chat-language-model.test.ts 439-440:
// expect(JSON.stringify(requestBody)).not.toContain('application/pdf') / ('mediaType')
func TestR87dprovrestCohereRequestOmitsMediaType(t *testing.T) {
	run := r87dprovrestReplay(t, "testdata/tscases/chat_asserts.json.gz", "should not include mediaType in the outgoing payload (category D)", 0,
		func(c *tsfixture.Case) { c.Call, _ = r79provaBuffers(c.Call).(map[string]interface{}) }, r87dprovrestCohereModel(t))
	body := run.req(t, 0).body
	r87dprovrestEqual(t, "requestBody.documents", []interface{}{map[string]interface{}{"data": map[string]interface{}{"text": "Some file content", "title": "document.pdf"}}}, body.(map[string]interface{})["documents"])
	b, _ := json.Marshal(body)
	for _, s := range []string{"application/pdf", "mediaType"} {
		if strings.Contains(string(b), s) {
			t.Errorf("JSON.stringify(requestBody) contains %q: %s", s, b)
		}
	}
}

// convert-to-cohere-chat-prompt.test.ts 155-157:
// const payload = JSON.stringify(result); expect(payload).not.toContain(...)
func TestR87dprovrestCohereConvertedPromptOmitsMediaType(t *testing.T) {
	args := r87dprovrestFnArgs(t, "testdata/tscases/convert_messages.json.gz", "should not read mediaType (document payload carries only text + title)")
	msgs, why := tsfixture.BuildPromptMessages(r79provaBuffers(args[0]))
	if why != "" {
		t.Fatal(why)
	}
	if err := prompt.RejectProviderReferenceFiles(msgs); err != nil {
		t.Fatal(err)
	}
	m := &LanguageModel{modelID: "command-r-plus"}
	body, warnings, err := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{Prompt: types.Prompt{Messages: msgs}})
	if err != nil {
		t.Fatal(err)
	}
	result := map[string]interface{}{"messages": body["messages"], "documents": body["documents"], "warnings": tsfixture.WarningsToTS(warnings)}
	r87dprovrestEqual(t, "result.documents", []interface{}{map[string]interface{}{"data": map[string]interface{}{"text": "PDF-like content", "title": "test.pdf"}}}, result["documents"])
	payload, _ := json.Marshal(result)
	for _, s := range []string{"application/pdf", "mediaType"} {
		if strings.Contains(string(payload), s) {
			t.Errorf("payload contains %q: %s", s, payload)
		}
	}
}

// cohere-prepare-tools.test.ts 84: expect(result.toolChoice).toBe(undefined)
func TestR87dprovrestCoherePrepareToolsAutoChoice(t *testing.T) {
	args := r87dprovrestFnArgs(t, "testdata/tscases/prepare_tools.json.gz", "should handle auto tool choice")
	in, _ := args[0].(map[string]interface{})
	ts, why := tsfixture.BuildTools(in["tools"])
	if why != "" {
		t.Fatal(why)
	}
	tc, _ := in["toolChoice"].(map[string]interface{})
	if tc["type"] != "auto" {
		t.Fatalf("case input toolChoice = %v", tc)
	}
	tools, toolChoice, _ := prepareCohereTools(ts, tsfixture.BuildToolChoice(tc), true)
	if len(tools) != 1 {
		t.Errorf("tools = %v", tools)
	}
	if toolChoice != nil {
		t.Errorf("result.toolChoice = %#v, TS undefined", toolChoice)
	}
}
