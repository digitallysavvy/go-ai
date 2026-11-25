package ai

import (
	"errors"
	"reflect"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// carry r90 low class: TS InvalidArgumentError carries the rejected
// argument as `value` (validate-object-generation-input.ts, data-url.ts,
// prepare-language-model-call-options.ts, prepare-retries.ts, output.ts,
// validate-decision.ts, batch.ts ...). These Go sites left Value unset.
func TestR90integInvalidArgumentValues(t *testing.T) {
	value := func(err error) interface{} {
		t.Helper()
		var iae *providererrors.InvalidArgumentError
		if !errors.As(err, &iae) {
			t.Fatalf("err = %T %v, want InvalidArgumentError", err, err)
		}
		return iae.Value
	}
	if got := value(validateObjectGenerationInput(ObjectModeNoSchema, false, nil, "name", "", nil)); got != "name" {
		t.Errorf("schemaName value = %#v", got)
	}
	if got := value(validateObjectGenerationInput(ObjectModeObject, true, "s", "", "", []string{"a"})); !reflect.DeepEqual(got, []string{"a"}) {
		t.Errorf("enumValues value = %#v", got)
	}
	_, err := GetTextFromDataURL("not-a-data-url")
	if got := value(err); got != "not-a-data-url" {
		t.Errorf("dataUrl value = %#v", got)
	}
	n := 0
	if got := value(validateMaxOutputTokens(&n)); got != 0 {
		t.Errorf("maxOutputTokens value = %#v", got)
	}
	m := -1
	if got := value(validateMaxRetries(&m)); got != -1 {
		t.Errorf("maxRetries value = %#v", got)
	}
	if got := value(validateBatchRequests(nil)); !reflect.DeepEqual(got, []BatchRequest(nil)) {
		t.Errorf("requests value = %#v", got)
	}
	if got := value(validateDecisionInput("state", nil, nil)); got == nil {
		t.Errorf("questions value = nil, want the questions map")
	}
}
