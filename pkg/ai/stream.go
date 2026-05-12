package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	promptutils "github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// StreamTextOptions contains options for streaming text generation
type StreamTextOptions struct {
	// Model to use for generation
	Model provider.LanguageModel

	// Prompt can be a simple string or a list of messages
	Prompt   string
	Messages []types.Message
	System   string

	// AllowSystemMessages permits system-role messages in Messages.
	// Defaults to false; use System for system instructions unless you are
	// intentionally passing provider-native system messages.
	AllowSystemMessages bool

	// AllowSystemInMessages is a TypeScript-compatible alias for
	// AllowSystemMessages. Either field enables system-role messages.
	AllowSystemInMessages bool

	// Generation parameters
	Temperature      *float64
	MaxTokens        *int
	TopP             *float64
	TopK             *int
	FrequencyPenalty *float64
	PresencePenalty  *float64
	StopSequences    []string
	Seed             *int

	// Tools available for the model to call
	Tools      []types.Tool
	ToolChoice types.ToolChoice

	// ToolApproval configures approval handling for tool execution.
	ToolApproval types.ToolApprovalConfig

	// MaxSteps is a convenience shorthand for StopWhen{StepCountIs(N)}.
	// Deprecated: use StopWhen with StepCountIs instead.
	// If StopWhen is set, MaxSteps is ignored.
	MaxSteps *int

	// StopWhen defines conditions that terminate the tool-calling loop.
	// Conditions are evaluated OR -- first non-empty string stops the loop.
	StopWhen []StopCondition

	// Response format (for structured output)
	// Deprecated: Use Output instead.
	ResponseFormat *provider.ResponseFormat

	// Output specifies how to handle and parse model output during streaming.
	// Use TextOutput(), ObjectOutput(), ArrayOutput(), ChoiceOutput(), or JSONOutput().
	// When set, the model is called with the appropriate ResponseFormat and
	// PartialOutput() is updated after each text chunk via ParsePartialOutput.
	// If nil, defaults to plain text streaming.
	Output interface{}

	// Timeout provides granular timeout controls
	// Supports total timeout, per-step timeout, and per-chunk timeout
	Timeout *TimeoutConfig

	// ExperimentalRetention controls what data is retained from LLM requests/responses.
	// Useful for reducing memory consumption with images or large contexts.
	// Default (nil) retains everything for backwards compatibility.
	ExperimentalRetention *types.RetentionSettings

	// Reasoning controls how much thinking effort the model applies.
	// nil means unset (use provider default). Set to types.ReasoningDefault to
	// explicitly omit from the API request.
	Reasoning *types.ReasoningLevel

	// SendReasoning controls whether reasoning/thinking boundary chunks are
	// exposed to OnChunk. nil and false suppress reasoning-start/end, matching
	// the TypeScript SDK default.
	SendReasoning *bool

	// ProviderOptions allows passing provider-specific options
	ProviderOptions map[string]interface{}

	// RuntimeContext is user-defined context that flows through callbacks.
	// It is passed as-is to all structured event callbacks.
	RuntimeContext interface{}

	// ToolsContext is the per-tool context map passed to approval and execution hooks.
	ToolsContext map[string]interface{}

	// ExperimentalContext is a deprecated alias for RuntimeContext.
	ExperimentalContext interface{}

	// Telemetry configures observability for this operation.
	// When both Telemetry and ExperimentalTelemetry are set, Telemetry wins.
	Telemetry *TelemetrySettings

	// Telemetry configuration for observability.
	//
	// Deprecated: use Telemetry.
	ExperimentalTelemetry *TelemetrySettings

	// Callbacks
	OnChunk  func(chunk provider.StreamChunk)
	OnFinish func(result *StreamTextResult)

	// ========================================================================
	// Structured Event Callbacks (v6.1)
	// These callbacks receive typed event structs and are panic-safe.
	// ========================================================================

	// OnStart is called once, before streaming begins.
	OnStart func(ctx context.Context, e OnStartEvent)

	// OnStepStart is called at the beginning of each LLM step.
	OnStepStart func(ctx context.Context, e OnStepStartEvent)

	// OnToolExecutionStart is called just before each tool's Execute function runs.
	OnToolExecutionStart func(ctx context.Context, e OnToolCallStartEvent)

	// OnToolExecutionEnd is called after each tool's Execute function returns.
	OnToolExecutionEnd func(ctx context.Context, e OnToolCallFinishEvent)

	// Deprecated: use OnToolExecutionStart.
	OnToolCallStart func(ctx context.Context, e OnToolCallStartEvent)

	// Deprecated: use OnToolExecutionEnd.
	OnToolCallFinish func(ctx context.Context, e OnToolCallFinishEvent)

	// OnStepFinishEvent is called at the end of each LLM step.
	OnStepFinishEvent func(ctx context.Context, e OnStepFinishEvent)

	// OnFinishEvent is called once when the stream fully completes.
	OnFinishEvent func(ctx context.Context, e OnFinishEvent)
}

// StreamStatus represents the lifecycle state of a streaming generation.
type StreamStatus string

const (
	// StreamStatusSubmitted indicates the request has been submitted and the
	// stream is actively receiving data from the model.
	StreamStatusSubmitted StreamStatus = "submitted"

	// StreamStatusStreaming indicates at least one chunk has been received.
	StreamStatusStreaming StreamStatus = "streaming"

	// StreamStatusDone indicates the stream has completed successfully.
	StreamStatusDone StreamStatus = "done"
)

