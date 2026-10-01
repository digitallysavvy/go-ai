package azure

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestBuildRequestBody_SystemOnlyPromptDoesNotPanic is a regression test for
// a nil type-assertion panic: buildRequestBody only set body["messages"]
// inside the IsMessages()/IsSimple() branches. A prompt with only System set
// (no Messages, no Text) hits neither branch, so body["messages"] stayed
// nil, and the System-prepend branch's
// `body["messages"].([]map[string]interface{})` (no comma-ok) panicked with
// "interface conversion: interface {} is nil, not []map[string]interface
// {}". The fix defaults body["messages"] to an empty slice up front and
// guards the assertion with comma-ok.
func TestBuildRequestBody_SystemOnlyPromptDoesNotPanic(t *testing.T) {
	m := NewLanguageModel(&Provider{}, "my-deployment")
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{System: "You are a helpful assistant."}, // no Messages, no Text
	}

	body := m.buildRequestBody(opts, false)

	messages, ok := body["messages"].([]map[string]interface{})
	if !ok {
		t.Fatalf("body[\"messages\"] has unexpected type %T", body["messages"])
	}
	if len(messages) != 1 {
		t.Fatalf("expected exactly the system message, got %d messages: %+v", len(messages), messages)
	}
	if messages[0]["role"] != "system" || messages[0]["content"] != "You are a helpful assistant." {
		t.Fatalf("unexpected system message: %+v", messages[0])
	}
}

// TestBuildRequestBody_EmptyPromptNoPanic covers the even-more-degenerate
// case: no System, no Messages, no Text. body["messages"] must still be a
// concrete empty slice, not nil.
func TestBuildRequestBody_EmptyPromptNoPanic(t *testing.T) {
	m := NewLanguageModel(&Provider{}, "my-deployment")
	opts := &provider.GenerateOptions{Prompt: types.Prompt{}}

	body := m.buildRequestBody(opts, false)

	messages, ok := body["messages"].([]map[string]interface{})
	if !ok {
		t.Fatalf("body[\"messages\"] has unexpected type %T", body["messages"])
	}
	if len(messages) != 0 {
		t.Fatalf("expected no messages, got %+v", messages)
	}
}
