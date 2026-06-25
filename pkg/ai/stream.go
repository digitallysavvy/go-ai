package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

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
	// Instructions is a TypeScript-compatible alias for System.
	// When set, Instructions takes precedence over System.
	Instructions *string

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
	Headers          map[string]string

	// Tools available for the model to call
	Tools       []types.Tool
	ToolChoice  types.ToolChoice
	ToolOrder   []string
	ActiveTools []string

	// ToolApproval configures approval handling for tool execution.
	ToolApproval types.ToolApprovalConfig

	// ExperimentalToolApprovalSecret signs server-issued approval requests so
	// resumed approval responses can be verified before execution.
	ExperimentalToolApprovalSecret []byte

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

	// Include controls which large request/response details are retained in step
	// results and whether raw provider chunks are requested. Defaults match the
	// TypeScript SDK.
	Include *IncludeOptions

	// ExperimentalInclude is a deprecated alias for Include.
	ExperimentalInclude *IncludeOptions

	// IncludeRawChunks is a deprecated alias for Include.RawChunks.
	IncludeRawChunks bool

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

	// MaxRetries controls transient provider call retries. For Gateway models,
	// nil uses the TypeScript SDK default of 2 retries; set to 0 to disable.
	MaxRetries *int

	// ExperimentalSandbox is passed through to tool execution. PrepareStep can
	// override it for an individual step.
	ExperimentalSandbox interface{}

	// ExperimentalRefineToolInput refines parsed tool inputs by tool name before
	// approval, callbacks, telemetry, execution, and response messages.
	ExperimentalRefineToolInput map[string]ToolInputRefiner

	// ExperimentalDownload downloads remote file URLs that need inline data
	// before sending tool-result messages back to the model. Defaults to
	// DefaultDownload, matching the TypeScript SDK's default download behavior.
	ExperimentalDownload DownloadFunction

	// RuntimeContext is user-defined context that flows through callbacks.
	// It is passed as-is to all structured event callbacks.
	RuntimeContext interface{}

	// SensitiveRuntimeContext omits runtime context from telemetry payloads.
	SensitiveRuntimeContext bool

	// ToolsContext is the per-tool context map passed to approval and execution hooks.
	ToolsContext map[string]interface{}

	// ExperimentalContext is a deprecated alias for RuntimeContext.
	ExperimentalContext interface{}

	// PrepareStep is called before each model step.
	PrepareStep func(ctx context.Context, step PrepareStepOptions) PrepareStepOptions

	// Telemetry configures observability for this operation.
	// When both Telemetry and ExperimentalTelemetry are set, Telemetry wins.
	Telemetry *TelemetrySettings

	// Telemetry configuration for observability.
	//
	// Deprecated: use Telemetry.
	ExperimentalTelemetry *TelemetrySettings

	// Internal contains test-only ID generators matching the TypeScript
	// _internal option.
	Internal *InternalOptions

	// Callbacks
	OnChunk func(chunk provider.StreamChunk)
	// OnEnd is called when the stream is fully consumed.
	OnEnd func(result *StreamTextResult)
	// OnFinish is called when the stream is fully consumed.
	//
	// Deprecated: use OnEnd.
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

	// OnStepEndEvent is called at the end of each LLM step.
	OnStepEndEvent func(ctx context.Context, e OnStepFinishEvent)

	// OnStepFinishEvent is called at the end of each LLM step.
	//
	// Deprecated: use OnStepEndEvent.
	OnStepFinishEvent func(ctx context.Context, e OnStepFinishEvent)

	// OnEndEvent is called once when the stream fully completes.
	OnEndEvent func(ctx context.Context, e OnFinishEvent)

	// OnFinishEvent is called once when the stream fully completes.
	//
	// Deprecated: use OnEndEvent.
	OnFinishEvent func(ctx context.Context, e OnFinishEvent)

	// OnError is called when an error chunk is received from the provider during
	// streaming. Mirrors the TypeScript SDK's onError callback. Unlike a fatal
	// stream error (which surfaces via TextStream.Err()), error chunks are
	// non-fatal stream events that can be observed and logged without aborting
	// the stream. If nil, error chunks are silently forwarded to OnChunk.
	OnError func(ctx context.Context, err error)

	// OnAbort is called when streaming is aborted by context cancellation or
	// deadline before normal completion.
	OnAbort func(ctx context.Context, steps []types.StepResult)

	// ExperimentalTransform is an ordered list of transform functions applied to
	// each stream chunk after provider emission but before forwarding to OnChunk.
	// Each function receives a chunk and returns zero or more replacement chunks.
	// Returning nil or an empty slice drops the chunk. Transforms are applied in
	// order; each transform receives the output of the previous one.
	// Mirrors the TypeScript SDK's experimental_transform option.
	ExperimentalTransform []StreamTransformFunc
}

// StreamTransformFunc is a transform applied to stream chunks in StreamText.
// It receives a single chunk and returns zero or more replacement chunks.
// Return nil or an empty slice to suppress the chunk.
type StreamTransformFunc func(ctx context.Context, chunk provider.StreamChunk) []provider.StreamChunk

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

// NoOutputGeneratedError is returned when a model stream ends before producing
// any model output and without a finish chunk.
type NoOutputGeneratedError struct {
	Message string
	Cause   error
}

func (e *NoOutputGeneratedError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *NoOutputGeneratedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// IsNoOutputGeneratedError reports whether err is a NoOutputGeneratedError.
func IsNoOutputGeneratedError(err error) bool {
	var target *NoOutputGeneratedError
	return errors.As(err, &target)
}

func newIncompleteModelStreamError() error {
	return &NoOutputGeneratedError{Message: "No output generated. The model stream ended without a finish chunk."}
}

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

	// initialStepCtx/Cancel keep the first step timeout alive across StreamText
	// returning and the lazy stream consumer starting.
	initialStepCtx    context.Context
	initialStepCancel context.CancelFunc

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
	cbCallID              string
	cbOnEnd               func(result *StreamTextResult)
	cbOnStepFinishEvent   func(ctx context.Context, e OnStepFinishEvent)
	cbOnEndEvent          func(ctx context.Context, e OnFinishEvent)
	cbOnToolCallStart     func(ctx context.Context, e OnToolCallStartEvent)
	cbOnToolCallFinish    func(ctx context.Context, e OnToolCallFinishEvent)
	cbFuncID              string
	cbMeta                map[string]any
	cbModelProvider       string
	cbModelID             string
	cbExperimentalCtx     interface{}
	cbRuntimeCtx          interface{}
	cbSensitiveRuntimeCtx bool
	cbToolsCtx            map[string]interface{}
	cbInclude             IncludeOptions
	cbSteps               []types.StepResult
	cbResponseMessages    []types.Message
	cbExperimentalSandbox interface{}
	// Snapshot of the initial messages and tools for event population
	cbMessages []types.Message
	cbTools    []types.Tool
	cbSystem   string

	// cbModel and cbStreamOpts are retained so that processStream can start
	// additional streaming steps when deferred provider tool results are pending.
	cbModel      provider.LanguageModel
	cbStreamOpts StreamTextOptions

	processingDone chan struct{}
}