// StreamTextResult contains the result of streaming text generation
type StreamTextResult struct {
	// Stream of chunks
	stream provider.TextStream

	// status tracks the lifecycle of the stream.
	// Protected by mu because it is read by Status() and written by processStream.
	status StreamStatus

	// Accumulated text (built as chunks arrive)
	text string

	// Finish reason (set when stream completes)
	finishReason types.FinishReason

	// stopReason is the reason string from the StopCondition that stopped the loop.
	stopReason string

	// Usage information (set when stream completes)
	usage types.Usage

	// Context management information (Anthropic-specific)
	// Contains statistics about automatic conversation history cleanup
	contextManagement interface{}

	// Error that occurred during streaming
	err error

	// Output spec resolved from StreamTextOptions.Output.
	// nil when no Output option was provided.
	outputSpec outputProcessor

	// outputResult holds the final parsed output after streaming completes.
	// Only populated when finishReason == Stop and an Output spec was provided.
	// Protected by mu.
	outputResult any

	// outputErr holds any error that occurred during final output parsing.
	// Protected by mu.
	outputErr error

	// partialOutput holds the most recently parsed partial output.
	// Updated after each text chunk (with deduplication).
	// Protected by mu because processStream updates it from a goroutine.
	mu            sync.Mutex
	partialOutput any

	// lastPartialJSON is the JSON representation of the last published partialOutput.
	// Used for deduplication — only written from the stream-consuming goroutine.
	lastPartialJSON string

	// Timeout configuration for per-chunk timeouts
	timeout *TimeoutConfig

	// telemetryCtx is the context returned by FireOnStart, with any integration
	// spans embedded.  processStream and ReadAll call FireOnFinish / FireOnError
	// using this context so OTel spans are correctly closed.
	telemetryCtx      context.Context
	telemetrySettings *TelemetrySettings

	// Accumulated tool calls from ChunkTypeToolCall chunks.
	// Populated during streaming; executed after stream ends.
	// Protected by mu.
	toolCalls   []types.ToolCall
	toolResults []types.ToolResult

	// providerMetadata accumulates provider-specific metadata from stream chunks.
	// Protected by mu.
	providerMetadata json.RawMessage

	// warnings accumulated from stream-start chunks
	warnings []types.Warning

	// sources accumulated from ChunkTypeSource chunks
	sources []types.SourceContent

	// files accumulated from ChunkTypeFile chunks
	files []types.GeneratedFileContent

	// responseHeaders accumulated from ChunkTypeResponseMetadata chunks.
	responseHeaders map[string]string

	// rawFinishReason is the raw finish reason string from the provider.
	rawFinishReason string

	// stepRequest holds the last request metadata for the Request() accessor.
	stepRequest types.StepRequest

	// stepResponse holds the last response metadata for the Response() accessor.
	stepResponse types.StepResponse

	// Structured event callbacks (v6.1)
	// Stored here so processStream can fire them when the stream completes.
	cbCallID            string
	cbOnStepFinishEvent func(ctx context.Context, e OnStepFinishEvent)
	cbOnFinishEvent     func(ctx context.Context, e OnFinishEvent)
	cbOnToolCallStart   func(ctx context.Context, e OnToolCallStartEvent)
	cbOnToolCallFinish  func(ctx context.Context, e OnToolCallFinishEvent)
	cbFuncID            string
	cbMeta              map[string]any
	cbModelProvider     string
	cbModelID           string
	cbExperimentalCtx   interface{}
	cbRuntimeCtx        interface{}
	cbToolsCtx          map[string]interface{}
	// Snapshot of the initial messages and tools for event population
	cbMessages []types.Message
	cbTools    []types.Tool
	cbSystem   string

	// cbModel and cbStreamOpts are retained so that processStream can start
	// additional streaming steps when deferred provider tool results are pending.
	cbModel      provider.LanguageModel
	cbStreamOpts StreamTextOptions
}

// StreamText performs streaming text generation
func StreamText(ctx context.Context, opts StreamTextOptions) (*StreamTextResult, error) {
	// Validate options
	if opts.Model == nil {
		return nil, fmt.Errorf("model is required")
	}
	telemetrySettings := effectiveTelemetrySettings(opts.Telemetry, opts.ExperimentalTelemetry)
	runtimeContext := effectiveRuntimeContext(opts.RuntimeContext, opts.ExperimentalContext)
	toolsContext := opts.ToolsContext
	if toolsContext == nil {
		toolsContext = map[string]interface{}{}
	}

	// Fire OnStart — integrations start their root spans here and embed them
	// in the returned context.  FireOnFinish / FireOnError are called later
	// from processStream or ReadAll once the stream completes.
	telPrompt := ""
	telSystem := ""
	if telemetrySettings != nil && telemetrySettings.RecordInputs {
		telPrompt = opts.Prompt
		telSystem = opts.System
	}
	ctx = telemetry.FireOnStart(ctx, telemetry.TelemetryStartEvent{
		OperationType:  "ai.streamText",
		ModelProvider:  opts.Model.Provider(),
		ModelID:        opts.Model.ModelID(),
		Settings:       telemetrySettings,
		Prompt:         telPrompt,
		System:         telSystem,
		RuntimeContext: telemetryRuntimeContext(telemetrySettings, runtimeContext),
		ToolsContext:   telemetryToolsContext(telemetrySettings, toolsContext),
	})
	telemetryCtx := ctx // snapshot ctx with embedded spans before timeout wrapping

	// Apply total timeout if configured
	if opts.Timeout != nil && opts.Timeout.HasTotal() {
		var cancel context.CancelFunc
		ctx, cancel = opts.Timeout.CreateTimeoutContext(ctx, "total")
		defer cancel()
	}

	// Build prompt
	prompt := buildPrompt(opts.Prompt, opts.Messages, opts.System)
	allowSystem := allowSystemMessages(opts.AllowSystemMessages, opts.AllowSystemInMessages)
	normalizedPrompt, normErr := promptutils.NormalizePrompt(prompt, allowSystem)
	if normErr != nil {
		telemetry.FireOnError(telemetryCtx, telemetry.TelemetryErrorEvent{Settings: telemetrySettings, Error: normErr})
		return nil, normErr
	}
	prompt = normalizedPrompt
	opts.RuntimeContext = runtimeContext
	opts.ExperimentalContext = runtimeContext
	opts.ToolsContext = toolsContext

	// Extract telemetry info once for all callback events
	cbFuncID, cbMeta := telemetryCallbackInfo(telemetrySettings)
	callID := newCallID()

	// Emit OnStartEvent before streaming begins.
	Notify(ctx, OnStartEvent{
		CallID:              callID,
		OperationID:         "ai.streamText",
		ModelProvider:       opts.Model.Provider(),
		ModelID:             opts.Model.ModelID(),
		System:              opts.System,
		Prompt:              opts.Prompt,
		Messages:            opts.Messages,
		Tools:               opts.Tools,
		ToolChoice:          opts.ToolChoice,
		Output:              opts.Output,
		ProviderOptions:     opts.ProviderOptions,
		Temperature:         opts.Temperature,
		MaxTokens:           opts.MaxTokens,
		TopP:                opts.TopP,
		TopK:                opts.TopK,
		FrequencyPenalty:    opts.FrequencyPenalty,
		PresencePenalty:     opts.PresencePenalty,
		StopSequences:       opts.StopSequences,
		Seed:                opts.Seed,
		ExperimentalContext: runtimeContext,
		RuntimeContext:      runtimeContext,
		ToolsContext:        toolsContext,
	}, opts.OnStart)

	// Emit OnStepStartEvent for the first stream step.
	Notify(ctx, OnStepStartEvent{
		CallID:              callID,
		StepNumber:          1,
		ModelProvider:       opts.Model.Provider(),
		ModelID:             opts.Model.ModelID(),
		System:              opts.System,
		Messages:            prompt.Messages,
		Tools:               opts.Tools,
		PreviousSteps:       nil, // first (and only) step
		ExperimentalContext: runtimeContext,
		RuntimeContext:      runtimeContext,
		ToolsContext:        toolsContext,
	}, opts.OnStepStart)

	// Resolve ResponseFormat: prefer explicit field, then derive from Output spec.
	responseFormat := opts.ResponseFormat
	var outputSpec outputProcessor
	if op, ok := opts.Output.(outputProcessor); ok {
		outputSpec = op
		if responseFormat == nil {
			rf, rfErr := op.ResponseFormat(ctx)
			if rfErr != nil {
				telemetry.FireOnError(telemetryCtx, telemetry.TelemetryErrorEvent{Settings: telemetrySettings, Error: rfErr})
				return nil, fmt.Errorf("output.ResponseFormat failed: %w", rfErr)
			}
			responseFormat = rf
		}
	}

	// Build generate options
	genOpts := &provider.GenerateOptions{
		Prompt:                prompt,
		AllowSystemMessages:   allowSystem,
		AllowSystemInMessages: allowSystem,
		Temperature:           opts.Temperature,
		MaxTokens:             opts.MaxTokens,
		TopP:                  opts.TopP,
		TopK:                  opts.TopK,
		FrequencyPenalty:      opts.FrequencyPenalty,
		PresencePenalty:       opts.PresencePenalty,
		StopSequences:         opts.StopSequences,
		Seed:                  opts.Seed,
		Tools:                 opts.Tools,
		ToolChoice:            opts.ToolChoice,
		RuntimeContext:        runtimeContext,
		ToolsContext:          toolsContext,
		ResponseFormat:        responseFormat,
		Reasoning:             opts.Reasoning,
		SendReasoning:         opts.SendReasoning,
		ProviderOptions:       opts.ProviderOptions,
		Telemetry:             telemetrySettings,
	}

	telemetry.FireOnLanguageModelCallStart(ctx, telemetry.LanguageModelCallStartEvent{
		Settings:      telemetrySettings,
		CallID:        callID,
		ModelProvider: opts.Model.Provider(),
		ModelID:       opts.Model.ModelID(),
		Prompt:        genOpts.Prompt,
		Tools:         genOpts.Tools,
	})

	// Start streaming
	stream, err := opts.Model.DoStream(ctx, genOpts)
	if err != nil {
		if opts.Timeout != nil && opts.Timeout.HasTotal() && ctx.Err() != nil {
			err = wrapTimeoutError(TimeoutReasonTotal, err)
		}
		telemetry.FireOnError(telemetryCtx, telemetry.TelemetryErrorEvent{Settings: telemetrySettings, Error: err})
		return nil, fmt.Errorf("failed to start stream: %w", err)
	}

	// Create result
	result := &StreamTextResult{
		stream:            stream,
		status:            StreamStatusSubmitted, // actively streaming; set before any chunks arrive
		timeout:           opts.Timeout,
		telemetryCtx:      telemetryCtx,
		telemetrySettings: telemetrySettings,
		outputSpec:        outputSpec,
		// Structured event callbacks
		cbCallID:            callID,
		cbOnStepFinishEvent: opts.OnStepFinishEvent,
		cbOnFinishEvent:     opts.OnFinishEvent,
		cbOnToolCallStart:   opts.OnToolExecutionStart,
		cbOnToolCallFinish:  opts.OnToolExecutionEnd,
		cbFuncID:            cbFuncID,
		cbMeta:              cbMeta,
		cbModelProvider:     opts.Model.Provider(),
		cbModelID:           opts.Model.ModelID(),
		cbExperimentalCtx:   runtimeContext,
		cbRuntimeCtx:        runtimeContext,
		cbToolsCtx:          toolsContext,
		cbMessages:          prompt.Messages,
		cbTools:             opts.Tools,
		cbSystem:            opts.System,
		// Retained for deferred provider tool continuation.
		cbModel:      opts.Model,
		cbStreamOpts: opts,
	}

	// Start goroutine to process chunks and call callbacks
	if opts.OnChunk != nil || opts.OnFinish != nil ||
		opts.OnStepFinishEvent != nil || opts.OnFinishEvent != nil {
		go result.processStream(ctx, opts.OnChunk, opts.OnFinish)
	}

	return result, nil
}

