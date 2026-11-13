package harness

import "testing"

// harnessV1QuestionsToolInputSchema's optional keys are .optional(): a
// present null fails, and the allowFreeForm object needs `secret` exactly.
func TestR96carryQuestionsInputOptionalNullAndSecretKey(t *testing.T) {
	ok := `{"allowPartialAnswers":false,"questions":[{"id":"q","question":"Q?","header":"h","options":[{"id":"o","label":"L","description":"d"}],"allowFreeForm":{"secret":true}}]}`
	if _, err := ParseQuestionsToolInput([]byte(ok)); err != nil {
		t.Fatalf("valid input: %v", err)
	}
	bad := []string{
		`{"allowPartialAnswers":false,"questions":[{"id":"q","question":"Q?","header":null}]}`,
		`{"allowPartialAnswers":false,"questions":[{"id":"q","question":"Q?","options":null}]}`,
		`{"allowPartialAnswers":false,"questions":[{"id":"q","question":"Q?","allowMultiple":null}]}`,
		`{"allowPartialAnswers":false,"questions":[{"id":"q","question":"Q?","allowFreeForm":null}]}`,
		`{"allowPartialAnswers":false,"questions":[{"id":"q","question":"Q?","options":[{"id":"o","label":"L","preview":null}]}]}`,
		`{"allowPartialAnswers":false,"questions":[{"id":"q","question":"Q?","allowFreeForm":{"Secret":true}}]}`,
	}
	for _, in := range bad {
		if _, err := ParseQuestionsToolInput([]byte(in)); err == nil {
			t.Errorf("accepted %s", in)
		}
	}
}

func TestR96carryQuestionsOutputExactKeysAndNull(t *testing.T) {
	if _, err := ParseQuestionsToolOutput([]byte(`{"action":"answered","answers":{"q":{"optionIds":["a"],"freeform":"x"}}}`)); err != nil {
		t.Fatalf("valid output: %v", err)
	}
	for _, in := range []string{
		`{"action":"answered","answers":{"q":{"OptionIds":["a"]}}}`,
		`{"action":"answered","answers":{"q":{"optionIds":["a"],"freeform":null}}}`,
	} {
		if _, err := ParseQuestionsToolOutput([]byte(in)); err == nil {
			t.Errorf("accepted %s", in)
		}
	}
}

func TestR96carryQuestionsOutputActionKeyExact(t *testing.T) {
	if _, err := ParseQuestionsToolOutput([]byte(`{"Action":"declined"}`)); err == nil {
		t.Error(`accepted {"Action":"declined"}`)
	}
}
