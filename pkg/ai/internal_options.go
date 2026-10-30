package ai

import (
	"context"
	"errors"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/internal/aibridge"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// IDGenerator returns a stable identifier for tests and integrations that need
// deterministic response or call IDs.
type IDGenerator func() string

// InternalOptions mirrors the TypeScript SDK _internal option. It is intended
// for tests and framework integrations and may change without notice.
type InternalOptions struct {
	GenerateID     IDGenerator
	GenerateCallID IDGenerator

	// ContinueOnMissingProviderResult makes the step loop continue after a
	// provider-executed tool call that got no result in its response. The
	// loop warns and answers the call with an empty text result, as TS
	// WorkflowAgent does (resolveProviderToolResult), instead of ending the
	// run. Calls to deferred-result tools are left pending as usual. Used by
	// WorkflowAgent; plain GenerateText/StreamText stop the loop like TS
	// generateText/streamText.
	ContinueOnMissingProviderResult bool

	// StopOnStreamError makes a step whose stream carried an error part end
	// the run without executing or answering that step's tool calls, like TS
	// WorkflowAgent's hasTerminalError (the loop is done; the error is
	// reported once). Plain StreamText keeps executing tools after an error
	// part, as TS streamText does. Used by WorkflowAgent.
	StopOnStreamError bool

	// StepCallSettings, when set, is called right after a StreamText step's
	// PrepareStep and returns the headers and maxRetries for that step's model
	// call; ok=false keeps the call's own. TS WorkflowAgent's prepareStep can
	// override both (mergePrepareStepGenerationSettings), core prepareStep
	// cannot. Used by WorkflowAgent's stream path.
	StepCallSettings func() (headers map[string]string, maxRetries *int, ok bool)

	// EndOnError makes a StreamText run that fails (not aborts) still fire
	// OnEndEvent, after OnError, with the steps completed so far, as TS
	// WorkflowAgent's stream mode calls onEnd for every run that was not
	// aborted. Plain StreamText fires no end event for a failed run, like TS
	// streamText. Used by WorkflowAgent.
	EndOnError bool

	// FailedEndMessages, with EndOnError, are the Messages of the end event
	// of a run that failed by a thrown error (a model call that failed to
	// open, a failed PrepareStep, a tool failure): TS WorkflowAgent falls back
	// to the standardized prompt messages (`finalMessages ?? prompt.messages`)
	// when its loop throws. A run that ended on a stream's own error keeps the
	// final conversation, as TS's iterator returns it. Used by WorkflowAgent.
	FailedEndMessages []types.Message

	// streamModeUsage makes a StreamText run report TS WorkflowAgent's
	// stream-mode total usage (aggregateExecutionUsage(steps, 'stream'), that
	// is aggregateUsage): the steps' input and output tokens summed with
	// missing counts as 0, and TotalTokens their sum, set even when no step
	// reported usage. It applies to the end event's Usage/TotalUsage (completed
	// and failed runs), the telemetry end event and the result's Usage. The
	// token details and raw usage of the plain sum are kept. Plain StreamText
	// keeps TS streamText's addLanguageModelUsage totals.
	//
	// onStreamDone, when set, is called once when a StreamText run ends
	// (completed, failed or aborted), after its end callbacks and before
	// ReadAll, Err and the full stream return. TS WorkflowAgent reports its
	// root telemetry end (onError, onEnd with the total usage) when the run's
	// loop ends, not when the stream opens.
	//
	// Both are set by WorkflowAgent's stream path through
	// aibridge.SetWorkflowStreamHooks; they are not public API.
	streamModeUsage bool
	onStreamDone    func(aibridge.StreamDoneInfo)

	// HeadersAsGiven makes the legacy agent loop (pkg/agent GenerateAgent)
	// send each step's headers to the model as they are, without its
	// ai-sdk-agent/tool-loop and ai/<version> user-agent tags. TS
	// WorkflowAgent hands its step headers straight to doGenerate, so a
	// PrepareStep that replaces them leaves no SDK tag at all. Used by
	// WorkflowAgent.
	HeadersAsGiven bool

	// ApprovalID, when set, gives each tool call's approval its ID instead of
	// GenerateID: TS WorkflowAgent names the approval of call X
	// `approval-X`, where generateText and streamText call generateId().
	// Used by WorkflowAgent.
	ApprovalID func(toolCallID string) string

	// RequestMessagesAsPrompt makes the legacy agent loop (pkg/agent
	// GenerateAgent) record each step's request messages
	// (Include.RequestMessages) as the model call's prompt, its system
	// message first: TS WorkflowAgent keeps its language model prompt there,
	// where generateText (and ToolLoopAgent) keeps the step's messages
	// without the instructions. Used by WorkflowAgent.
	RequestMessagesAsPrompt bool

	// DeferApprovalRequests makes StreamText resolve a step's tool approvals
	// after the model stream, with its tools, and emit the approval parts
	// after the step's tool results, as TS WorkflowAgent's stream mode does
	// (writeToolResults, then writeApprovalRequests). Plain StreamText
	// resolves each approval at its tool-call part, as TS streamText. Used by
	// WorkflowAgent.
	DeferApprovalRequests bool

	// ApprovalsResumed tells the legacy agent loop (pkg/agent GenerateAgent)
	// that the caller already resumed the tool approvals of the input
	// messages, so the loop does not resume them again: TS WorkflowAgent
	// resumes approvals itself and forwards a provider-executed call's
	// approval response as is, without generateText's execution-denied
	// result. Used by WorkflowAgent.
	ApprovalsResumed bool
}

// setWorkflowStreamHooks is aibridge.SetWorkflowStreamHooks.
func setWorkflowStreamHooks(internal interface{}, onDone func(aibridge.StreamDoneInfo)) {
	if o, ok := internal.(*InternalOptions); ok && o != nil {
		o.streamModeUsage = true
		o.onStreamDone = onDone
	}
}

func deferApprovalRequests(internal *InternalOptions) bool {
	return internal != nil && internal.DeferApprovalRequests
}

// hasApprovalStatus reports whether a tool result carries an approval
// outcome (a request, or an automatic approval or denial).
func hasApprovalStatus(tr types.ToolResult) bool {
	switch tr.ApprovalStatus {
	case types.ToolApprovalStatusUserApproval, types.ToolApprovalStatusApproved, types.ToolApprovalStatusDenied:
		return true
	}
	return false
}

// internalApprovalID returns the approval ID function for a call: the
// InternalOptions.ApprovalID override, or gen (the call's ID generator).
func internalApprovalID(internal *InternalOptions, gen IDGenerator) func(toolCallID string) string {
	if internal != nil && internal.ApprovalID != nil {
		return internal.ApprovalID
	}
	if gen == nil {
		gen = newTextID
	}
	return func(string) string { return gen() }
}

// notifyFailedEnd fires the end event of a failed run when
// InternalOptions.EndOnError asks for it (TS WorkflowAgent notifyEnd: the last
// step's text and finish reason, 'other' without steps, and the steps' total
// usage).
//
// err is the failure. Unless it is a NoOutputGeneratedError (the model stream
// ended without output, which TS's model call returns as the step's terminal
// error instead of throwing), the event's Messages are FailedEndMessages.
func notifyFailedEnd(ctx context.Context, internal *InternalOptions, onEndEvent func(context.Context, OnFinishEvent), callID string, runtimeContext interface{}, toolsContext map[string]interface{}, initialResponseMessages []types.Message, steps []types.StepResult, err error) {
	if internal == nil || !internal.EndOnError || onEndEvent == nil {
		return
	}
	steps = append([]types.StepResult(nil), steps...)
	e := OnFinishEvent{
		CallID:              callID,
		FinishReason:        types.FinishReasonOther,
		Steps:               steps,
		ExperimentalContext: runtimeContext,
		RuntimeContext:      runtimeContext,
		ToolsContext:        toolsContext,
	}
	for _, step := range steps {
		e.TotalUsage = e.TotalUsage.Add(step.Usage)
	}
	if internal.streamModeUsage {
		e.TotalUsage = streamModeUsage(steps)
	}
	if n := len(steps); n > 0 {
		last := steps[n-1]
		e.StepNumber, e.Model, e.ModelProvider, e.ModelID = last.StepNumber, last.Model, last.Model.Provider, last.Model.ModelID
		e.Text, e.FinishReason, e.RawFinishReason = last.Text, last.FinishReason, last.RawFinishReason
	}
	e.SetAggregatesFromSteps(steps, responseMessagesWithInitial(initialResponseMessages, steps))
	var noOutput *NoOutputGeneratedError
	if internal.FailedEndMessages != nil && !errors.As(err, &noOutput) {
		e.Messages = append([]types.Message(nil), internal.FailedEndMessages...)
	}
	Notify(ctx, e, onEndEvent)
}

// streamModeUsage is TS WorkflowAgent's stream-mode total (aggregateUsage):
// inputTokens and outputTokens are the steps' sums with a missing count as 0,
// and totalTokens is input + output, whatever the steps' own totals say. The
// details and raw usage of the plain step sum are kept as Go extras.
func streamModeUsage(steps []types.StepResult) types.Usage {
	var total types.Usage
	var input, output int64
	for _, step := range steps {
		total = total.Add(step.Usage)
		input += step.Usage.GetInputTokens()
		output += step.Usage.GetOutputTokens()
	}
	sum := input + output
	total.InputTokens, total.OutputTokens, total.TotalTokens = &input, &output, &sum
	return total
}

// stepCallSettings returns the headers and maxRetries for a step's model
// call: the StepCallSettings override when it gives one, else the call's.
func (o *InternalOptions) stepCallSettings(headers map[string]string, maxRetries *int) (map[string]string, *int) {
	if o == nil || o.StepCallSettings == nil {
		return headers, maxRetries
	}
	if h, r, ok := o.StepCallSettings(); ok {
		return h, r
	}
	return headers, maxRetries
}

// providerResultFinishAllowed reports whether TS WorkflowAgent processes the
// tool calls of a step with this finish reason (tool-calls or stop); other
// finish reasons end the run without answering missing provider results.
func providerResultFinishAllowed(reason types.FinishReason) bool {
	return reason == types.FinishReasonToolCalls || reason == types.FinishReasonStop
}

// missingProviderResultCalls returns the valid provider-executed calls of a step
// that got no result and are not deferred (TS resolveProviderToolResult:
// a result-less call is only left alone for deferred-result provider tools).
func missingProviderResultCalls(calls []types.ToolCall, toolsByName map[string]*types.Tool, hasResult func(id string) bool) []types.ToolCall {
	var missing []types.ToolCall
	for _, call := range calls {
		if call.Invalid || hasResult(call.ID) {
			continue
		}
		tool := toolsByName[call.ToolName]
		if !call.ProviderExecuted && (tool == nil || !tool.ProviderExecuted) {
			continue
		}
		if tool != nil && tool.ProviderExecuted && tool.SupportsDeferredResults {
			continue
		}
		missing = append(missing, call)
	}
	return missing
}

// emptyProviderResult is the synthetic result TS WorkflowAgent sends for a
// provider-executed call the provider never answered: an empty text output.
func emptyProviderResult(call types.ToolCall, provider, model string) types.ToolResult {
	LogWarnings(LogWarningsOptions{
		Warnings: []types.Warning{{
			Type: "other",
			Message: fmt.Sprintf("[WorkflowAgent] Provider-executed tool \"%s\" (%s) did not receive a result from the stream. This may indicate a provider issue.",
				call.ToolName, call.ID),
		}},
		Provider: provider,
		Model:    model,
	})
	return types.ToolResult{
		ToolCallID:       call.ID,
		ToolName:         call.ToolName,
		Title:            call.Title,
		Input:            call.Arguments,
		Result:           "",
		ModelOutput:      &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: ""},
		ProviderExecuted: true,
		ProviderMetadata: call.ProviderMetadata,
		ToolMetadata:     call.ToolMetadata,
		Dynamic:          call.Dynamic,
	}
}