// processStream processes the stream and calls callbacks.
// Implements the core streaming architecture changes:
//
//  1. Chunks are forwarded to the consumer (onChunk) before any tool Execute fires.
//  2. Tool calls are accumulated during streaming; Execute is called only after the
//     stream is fully consumed (after the loop, not mid-stream).
//  3. Telemetry is recorded through the telemetry.Span interface (no direct OTel imports).
//
// Supports multi-step continuation for provider tools with SupportsDeferredResults.
// When such tools are pending, processStream starts a new DoStream call and loops.
func (r *StreamTextResult) processStream(ctx context.Context, onChunk func(provider.StreamChunk), onFinish func(*StreamTextResult)) {
	opts := r.cbStreamOpts
	currentMessages := r.cbMessages
	stopConditions := resolveStopConditions(opts.StopWhen, opts.MaxSteps)

	// Build tool name → pointer map for deferred provider tool tracking.
	toolsByName := make(map[string]*types.Tool, len(opts.Tools))
	for i := range opts.Tools {
		toolsByName[opts.Tools[i].Name] = &opts.Tools[i]
	}
	// pendingDeferredToolCalls tracks provider tools (SupportsDeferredResults=true) whose
	// results haven't arrived yet. Key = toolCallID, value = toolName.
	pendingDeferredToolCalls := make(map[string]string)

	var allSteps []types.StepResult
	firstChunkEver := true
	suppressReasoningBoundaries := shouldSuppressReasoningBoundaries(opts.SendReasoning)
	var accumulatedTextParts []string

	for stepNum := 1; ; stepNum++ {
		// Fire step-start telemetry. OTel implementations create a child step span.
		stepCtx := telemetry.FireOnStepStart(ctx, telemetry.TelemetryStepStartEvent{
			OperationType:  "ai.streamText",
			Settings:       r.telemetrySettings,
			StepNumber:     stepNum,
			ModelProvider:  r.cbModelProvider,
			ModelID:        r.cbModelID,
			RuntimeContext: telemetryRuntimeContext(r.telemetrySettings, r.cbRuntimeCtx),
			ToolsContext:   telemetryToolsContext(r.telemetrySettings, r.cbToolsCtx),
		})

		// Track how many files existed before this step so we can slice per-step files.
		stepFilesStart := len(r.files)

		// pendingToolCalls accumulates tool call chunks received during this step's stream.
		// All Execute() calls happen after the stream loop ends.
		var stepTextParts []string
		var stepToolCalls []types.ToolCall
		var stepReasoningBuilder strings.Builder
		var modelCallEndFired bool
		// streamedToolResultIDs tracks tool call IDs for which the provider returned a
		// result inline in this step's stream (used for the deferred hasResult check).
		streamedToolResultIDs := make(map[string]bool)

		for {
			chunk, err := r.nextChunk(ctx)
			if err == io.EOF {
				break
			}
			if err != nil {
				r.err = err
				break
			}
			forwardChunk := !(suppressReasoningBoundaries && isReasoningBoundaryChunk(chunk.Type))

			// Transition from Submitted to Streaming on the first content chunk.
			// Metadata and stream lifecycle chunks are forwarded before this
			// marker, matching the TypeScript stream part order.
			if firstChunkEver && forwardChunk && isFirstChunkContent(chunk.Type) {
				firstChunkEver = false
				r.mu.Lock()
				r.status = StreamStatusStreaming
				r.mu.Unlock()
				first := provider.StreamChunk{Type: provider.ChunkTypeFirstChunk}
				if onChunk != nil {
					onChunk(first)
				}
				telemetry.FireOnChunk(ctx, telemetry.TelemetryChunkEvent{
					Settings:  r.telemetrySettings,
					ChunkType: string(provider.ChunkTypeFirstChunk),
				})
			}

			// Accumulate warnings from stream-start chunks
			if chunk.Type == provider.ChunkTypeStreamStart {
				r.warnings = append(r.warnings, chunk.Warnings...)
			}

			// Accumulate text
			if chunk.Type == provider.ChunkTypeText {
				stepTextParts = append(stepTextParts, chunk.Text)
				accumulatedTextParts = append(accumulatedTextParts, chunk.Text)

				// Update partial output after each text chunk (with deduplication).
				// Only publishes when the JSON representation of the partial changes,
				// matching the TypeScript SDK's deduplication behavior.
				if r.outputSpec != nil {
					currentText := strings.Join(accumulatedTextParts, "")
					partial := r.outputSpec.parsePartialOutput(ctx, ParsePartialOutputOptions{
						Text: currentText,
					})
					if partial != nil {
						if newJSON, err := json.Marshal(partial); err == nil {
							if newJSONStr := string(newJSON); newJSONStr != r.lastPartialJSON {
								r.lastPartialJSON = newJSONStr
								r.mu.Lock()
								r.partialOutput = partial
								r.mu.Unlock()
							}
						}
					}
				}
			}

			// Accumulate reasoning text from reasoning chunks.
			if chunk.Type == provider.ChunkTypeReasoning && chunk.Text != "" {
				stepReasoningBuilder.WriteString(chunk.Text)
			}

			// Accumulate tool call chunks without executing until the stream is consumed.
			// The chunk is still forwarded to the consumer below.
			if chunk.Type == provider.ChunkTypeToolCall && chunk.ToolCall != nil {
				stepToolCalls = append(stepToolCalls, *chunk.ToolCall)
			}

			// Track provider-inline tool results for the deferred hasResult check.
			if chunk.Type == provider.ChunkTypeToolResult && chunk.ToolResult != nil {
				streamedToolResultIDs[chunk.ToolResult.ToolCallID] = true
			}

			if chunk.Usage != nil {
				r.usage = *chunk.Usage
			}

			// Update finish reason and context management
			if chunk.Type == provider.ChunkTypeFinish {
				r.finishReason = chunk.FinishReason
				if chunk.ContextManagement != nil {
					r.contextManagement = chunk.ContextManagement
				}
				if !modelCallEndFired {
					modelCallEndFired = true
					telemetry.FireOnLanguageModelCallEnd(ctx, telemetry.LanguageModelCallEndEvent{
						Settings:      r.telemetrySettings,
						CallID:        r.cbCallID,
						ModelProvider: r.cbModelProvider,
						ModelID:       r.cbModelID,
						FinishReason:  string(chunk.FinishReason),
						Usage:         telemetryUsageFromUsage(r.usage),
						ResponseID:    responseIDFromMetadata(chunk.ResponseMetadata),
					})
				}
			}

			// Accumulate provider metadata from each chunk that carries it.
			if len(chunk.ProviderMetadata) > 0 {
				r.mu.Lock()
				r.providerMetadata = chunk.ProviderMetadata
				r.mu.Unlock()
			}

			// Accumulate sources from ChunkTypeSource chunks.
			if chunk.Type == provider.ChunkTypeSource && chunk.SourceContent != nil {
				r.sources = append(r.sources, *chunk.SourceContent)
			}

			// Accumulate generated files from ChunkTypeFile chunks.
			if chunk.Type == provider.ChunkTypeFile && chunk.GeneratedFileContent != nil {
				r.files = append(r.files, *chunk.GeneratedFileContent)
			}

			// Update response headers from ChunkTypeResponseMetadata.
			// Mirrors TS SDK's 'response-metadata' chunk handling in stream-text.ts.
			if chunk.Type == provider.ChunkTypeResponseMetadata && chunk.ResponseMetadata != nil {
				if chunk.ResponseMetadata.Headers != nil {
					r.responseHeaders = chunk.ResponseMetadata.Headers
				}
			}

			// Forward chunk to consumer before any tool Execute fires.
			if onChunk != nil && forwardChunk {
				onChunk(*chunk)
			}
			// Notify telemetry integrations of each chunk.
			if forwardChunk {
				telemetry.FireOnChunk(ctx, telemetry.TelemetryChunkEvent{
					Settings:  r.telemetrySettings,
					ChunkType: string(chunk.Type),
					Text:      chunk.Text,
				})
			}
		}
		if r.err != nil {
			break
		}
		stepText := strings.Join(stepTextParts, "")
		r.text = strings.Join(accumulatedTextParts, "")

		// Execute accumulated tool calls after stream is fully consumed.
		// All chunks (including tool call chunks) have already been forwarded above.
		var stepToolResults []types.ToolResult
		if len(stepToolCalls) > 0 && len(opts.Tools) > 0 {
			toolCallbacks := toolCallEventCallbacks{
				callID:              r.cbCallID,
				onStart:             r.cbOnToolCallStart,
				onFinish:            r.cbOnToolCallFinish,
				fallbackStart:       opts.OnToolCallStart,
				fallbackFinish:      opts.OnToolCallFinish,
				stepNum:             stepNum,
				modelProvider:       r.cbModelProvider,
				modelID:             r.cbModelID,
				messages:            currentMessages,
				experimentalContext: r.cbExperimentalCtx,
				runtimeContext:      r.cbRuntimeCtx,
				toolsContext:        r.cbToolsCtx,
				functionID:          r.cbFuncID,
				metadata:            r.cbMeta,
				timeout:             r.timeout,
				telemetrySettings:   r.telemetrySettings,
			}
			stepToolResults, _ = executeTools(ctx, stepToolCalls, opts.Tools, r.cbRuntimeCtx, r.cbToolsCtx, opts.ToolApproval, &r.usage, toolCallbacks)
		}
		hasUserApproval := false
		for _, tr := range stepToolResults {
			if tr.ApprovalStatus == types.ToolApprovalStatusUserApproval {
				hasUserApproval = true
				r.finishReason = types.FinishReasonUserApproval
				break
			}
		}

		// Forward tool-result chunks to OnChunk consumers after execution.
		for i := range stepToolResults {
			resultChunk := provider.StreamChunk{
				Type:       provider.ChunkTypeToolResult,
				ToolResult: &stepToolResults[i],
			}
			if onChunk != nil {
				onChunk(resultChunk)
			}
			telemetry.FireOnChunk(ctx, telemetry.TelemetryChunkEvent{
				Settings:  r.telemetrySettings,
				ChunkType: string(provider.ChunkTypeToolResult),
			})
		}

		// Deferred provider tool tracking, mirroring the TS SDK pendingDeferredToolCalls map.
		// Add tool calls whose results haven't arrived yet.
		// Note: check tool.ProviderExecuted on the definition because some providers
		// (e.g. Anthropic) do not set ProviderExecuted on the ToolCall itself.
		for _, call := range stepToolCalls {
			tool := toolsByName[call.ToolName]
			if tool == nil || !tool.ProviderExecuted || !tool.SupportsDeferredResults {
				continue
			}
			if !streamedToolResultIDs[call.ID] {
				pendingDeferredToolCalls[call.ID] = call.ToolName
			}
		}
		// Remove entries resolved by inline provider results (ChunkTypeToolResult chunks)
		// delivered in this step's stream. This is the primary resolution path for
		// deferred tools: the provider streams the result in a subsequent response.
		// Note: stepToolResults includes placeholder entries for provider-executed tools
		// (ProviderExecuted=true, Result=nil) — those must NOT clear the pending map.
		// Only real locally-executed results (ProviderExecuted=false) are safe to clear,
		// and those tools are never added to pendingDeferredToolCalls anyway, so this is
		// a no-op for them. Only streamedToolResultIDs represents actual inline results.
		for callID := range streamedToolResultIDs {
			delete(pendingDeferredToolCalls, callID)
		}

		// Accumulate step tool calls and results into the overall result.
		r.mu.Lock()
		r.toolCalls = append(r.toolCalls, stepToolCalls...)
		r.toolResults = append(r.toolResults, stepToolResults...)
		r.mu.Unlock()
		// Decode provider metadata for this step.
		r.mu.Lock()
		var stepProviderMeta map[string]interface{}
		if len(r.providerMetadata) > 0 {
			_ = json.Unmarshal(r.providerMetadata, &stepProviderMeta)
		}
		r.mu.Unlock()

		// Build reasoning content from deltas accumulated during this step.
		var stepReasoning []types.ReasoningContent
		if stepReasoningBuilder.Len() > 0 {
			stepReasoning = []types.ReasoningContent{{Text: stepReasoningBuilder.String()}}
		}

		stepRawFinishReason := ""

		// Build response headers snapshot.
		r.mu.Lock()
		stepHeaders := make(map[string]string, len(r.responseHeaders))
		for k, v := range r.responseHeaders {
			stepHeaders[k] = v
		}
		r.mu.Unlock()

		// Record this step. For multi-step streaming, r.text accumulates across steps;
		// use the current snapshot as the step's text.
		stepResult := types.StepResult{
			CallID:             r.cbCallID,
			StepNumber:         stepNum,
			Model:              types.StepModel{Provider: r.cbModelProvider, ModelID: r.cbModelID},
			Text:               stepText,
			Reasoning:          stepReasoning,
			ReasoningText:      buildReasoningText(stepReasoning),
			ToolCalls:          stepToolCalls,
			StaticToolCalls:    filterStaticToolCalls(stepToolCalls),
			DynamicToolCalls:   filterDynamicToolCalls(stepToolCalls),
			ToolResults:        stepToolResults,
			StaticToolResults:  filterStaticToolResults(stepToolResults),
			DynamicToolResults: filterDynamicToolResults(stepToolResults),
			FinishReason:       r.finishReason,
			RawFinishReason:    stepRawFinishReason,
			Usage:              r.usage,
			Sources:            r.sources,
			Response:           types.StepResponse{Headers: stepHeaders},
			ProviderMetadata:   stepProviderMeta,
			ToolsContext:       r.cbToolsCtx,
			RuntimeContext:     r.cbRuntimeCtx,
		}
		allSteps = append(allSteps, stepResult)

		// Fire step-finish telemetry — OTel implementation ends the child step span.
		{
			stepTelUsage := telemetry.TelemetryUsage{
				InputTokens:  r.usage.InputTokens,
				OutputTokens: r.usage.OutputTokens,
				TotalTokens:  r.usage.TotalTokens,
			}
			if r.usage.InputDetails != nil {
				stepTelUsage.NoCacheInputTokens = r.usage.InputDetails.NoCacheTokens
				stepTelUsage.CacheReadInputTokens = r.usage.InputDetails.CacheReadTokens
				stepTelUsage.CacheCreationInputTokens = r.usage.InputDetails.CacheWriteTokens
			}
			if r.usage.OutputDetails != nil {
				stepTelUsage.OutputTextTokens = r.usage.OutputDetails.TextTokens
				stepTelUsage.ReasoningTokens = r.usage.OutputDetails.ReasoningTokens
			}
			var stepFiles []types.GeneratedFileContent
			r.mu.Lock()
			if len(r.files) > stepFilesStart {
				stepFiles = append([]types.GeneratedFileContent(nil), r.files[stepFilesStart:]...)
			}
			r.mu.Unlock()
			telemetry.FireOnStepFinish(stepCtx, telemetry.TelemetryStepFinishEvent{
				StepNumber:     stepNum,
				FinishReason:   string(r.finishReason),
				Usage:          stepTelUsage,
				Text:           stepText,
				ToolCalls:      stepToolCalls,
				Files:          stepFiles,
				Settings:       r.telemetrySettings,
				RuntimeContext: telemetryRuntimeContext(r.telemetrySettings, r.cbRuntimeCtx),
				ToolsContext:   telemetryToolsContext(r.telemetrySettings, r.cbToolsCtx),
			})
		}

		// Populate ResponseMessages on the step that just completed.
		stepResponseMsgs := providerutils.ConvertToResponseMessages(
			stepToolCalls,
			[]types.ContentPart{types.TextContent{Text: stepText}},
			stepToolResults,
		)
		currentMessages = append(currentMessages, stepResponseMsgs...)
		allSteps[len(allSteps)-1].ResponseMessages = stepResponseMsgs
		allSteps[len(allSteps)-1].Response.Messages = stepResponseMsgs

		if hasUserApproval {
			break
		}

		if len(stopConditions) > 0 {
			state := StopConditionState{
				Steps:    allSteps,
				Messages: currentMessages,
				Usage:    r.usage,
			}
			if reason := EvaluateStopConditions(stopConditions, state); reason != "" {
				r.stopReason = reason
				break
			}
		}

		// Continue when this step included local tool calls that were executed
		// in-step, or when a deferred provider tool has not yet delivered its result.
		hasLocalToolCalls := false
		for _, call := range stepToolCalls {
			tool := toolsByName[call.ToolName]
			if tool != nil && !tool.ProviderExecuted {
				hasLocalToolCalls = true
				break
			}
		}
		if !hasLocalToolCalls && len(pendingDeferredToolCalls) == 0 {
			break
		}

		// Resolve ResponseFormat for the next step.
		responseFormat := opts.ResponseFormat
		if responseFormat == nil && r.outputSpec != nil {
			if rf, rfErr := r.outputSpec.ResponseFormat(ctx); rfErr == nil {
				responseFormat = rf
			}
		}

		// Start a new stream for the next step.
		nextGenOpts := &provider.GenerateOptions{
			Prompt: types.Prompt{
				Messages: currentMessages,
				System:   opts.System,
			},
			AllowSystemMessages:   allowSystemMessages(opts.AllowSystemMessages, opts.AllowSystemInMessages),
			AllowSystemInMessages: allowSystemMessages(opts.AllowSystemMessages, opts.AllowSystemInMessages),
			Temperature:           opts.Temperature,
			MaxTokens:             opts.MaxTokens,
			TopP:                  opts.TopP,
			TopK:                  opts.TopK,
			FrequencyPenalty:      opts.FrequencyPenalty,
			PresencePenalty:       opts.PresencePenalty,
			StopSequences:         opts.StopSequences,
			Seed:                  opts.Seed,
			Tools:                 opts.Tools,
			ToolChoice:            opts.ToolChoice,
			RuntimeContext:        r.cbRuntimeCtx,
			ToolsContext:          r.cbToolsCtx,
			ResponseFormat:        responseFormat,
			Reasoning:             opts.Reasoning,
			SendReasoning:         opts.SendReasoning,
			ProviderOptions:       opts.ProviderOptions,
			Telemetry:             r.telemetrySettings,
		}
		telemetry.FireOnLanguageModelCallStart(ctx, telemetry.LanguageModelCallStartEvent{
			Settings:      r.telemetrySettings,
			CallID:        r.cbCallID,
			ModelProvider: r.cbModelProvider,
			ModelID:       r.cbModelID,
			Prompt:        nextGenOpts.Prompt,
			Tools:         nextGenOpts.Tools,
		})
		newStream, err := r.cbModel.DoStream(ctx, nextGenOpts)
		if err != nil {
			if r.timeout != nil && r.timeout.HasTotal() && ctx.Err() != nil {
				err = wrapTimeoutError(TimeoutReasonTotal, err)
			}
			r.err = fmt.Errorf("failed to start stream for step %d: %w", stepNum+1, err)
			break
		}
		r.stream = newStream
	}

	if r.err == nil {
		finishChunk := provider.StreamChunk{Type: provider.ChunkTypeStreamFinish}
		if onChunk != nil {
			onChunk(finishChunk)
		}
		telemetry.FireOnChunk(ctx, telemetry.TelemetryChunkEvent{
			Settings:  r.telemetrySettings,
			ChunkType: string(provider.ChunkTypeStreamFinish),
		})
	}

	// Resolve final typed output if spec was provided and stream completed cleanly.
	// Only parse when finishReason is Stop; truncated responses (e.g. length limit)
	// would produce invalid JSON, matching the TypeScript SDK's behavior.
	if r.outputSpec != nil && r.finishReason == types.FinishReasonStop {
		parsed, parseErr := r.outputSpec.parseCompleteOutput(ctx, ParseCompleteOutputOptions{
			Text:         r.text,
			FinishReason: r.finishReason,
			Usage:        &r.usage,
		})
		r.mu.Lock()
		r.outputResult = parsed
		r.outputErr = parseErr
		r.mu.Unlock()
	}

	// Fire OnFinish — integrations record output attributes and end their spans.
	streamTelUsage := telemetry.TelemetryUsage{
		InputTokens:  r.usage.InputTokens,
		OutputTokens: r.usage.OutputTokens,
		TotalTokens:  r.usage.TotalTokens,
	}
	if r.usage.InputDetails != nil {
		streamTelUsage.NoCacheInputTokens = r.usage.InputDetails.NoCacheTokens
		streamTelUsage.CacheReadInputTokens = r.usage.InputDetails.CacheReadTokens
		streamTelUsage.CacheCreationInputTokens = r.usage.InputDetails.CacheWriteTokens
	}
	if r.usage.OutputDetails != nil {
		streamTelUsage.OutputTextTokens = r.usage.OutputDetails.TextTokens
		streamTelUsage.ReasoningTokens = r.usage.OutputDetails.ReasoningTokens
	}
	r.mu.Lock()
	streamFiles := r.files
	streamWarnings := r.warnings
	streamSources := r.sources
	r.mu.Unlock()
	telemetry.FireOnFinish(r.telemetryCtx, telemetry.TelemetryFinishEvent{
		FinishReason:   string(r.finishReason),
		Usage:          streamTelUsage,
		ModelProvider:  r.cbModelProvider,
		ModelID:        r.cbModelID,
		Text:           r.text,
		Files:          streamFiles,
		Settings:       r.telemetrySettings,
		RuntimeContext: telemetryRuntimeContext(r.telemetrySettings, r.cbRuntimeCtx),
		ToolsContext:   telemetryToolsContext(r.telemetrySettings, r.cbToolsCtx),
	})

	// Mark stream as done before firing callbacks so callers that check
	// Status() inside callbacks observe the terminal state.
	r.mu.Lock()
	r.status = StreamStatusDone
	r.mu.Unlock()

	// Call finish callback
	if onFinish != nil {
		onFinish(r)
	}

	// Emit structured step-finish and generation-finish events.
	// These fire after all chunks are processed and the legacy callbacks have run.
	// For multi-step streaming, allSteps contains one entry per step.
	r.mu.Lock()
	finalToolCalls := r.toolCalls
	finalToolResults := r.toolResults
	r.mu.Unlock()

	// Decode final provider metadata for the single-step path.
	r.mu.Lock()
	var finalProviderMeta map[string]interface{}
	if len(r.providerMetadata) > 0 {
		_ = json.Unmarshal(r.providerMetadata, &finalProviderMeta)
	}
	r.mu.Unlock()

	// Emit per-step finish events and use the last step for the single-step path.
	lastStep := types.StepResult{
		CallID:             r.cbCallID,
		StepNumber:         1,
		Model:              types.StepModel{Provider: r.cbModelProvider, ModelID: r.cbModelID},
		Text:               r.text,
		ToolCalls:          finalToolCalls,
		StaticToolCalls:    filterStaticToolCalls(finalToolCalls),
		DynamicToolCalls:   filterDynamicToolCalls(finalToolCalls),
		ToolResults:        finalToolResults,
		StaticToolResults:  filterStaticToolResults(finalToolResults),
		DynamicToolResults: filterDynamicToolResults(finalToolResults),
		FinishReason:       r.finishReason,
		RawFinishReason:    r.rawFinishReason,
		Usage:              r.usage,
		Sources:            r.sources,
		ProviderMetadata:   finalProviderMeta,
		Response:           types.StepResponse{Headers: r.responseHeaders},
		ToolsContext:       r.cbToolsCtx,
		RuntimeContext:     r.cbRuntimeCtx,
		ResponseMessages: providerutils.ConvertToResponseMessages(
			finalToolCalls,
			[]types.ContentPart{types.TextContent{Text: r.text}},
			nil,
		),
	}
	if len(allSteps) > 0 {
		lastStep = allSteps[len(allSteps)-1]
	}
	Notify(ctx, OnStepFinishEvent{
		CallID:             r.cbCallID,
		StepNumber:         lastStep.StepNumber,
		Model:              lastStep.Model,
		ModelProvider:      r.cbModelProvider,
		ModelID:            r.cbModelID,
		Text:               lastStep.Text,
		Reasoning:          lastStep.Reasoning,
		ReasoningText:      lastStep.ReasoningText,
		ToolCalls:          lastStep.ToolCalls,
		StaticToolCalls:    lastStep.StaticToolCalls,
		DynamicToolCalls:   lastStep.DynamicToolCalls,
		ToolResults:        lastStep.ToolResults,
		StaticToolResults:  lastStep.StaticToolResults,
		DynamicToolResults: lastStep.DynamicToolResults,
		FinishReason:       lastStep.FinishReason,
		RawFinishReason:    lastStep.RawFinishReason,
		Usage:              lastStep.Usage,
		Warnings:           streamWarnings,
		Sources:            lastStep.Sources,
		Files:              streamFiles,
		ProviderMetadata:   lastStep.ProviderMetadata,
		ResponseHeaders:    r.responseHeaders,
		Response: GenerateStepResponse{
			Headers:  r.responseHeaders,
			Messages: lastStep.ResponseMessages,
		},
		ExperimentalContext: r.cbExperimentalCtx,
		RuntimeContext:      r.cbRuntimeCtx,
		ToolsContext:        r.cbToolsCtx,
	}, r.cbOnStepFinishEvent)

	stepsForEvent := allSteps
	if len(stepsForEvent) == 0 {
		stepsForEvent = []types.StepResult{lastStep}
	}
	Notify(ctx, OnFinishEvent{
		CallID:             r.cbCallID,
		StepNumber:         lastStep.StepNumber,
		Model:              lastStep.Model,
		ModelProvider:      r.cbModelProvider,
		ModelID:            r.cbModelID,
		Text:               r.text,
		Reasoning:          lastStep.Reasoning,
		ReasoningText:      lastStep.ReasoningText,
		ToolCalls:          finalToolCalls,
		StaticToolCalls:    filterStaticToolCalls(finalToolCalls),
		DynamicToolCalls:   filterDynamicToolCalls(finalToolCalls),
		ToolResults:        finalToolResults,
		StaticToolResults:  filterStaticToolResults(finalToolResults),
		DynamicToolResults: filterDynamicToolResults(finalToolResults),
		FinishReason:       r.finishReason,
		RawFinishReason:    lastStep.RawFinishReason,
		Usage:              lastStep.Usage,
		Steps:              stepsForEvent,
		TotalUsage:         r.usage,
		Warnings:           streamWarnings,
		Sources:            streamSources,
		Files:              streamFiles,
		ProviderMetadata:   lastStep.ProviderMetadata,
		ResponseHeaders:    r.responseHeaders,
		Response: GenerateStepResponse{
			Headers:  r.responseHeaders,
			Messages: lastStep.ResponseMessages,
		},
		ExperimentalContext: r.cbExperimentalCtx,
		RuntimeContext:      r.cbRuntimeCtx,
		ToolsContext:        r.cbToolsCtx,
	}, r.cbOnFinishEvent)
}

