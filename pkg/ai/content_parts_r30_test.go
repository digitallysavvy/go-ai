package ai

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// A provider-executed tool-error part whose error is a structured object
// keeps that object on the ToolResult (TS tool-error.error is any value).
func TestProviderToolResultsFromContentKeepsStructuredError(t *testing.T) {
	errObj := map[string]interface{}{"type": "web_fetch_tool_result_error", "errorCode": "unavailable"}
	got := providerToolResultsFromContent([]types.ContentPart{
		types.ToolErrorContent{ToolCallID: "c1", ToolName: "web_fetch", Error: errObj},
	}, nil)
	if len(got) != 1 || got[0].Error == nil {
		t.Fatalf("results = %+v", got)
	}
	if !reflect.DeepEqual(got[0].Result, errObj) {
		t.Fatalf("Result = %#v, want the structured error %#v", got[0].Result, errObj)
	}
	back, ok := toolResultContentFromToolResult(got[0]).(types.ToolErrorContent)
	if !ok || !reflect.DeepEqual(back.Error, errObj) {
		t.Fatalf("replayed error = %#v", back)
	}
}