func internalGenerateID(internal *InternalOptions) IDGenerator {
	if internal != nil && internal.GenerateID != nil {
		return internal.GenerateID
	}
	return newTextID
}

func internalGenerateCallID(internal *InternalOptions) IDGenerator {
	if internal != nil && internal.GenerateCallID != nil {
		return internal.GenerateCallID
	}
	return newCallID
}

// emptyProviderResults answers the provider-executed calls of a step that got
// no result in content with the empty text result TS WorkflowAgent sends
// (warning for each). It is the building block of
// InternalOptions.ContinueOnMissingProviderResult for the legacy agent loop
// (through pkg/internal/aibridge).
func emptyProviderResults(calls []types.ToolCall, tools []types.Tool, content []types.ContentPart, finishReason types.FinishReason, provider, model string) []types.ToolResult {
	if !providerResultFinishAllowed(finishReason) {
		return nil
	}
	byName := make(map[string]*types.Tool, len(tools))
	for i := range tools {
		byName[tools[i].Name] = &tools[i]
	}
	answered := func(id string) bool { return contentAnswersProviderCall(content, id) }
	var results []types.ToolResult
	for _, call := range missingProviderResultCalls(calls, byName, answered) {
		results = append(results, emptyProviderResult(call, provider, model))
	}
	return results
}

