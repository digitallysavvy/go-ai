package providers

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/gateway"
	"github.com/digitallysavvy/go-ai/pkg/providers/gemini"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
)

// r94carryResponseTargets are response types decoded with case-exact keys
// (jsonexact.Filter, directly or through internalhttp DoJSON*, UnmarshalChunk
// or securejson.Unmarshal). Register each response type here when Filter is
// applied to its decode site. An unexported type is checked by a one-line
// test in its own package: jsonexact.UntaggedFields(&T{}) must be empty.
var r94carryResponseTargets = []interface{}{
	&anthropic.AppliedClearThinkingEdit{},
	&anthropic.AppliedClearToolUsesEdit{},
	&anthropic.AppliedCompactEdit{},
	&anthropic.ClearToolUsesEdit{},
	&anthropic.CompactEdit{},
	&anthropic.KeepRecentThinkingTurns{},
	&fireworks.AsyncPollResponse{},
	&fireworks.AsyncSubmitResponse{},
	&gateway.SSEVideoEvent{},
	&gemini.GenerateResponse{},
	&gemini.Response{},
	&openai.WebSearchCallItem{},
	&provider.RealtimeServerEvent{},
	&responses.ApplyPatchCall{},
	&responses.ApplyPatchCallOperationDiffDeltaEvent{},
	&responses.ApplyPatchCallOperationDiffDoneEvent{},
	&responses.AssistantMessageItem{},
	&responses.CodeInterpreterCallCodeDeltaEvent{},
	&responses.CodeInterpreterCallCodeDoneEvent{},
	&responses.CodeInterpreterCallItem{},
	&responses.CompactionEvent{},
	&responses.ComputerCall{},
	&responses.CustomToolCallInputDeltaEvent{},
	&responses.CustomToolCallItem{},
	&responses.FileSearchCallItem{},
	&responses.FunctionCallArgumentsDeltaEvent{},
	&responses.FunctionCallItem{},
	&responses.ImageGenerationCallItem{},
	&responses.ImageGenerationPartialImageEvent{},
	&responses.LocalShellCall{},
	&responses.McpApprovalRequestItem{},
	&responses.McpCallItem{},
	&responses.OutputItemAddedEvent{},
	&responses.OutputItemDoneEvent{},
	&responses.OutputTextAnnotationAddedEvent{},
	&responses.OutputTextDeltaEvent{},
	&responses.ProgramItem{},
	&responses.ProgramOutputItem{},
	&responses.ReasoningSummaryPartAddedEvent{},
	&responses.ReasoningSummaryPartDoneEvent{},
	&responses.ReasoningSummaryTextDeltaEvent{},
	&responses.ResponseCompletedEvent{},
	&responses.ResponseFailedEvent{},
	&responses.ResponsesStreamErrorEvent{},
	&responses.ResponsesStreamEvent{},
	&responses.ToolSearchCallItem{},
	&responses.ToolSearchOutputItem{},
	&tools.BashCodeExecutionInput{},
	&tools.BashExecutionError{},
	&tools.BashExecutionResult{},
	&tools.EncryptedProgrammaticExecutionResult{},
	&tools.ProgrammaticExecutionResult{},
	&tools.ProgrammaticToolCallInput{},
	&tools.TextEditorCreateResult{},
	&tools.TextEditorExecutionError{},
	&tools.TextEditorInput{},
	&tools.TextEditorStrReplaceResult{},
	&tools.TextEditorViewResult{},
	&tools.WebFetchResult20260209{},
	&tools.WebFetchResult20260318{},
	&types.FileData{},
}

// A response field with no json tag matches only a wire key spelled like the
// Go field name under case-exact decoding, so it silently stops decoding
// (gateway rerank, r94). Every reachable exported field must be tagged.
func TestR94carryResponseTypesAreTagged(t *testing.T) {
	if bad := jsonexact.UntaggedFields(r94carryResponseTargets...); len(bad) > 0 {
		t.Fatalf("response fields without a json tag (tag them before decoding with jsonexact.Filter): %v", bad)
	}
}