// Stream returns the underlying text stream
func (r *StreamTextResult) Stream() provider.TextStream {
	return r.stream
}

// Text returns the accumulated text so far
func (r *StreamTextResult) Text() string {
	return r.text
}

// FinishReason returns the finish reason (only available after stream completes)
func (r *StreamTextResult) FinishReason() types.FinishReason {
	return r.finishReason
}

// StopReason returns the reason from the stop condition that ended the loop.
// It is empty when the stream ended naturally.
func (r *StreamTextResult) StopReason() string {
	return r.stopReason
}

// Usage returns the usage information (only available after stream completes)
func (r *StreamTextResult) Usage() types.Usage {
	return r.usage
}

// ContextManagement returns context management statistics (Anthropic-specific)
// Only available after stream completes
func (r *StreamTextResult) ContextManagement() interface{} {
	return r.contextManagement
}

// ToolCalls returns tool calls received during streaming.
// Only populated after stream completes (processStream or ReadAll).
// Safe to call concurrently with streaming.
func (r *StreamTextResult) ToolCalls() []types.ToolCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.toolCalls
}

// ToolResults returns results from tool executions that ran after stream end.
// Only populated when StreamText was called with callbacks and tool definitions.
// Safe to call concurrently with streaming.
func (r *StreamTextResult) ToolResults() []types.ToolResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.toolResults
}

