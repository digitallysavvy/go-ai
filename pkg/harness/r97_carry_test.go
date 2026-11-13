package harness

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

// The askUserQuestions JSON Schemas are what TS asSchema(...).jsonSchema gives
// for harnessV1QuestionsToolInputSchema / harnessV1QuestionsToolOutputSchema
// (node, @ai-sdk/harness at 2ebc78b654): z.record(z.string(), ...) carries
// propertyNames {type: string}.
const r97carryTSQuestionsInputSchema = `{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{"allowPartialAnswers":{"type":"boolean"},"questions":{"minItems":1,"type":"array","items":{"type":"object","properties":{"id":{"type":"string","minLength":1},"question":{"type":"string","minLength":1},"header":{"type":"string"},"options":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string","minLength":1},"label":{"type":"string","minLength":1},"description":{"type":"string"},"preview":{"type":"string"}},"required":["id","label"],"additionalProperties":false}},"allowMultiple":{"type":"boolean"},"allowFreeForm":{"anyOf":[{"type":"boolean"},{"type":"object","properties":{"secret":{"type":"boolean"}},"required":["secret"],"additionalProperties":false}]}},"required":["id","question"],"additionalProperties":false}}},"required":["allowPartialAnswers","questions"],"additionalProperties":false}`

const r97carryTSQuestionsOutputSchema = `{"$schema":"http://json-schema.org/draft-07/schema#","oneOf":[{"type":"object","properties":{"action":{"type":"string","const":"answered"},"answers":{"type":"object","propertyNames":{"type":"string"},"additionalProperties":{"type":"object","properties":{"optionIds":{"type":"array","items":{"type":"string"}},"freeform":{"type":"string"}},"required":["optionIds"],"additionalProperties":false}}},"required":["action","answers"],"additionalProperties":false},{"type":"object","properties":{"action":{"type":"string","const":"partially-answered"},"answers":{"type":"object","propertyNames":{"type":"string"},"additionalProperties":{"type":"object","properties":{"optionIds":{"type":"array","items":{"type":"string"}},"freeform":{"type":"string"}},"required":["optionIds"],"additionalProperties":false}}},"required":["action","answers"],"additionalProperties":false},{"type":"object","properties":{"action":{"type":"string","const":"declined"}},"required":["action"],"additionalProperties":false},{"type":"object","properties":{"action":{"type":"string","const":"cancelled"}},"required":["action"],"additionalProperties":false}]}`

func TestR97carryQuestionsToolJSONSchemasMatchTS(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  map[string]any
		want string
	}{
		{"input", QuestionsToolInputJSONSchema(), r97carryTSQuestionsInputSchema},
		{"output", QuestionsToolOutputJSONSchema(), r97carryTSQuestionsOutputSchema},
	} {
		b, err := json.Marshal(tc.got)
		if err != nil {
			t.Fatal(err)
		}
		var got, want any
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s schema = %s\nwant %s", tc.name, b, tc.want)
		}
	}
}

// harnessV1QuestionAnswerSchema optionIds is z.array(z.string()): node
// harnessV1QuestionsToolOutputSchema.safeParse({action:'answered',answers:
// {a:{optionIds:[null]}}}).success === false; encoding/json read the null as "".
func TestR97carryQuestionsOutputRejectsNullOptionID(t *testing.T) {
	if _, err := ParseQuestionsToolOutput([]byte(`{"action":"answered","answers":{"a":{"optionIds":["x",null]}}}`)); err == nil {
		t.Fatal("ParseQuestionsToolOutput accepted a null option id")
	}
	out, err := ParseQuestionsToolOutput([]byte(`{"action":"answered","answers":{"a":{"optionIds":["x",""]}}}`))
	if err != nil || len(out.Answers["a"].OptionIDs) != 2 {
		t.Fatalf("ParseQuestionsToolOutput = %+v, %v", out, err)
	}
}

// Bridge frames and stream parts are JSON.stringify'd in TS: NaN and +-Inf
// write null (ruling 4); json.Marshal failed the whole frame.
func TestR97carryMarshalTaggedNonFiniteAsNull(t *testing.T) {
	got, err := MarshalTagged("tool-result", struct {
		Output interface{} `json:"output"`
		Note   string      `json:"note"`
	}{Output: map[string]interface{}{"n": math.Inf(1), "m": math.NaN()}, Note: "<a&b>"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"type":"tool-result","output":{"m":null,"n":null},"note":"<a&b>"}`; string(got) != want {
		t.Fatalf("MarshalTagged = %s, want %s", got, want)
	}
}

// DecodeStreamPart is harnessV1StreamPartSchema over a JSON.parse'd frame:
// 1e400 is Infinity in z.unknown() values (tool result, raw value, error),
// and keys match exactly (a differently cased key is stripped, not read).
func TestR97carryDecodeStreamPartLikeJSONParse(t *testing.T) {
	part, err := DecodeStreamPart([]byte(`{"type":"tool-result","toolCallId":"c","toolName":"t","result":{"n":1e400}}`))
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := part.(*ToolResultPart).Result.(map[string]any)["n"].(float64); !math.IsInf(n, 1) {
		t.Fatalf("result = %v, want n = +Inf", part.(*ToolResultPart).Result)
	}
	part, err = DecodeStreamPart([]byte(`{"type":"error","error":{"code":-1e400}}`))
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := part.(*ErrorPart).Error.(map[string]any)["code"].(float64); !math.IsInf(n, -1) {
		t.Fatalf("error = %v, want code = -Inf", part.(*ErrorPart).Error)
	}
	part, err = DecodeStreamPart([]byte(`{"type":"text-delta","id":"a","delta":"x","DELTA":"y"}`))
	if err != nil || part.(*TextDeltaPart).Delta != "x" {
		t.Fatalf("text-delta = %+v, %v; want delta x (DELTA stripped)", part, err)
	}
}
