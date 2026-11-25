package harness

import (
	"testing"
)

// harnessV1ToolResultPartSchema's result and harnessV1UsageSchema's raw are
// harnessV1JsonValueSchema values, whose z.number() rejects the Infinity
// JSON.parse reads for 1e400 (node, zod 4.6.7: safeParse fails). An error's
// value and a raw part's rawValue are z.unknown() and keep it.
func TestR98carryDecodeStreamPartRejectsNonFiniteJSONValues(t *testing.T) {
	for _, frame := range []string{
		`{"type":"tool-result","toolCallId":"c","toolName":"t","result":{"n":1e400}}`,
		`{"type":"tool-result","toolCallId":"c","toolName":"t","result":[-1e400]}`,
		`{"type":"finish-step","finishReason":{"unified":"stop"},"usage":{"inputTokens":{},"outputTokens":{},"raw":{"x":1e400}}}`,
		`{"type":"finish","finishReason":{"unified":"stop"},"totalUsage":{"inputTokens":{},"outputTokens":{},"raw":{"x":{"y":1e400}}}}`,
	} {
		if _, err := DecodeStreamPart([]byte(frame)); err == nil {
			t.Errorf("%s: want an error", frame)
		}
	}
	for _, frame := range []string{
		`{"type":"tool-result","toolCallId":"c","toolName":"t","result":{"n":1e300}}`,
		`{"type":"raw","rawValue":{"n":1e400}}`,
		`{"type":"error","error":{"n":1e400}}`,
	} {
		if _, err := DecodeStreamPart([]byte(frame)); err != nil {
			t.Errorf("%s: %v", frame, err)
		}
	}
}

// zod matches keys exactly at every level: a differently cased key is
// stripped (node: {"inputTokens":{"Total":5}} parses to inputTokens {}).
// The usage objects, the error part and allowFreeForm have their own
// UnmarshalJSON, which the exact-key filter does not enter.
func TestR98carryNestedDecodersMatchKeysExactly(t *testing.T) {
	part, err := DecodeStreamPart([]byte(`{"type":"finish-step","finishReason":{"unified":"stop"},"usage":{"inputTokens":{"Total":5},"outputTokens":{"TEXT":3}}}`))
	if err != nil {
		t.Fatal(err)
	}
	usage := part.(*FinishStepPart).Usage
	if usage.InputTokens.Total != nil || usage.OutputTokens.Text != nil {
		t.Fatalf("usage = %+v / %+v, want no total and no text", usage.InputTokens, usage.OutputTokens)
	}
	part, err = DecodeStreamPart([]byte(`{"type":"error","ERROR":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if e := part.(*ErrorPart).Error; e != nil {
		t.Fatalf("error = %v, want nil (ERROR stripped)", e)
	}
	in, err := ParseQuestionsToolInput([]byte(`{"allowPartialAnswers":false,"questions":[{"id":"q","question":"Q?","allowFreeForm":{"secret":true,"Secret":false}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if form := in.Questions[0].AllowFreeForm; form == nil || form.Secret == nil || !*form.Secret {
		t.Fatalf("allowFreeForm = %+v, want secret true", form)
	}
}