// Sources returns citation or grounding references accumulated during streaming.
// Populated by providers such as Perplexity and Google Generative AI.
// Only populated after stream completes.
func (r *StreamTextResult) Sources() []types.SourceContent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sources
}

// Output returns the final parsed typed output after streaming completes.
// This calls ParseCompleteOutput on the full accumulated text, matching the
// TypeScript SDK's `.output` property behavior.
//
// Returns nil if:
//   - no Output option was provided to StreamText
//   - the stream has not yet completed
//   - finishReason was not Stop (e.g. length limit hit)
//   - output parsing failed (check OutputErr for details)
//
// Safe to call concurrently with streaming.
func (r *StreamTextResult) Output() any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.outputResult
}

// OutputErr returns any error that occurred during final output parsing.
// Only relevant after streaming completes when an Output option was provided.
// Safe to call concurrently with streaming.
func (r *StreamTextResult) OutputErr() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.outputErr
}

// PartialOutput returns the most recently parsed partial output.
// Only populated when an Output option was provided to StreamText.
// Safe to call concurrently with streaming.
func (r *StreamTextResult) PartialOutput() any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.partialOutput
}

// StaticToolCalls returns tool calls from non-dynamic (typed) tools.
// Only populated after stream completes.
func (r *StreamTextResult) StaticToolCalls() []types.ToolCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return filterStaticToolCalls(r.toolCalls)
}