// StreamText performs streaming text generation
func StreamText(ctx context.Context, opts StreamTextOptions) (*StreamTextResult, error) {
	// Validate options
	if opts.Model == nil {
		return nil, fmt.Errorf("model is required")
	}
	if err := validateMaxRetries(opts.MaxRetries); err != nil {
		return nil, err
	}
	telemetrySettings := effectiveTelemetrySettings(opts.Telemetry, opts.ExperimentalTelemetry)
	runtimeContext := effectiveRuntimeContext(opts.RuntimeContext, opts.ExperimentalContext)
	system := effectiveSystem(opts.System, opts.Instructions)
	include := effectiveInclude(opts.Include, opts.ExperimentalInclude, opts.IncludeRawChunks)
	if opts.Include == nil && opts.ExperimentalInclude == nil && opts.ExperimentalRetention != nil {
		include.RequestBody = opts.ExperimentalRetention.ShouldRetainRequestBody()
		include.ResponseBody = opts.ExperimentalRetention.ShouldRetainResponseBody()
	}
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
		telSystem = system
	}
	ctx = telemetry.FireOnStart(ctx, telemetry.TelemetryStartEvent{
		OperationType:  "ai.streamText",
		ModelProvider:  opts.Model.Provider(),
		ModelID:        opts.Model.ModelID(),
		Settings:       telemetrySettings,
		Prompt:         telPrompt,
		System:         telSystem,
		RuntimeContext: telemetryRuntimeContextWithSensitivity(telemetrySettings, runtimeContext, opts.SensitiveRuntimeContext),
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
	prompt := buildPrompt(opts.Prompt, opts.Messages, system)
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
	opts.Include = &include

	// Extract telemetry info once for all callback events
	cbFuncID, cbMeta := telemetryCallbackInfo(telemetrySettings)
	callID := internalGenerateCallID(opts.Internal)()
	onStepEndEvent := opts.OnStepEndEvent
	if onStepEndEvent == nil {
		onStepEndEvent = opts.OnStepFinishEvent
	}
	onEnd := opts.OnEnd
	if onEnd == nil {
		onEnd = opts.OnFinish
	}
	onEndEvent := opts.OnEndEvent
	if onEndEvent == nil {
		onEndEvent = opts.OnFinishEvent
	}

	// Emit OnStartEvent before streaming begins.
	Notify(ctx, OnStartEvent{
		CallID:              callID,
		OperationID:         "ai.streamText",
		ModelProvider:       opts.Model.Provider(),
		ModelID:             opts.Model.ModelID(),
		System:              system,
		Prompt:              opts.Prompt,
		Messages:            opts.Messages,
		Tools:               opts.Tools,
		ToolChoice:          opts.ToolChoice,
		Output:              opts.Output,
		ProviderOptions:     opts.ProviderOptions,
		Headers:             opts.Headers,
		Temperature:         opts.Temperature,
		MaxTokens:           opts.MaxTokens,
		TopP:                opts.TopP,
		TopK:                opts.TopK,
		FrequencyPenalty:    opts.FrequencyPenalty,
		PresencePenalty:     opts.PresencePenalty,
		StopSequences:       opts.StopSequences,
		Seed:                opts.Seed,
		MaxRetries:          preparedMaxRetries(opts.MaxRetries),
		ExperimentalContext: runtimeContext,
		RuntimeContext:      runtimeContext,
		ToolsContext:        toolsContext,
	}, opts.OnStart)

	stepModel := opts.Model
	stepSystem := system
	stepMessages := prompt.Messages
	stepTools := FilterActiveTools(opts.Tools, opts.ActiveTools)
	stepToolChoice := opts.ToolChoice
	stepToolOrder := opts.ToolOrder
	stepProviderOptions := opts.ProviderOptions
	stepSandbox := opts.ExperimentalSandbox
	if opts.PrepareStep != nil {
		prepared := opts.PrepareStep(ctx, PrepareStepOptions{
			Model:               stepModel,
			System:              stepSystem,
			Instructions:        &stepSystem,
			InitialInstructions: &system,
			Messages:            append([]types.Message(nil), stepMessages...),
			InitialMessages:     append([]types.Message(nil), prompt.Messages...),
			ResponseMessages:    nil,
			UserContext:         runtimeContext,
			RuntimeContext:      runtimeContext,
			ToolsContext:        toolsContext,
			StepNumber:          0,
			Steps:               nil,
			Tools:               stepTools,
			ToolChoice:          stepToolChoice,
			ActiveTools:         opts.ActiveTools,
			ToolOrder:           stepToolOrder,
			ProviderOptions:     stepProviderOptions,
			ExperimentalSandbox: stepSandbox,
		})
		if prepared.Model != nil {
			stepModel = prepared.Model
		}
		if prepared.Instructions != nil {
			stepSystem = *prepared.Instructions
		} else if prepared.System != "" {
			stepSystem = prepared.System
		}
		if prepared.Messages != nil {
			stepMessages = prepared.Messages
		}
		if prepared.Tools != nil {
			stepTools = prepared.Tools
		}
		if prepared.ToolChoice.Type != "" {
			stepToolChoice = prepared.ToolChoice
		}
		if prepared.ToolOrder != nil {
			stepToolOrder = prepared.ToolOrder
		}
		if prepared.ProviderOptions != nil {
			stepProviderOptions = prepared.ProviderOptions
		}
		if prepared.RuntimeContext != nil {
			runtimeContext = prepared.RuntimeContext
		}
		if prepared.ToolsContext != nil {
			toolsContext = prepared.ToolsContext
		}
		if prepared.ExperimentalSandbox != nil {
			stepSandbox = prepared.ExperimentalSandbox
		}
	}
	stepTools = resolveStepTools(ctx, stepTools, toolsContext, stepSandbox)
	stepTools = orderStepTools(stepTools, stepToolOrder)
	opts.ExperimentalSandbox = stepSandbox

	stepCtx := ctx
	var stepCancel context.CancelFunc
	if opts.Timeout != nil && opts.Timeout.HasPerStep() {
		stepCtx, stepCancel = opts.Timeout.CreateTimeoutContext(ctx, "step")
	}

	// Emit OnStepStartEvent for the first stream step.
	Notify(stepCtx, OnStepStartEvent{
		CallID:              callID,
		StepNumber:          0,
		ModelProvider:       stepModel.Provider(),
		ModelID:             stepModel.ModelID(),
		System:              stepSystem,
		Messages:            stepMessages,
		Tools:               stepTools,
		ActiveTools:         opts.ActiveTools,
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
			rf, rfErr := op.ResponseFormat(stepCtx)
			if rfErr != nil {
				if stepCancel != nil {
					stepCancel()
				}
				telemetry.FireOnError(telemetryCtx, telemetry.TelemetryErrorEvent{Settings: telemetrySettings, Error: rfErr})
				return nil, fmt.Errorf("output.ResponseFormat failed: %w", rfErr)
			}
			responseFormat = rf
		}
	}

	stepPrompt, normErr := promptutils.NormalizePromptWithDownloadSupport(stepCtx, types.Prompt{System: appendSandboxDescription(stepSystem, stepSandbox), Messages: stepMessages}, allowSystem, effectiveDownload(opts.ExperimentalDownload), supportedURLChecker(stepModel))
	if normErr != nil {
		if stepCancel != nil {
			stepCancel()
		}
		telemetry.FireOnError(telemetryCtx, telemetry.TelemetryErrorEvent{Settings: telemetrySettings, Error: normErr})
		return nil, fmt.Errorf("prompt normalization failed: %w", normErr)
	}

	// Build generate options
	genOpts := &provider.GenerateOptions{
		Prompt:                stepPrompt,
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
		Headers:               opts.Headers,
		Tools:                 stepTools,
		ToolChoice:            stepToolChoice,
		IncludeRawChunks:      includeRawChunksValue(include),
		RuntimeContext:        runtimeContext,
		ToolsContext:          toolsContext,
		ResponseFormat:        responseFormat,
		Reasoning:             opts.Reasoning,
		SendReasoning:         opts.SendReasoning,
		ProviderOptions:       stepProviderOptions,
		Telemetry:             telemetrySettings,
	}

	telemetry.FireOnLanguageModelCallStart(stepCtx, telemetry.LanguageModelCallStartEvent{
		Settings:      telemetrySettings,
		CallID:        callID,
		ModelProvider: stepModel.Provider(),
		ModelID:       stepModel.ModelID(),
		Prompt:        genOpts.Prompt,
		Tools:         genOpts.Tools,
	})

	// Start streaming
	stream, err := doStreamWithGatewayRetry(stepCtx, stepModel, genOpts, opts.MaxRetries)
	if err != nil {
		if stepCancel != nil {
			stepCancel()
		}
		if opts.Timeout != nil && opts.Timeout.HasPerStep() && stepCtx.Err() != nil {
			err = wrapTimeoutError(TimeoutReasonStep, stepCtx.Err())
		} else if opts.Timeout != nil && opts.Timeout.HasTotal() && ctx.Err() != nil {
			err = wrapTimeoutError(TimeoutReasonTotal, err)
		}
		if isAbortErr(stepCtx, err) {
			if opts.OnAbort != nil {
				opts.OnAbort(stepCtx, nil)
			}
			telemetry.FireOnAbort(telemetryCtx, telemetry.TelemetryAbortEvent{Settings: telemetrySettings, CallID: callID, Reason: err})
		} else {
			telemetry.FireOnError(telemetryCtx, telemetry.TelemetryErrorEvent{Settings: telemetrySettings, Error: err})
		}
		return nil, fmt.Errorf("failed to start stream: %w", err)
	}

	// Create result
	result := &StreamTextResult{
		stream:            stream,
		status:            StreamStatusSubmitted, // actively streaming; set before any chunks arrive
		timeout:           opts.Timeout,
		initialStepCtx:    stepCtx,
		initialStepCancel: stepCancel,
		telemetryCtx:      telemetryCtx,
		telemetrySettings: telemetrySettings,
		outputSpec:        outputSpec,
		// Structured event callbacks
		cbCallID:              callID,
		cbOnEnd:               onEnd,
		cbOnStepFinishEvent:   onStepEndEvent,
		cbOnEndEvent:          onEndEvent,
		cbOnToolCallStart:     opts.OnToolExecutionStart,
		cbOnToolCallFinish:    opts.OnToolExecutionEnd,
		cbFuncID:              cbFuncID,
		cbMeta:                cbMeta,
		cbModelProvider:       stepModel.Provider(),
		cbModelID:             stepModel.ModelID(),
		cbExperimentalCtx:     runtimeContext,
		cbRuntimeCtx:          runtimeContext,
		cbSensitiveRuntimeCtx: opts.SensitiveRuntimeContext,
		cbToolsCtx:            toolsContext,
		cbInclude:             include,
		cbMessages:            stepMessages,
		cbTools:               stepTools,
		cbSystem:              stepSystem,
		cbExperimentalSandbox: stepSandbox,
		// Retained for deferred provider tool continuation.
		cbModel:      stepModel,
		cbStreamOpts: opts,
	}

	// Start the processing loop when any callback depends on post-stream tool
	// execution or multi-step continuation.
	if opts.OnChunk != nil || onEnd != nil ||
		opts.OnStepStart != nil ||
		opts.OnStepEndEvent != nil || opts.OnStepFinishEvent != nil || onEndEvent != nil ||
		opts.OnToolExecutionStart != nil || opts.OnToolExecutionEnd != nil ||
		opts.OnToolCallStart != nil || opts.OnToolCallFinish != nil ||
		opts.OnError != nil || opts.OnAbort != nil {
		result.processingDone = make(chan struct{})
		go result.processStream(ctx, opts.OnChunk, onEnd)
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
	if r.processingDone != nil {
		defer close(r.processingDone)
	}
	opts := r.cbStreamOpts
	currentMessages := r.cbMessages
	currentTools := append([]types.Tool(nil), r.cbTools...)
	stopConditions := resolveStopConditions(opts.StopWhen, opts.MaxSteps)
	// pendingDeferredToolCalls tracks provider tools (SupportsDeferredResults=true) whose
	// results haven't arrived yet. Key = toolCallID, value = toolName.
	pendingDeferredToolCalls := make(map[string]string)

	var allSteps []types.StepResult
	firstChunkEver := true
	suppressReasoningBoundaries := shouldSuppressReasoningBoundaries(opts.SendReasoning)
	var accumulatedTextParts []string
	pendingStepCtx := r.initialStepCtx
	pendingStepCancel := r.initialStepCancel
	abortFired := false
	fireAbort := func(reason error) {
		if abortFired {
			return
		}
		abortFired = true
		if opts.OnAbort != nil {
			opts.OnAbort(ctx, allSteps)
		}
		telemetry.FireOnAbort(r.telemetryCtx, telemetry.TelemetryAbortEvent{
			Settings: r.telemetrySettings,
			CallID:   r.cbCallID,
			Reason:   reason,
			Steps:    append([]types.StepResult(nil), allSteps...),
		})
		if onChunk != nil {
			abortChunk := provider.StreamChunk{Type: provider.ChunkTypeAbort}
			if reason != nil {
				abortChunk.AbortReason = reason.Error()
			}
			onChunk(abortChunk)
		}
	}

	for stepNum := 1; ; stepNum++ {
		stepIndex := stepNum - 1
		stepStart := time.Now()
		stepCtx := ctx
		cancelStep := func() {}
		if pendingStepCtx != nil {
			stepCtx = pendingStepCtx
			if pendingStepCancel != nil {
				cancelStep = pendingStepCancel
			}
			pendingStepCtx = nil
			pendingStepCancel = nil
		} else if r.timeout != nil && r.timeout.HasPerStep() {
			stepCtx, cancelStep = r.timeout.CreateTimeoutContext(ctx, "step")
		}
		var firstTokenAt *time.Time
		var previousOutputChunkAt *time.Time
		var outputChunkGapsMs []int64
		stepProvider := r.cbModel.Provider()
		stepModelID := r.cbModel.ModelID()
		stepTools := append([]types.Tool(nil), currentTools...)
		toolsByName := make(map[string]*types.Tool, len(stepTools))
		for i := range stepTools {
			toolsByName[stepTools[i].Name] = &stepTools[i]
		}
		// Fire step-start telemetry. OTel implementations create a child step span.
		telemetryStepCtx := telemetry.FireOnStepStart(ctx, telemetry.TelemetryStepStartEvent{
			OperationType:  "ai.streamText",
			Settings:       r.telemetrySettings,
			StepNumber:     stepIndex,
			ModelProvider:  stepProvider,
			ModelID:        stepModelID,
			RuntimeContext: telemetryRuntimeContextWithSensitivity(r.telemetrySettings, r.cbRuntimeCtx, r.cbSensitiveRuntimeCtx),
			ToolsContext:   telemetryToolsContext(r.telemetrySettings, r.cbToolsCtx),
		})

		// Track per-step slices before we accumulate more stream data.
		stepSourcesStart := len(r.sources)
		// Track how many files existed before this step so we can slice per-step files.
		stepFilesStart := len(r.files)
		stepWarningsStart := len(r.warnings)

		// pendingToolCalls accumulates tool call chunks received during this step's stream.
		// All Execute() calls happen after the stream loop ends.
		var stepTextParts []string
		var stepToolCalls []types.ToolCall
		var stepContent []types.ContentPart
		var stepReasoningBuilder strings.Builder
		var modelCallEndFired bool
		var stepUsage types.Usage
		var stepSawTerminal bool
		var stepSawFinish bool
		var stepSawOutput bool
		// streamedToolResultIDs tracks tool call IDs for which the provider returned a
		// result inline in this step's stream (used for the deferred hasResult check).
		streamedToolResultIDs := make(map[string]bool)

		for {
			chunk, err := r.nextChunk(stepCtx)
			if err == io.EOF {
				break
			}
			if err != nil {
				if errors.Is(stepCtx.Err(), context.DeadlineExceeded) && r.timeout != nil && r.timeout.HasPerStep() {
					err = wrapTimeoutError(TimeoutReasonStep, stepCtx.Err())
				}
				r.err = err
				if isAbortErr(ctx, err) {
					fireAbort(err)
				}
				break
			}
			forwardChunk := !(suppressReasoningBoundaries && isReasoningBoundaryChunk(chunk.Type))
			if chunk.Type == provider.ChunkTypeRaw && !includeRawChunksValue(r.cbInclude) {
				forwardChunk = false
			}
			if chunk.Type == provider.ChunkTypeText && chunk.Text == "" {
				forwardChunk = false
			}

			// Transition from Submitted to Streaming on the first content chunk.
			// Metadata and stream lifecycle chunks are forwarded before this
			// marker, matching the TypeScript stream part order.
			if forwardChunk && isOutputChunkForTiming(*chunk) {
				now := time.Now()
				if firstTokenAt == nil {
					firstTokenAt = &now
				} else if previousOutputChunkAt != nil {
					outputChunkGapsMs = append(outputChunkGapsMs, now.Sub(*previousOutputChunkAt).Milliseconds())
				}
				previousOutputChunkAt = &now
			}
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

			if isModelOutputChunkType(chunk.Type) {
				stepSawOutput = true
			}

			// Accumulate warnings from stream-start chunks
			if chunk.Type == provider.ChunkTypeStreamStart {
				r.warnings = append(r.warnings, chunk.Warnings...)
			}

			// Accumulate text
			if chunk.Type == provider.ChunkTypeText {
				stepTextParts = append(stepTextParts, chunk.Text)
				accumulatedTextParts = append(accumulatedTextParts, chunk.Text)
				stepContent = appendTextPart(stepContent, chunk.Text)

				// Update partial output after each text chunk (with deduplication).
				// Only publishes when the JSON representation of the partial changes,
				// matching the TypeScript SDK's deduplication behavior.
				if r.outputSpec != nil {
					currentText := strings.Join(accumulatedTextParts, "")
					partial, partialErr := r.outputSpec.parsePartialOutput(ctx, ParsePartialOutputOptions{
						Text: currentText,
					})
					if partialErr != nil {
						r.err = partialErr
						break
					}
					if partial != nil {
						newJSONStr, ok := partialOutputDedupKey(partial)
						if ok && newJSONStr != r.lastPartialJSON {
							r.lastPartialJSON = newJSONStr
							r.mu.Lock()
							r.partialOutput = partial
							r.mu.Unlock()
						}
					}
				}
			}

			// Accumulate reasoning text from reasoning chunks.
			if chunk.Type == provider.ChunkTypeReasoning && (chunk.Text != "" || chunk.Reasoning != "") {
				reasoningText := chunk.Reasoning
				if reasoningText == "" {
					reasoningText = chunk.Text
				}
				stepReasoningBuilder.WriteString(reasoningText)
				stepContent = appendReasoningPart(stepContent, reasoningText)
			}

			// Accumulate tool call chunks without executing until the stream is consumed.
			// The chunk is still forwarded to the consumer below.
			if chunk.Type == provider.ChunkTypeToolCall && chunk.ToolCall != nil {
				enriched := enrichToolCallMetadata([]types.ToolCall{*chunk.ToolCall}, stepTools)[0]
				chunk.ToolCall = &enriched
				stepToolCalls = append(stepToolCalls, enriched)
				stepContent = append(stepContent, types.ToolCallContent{
					ToolCallID:       enriched.ID,
					ToolName:         enriched.ToolName,
					Title:            enriched.Title,
					Input:            enriched.RawArguments,
					Arguments:        enriched.Arguments,
					ProviderExecuted: enriched.ProviderExecuted,
					ProviderMetadata: providerMetadataRaw(enriched.ProviderMetadata),
					ToolMetadata:     enriched.ToolMetadata,
					Dynamic:          enriched.Dynamic,
					Invalid:          enriched.Invalid,
					Error:            toolCallContentError(enriched.Error),
					ThoughtSignature: enriched.ThoughtSignature,
				})
			}

			// Track provider-inline tool results for the deferred hasResult check.
			if chunk.Type == provider.ChunkTypeToolResult && chunk.ToolResult != nil {
				streamedToolResultIDs[chunk.ToolResult.ToolCallID] = true
				stepContent = append(stepContent, toolResultContentFromToolResult(*chunk.ToolResult))
			}

			if chunk.Usage != nil {
				stepUsage = *chunk.Usage
			}

			// Update finish reason and context management
			if chunk.Type == provider.ChunkTypeFinish {
				stepSawTerminal = true
				stepSawFinish = true
				r.finishReason = chunk.FinishReason
				if chunk.ContextManagement != nil {
					r.contextManagement = chunk.ContextManagement
				}
				if !modelCallEndFired {
					modelCallEndFired = true
					performance := stepPerformance(stepStart, stepUsage, firstTokenAt, outputChunkGapsMs)
					telemetry.FireOnLanguageModelCallEnd(ctx, telemetry.LanguageModelCallEndEvent{
						Settings:      r.telemetrySettings,
						CallID:        r.cbCallID,
						ModelProvider: stepProvider,
						ModelID:       stepModelID,
						FinishReason:  string(chunk.FinishReason),
						Usage:         telemetryUsageFromUsage(stepUsage),
						ResponseID:    responseIDFromMetadata(chunk.ResponseMetadata),
						Performance:   languageModelCallPerformance(performance),
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
				stepContent = append(stepContent, *chunk.SourceContent)
			}

			// Accumulate generated files from ChunkTypeFile chunks.
			if chunk.Type == provider.ChunkTypeFile && chunk.GeneratedFileContent != nil {
				r.files = append(r.files, *chunk.GeneratedFileContent)
				stepContent = append(stepContent, *chunk.GeneratedFileContent)
			}

			if chunk.Type == provider.ChunkTypeCustom && chunk.CustomContent != nil {
				stepContent = append(stepContent, *chunk.CustomContent)
			}

			if chunk.Type == provider.ChunkTypeReasoningFile && chunk.ReasoningFileContent != nil {
				stepContent = append(stepContent, *chunk.ReasoningFileContent)
			}

			// Update response headers from ChunkTypeResponseMetadata.
			// Mirrors TS SDK's 'response-metadata' chunk handling in stream-text.ts.
			if chunk.Type == provider.ChunkTypeResponseMetadata && chunk.ResponseMetadata != nil {
				if chunk.ResponseMetadata.Headers != nil {
					r.responseHeaders = chunk.ResponseMetadata.Headers
				}
			}

			// Call OnError for error chunks before forwarding.
			if chunk.Type == provider.ChunkTypeError {
				stepSawTerminal = true
				if r.finishReason == "" {
					r.finishReason = types.FinishReasonError
				}
				if opts.OnError != nil {
					opts.OnError(ctx, errors.New(chunk.Text))
				}
			}

			// Apply experimental transforms to produce the consumer-facing chunks.
			chunksToForward := []provider.StreamChunk{*chunk}
			if forwardChunk && len(opts.ExperimentalTransform) > 0 {
				for _, transform := range opts.ExperimentalTransform {
					var transformed []provider.StreamChunk
					for _, c := range chunksToForward {
						transformed = append(transformed, transform(ctx, c)...)
					}
					chunksToForward = transformed
				}
			}

			// Forward chunk(s) to consumer before any tool Execute fires.
			if forwardChunk {
				for _, c := range chunksToForward {
					if onChunk != nil {
						onChunk(c)
					}
					telemetry.FireOnChunk(ctx, telemetry.TelemetryChunkEvent{
						Settings:  r.telemetrySettings,
						ChunkType: string(c.Type),
						Text:      c.Text,
					})
				}
			}
		}
		if r.err != nil {
			cancelStep()
			break
		}
		if !stepSawTerminal {
			if !stepSawOutput {
				r.err = newIncompleteModelStreamError()
				errorChunk := provider.StreamChunk{
					Type: provider.ChunkTypeError,
					Text: r.err.Error(),
				}
				if onChunk != nil {
					onChunk(errorChunk)
				}
				telemetry.FireOnChunk(ctx, telemetry.TelemetryChunkEvent{
					Settings:  r.telemetrySettings,
					ChunkType: string(errorChunk.Type),
					Text:      errorChunk.Text,
				})
				cancelStep()
				break
			}
			r.finishReason = types.FinishReasonOther
		}
		if !stepSawFinish && r.finishReason != "" {
			finishChunk := provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: r.finishReason}
			if onChunk != nil {
				onChunk(finishChunk)
			}
			telemetry.FireOnChunk(ctx, telemetry.TelemetryChunkEvent{
				Settings:  r.telemetrySettings,
				ChunkType: string(finishChunk.Type),
			})
		}
		stepText := strings.Join(stepTextParts, "")
		r.text = strings.Join(accumulatedTextParts, "")
		var refineErr error
		stepToolCalls, refineErr = RefineToolCalls(ctx, stepToolCalls, stepTools, opts.ExperimentalRefineToolInput, r.cbRuntimeCtx, r.cbToolsCtx)
		if refineErr != nil {
			r.err = fmt.Errorf("tool input refinement failed at step %d: %w", stepNum, refineErr)
			break
		}
		stepToolCalls = enrichToolCallMetadata(stepToolCalls, stepTools)
		stepContent = replaceToolCallContentParts(stepContent, stepToolCalls)
		stepContent = replaceToolResultContentParts(stepContent, stepToolCalls)

		// Execute accumulated tool calls after stream is fully consumed.
		// All chunks (including tool call chunks) have already been forwarded above.
		var stepToolResults []types.ToolResult
		var toolExecutionMs map[string]int64
		if len(stepToolCalls) > 0 && len(stepTools) > 0 {
			toolExecutionMs = map[string]int64{}
			toolCallbacks := toolCallEventCallbacks{
				callID:              r.cbCallID,
				onStart:             r.cbOnToolCallStart,
				onFinish:            r.cbOnToolCallFinish,
				fallbackStart:       opts.OnToolCallStart,
				fallbackFinish:      opts.OnToolCallFinish,
				stepNum:             stepIndex,
				modelProvider:       stepProvider,
				modelID:             stepModelID,
				messages:            currentMessages,
				experimentalContext: r.cbExperimentalCtx,
				runtimeContext:      r.cbRuntimeCtx,
				toolsContext:        r.cbToolsCtx,
				functionID:          r.cbFuncID,
				metadata:            r.cbMeta,
				timeout:             r.timeout,
				telemetrySettings:   r.telemetrySettings,
				experimentalSandbox: opts.ExperimentalSandbox,
				toolExecutionMs:     toolExecutionMs,
			}
			usageForTools := r.usage.Add(stepUsage)
			stepToolResults, _ = executeTools(stepCtx, stepToolCalls, stepTools, r.cbRuntimeCtx, r.cbToolsCtx, opts.ToolApproval, &usageForTools, toolCallbacks)
			attachToolApprovalSignatures(stepToolResults, opts.ExperimentalToolApprovalSecret)
		}
		if stepCtx.Err() != nil && r.timeout != nil && r.timeout.HasPerStep() {
			r.err = wrapTimeoutError(TimeoutReasonStep, stepCtx.Err())
			fireAbort(r.err)
			cancelStep()
			break
		}
		hasUserApproval := false
		for _, tr := range stepToolResults {
			if tr.ApprovalStatus == types.ToolApprovalStatusUserApproval {
				hasUserApproval = true
				r.finishReason = types.FinishReasonUserApproval
				break
			}
		}

		// Forward tool execution chunks to OnChunk consumers after execution.
		for i := range stepToolResults {
			tr := &stepToolResults[i]
			emitChunk := func(resultChunk provider.StreamChunk) {
				if onChunk != nil {
					onChunk(resultChunk)
				}
				telemetry.FireOnChunk(ctx, telemetry.TelemetryChunkEvent{
					Settings:  r.telemetrySettings,
					ChunkType: string(resultChunk.Type),
				})
			}
			switch tr.ApprovalStatus {
			case types.ToolApprovalStatusUserApproval, types.ToolApprovalStatusApproved, types.ToolApprovalStatusDenied:
				request := toolApprovalRequestFromToolResult(*tr)
				emitChunk(provider.StreamChunk{
					Type:                provider.ChunkTypeToolApprovalRequest,
					ToolApprovalRequest: &request,
				})
				if tr.ApprovalStatus == types.ToolApprovalStatusUserApproval {
					continue
				}
				response := toolApprovalResponseFromToolResult(*tr)
				emitChunk(provider.StreamChunk{
					Type:                 provider.ChunkTypeToolApprovalResponse,
					ToolApprovalResponse: &response,
				})
				if tr.ApprovalStatus == types.ToolApprovalStatusDenied {
					emitChunk(provider.StreamChunk{
						Type:       provider.ChunkTypeToolOutputDenied,
						ToolResult: tr,
					})
					continue
				}
				if tr.ProviderExecuted && tr.Result == nil && tr.Error == nil {
					continue
				}
			}
			emitChunk(provider.StreamChunk{
				Type:       provider.ChunkTypeToolResult,
				ToolResult: tr,
			})
		}
		stepContent = append(stepContent, toolResultsToContentParts(stepToolResults, opts.ExperimentalToolApprovalSecret)...)

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
		// Note: stepToolResults includes pending markers for provider-executed tools
		// (ProviderExecuted=true, Result=nil); those must NOT clear the pending map.
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
		r.usage = r.usage.Add(stepUsage)
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
		performance := stepPerformance(stepStart, stepUsage, firstTokenAt, outputChunkGapsMs)
		performance = finishStepPerformance(performance, stepStart, toolExecutionMs)
		stepSources := append([]types.SourceContent(nil), r.sources[stepSourcesStart:]...)
		stepFiles := append([]types.GeneratedFileContent(nil), r.files[stepFilesStart:]...)
		stepResult := types.StepResult{
			CallID:             r.cbCallID,
			StepNumber:         stepIndex,
			Model:              types.StepModel{Provider: stepProvider, ModelID: stepModelID},
			Text:               stepText,
			Content:            stepContent,
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
			Usage:              stepUsage,
			Performance:        performance,
			Sources:            stepSources,
			Request: types.StepRequest{
				Messages: includedRequestMessages(r.cbInclude.RequestMessages, currentMessages),
			},
			Response:         types.StepResponse{Headers: stepHeaders},
			ProviderMetadata: stepProviderMeta,
			ToolsContext:     r.cbToolsCtx,
			RuntimeContext:   r.cbRuntimeCtx,
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
			telemetry.FireOnStepEnd(telemetryStepCtx, telemetry.TelemetryStepEndEvent{
				StepNumber:     stepIndex,
				FinishReason:   string(r.finishReason),
				Usage:          stepTelUsage,
				Text:           stepText,
				ToolCalls:      stepToolCalls,
				Files:          stepFiles,
				Settings:       r.telemetrySettings,
				RuntimeContext: telemetryRuntimeContextWithSensitivity(r.telemetrySettings, r.cbRuntimeCtx, r.cbSensitiveRuntimeCtx),
				ToolsContext:   telemetryToolsContext(r.telemetrySettings, r.cbToolsCtx),
			})
		}

		// Populate ResponseMessages on the step that just completed.
		stepResponseMsgs := providerutils.ConvertToResponseMessages(
			stepToolCalls,
			stepResult.Content,
			stepToolResults,
		)
		currentMessages = append(currentMessages, stepResponseMsgs...)
		allSteps[len(allSteps)-1].ResponseMessages = stepResponseMsgs
		allSteps[len(allSteps)-1].Response.Messages = stepResponseMsgs
		stepWarnings := append([]types.Warning(nil), r.warnings[stepWarningsStart:]...)
		stepResult = allSteps[len(allSteps)-1]
		r.mu.Lock()
		r.cbSteps = append([]types.StepResult(nil), allSteps...)
		r.cbResponseMessages = responseMessagesFromSteps(allSteps)
		r.mu.Unlock()
		Notify(ctx, OnStepFinishEvent{
			CallID:             r.cbCallID,
			StepNumber:         stepResult.StepNumber,
			Model:              stepResult.Model,
			ModelProvider:      stepResult.Model.Provider,
			ModelID:            stepResult.Model.ModelID,
			Text:               stepResult.Text,
			Reasoning:          stepResult.Reasoning,
			ReasoningText:      stepResult.ReasoningText,
			ToolCalls:          stepResult.ToolCalls,
			StaticToolCalls:    stepResult.StaticToolCalls,
			DynamicToolCalls:   stepResult.DynamicToolCalls,
			ToolResults:        stepResult.ToolResults,
			StaticToolResults:  stepResult.StaticToolResults,
			DynamicToolResults: stepResult.DynamicToolResults,
			FinishReason:       stepResult.FinishReason,
			RawFinishReason:    stepResult.RawFinishReason,
			Usage:              stepResult.Usage,
			Warnings:           stepWarnings,
			Sources:            stepResult.Sources,
			Files:              stepFiles,
			ProviderMetadata:   stepResult.ProviderMetadata,
			ResponseHeaders:    stepHeaders,
			Response: GenerateStepResponse{
				Headers:  stepHeaders,
				Messages: stepResult.ResponseMessages,
			},
			ExperimentalContext: r.cbExperimentalCtx,
			RuntimeContext:      r.cbRuntimeCtx,
			ToolsContext:        r.cbToolsCtx,
		}, r.cbOnStepFinishEvent)
		cancelStep()

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
		nextModel := r.cbModel
		nextSystem := r.cbSystem
		nextMessages := currentMessages
		nextTools := FilterActiveTools(opts.Tools, opts.ActiveTools)
		nextToolChoice := opts.ToolChoice
		nextToolOrder := opts.ToolOrder
		nextProviderOptions := opts.ProviderOptions
		nextSandbox := opts.ExperimentalSandbox
		if opts.PrepareStep != nil {
			prepared := opts.PrepareStep(ctx, PrepareStepOptions{
				Model:               nextModel,
				System:              nextSystem,
				Instructions:        &nextSystem,
				InitialInstructions: &r.cbSystem,
				Messages:            append([]types.Message(nil), nextMessages...),
				InitialMessages:     append([]types.Message(nil), r.cbMessages...),
				ResponseMessages:    responseMessagesFromSteps(allSteps),
				UserContext:         r.cbRuntimeCtx,
				RuntimeContext:      r.cbRuntimeCtx,
				ToolsContext:        r.cbToolsCtx,
				StepNumber:          stepNum,
				Steps:               append([]types.StepResult(nil), allSteps...),
				Tools:               nextTools,
				ToolChoice:          nextToolChoice,
				ActiveTools:         opts.ActiveTools,
				ToolOrder:           nextToolOrder,
				ProviderOptions:     nextProviderOptions,
				ExperimentalSandbox: nextSandbox,
				AccumulatedUsage:    r.usage,
			})
			if prepared.Model != nil {
				nextModel = prepared.Model
			}
			if prepared.Instructions != nil {
				nextSystem = *prepared.Instructions
			} else if prepared.System != "" {
				nextSystem = prepared.System
			}
			if prepared.Messages != nil {
				nextMessages = prepared.Messages
				currentMessages = prepared.Messages
			}
			if prepared.Tools != nil {
				nextTools = prepared.Tools
			}
			if prepared.ToolChoice.Type != "" {
				nextToolChoice = prepared.ToolChoice
			}
			if prepared.ToolOrder != nil {
				nextToolOrder = prepared.ToolOrder
			}
			if prepared.ProviderOptions != nil {
				nextProviderOptions = prepared.ProviderOptions
			}
			if prepared.RuntimeContext != nil {
				r.cbRuntimeCtx = prepared.RuntimeContext
			}
			if prepared.ToolsContext != nil {
				r.cbToolsCtx = prepared.ToolsContext
			}
			if prepared.ExperimentalSandbox != nil {
				nextSandbox = prepared.ExperimentalSandbox
			}
		}
		nextTools = resolveStepTools(ctx, nextTools, r.cbToolsCtx, nextSandbox)
		nextTools = orderStepTools(nextTools, nextToolOrder)
		r.cbModel = nextModel
		r.cbModelProvider = nextModel.Provider()
		r.cbModelID = nextModel.ModelID()
		r.cbSystem = nextSystem
		currentTools = append([]types.Tool(nil), nextTools...)
		opts.ExperimentalSandbox = nextSandbox
		nextStepCtx := ctx
		nextStepCancel := func() {}
		if r.timeout != nil && r.timeout.HasPerStep() {
			nextStepCtx, nextStepCancel = r.timeout.CreateTimeoutContext(ctx, "step")
		}
		Notify(nextStepCtx, OnStepStartEvent{
			CallID:              r.cbCallID,
			StepNumber:          stepNum,
			ModelProvider:       nextModel.Provider(),
			ModelID:             nextModel.ModelID(),
			System:              nextSystem,
			Messages:            nextMessages,
			Tools:               nextTools,
			ActiveTools:         opts.ActiveTools,
			Steps:               allSteps,
			PreviousSteps:       allSteps,
			ExperimentalContext: r.cbExperimentalCtx,
			RuntimeContext:      r.cbRuntimeCtx,
			ToolsContext:        r.cbToolsCtx,
		}, opts.OnStepStart)
		nextPrompt, normErr := promptutils.NormalizePromptWithDownloadSupport(nextStepCtx, types.Prompt{
			Messages: nextMessages,
			System:   appendSandboxDescription(nextSystem, nextSandbox),
		}, allowSystemMessages(opts.AllowSystemMessages, opts.AllowSystemInMessages), effectiveDownload(opts.ExperimentalDownload), supportedURLChecker(nextModel))
		if normErr != nil {
			nextStepCancel()
			r.err = fmt.Errorf("prompt normalization failed for step %d: %w", stepNum+1, normErr)
			break
		}
		nextGenOpts := &provider.GenerateOptions{
			Prompt:                nextPrompt,
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
			Headers:               opts.Headers,
			Tools:                 nextTools,
			ToolChoice:            nextToolChoice,
			IncludeRawChunks:      includeRawChunksValue(r.cbInclude),
			RuntimeContext:        r.cbRuntimeCtx,
			ToolsContext:          r.cbToolsCtx,
			ResponseFormat:        responseFormat,
			Reasoning:             opts.Reasoning,
			SendReasoning:         opts.SendReasoning,
			ProviderOptions:       nextProviderOptions,
			Telemetry:             r.telemetrySettings,
		}
		telemetry.FireOnLanguageModelCallStart(nextStepCtx, telemetry.LanguageModelCallStartEvent{
			Settings:      r.telemetrySettings,
			CallID:        r.cbCallID,
			ModelProvider: nextModel.Provider(),
			ModelID:       nextModel.ModelID(),
			Prompt:        nextGenOpts.Prompt,
			Tools:         nextGenOpts.Tools,
		})
		newStream, err := nextModel.DoStream(nextStepCtx, nextGenOpts)
		if err != nil {
			nextStepCancel()
			if r.timeout != nil && r.timeout.HasPerStep() && nextStepCtx.Err() != nil {
				err = wrapTimeoutError(TimeoutReasonStep, nextStepCtx.Err())
			} else if r.timeout != nil && r.timeout.HasTotal() && ctx.Err() != nil {
				err = wrapTimeoutError(TimeoutReasonTotal, err)
			}
			r.err = fmt.Errorf("failed to start stream for step %d: %w", stepNum+1, err)
			if isAbortErr(nextStepCtx, r.err) {
				fireAbort(r.err)
			}
			break
		}
		pendingStepCtx = nextStepCtx
		pendingStepCancel = nextStepCancel
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
	if r.err != nil {
		if isAbortErr(ctx, r.err) {
			fireAbort(r.err)
		}
		if opts.OnError != nil && !isAbortErr(ctx, r.err) {
			opts.OnError(ctx, r.err)
		}
		if !isAbortErr(ctx, r.err) {
			telemetry.FireOnError(r.telemetryCtx, telemetry.TelemetryErrorEvent{
				Settings: r.telemetrySettings,
				Error:    r.err,
			})
		}
		r.mu.Lock()
		r.status = StreamStatusDone
		r.mu.Unlock()
		return
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
	r.mu.Unlock()
	telemetry.FireOnFinish(r.telemetryCtx, telemetry.TelemetryFinishEvent{
		FinishReason:   string(r.finishReason),
		Usage:          streamTelUsage,
		ModelProvider:  r.cbModelProvider,
		ModelID:        r.cbModelID,
		Text:           r.text,
		Files:          streamFiles,
		Settings:       r.telemetrySettings,
		RuntimeContext: telemetryRuntimeContextWithSensitivity(r.telemetrySettings, r.cbRuntimeCtx, r.cbSensitiveRuntimeCtx),
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

	// Use the last step for the single-step path.
	fallbackContent := []types.ContentPart{types.TextContent{Text: r.text}}
	lastStep := types.StepResult{
		CallID:             r.cbCallID,
		StepNumber:         0,
		Model:              types.StepModel{Provider: r.cbModelProvider, ModelID: r.cbModelID},
		Text:               r.text,
		Content:            fallbackContent,
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
			fallbackContent,
			nil,
		),
	}
	if len(allSteps) > 0 {
		lastStep = allSteps[len(allSteps)-1]
	}
	stepsForEvent := allSteps
	if len(stepsForEvent) == 0 {
		stepsForEvent = []types.StepResult{lastStep}
	}
	Notify(ctx, OnFinishEvent{
		CallID:             r.cbCallID,
		StepNumber:         lastStep.StepNumber,
		Model:              lastStep.Model,
		ModelProvider:      lastStep.Model.Provider,
		ModelID:            lastStep.Model.ModelID,
		Text:               r.text,
		Reasoning:          lastStep.Reasoning,
		ReasoningText:      lastStep.ReasoningText,
		ToolCalls:          lastStep.ToolCalls,
		StaticToolCalls:    lastStep.StaticToolCalls,
		DynamicToolCalls:   lastStep.DynamicToolCalls,
		ToolResults:        lastStep.ToolResults,
		StaticToolResults:  lastStep.StaticToolResults,
		DynamicToolResults: lastStep.DynamicToolResults,
		FinishReason:       r.finishReason,
		RawFinishReason:    lastStep.RawFinishReason,
		Usage:              lastStep.Usage,
		Steps:              stepsForEvent,
		TotalUsage:         r.usage,
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
	}, r.cbOnEndEvent)
}

// Stream returns the underlying text stream
func (r *StreamTextResult) Stream() provider.TextStream {
	return r.stream
}

// FullStream returns the underlying text stream.
//
// Deprecated: use Stream.
func (r *StreamTextResult) FullStream() provider.TextStream {
	return r.Stream()
}

// ConsumeStream drains the stream and waits for completion.
func (r *StreamTextResult) ConsumeStream() error {
	_, err := r.ReadAll()
	return err
}

// ToTextStreamResponse creates a text Response object from the stream result.
//
// Deprecated: use CreateTextStreamResponseFromStream with Stream.
func (r *StreamTextResult) ToTextStreamResponse(ctx context.Context, init *TextStreamResponseInit) (*http.Response, error) {
	return CreateTextStreamResponseFromStream(ctx, r.Stream(), init)
}

// ToUIMessageStream converts the stream result into UI message chunks.
//
// Deprecated: use ToUIMessageStream with Stream.
func (r *StreamTextResult) ToUIMessageStream(ctx context.Context, opts ...UIMessageStreamResultOptions) (<-chan UIMessageChunk, <-chan error) {
	return ToUIMessageStream(ctx, r.Stream(), opts...)
}

// ToUIMessageStreamResponse creates an SSE response from the stream result.
//
// Deprecated: use ToUIMessageStream with Stream and create a response with the
// standalone helpers.
func (r *StreamTextResult) ToUIMessageStreamResponse(ctx context.Context, init *UIMessageStreamResponseInit, opts ...UIMessageStreamResultOptions) (*http.Response, error) {
	return CreateUIMessageStreamResponseWithInit(ctx, r, init, opts...)
}

// PipeTextStreamToResponse writes text delta output to the writer.
//
// Deprecated: use PipeTextStreamToWriter with Stream.
func (r *StreamTextResult) PipeTextStreamToResponse(ctx context.Context, w io.Writer) error {
	return PipeTextStreamToWriter(ctx, r.Stream(), w)
}

// PipeUIMessageStreamToResponse writes UI message chunk output to the writer.
//
// Deprecated: use ToUIMessageStream with Stream and write the resulting chunks.
func (r *StreamTextResult) PipeUIMessageStreamToResponse(ctx context.Context, w io.Writer, opts ...UIMessageStreamResultOptions) error {
	return PipeUIMessageStreamToResponse(ctx, r, w, opts...)
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
	_ = r.ensureConsumed()
	return r.usage
}

// TotalUsage returns aggregate token usage across all steps.
func (r *StreamTextResult) TotalUsage() types.Usage {
	return r.Usage()
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
	_ = r.ensureConsumed()
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.toolCalls
}

// ToolResults returns results from tool executions that ran after stream end.
// Only populated when StreamText was called with callbacks and tool definitions.
// Safe to call concurrently with streaming.
func (r *StreamTextResult) ToolResults() []types.ToolResult {
	_ = r.ensureConsumed()
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.toolResults
}

// Sources returns citation or grounding references accumulated during streaming.
// Populated by providers such as Perplexity and Google Generative AI.
// Only populated after stream completes.
func (r *StreamTextResult) Sources() []types.SourceContent {
	_ = r.ensureConsumed()
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
	_ = r.ensureConsumed()
	r.mu.Lock()
	defer r.mu.Unlock()
	return filterStaticToolCalls(r.toolCalls)
}

// DynamicToolCalls returns tool calls from dynamically registered tools.
// Only populated after stream completes.
func (r *StreamTextResult) DynamicToolCalls() []types.ToolCall {
	_ = r.ensureConsumed()
	r.mu.Lock()
	defer r.mu.Unlock()
	return filterDynamicToolCalls(r.toolCalls)
}

// StaticToolResults returns results from non-dynamic (typed) tools.
// Only populated after stream completes.
func (r *StreamTextResult) StaticToolResults() []types.ToolResult {
	_ = r.ensureConsumed()
	r.mu.Lock()
	defer r.mu.Unlock()
	return filterStaticToolResults(r.toolResults)
}

// DynamicToolResults returns results from dynamically registered tools.
// Only populated after stream completes.
func (r *StreamTextResult) DynamicToolResults() []types.ToolResult {
	_ = r.ensureConsumed()
	r.mu.Lock()
	defer r.mu.Unlock()
	return filterDynamicToolResults(r.toolResults)
}

// RawFinishReason returns the raw finish reason string from the provider.
// Only available after stream completes.
func (r *StreamTextResult) RawFinishReason() string {
	_ = r.ensureConsumed()
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
	_ = r.ensureConsumed()
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stepRequest
}

// Response returns metadata about the last response from the provider.
func (r *StreamTextResult) Response() types.StepResponse {
	_ = r.ensureConsumed()
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

// Steps returns all completed stream steps, consuming the stream if needed.
func (r *StreamTextResult) Steps() []types.StepResult {
	_ = r.ensureConsumed()
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]types.StepResult(nil), r.cbSteps...)
}

// FinalStep returns the final completed stream step, consuming the stream if needed.
func (r *StreamTextResult) FinalStep() types.StepResult {
	steps := r.Steps()
	if len(steps) == 0 {
		return types.StepResult{}
	}
	return steps[len(steps)-1]
}

// Content returns generated content from all stream steps in order.
func (r *StreamTextResult) Content() []types.ContentPart {
	steps := r.Steps()
	var content []types.ContentPart
	for _, step := range steps {
		content = append(content, step.Content...)
	}
	return content
}

// ResponseMessages returns accumulated assistant/tool response messages.
func (r *StreamTextResult) ResponseMessages() []types.Message {
	_ = r.ensureConsumed()
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]types.Message(nil), r.cbResponseMessages...)
}

func (r *StreamTextResult) ensureConsumed() error {
	r.mu.Lock()
	done := r.status == StreamStatusDone
	processingDone := r.processingDone
	r.mu.Unlock()
	if done {
		return nil
	}
	if processingDone != nil {
		<-processingDone
		return r.err
	}
	_, err := r.ReadAll()
	return err
}

// Close closes the stream
func (r *StreamTextResult) Close() error {
	if r.initialStepCancel != nil {
		r.initialStepCancel()
	}
	return r.stream.Close()
}

// ReadAll reads all chunks from the stream and returns the complete text.
// Tool call chunks are collected and stored in the result, but Execute is not
// called — use StreamText with callbacks for tool execution.
func (r *StreamTextResult) ReadAll() (string, error) {
	ctx := context.Background()
	stepCtx := ctx
	cancelStep := func() {}
	if r.initialStepCtx != nil {
		stepCtx = r.initialStepCtx
		if r.initialStepCancel != nil {
			cancelStep = r.initialStepCancel
		}
	} else if r.timeout != nil && r.timeout.HasPerStep() {
		stepCtx, cancelStep = r.timeout.CreateTimeoutContext(ctx, "step")
	}
	defer cancelStep()
	stepStart := time.Now()
	var firstTokenAt *time.Time
	var previousOutputChunkAt *time.Time
	var outputChunkGapsMs []int64
	firstChunk := true
	var pendingToolCalls []types.ToolCall
	var stepContent []types.ContentPart
	var stepReasoning []types.ReasoningContent
	var sawTerminal bool
	var sawOutput bool

	for {
		chunk, err := r.nextChunk(stepCtx)
		if err == io.EOF {
			break
		}
		if err != nil {
			if errors.Is(stepCtx.Err(), context.DeadlineExceeded) && r.timeout != nil && r.timeout.HasPerStep() {
				err = wrapTimeoutError(TimeoutReasonStep, stepCtx.Err())
			}
			r.err = err
			if isAbortErr(ctx, err) {
				telemetry.FireOnAbort(r.telemetryCtx, telemetry.TelemetryAbortEvent{
					Settings: r.telemetrySettings,
					CallID:   r.cbCallID,
					Reason:   err,
					Steps:    append([]types.StepResult(nil), r.cbSteps...),
				})
			}
			return "", err
		}

		if isModelOutputChunkType(chunk.Type) {
			sawOutput = true
		}

		if isOutputChunkForTiming(*chunk) {
			now := time.Now()
			if firstTokenAt == nil {
				firstTokenAt = &now
			} else if previousOutputChunkAt != nil {
				outputChunkGapsMs = append(outputChunkGapsMs, now.Sub(*previousOutputChunkAt).Milliseconds())
			}
			previousOutputChunkAt = &now
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
			stepContent = appendTextPart(stepContent, chunk.Text)

			// Update partial output after each text chunk (with deduplication).
			if r.outputSpec != nil {
				partial, partialErr := r.outputSpec.parsePartialOutput(ctx, ParsePartialOutputOptions{
					Text: r.text,
				})
				if partialErr != nil {
					r.err = partialErr
					return "", partialErr
				}
				if partial != nil {
					newJSONStr, ok := partialOutputDedupKey(partial)
					if ok && newJSONStr != r.lastPartialJSON {
						r.lastPartialJSON = newJSONStr
						r.mu.Lock()
						r.partialOutput = partial
						r.mu.Unlock()
					}
				}
			}
		}

		// Collect tool call chunks.
		if chunk.Type == provider.ChunkTypeToolCall && chunk.ToolCall != nil {
			enriched := enrichToolCallMetadata([]types.ToolCall{*chunk.ToolCall}, r.cbTools)[0]
			chunk.ToolCall = &enriched
			pendingToolCalls = append(pendingToolCalls, enriched)
			stepContent = append(stepContent, types.ToolCallContent{
				ToolCallID:       enriched.ID,
				ToolName:         enriched.ToolName,
				Title:            enriched.Title,
				Input:            enriched.RawArguments,
				Arguments:        enriched.Arguments,
				ProviderExecuted: enriched.ProviderExecuted,
				ProviderMetadata: providerMetadataRaw(enriched.ProviderMetadata),
				ToolMetadata:     enriched.ToolMetadata,
				Dynamic:          enriched.Dynamic,
				Invalid:          enriched.Invalid,
				Error:            toolCallContentError(enriched.Error),
				ThoughtSignature: enriched.ThoughtSignature,
			})
		}
		if chunk.Type == provider.ChunkTypeToolResult && chunk.ToolResult != nil {
			stepContent = append(stepContent, toolResultContentFromToolResult(*chunk.ToolResult))
		}
		if chunk.Type == provider.ChunkTypeReasoning && (chunk.Text != "" || chunk.Reasoning != "") {
			reasoningText := chunk.Reasoning
			if reasoningText == "" {
				reasoningText = chunk.Text
			}
			stepContent = appendReasoningPart(stepContent, reasoningText)
			if n := len(stepReasoning); n > 0 {
				stepReasoning[n-1].Text += reasoningText
			} else {
				stepReasoning = append(stepReasoning, types.ReasoningContent{Text: reasoningText})
			}
		}

		// Update finish reason, usage, and context management
		if chunk.Type == provider.ChunkTypeFinish {
			sawTerminal = true
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
			stepContent = append(stepContent, *chunk.GeneratedFileContent)
		}
		if chunk.Type == provider.ChunkTypeSource && chunk.SourceContent != nil {
			r.sources = append(r.sources, *chunk.SourceContent)
			stepContent = append(stepContent, *chunk.SourceContent)
		}
		if chunk.Type == provider.ChunkTypeCustom && chunk.CustomContent != nil {
			stepContent = append(stepContent, *chunk.CustomContent)
		}
		if chunk.Type == provider.ChunkTypeReasoningFile && chunk.ReasoningFileContent != nil {
			stepContent = append(stepContent, *chunk.ReasoningFileContent)
		}
		if chunk.Type == provider.ChunkTypeResponseMetadata && chunk.ResponseMetadata != nil {
			r.responseHeaders = chunk.ResponseMetadata.Headers
			r.stepResponse = types.StepResponse{
				ID:        chunk.ResponseMetadata.ID,
				Timestamp: chunk.ResponseMetadata.Timestamp,
				ModelID:   chunk.ResponseMetadata.ModelID,
				Headers:   chunk.ResponseMetadata.Headers,
			}
		}
		if chunk.Type == provider.ChunkTypeError {
			sawTerminal = true
			if r.finishReason == "" {
				r.finishReason = types.FinishReasonError
			}
		}
	}

	if !sawTerminal {
		if !sawOutput {
			err := newIncompleteModelStreamError()
			r.err = err
			return "", err
		}
		r.finishReason = types.FinishReasonOther
	}

	// Store collected tool calls.
	telemetry.FireOnChunk(ctx, telemetry.TelemetryChunkEvent{
		Settings:  r.telemetrySettings,
		ChunkType: string(provider.ChunkTypeStreamFinish),
	})

	// Store collected tool calls.
	if len(pendingToolCalls) > 0 {
		stepContent = replaceToolCallContentParts(stepContent, pendingToolCalls)
		stepContent = replaceToolResultContentParts(stepContent, pendingToolCalls)
		r.mu.Lock()
		r.toolCalls = pendingToolCalls
		r.mu.Unlock()
	}
	responseMessages := providerutils.ConvertToResponseMessages(
		pendingToolCalls,
		stepContent,
		nil,
	)
	step := types.StepResult{
		CallID:           r.cbCallID,
		StepNumber:       0,
		Model:            types.StepModel{Provider: r.cbModelProvider, ModelID: r.cbModelID},
		Text:             r.text,
		Content:          stepContent,
		Reasoning:        stepReasoning,
		ReasoningText:    buildReasoningText(stepReasoning),
		ToolCalls:        pendingToolCalls,
		StaticToolCalls:  filterStaticToolCalls(pendingToolCalls),
		DynamicToolCalls: filterDynamicToolCalls(pendingToolCalls),
		FinishReason:     r.finishReason,
		Usage:            r.usage,
		Performance:      stepPerformance(stepStart, r.usage, firstTokenAt, outputChunkGapsMs),
		Sources:          r.sources,
		Files:            r.files,
		Request: types.StepRequest{
			Messages: includedRequestMessages(r.cbInclude.RequestMessages, r.cbMessages),
		},
		Response: types.StepResponse{
			ID:        r.stepResponse.ID,
			Timestamp: r.stepResponse.Timestamp,
			ModelID:   r.stepResponse.ModelID,
			Headers:   r.responseHeaders,
			Messages:  responseMessages,
		},
		ResponseMessages: responseMessages,
		ToolsContext:     r.cbToolsCtx,
		RuntimeContext:   r.cbRuntimeCtx,
	}
	r.mu.Lock()
	r.cbSteps = []types.StepResult{step}
	r.cbResponseMessages = responseMessages
	r.stepRequest = step.Request
	r.stepResponse = step.Response
	r.mu.Unlock()

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
		RuntimeContext: telemetryRuntimeContextWithSensitivity(r.telemetrySettings, r.cbRuntimeCtx, r.cbSensitiveRuntimeCtx),
		ToolsContext:   telemetryToolsContext(r.telemetrySettings, r.cbToolsCtx),
	})

	// Mark stream as done.
	r.mu.Lock()
	r.status = StreamStatusDone
	r.mu.Unlock()

	if r.cbOnEnd != nil {
		r.cbOnEnd(r)
	}
	Notify(ctx, OnFinishEvent{
		CallID:             r.cbCallID,
		StepNumber:         step.StepNumber,
		Model:              step.Model,
		ModelProvider:      step.Model.Provider,
		ModelID:            step.Model.ModelID,
		Text:               r.text,
		Reasoning:          step.Reasoning,
		ReasoningText:      step.ReasoningText,
		ToolCalls:          step.ToolCalls,
		StaticToolCalls:    step.StaticToolCalls,
		DynamicToolCalls:   step.DynamicToolCalls,
		ToolResults:        step.ToolResults,
		StaticToolResults:  step.StaticToolResults,
		DynamicToolResults: step.DynamicToolResults,
		FinishReason:       r.finishReason,
		RawFinishReason:    step.RawFinishReason,
		Usage:              step.Usage,
		Steps:              []types.StepResult{step},
		TotalUsage:         r.usage,
		Warnings:           r.warnings,
		Sources:            step.Sources,
		Files:              step.Files,
		ProviderMetadata:   step.ProviderMetadata,
		ResponseHeaders:    r.responseHeaders,
		Response: GenerateStepResponse{
			ID:       step.Response.ID,
			Headers:  r.responseHeaders,
			Messages: step.ResponseMessages,
			Body:     step.Response.Body,
		},
		ExperimentalContext: r.cbExperimentalCtx,
		RuntimeContext:      r.cbRuntimeCtx,
		ToolsContext:        r.cbToolsCtx,
	}, r.cbOnEndEvent)

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

func isOutputChunkForTiming(chunk provider.StreamChunk) bool {
	switch chunk.Type {
	case provider.ChunkTypeText:
		return chunk.Text != ""
	case provider.ChunkTypeReasoning:
		return chunk.Text != "" || chunk.Reasoning != ""
	case provider.ChunkTypeToolInputDelta:
		return chunk.Text != ""
	case provider.ChunkTypeFile,
		provider.ChunkTypeReasoningFile,
		provider.ChunkTypeToolCall:
		return true
	default:
		return false
	}
}

func isReasoningBoundaryChunk(chunkType provider.ChunkType) bool {
	return chunkType == provider.ChunkTypeReasoningStart || chunkType == provider.ChunkTypeReasoningEnd
}

func isAbortErr(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return ctx != nil && ctx.Err() != nil
}

func shouldSuppressReasoningBoundaries(sendReasoning *bool) bool {
	return sendReasoning == nil || !*sendReasoning
}

func includedRequestMessages(include bool, messages []types.Message) []types.Message {
	if !include {
		return nil
	}
	return append([]types.Message(nil), messages...)
}

// nextChunk reads the next chunk with optional per-chunk timeout
func (r *StreamTextResult) nextChunk(ctx context.Context) (*provider.StreamChunk, error) {
	if r.timeout == nil || !r.timeout.HasPerChunk() {
		if ctx == nil || ctx.Done() == nil {
			return r.stream.Next()
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		type chunkResult struct {
			chunk *provider.StreamChunk
			err   error
		}
		resultCh := make(chan chunkResult, 1)
		go func() {
			chunk, err := r.stream.Next()
			resultCh <- chunkResult{chunk: chunk, err: err}
		}()
		select {
		case result := <-resultCh:
			return result.chunk, result.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if ctx == nil {
		ctx = context.Background()
	}

	// If per-chunk and per-step/total contexts are both active, the first one
	// to expire wins.
	parentCtx := ctx
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	if parentCtx.Err() != nil {
		return nil, parentCtx.Err()
	}

	// Use per-chunk timeout
	chunkCtx, cancel := r.timeout.CreateTimeoutContext(parentCtx, "chunk")
	defer cancel()
	if chunkCtx == parentCtx && chunkCtx.Done() == nil {
		return r.stream.Next()
	}

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

func isModelOutputChunkType(chunkType provider.ChunkType) bool {
	switch chunkType {
	case provider.ChunkTypeFile,
		provider.ChunkTypeCustom,
		provider.ChunkTypeSource,
		provider.ChunkTypeTextStart,
		provider.ChunkTypeText,
		provider.ChunkTypeTextEnd,
		provider.ChunkTypeReasoningStart,
		provider.ChunkTypeReasoning,
		provider.ChunkTypeReasoningEnd,
		provider.ChunkTypeReasoningFile,
		provider.ChunkTypeToolInputStart,
		provider.ChunkTypeToolInputDelta,
		provider.ChunkTypeToolInputEnd,
		provider.ChunkTypeToolApprovalRequest,
		provider.ChunkTypeToolApprovalResponse,
		provider.ChunkTypeToolCall,
		provider.ChunkTypeToolResult,
		provider.ChunkTypeToolOutputDenied:
		return true
	default:
		return false
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

func partialOutputDedupKey(partial interface{}) (string, bool) {
	// TS parity: for text/string partial outputs, avoid JSON serialization on each chunk.
	if s, ok := partial.(string); ok {
		return s, true
	}
	newJSON, err := json.Marshal(partial)
	if err != nil {
		return "", false
	}
	return string(newJSON), true
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
