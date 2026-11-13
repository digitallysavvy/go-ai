package harness

import "testing"

// harnessV1QuestionsToolInputSchema.parse keeps only the schema's exact keys:
// an `AllowPartialAnswers` key does not set allowPartialAnswers (review round
// 95 repo sweep).
func TestR95integQuestionsToolInputKeysMatchExactly(t *testing.T) {
	in, err := ParseQuestionsToolInput([]byte(`{"allowPartialAnswers":false,"AllowPartialAnswers":true,"questions":[{"id":"q","question":"Which?","allowMultiple":false}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if in.AllowPartialAnswers {
		t.Fatal("AllowPartialAnswers key overrode allowPartialAnswers")
	}
}