// DynamicToolCalls returns tool calls from dynamically registered tools.
// Only populated after stream completes.
func (r *StreamTextResult) DynamicToolCalls() []types.ToolCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return filterDynamicToolCalls(r.toolCalls)
}

// StaticToolResults returns results from non-dynamic (typed) tools.
// Only populated after stream completes.
func (r *StreamTextResult) StaticToolResults() []types.ToolResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	return filterStaticToolResults(r.toolResults)
}

// DynamicToolResults returns results from dynamically registered tools.
// Only populated after stream completes.
func (r *StreamTextResult) DynamicToolResults() []types.ToolResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	return filterDynamicToolResults(r.toolResults)
}

// RawFinishReason returns the raw finish reason string from the provider.
// Only available after stream completes.
func (r *StreamTextResult) RawFinishReason() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rawFinishReason
}

// ResponseHeaders returns the raw HTTP response headers from the provider.
// Only available after stream completes.
func (r *StreamTextResult) ResponseHeadersMap() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.responseHeaders
}

// Request returns metadata about the last request sent to the provider.
func (r *StreamTextResult) Request() types.StepRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stepRequest
}

// Response returns metadata about the last response from the provider.
func (r *StreamTextResult) Response() types.StepResponse {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stepResponse
}

// Status returns the current lifecycle state of the stream.
// Safe to call concurrently with streaming.
func (r *StreamTextResult) Status() StreamStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