// contentAnswersProviderCall reports whether step content already settles a
// provider-executed call: it has a result or error, or the provider asked for
// approval of it. TS WorkflowAgent pauses for a provider tool-approval-request
// (providerApprovalRequests) and never answers that call with an empty result.
func contentAnswersProviderCall(content []types.ContentPart, id string) bool {
	for _, part := range content {
		switch v := part.(type) {
		case types.ToolResultContent:
			if v.ToolCallID == id {
				return true
			}
		case types.ToolErrorContent:
			if v.ToolCallID == id {
				return true
			}
		case types.ToolApprovalRequestContent:
			if v.ToolCallID == id || v.ToolCall.ID == id {
				return true
			}
		case *types.ToolApprovalRequestContent:
			if v != nil && (v.ToolCallID == id || v.ToolCall.ID == id) {
				return true
			}
		}
	}
	return false
}

// workflowBlockedToolStep reports whether TS WorkflowAgent leaves a step's
// tool calls untouched: modelCallIterator processes tools only when the finish
// reason is stop or tool-calls (or length with provider-executed results). On
// any other step the run just ends with the assistant message, which carries
// neither the invalid calls nor any tool-error message.
func workflowBlockedToolStep(internal *InternalOptions, reason types.FinishReason, content []types.ContentPart) bool {
	if internal == nil || !internal.ContinueOnMissingProviderResult || providerResultFinishAllowed(reason) {
		return false
	}
	if reason == types.FinishReasonLength {
		for _, part := range content {
			switch p := part.(type) {
			case types.ToolResultContent:
				if p.ProviderExecuted {
					return false
				}
			case *types.ToolResultContent:
				if p != nil && p.ProviderExecuted {
					return false
				}
			}
		}
	}
	return true
}

// workflowBlockedResponse returns the tool calls and content of a blocked
// workflow step as TS puts them in the assistant message: invalid local calls
// and their tool-error parts are dropped.
func workflowBlockedResponse(calls []types.ToolCall, content []types.ContentPart) ([]types.ToolCall, []types.ContentPart) {
	outCalls := make([]types.ToolCall, 0, len(calls))
	for _, c := range calls {
		if c.Invalid && !c.ProviderExecuted {
			continue
		}
		outCalls = append(outCalls, c)
	}
	outContent := make([]types.ContentPart, 0, len(content))
	for _, part := range content {
		switch p := part.(type) {
		case types.ToolErrorContent:
			if !p.ProviderExecuted {
				continue
			}
		case *types.ToolErrorContent:
			if p != nil && !p.ProviderExecuted {
				continue
			}
		}
		outContent = append(outContent, part)
	}
	return outCalls, outContent
}