// Resume returns an error when there is no active stream to resume.
// A stream that has already reached StreamStatusDone cannot be continued,
// which prevents the status from incorrectly flashing back to "submitted" (#12102).
func (r *StreamTextResult) Resume(ctx context.Context) error {
	r.mu.Lock()
	s := r.status
	r.mu.Unlock()
	if s == StreamStatusDone {
		return fmt.Errorf("cannot resume stream: stream is already done")
	}
	return nil
}

// Err returns any error that occurred during streaming
func (r *StreamTextResult) Err() error {
	return r.err
}

// Close closes the stream
func (r *StreamTextResult) Close() error {
	return r.stream.Close()
}

// ReadAll reads all chunks from the stream and returns the complete text.
// Tool call chunks are collected and stored in the result, but Execute is not
// called — use StreamText with callbacks for tool execution.
func (r *StreamTextResult) ReadAll() (string, error) {
	ctx := context.Background()
	firstChunk := true
	var pendingToolCalls []types.ToolCall

	for {
		chunk, err := r.nextChunk(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		// Transition Submitted to Streaming on the first content chunk.
		if firstChunk && isFirstChunkContent(chunk.Type) {
			firstChunk = false
			r.mu.Lock()
			r.status = StreamStatusStreaming
			r.mu.Unlock()
			telemetry.FireOnChunk(ctx, telemetry.TelemetryChunkEvent{
				Settings:  r.telemetrySettings,
				ChunkType: string(provider.ChunkTypeFirstChunk),
			})
		}

		// Accumulate warnings from stream-start chunks
		if chunk.Type == provider.ChunkTypeStreamStart {
			r.warnings = append(r.warnings, chunk.Warnings...)
		}

		// Accumulate text
		if chunk.Type == provider.ChunkTypeText {
			r.text += chunk.Text

			// Update partial output after each text chunk (with deduplication).
			if r.outputSpec != nil {
				partial := r.outputSpec.parsePartialOutput(ctx, ParsePartialOutputOptions{
					Text: r.text,
				})
				if partial != nil {
					if newJSON, err := json.Marshal(partial); err == nil {
						if newJSONStr := string(newJSON); newJSONStr != r.lastPartialJSON {
							r.lastPartialJSON = newJSONStr
							r.mu.Lock()
							r.partialOutput = partial
							r.mu.Unlock()
						}
					}
				}
			}
		}

		// Collect tool call chunks.
		if chunk.Type == provider.ChunkTypeToolCall && chunk.ToolCall != nil {
			pendingToolCalls = append(pendingToolCalls, *chunk.ToolCall)
		}

		// Update finish reason, usage, and context management
		if chunk.Type == provider.ChunkTypeFinish {
			r.finishReason = chunk.FinishReason
			if chunk.ContextManagement != nil {
				r.contextManagement = chunk.ContextManagement
			}
		}
		if chunk.Usage != nil {
			r.usage = *chunk.Usage
		}

		// Accumulate provider metadata.
		if len(chunk.ProviderMetadata) > 0 {
			r.providerMetadata = chunk.ProviderMetadata
		}

		// Accumulate generated files.
		if chunk.Type == provider.ChunkTypeFile && chunk.GeneratedFileContent != nil {
			r.files = append(r.files, *chunk.GeneratedFileContent)
		}
	}

	// Store collected tool calls.
	telemetry.FireOnChunk(ctx, telemetry.TelemetryChunkEvent{
		Settings:  r.telemetrySettings,
		ChunkType: string(provider.ChunkTypeStreamFinish),
	})

	// Store collected tool calls.
	if len(pendingToolCalls) > 0 {
		r.mu.Lock()
		r.toolCalls = pendingToolCalls
		r.mu.Unlock()
	}

	// Resolve final typed output if spec was provided and stream completed cleanly.
	if r.outputSpec != nil && r.finishReason == types.FinishReasonStop {
		parsed, parseErr := r.outputSpec.parseCompleteOutput(ctx, ParseCompleteOutputOptions{
			Text:         r.text,
			FinishReason: r.finishReason,
			Usage:        &r.usage,
		})
		r.mu.Lock()
		r.outputResult = parsed
		r.outputErr = parseErr
		r.mu.Unlock()
	}

	// Fire OnFinish — integrations record output attributes and end their spans.
	readAllTelUsage := telemetry.TelemetryUsage{
		InputTokens:  r.usage.InputTokens,
		OutputTokens: r.usage.OutputTokens,
		TotalTokens:  r.usage.TotalTokens,
	}
	if r.usage.InputDetails != nil {
		readAllTelUsage.NoCacheInputTokens = r.usage.InputDetails.NoCacheTokens
		readAllTelUsage.CacheReadInputTokens = r.usage.InputDetails.CacheReadTokens
		readAllTelUsage.CacheCreationInputTokens = r.usage.InputDetails.CacheWriteTokens
	}
	if r.usage.OutputDetails != nil {
		readAllTelUsage.OutputTextTokens = r.usage.OutputDetails.TextTokens
		readAllTelUsage.ReasoningTokens = r.usage.OutputDetails.ReasoningTokens
	}
	r.mu.Lock()
	readAllFiles := r.files
	r.mu.Unlock()
	telemetry.FireOnFinish(r.telemetryCtx, telemetry.TelemetryFinishEvent{
		FinishReason:   string(r.finishReason),
		Usage:          readAllTelUsage,
		ModelProvider:  r.cbModelProvider,
		ModelID:        r.cbModelID,
		Text:           r.text,
		Files:          readAllFiles,
		Settings:       r.telemetrySettings,
		RuntimeContext: telemetryRuntimeContext(r.telemetrySettings, r.cbRuntimeCtx),
		ToolsContext:   telemetryToolsContext(r.telemetrySettings, r.cbToolsCtx),
	})

	// Mark stream as done.
	r.mu.Lock()
	r.status = StreamStatusDone
	r.mu.Unlock()

	return r.text, nil
}

func isFirstChunkContent(chunkType provider.ChunkType) bool {
	switch chunkType {
	case provider.ChunkTypeStreamStart,
		provider.ChunkTypeResponseMetadata,
		provider.ChunkTypeFirstChunk,
		provider.ChunkTypeStreamFinish:
		return false
	default:
		return true
	}
}

func isReasoningBoundaryChunk(chunkType provider.ChunkType) bool {
	return chunkType == provider.ChunkTypeReasoningStart || chunkType == provider.ChunkTypeReasoningEnd
}

func shouldSuppressReasoningBoundaries(sendReasoning *bool) bool {
	return sendReasoning == nil || !*sendReasoning
}

// nextChunk reads the next chunk with optional per-chunk timeout
func (r *StreamTextResult) nextChunk(ctx context.Context) (*provider.StreamChunk, error) {
	// If no per-chunk timeout, just call Next() directly
	if r.timeout == nil || !r.timeout.HasPerChunk() {
		return r.stream.Next()
	}

	// Use per-chunk timeout
	chunkCtx, cancel := r.timeout.CreateTimeoutContext(ctx, "chunk")
	defer cancel()

	// Channel to receive the chunk
	type chunkResult struct {
		chunk *provider.StreamChunk
		err   error
	}
	resultCh := make(chan chunkResult, 1)

	// Start goroutine to read chunk
	go func() {
		chunk, err := r.stream.Next()
		resultCh <- chunkResult{chunk: chunk, err: err}
	}()

	// Wait for chunk or timeout
	select {
	case result := <-resultCh:
		return result.chunk, result.err
	case <-chunkCtx.Done():
		return nil, wrapTimeoutError(TimeoutReasonChunk, chunkCtx.Err())
	}
}

// ProviderMetadata returns the most recently received provider-specific metadata
// from stream chunks. Only populated when the provider emits metadata in chunks.
// Safe to call concurrently with streaming.
func (r *StreamTextResult) ProviderMetadata() json.RawMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.providerMetadata
}

// ResponseHeaders returns the raw HTTP response headers received from the provider.
// Populated once a ChunkTypeResponseMetadata chunk has been processed.
func (r *StreamTextResult) ResponseHeaders() map[string]string {
	return r.responseHeaders
}

func responseIDFromMetadata(metadata *provider.ResponseMetadata) string {
	if metadata == nil {
		return ""
	}
	return metadata.ID
}

// Warnings returns any provider warnings surfaced via stream-start chunks.
func (r *StreamTextResult) Warnings() []types.Warning {
	return r.warnings
}

// Chunks returns a channel that streams chunks
// This provides an idiomatic Go way to consume the stream
func (r *StreamTextResult) Chunks() <-chan provider.StreamChunk {
	ch := make(chan provider.StreamChunk, 10)

	go func() {
		defer close(ch)
		ctx := context.Background()
		for {
			chunk, err := r.nextChunk(ctx)
			if err == io.EOF {
				break
			}
			if err != nil {
				r.err = err
				break
			}

			ch <- *chunk
		}
	}()

	return ch
}
