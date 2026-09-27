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
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	promptutils "github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// StreamTextOnErrorRetryEvent is the argument to StreamTextOptions.OnErrorRetry.
type StreamTextOnErrorRetryEvent struct {
	// Error is the normalized mid-stream provider error (typically a
	// *providererrors.StreamProviderError).
	Error error
}

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

	// InstructionMessages supplies the instructions as system messages (TS
	// instructions: SystemModelMessage | SystemModelMessage[]), preserving
	// per-message ProviderOptions. When non-empty it takes precedence over
	// Instructions and System. Every message must have the system role.
	InstructionMessages []types.Message

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

	// ExperimentalToolCallers configures which tools may call which other
	// tools (programmatic tool calling / code mode), and which tools stay
	// hidden from the model until discovered via ai.ToolSearch. Mirrors the
	// TypeScript SDK's experimental_toolCallers.
	ExperimentalToolCallers ExperimentalToolCallers

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

	// RepairToolCall attempts to repair tool calls that fail to parse because
	// the tool does not exist or its input is invalid. When it returns a call,
	// the repaired call is parsed again; (nil, nil) keeps the call invalid.
	RepairToolCall ToolCallRepairFunction

	// ExperimentalRepairToolCall is a deprecated alias for RepairToolCall.
	//
	// Deprecated: use RepairToolCall.
	ExperimentalRepairToolCall ToolCallRepairFunction

	// OnLanguageModelCallStart is called immediately before each provider
	// model call begins.
	OnLanguageModelCallStart OnLanguageModelCallStartCallback

	// ExperimentalOnLanguageModelCallStart is a deprecated alias for
	// OnLanguageModelCallStart.
	//
	// Deprecated: use OnLanguageModelCallStart.
	ExperimentalOnLanguageModelCallStart OnLanguageModelCallStartCallback

	// OnLanguageModelCallEnd is called after each provider model response is
	// normalized and parsed, before client-side tool execution.
	OnLanguageModelCallEnd OnLanguageModelCallEndCallback

	// ExperimentalOnLanguageModelCallEnd is a deprecated alias for
	// OnLanguageModelCallEnd.
	//
	// Deprecated: use OnLanguageModelCallEnd.
	ExperimentalOnLanguageModelCallEnd OnLanguageModelCallEndCallback

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
	// InitialStreamChunks are emitted before the provider stream. Higher-level
	// adapters use this for work resolved before the next model step while
	// preserving stream event ordering.
	InitialStreamChunks []provider.StreamChunk
	// OnEnd is called when the stream is fully consumed, with the full
	// *StreamTextResult (Text(), Usage(), ToolCalls(), etc. give the same
	// data TS's onEnd event carries). This is a Go-only convenience
	// signature that predates OnEndEvent below; it is NOT the same field as
	// GenerateTextOptions.OnEnd (whose signature is
	// func(ctx, *GenerateTextResult, userContext) — StreamText's per-step
	// and per-call analogues of that tuple shape are OnStepEnd below and
	// OnEndEvent/OnFinishEvent, since a StreamTextResult is a different Go
	// type from a GenerateTextResult and can't share that field's type).
	// For code that wants the same shared, TS-parity event struct that
	// GenerateText/Agent use, prefer OnEndEvent.
	OnEnd func(result *StreamTextResult)
	// OnFinish is called when the stream is fully consumed.
	//
	// Deprecated: use OnEnd.
	OnFinish func(result *StreamTextResult)

	// OnStepEnd is called after each step completes, mirroring
	// GenerateTextOptions.OnStepEnd exactly (same func type, since
	// types.StepResult — unlike *GenerateTextResult/*StreamTextResult — is
	// shared between generate and stream). TS shares one onStepEnd callback
	// type between generateText and streamText; this is the Go equivalent.
	OnStepEnd func(ctx context.Context, step types.StepResult, userContext interface{})
	// OnStepFinish is called after each step completes.
	//
	// Deprecated: use OnStepEnd.
	OnStepFinish func(ctx context.Context, step types.StepResult, userContext interface{})

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

	// StreamRetries is the maximum number of automatic retries for ANY
	// provider error chunk received after streaming has already started
	// (a mid-stream error chunk). Matching TS (stream-text.ts's
	// `automaticStreamRetryCount < streamRetries`), this budget is
	// consulted unconditionally — it does not check whether the
	// normalized providererrors.StreamProviderError considers itself
	// retryable; that classification is informational, for OnError/
	// OnErrorRetry to build their own heuristics on.
	//
	// nil (the option omitted entirely) disables ALL stream retry
	// behavior, both automatic and OnErrorRetry-directed, and preserves
	// pre-streamRetries incremental chunk delivery for existing OnError
	// observers (TS: "Omit this option to disable all stream retry
	// behavior and preserve incremental tool streaming for existing
	// onError observers"). An explicit 0 disables automatic retries but
	// still allows OnErrorRetry to request one retry (TS
	// canRetryStreamViaOnError = streamRetries !== undefined &&
	// onErrorArg != null). A negative value is rejected synchronously by
	// StreamText (TS "streamRetries must be >= 0").
	//
	// A ToolChoiceViolationError is never retried, automatically or via
	// OnErrorRetry (TS stream-text.ts isToolChoiceViolation check). Audit
	// row 802af1e / WG8.
	StreamRetries *int

	// OnErrorRetry is called for a mid-stream provider error (after
	// OnError) and, if StreamRetries permits a retry, may request one by
	// returning true. It is consulted only when StreamRetries was
	// explicitly set (even to 0) and no automatic retry applies
	// (StreamRetries exhausted), and is honored at most once per
	// streamText call, matching TS's StreamTextOnErrorRetryCallback
	// semantics.
	OnErrorRetry func(ctx context.Context, event StreamTextOnErrorRetryEvent) bool

	// OnAbort is called when streaming is aborted by context cancellation or
	// deadline before normal completion.
	//
	// Deprecated: use OnAbortEvent, which also carries the call ID and abort
	// reason.
	OnAbort func(ctx context.Context, steps []types.StepResult)

	// OnAbortEvent is called when streaming is aborted by context
	// cancellation or deadline before normal completion, with a
	// GenerateTextAbortEvent carrying the call ID, completed steps, and
	// abort reason. Takes precedence over the deprecated OnAbort.
	OnAbortEvent OnAbortCallback

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
//
// A transform that needs to emit chunks incrementally rather than all at
// once when it returns (e.g. SmoothStream, which paces matches out with a
// delay between them) can retrieve an emitter via
// StreamTransformEmitterFromContext and call it as each chunk becomes ready;
// stream.go forwards every emitted chunk (onChunk + telemetry + stream
// consumers) the instant it is emitted, rather than waiting for the
// transform call to return. The function's own return value is still
// forwarded afterward and should contain only chunks not already emitted —
// when a transform emits everything itself, it can safely return nil.
type StreamTransformFunc func(ctx context.Context, chunk provider.StreamChunk) []provider.StreamChunk

// StreamTransformEmitter emits a single stream chunk immediately, ahead of
// the enclosing StreamTransformFunc call returning. See StreamTransformFunc.
type StreamTransformEmitter func(provider.StreamChunk)

type streamTransformEmitterContextKey struct{}

// WithStreamTransformEmitter returns a context carrying emit, retrievable by
// a StreamTransformFunc via StreamTransformEmitterFromContext. processStream
// installs this around every ExperimentalTransform call.
func WithStreamTransformEmitter(ctx context.Context, emit StreamTransformEmitter) context.Context {
	return context.WithValue(ctx, streamTransformEmitterContextKey{}, emit)
}

// StreamTransformEmitterFromContext retrieves the emitter installed by
// WithStreamTransformEmitter, if any. ok is false outside of a
// StreamTransformFunc call (or when the caller invoked the transform
// directly without installing one), in which case the transform should fall
// back to returning chunks in a batch.
func StreamTransformEmitterFromContext(ctx context.Context) (emit StreamTransformEmitter, ok bool) {
	emit, ok = ctx.Value(streamTransformEmitterContextKey{}).(StreamTransformEmitter)
	return emit, ok
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

	// stepReopenModel/stepReopenGenOpts/stepReopenCtx record the exact model
	// call used to open the CURRENT step's stream, so a retryable mid-stream
	// provider error (streamRetries, audit row 802af1e / WG8) can re-issue
	// the same call. Only ever read/written by the single background
	// goroutine that runs bootstrapAndStream+processStream, so (unlike most
	// StreamTextResult fields) these need no mutex.
	stepReopenModel   provider.LanguageModel
	stepReopenGenOpts *provider.GenerateOptions
	stepReopenCtx     context.Context

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

	// hasPublishedPartialLegacy tracks whether readAllLegacy has published a
	// partial yet, distinguishing that from lastPartialJSON's zero value so a
	// genuine first empty-string/null partial is not suppressed (audit row
	// 84f5d1b / WG4). Only the multi-step path (processStream) is used in
	// practice; this exists so readAllLegacy compiles against the same
	// outputProcessor.parsePartialOutput contract.
	hasPublishedPartialLegacy bool

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
	cbOnStepEnd           func(ctx context.Context, step types.StepResult, userContext interface{})
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
	// cbToolChoice carries the current step's effective tool choice across
	// into processStream, for TelemetryStepStartEvent.ToolChoice (152c67c)
	// and the tool-choice violation check.
	cbToolChoice       types.ToolChoice
	cbInclude          IncludeOptions
	cbSteps            []types.StepResult
	cbResponseMessages []types.Message
	// initialResponseMessages holds the tool message produced by resuming
	// tool approvals from the input messages (prepended to ResponseMessages).
	initialResponseMessages []types.Message
	// resumeChunksRemaining counts the resumed-approval chunks at the head of
	// the stream; they are forwarded but not accumulated into step content.
	resumeChunksRemaining int
	cbExperimentalSandbox interface{}
	// Snapshot of the initial messages and tools for event population
	cbMessages []types.Message
	cbTools    []types.Tool
	cbSystem   string

	// cbExecutionTools is the tool set actually invoked when a tool call
	// arrives (bound local tool callers), as opposed to cbTools (the
	// model-visible set, which may carry a caller's stable unbound
	// definition instead). Equal to cbTools unless tool callers are
	// configured. See PrepareToolsForToolCallers.
	cbExecutionTools []types.Tool

	// cbInstructionMessages is the current step's instructions, when given as
	// system messages (TS instructions: SystemModelMessage[]). Updated by
	// processStream at the start of each continuation step, mirroring
	// cbSystem.
	cbInstructionMessages []types.Message
	// cbInitialInstructionMessages are the instruction messages originally
	// passed to StreamText, fixed for the lifetime of the call.
	cbInitialInstructionMessages []types.Message

	// cbModel and cbStreamOpts are retained so that processStream can start
	// additional streaming steps when deferred provider tool results are pending.
	cbModel      provider.LanguageModel
	cbStreamOpts StreamTextOptions

	// chunkBuf backs Stream()/Chunks() with a replayable copy of every chunk
	// processStream forwards, so callers can consume the full multi-step
	// stream without racing processStream's own consumption of the raw
	// per-step provider stream. nil for StreamTextResult values built by hand
	// (e.g. in tests exercising the lower-level stream helpers directly),
	// which fall back to the raw stream field.
	chunkBuf *chunkBuffer
	// chunkBufReader is the single cursor over chunkBuf handed out by
	// Stream(); it is created lazily on first use and memoized (guarded by
	// mu) so that repeated Stream() calls resume the same cursor rather than
	// restarting from the first chunk, matching Stream()'s historical
	// contract of returning one shared stream for the lifetime of the
	// result.
	chunkBufReader *chunkBufferReader

	processingDone chan struct{}

	// cancelBootstrap cancels bootstrapAndStream's context. It lets Close()
	// interrupt in-flight bootstrap work (resuming approved tool calls, or
	// making the first provider stream request) that hasn't produced a
	// stream yet — see the StreamText/bootstrapAndStream split below.
	cancelBootstrap context.CancelFunc

	// resolvedToolCallers and toolSearchState are resolved once,
	// synchronously, in StreamText and reused for every step (initial and
	// continuation) by bootstrapAndStream/processStream.
	resolvedToolCallers ResolvedToolCallers
	toolSearchState     *ToolSearchState
}

// StreamText performs streaming text generation.
//
// It returns almost immediately, matching TS streamText(): that is a plain
// synchronous function (packages/ai/src/generate-text/stream-text.ts) that
// constructs a DefaultStreamTextResult and hands it back to the caller before
// any I/O happens; everything else — normalizing the prompt, firing OnStart,
// resuming approved tool calls from the input history, and making the first
// provider stream request — runs in an async IIFE afterward. Go mirrors that
// with bootstrapAndStream, running in the background: a slow resumed tool
// call or a slow model connection no longer blocks the caller from getting a
// *StreamTextResult back (review finding F6 — approval resume must not run
// before StreamText returns).
func StreamText(ctx context.Context, opts StreamTextOptions) (*StreamTextResult, error) {
	// Validate options. These are pure, in-memory checks with no I/O — the Go
	// equivalent of the synchronous `prepareRetries` throw inside TS's
	// DefaultStreamTextResult constructor, which happens before its async
	// work begins.
	if opts.Model == nil {
		return nil, fmt.Errorf("model is required")
	}
	if err := validateMaxRetries(opts.MaxRetries); err != nil {
		return nil, err
	}
	if err := validateStreamRetries(opts.StreamRetries); err != nil {
		return nil, err
	}
	instructionMessages := cloneInstructionMessages(opts.InstructionMessages)
	if err := validateInstructionMessages(instructionMessages); err != nil {
		return nil, err
	}
	// Resolved synchronously, matching TS's DefaultStreamTextResult
	// constructor (which runs synchronously before the async IIFE starts).
	resolvedToolCallers, err := ResolveToolCallerConfiguration(opts.Tools, opts.ExperimentalToolCallers)
	if err != nil {
		return nil, err
	}
	toolSearchState, err := NewToolSearchState(opts.Tools, resolvedToolCallers)
	if err != nil {
		return nil, err
	}

	bootstrapCtx, cancelBootstrap := context.WithCancel(ctx)
	result := &StreamTextResult{
		status:              StreamStatusSubmitted, // actively streaming; set before any chunks arrive
		timeout:             opts.Timeout,
		cancelBootstrap:     cancelBootstrap,
		resolvedToolCallers: resolvedToolCallers,
		toolSearchState:     toolSearchState,
	}
	result.chunkBuf = newChunkBuffer()
	result.processingDone = make(chan struct{})

	go result.bootstrapAndStream(bootstrapCtx, opts, instructionMessages)

	return result, nil
}

// bootstrapAndStream runs everything that TS's streamText() does inside its
// async IIFE before the first step's stream loop starts: normalizing the
// prompt, firing OnStart, resuming approved tool calls from the input
// history, running PrepareStep and firing OnStepStart for the first step,
// and making the first provider stream request. On success it hands off to
// processStream; on failure it fails the result the same way a mid-stream
// error does (via fail), so Err()/ReadAll()/Stream() behave identically
// regardless of which phase produced the error — matching TS, where none of
// this can make streamText() itself throw or return a rejected promise.
func (r *StreamTextResult) bootstrapAndStream(ctx context.Context, opts StreamTextOptions, instructionMessages []types.Message) {
	// Release the bootstrap context once all processing is done, so callers
	// that never call Close() don't leave it registered on a long-lived
	// parent context. processStream runs synchronously below, so nothing
	// uses ctx after this returns. Close() may also call it; that's a no-op.
	defer r.cancelBootstrap()
	telemetrySettings := effectiveTelemetrySettings(opts.Telemetry, opts.ExperimentalTelemetry)
	runtimeContext := effectiveRuntimeContext(opts.RuntimeContext, opts.ExperimentalContext)
	system := effectiveSystem(opts.System, opts.Instructions)
	if len(instructionMessages) > 0 {
		// System-message instructions take precedence over text instructions.
		system = ""
	}
	// repairToolCall and onLanguageModelCallEnd are only needed once tool
	// call chunks / finish chunks arrive, which happens inside processStream
	// (which now handles every step, including the first — see below);
	// onLanguageModelCallStart is used both here (for the first step) and in
	// processStream (for continuation steps).
	onLanguageModelCallStart := firstLMCallStart(opts.OnLanguageModelCallStart, opts.ExperimentalOnLanguageModelCallStart)
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
	telSystem := ""
	var telMessages []types.Message
	if telemetrySettings != nil && telemetrySettings.RecordInputs {
		telSystem = system
		if len(instructionMessages) > 0 {
			telSystem = instructionMessagesText(instructionMessages)
		}
		// See the matching comment in generate.go: TS's
		// GenerateTextStartEvent.messages is always the normalized message
		// list, even for the `prompt` string convenience.
		telMessages = buildPrompt(opts.Prompt, opts.Messages, "").Messages
	}
	streamTextMaxRetries := preparedMaxRetries(opts.MaxRetries)
	ctx = telemetry.FireOnStart(ctx, telemetry.TelemetryStartEvent{
		OperationType:    "ai.streamText",
		ModelProvider:    opts.Model.Provider(),
		ModelID:          opts.Model.ModelID(),
		Settings:         telemetrySettings,
		System:           telSystem,
		Messages:         telMessages,
		Headers:          opts.Headers,
		MaxOutputTokens:  opts.MaxTokens,
		Temperature:      opts.Temperature,
		TopP:             opts.TopP,
		TopK:             opts.TopK,
		PresencePenalty:  opts.PresencePenalty,
		FrequencyPenalty: opts.FrequencyPenalty,
		StopSequences:    opts.StopSequences,
		Seed:             opts.Seed,
		MaxRetries:       &streamTextMaxRetries,
		RuntimeContext:   telemetryRuntimeContextWithSensitivity(telemetrySettings, runtimeContext, opts.SensitiveRuntimeContext),
		ToolsContext:     telemetryToolsContext(telemetrySettings, toolsContext),
	})
	telemetryCtx := ctx // snapshot ctx with embedded spans before timeout wrapping

	// Apply total timeout if configured. This cancel must live for the
	// lifetime of this goroutine — which now always runs processStream to
	// completion — not for the lifetime of the StreamText() call, which
	// returns almost immediately (previously this defer fired essentially
	// immediately after starting the background goroutine, cancelling the
	// total-timeout context before step 1 could stream anything).
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
		r.fail(normErr)
		return
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
	onStepEndSimple := opts.OnStepEnd
	if onStepEndSimple == nil {
		onStepEndSimple = opts.OnStepFinish
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
		Provider:            opts.Model.Provider(),
		ModelProvider:       opts.Model.Provider(),
		ModelID:             opts.Model.ModelID(),
		Instructions:        system,
		InstructionMessages: instructionMessages,
		System:              system,
		Prompt:              opts.Prompt,
		Messages:            opts.Messages,
		Tools:               opts.Tools,
		ActiveTools:         opts.ActiveTools,
		ToolOrder:           opts.ToolOrder,
		ToolChoice:          opts.ToolChoice,
		Timeout:             opts.Timeout,
		Reasoning:           opts.Reasoning,
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

	// Resume tool approvals from the input messages before the first step
	// (TS streamText initial tool execution stream).
	var resumeUsage types.Usage
	resumed, resumeErr := resumeToolApprovals(ctx, toolApprovalResumeOptions{
		messages:       prompt.Messages,
		tools:          opts.Tools,
		toolApproval:   opts.ToolApproval,
		toolsContext:   toolsContext,
		runtimeContext: runtimeContext,
		secret:         opts.ExperimentalToolApprovalSecret,
		refine:         opts.ExperimentalRefineToolInput,
		usage:          &resumeUsage,
		streaming:      true,
		callbacks: toolCallEventCallbacks{
			callID:              callID,
			onStart:             opts.OnToolExecutionStart,
			onFinish:            opts.OnToolExecutionEnd,
			fallbackStart:       opts.OnToolCallStart,
			fallbackFinish:      opts.OnToolCallFinish,
			modelProvider:       opts.Model.Provider(),
			modelID:             opts.Model.ModelID(),
			messages:            prompt.Messages,
			experimentalContext: runtimeContext,
			runtimeContext:      runtimeContext,
			toolsContext:        toolsContext,
			functionID:          cbFuncID,
			metadata:            cbMeta,
			timeout:             opts.Timeout,
			telemetrySettings:   telemetrySettings,
			experimentalSandbox: opts.ExperimentalSandbox,
		},
	})
	if resumeErr != nil {
		// TS runs approval resume inside streamText's output stream, so a
		// failure there (e.g. an invalid approval signature) surfaces as a
		// stream error rather than a synchronous StreamText() error — the
		// caller already has this *StreamTextResult and reads the error via
		// Err()/ReadAll()/Stream(), matching review finding F6.
		telemetry.FireOnError(telemetryCtx, telemetry.TelemetryErrorEvent{Settings: telemetrySettings, Error: resumeErr})
		r.fail(resumeErr)
		return
	}
	initialResponseMessages := resumed.responseMessages

	stepModel := opts.Model
	stepSystem := system
	stepMessages := prompt.Messages
	if len(initialResponseMessages) > 0 {
		stepMessages = append(append([]types.Message(nil), prompt.Messages...), initialResponseMessages...)
	}
	stepTools := FilterActiveTools(opts.Tools, opts.ActiveTools)
	stepToolChoice := opts.ToolChoice
	stepToolOrder := opts.ToolOrder
	stepProviderOptions := opts.ProviderOptions
	stepSandbox := opts.ExperimentalSandbox
	stepInstructionMessages := instructionMessages
	// Per-step call-setting overrides (audit row 60f97f6 / WG-STEP); see the
	// analogous comment in generate.go's GenerateText.
	stepMaxTokens := opts.MaxTokens
	stepTemperature := opts.Temperature
	stepTopP := opts.TopP
	stepTopK := opts.TopK
	stepPresencePenalty := opts.PresencePenalty
	stepFrequencyPenalty := opts.FrequencyPenalty
	stepStopSequences := opts.StopSequences
	stepSeed := opts.Seed
	stepReasoningLevel := opts.Reasoning
	if opts.PrepareStep != nil {
		prepared := opts.PrepareStep(ctx, PrepareStepOptions{
			Model:                      stepModel,
			System:                     stepSystem,
			Instructions:               &stepSystem,
			InitialInstructions:        &system,
			InstructionMessages:        cloneInstructionMessages(stepInstructionMessages),
			InitialInstructionMessages: cloneInstructionMessages(instructionMessages),
			Messages:                   append([]types.Message(nil), stepMessages...),
			InitialMessages:            append([]types.Message(nil), prompt.Messages...),
			ResponseMessages:           responseMessagesWithInitial(initialResponseMessages, nil),
			UserContext:                runtimeContext,
			RuntimeContext:             runtimeContext,
			ToolsContext:               toolsContext,
			StepNumber:                 0,
			Steps:                      nil,
			Tools:                      stepTools,
			ToolChoice:                 stepToolChoice,
			ActiveTools:                opts.ActiveTools,
			ToolOrder:                  stepToolOrder,
			ProviderOptions:            stepProviderOptions,
			ExperimentalSandbox:        stepSandbox,
			MaxOutputTokens:            stepMaxTokens,
			Temperature:                stepTemperature,
			TopP:                       stepTopP,
			TopK:                       stepTopK,
			PresencePenalty:            stepPresencePenalty,
			FrequencyPenalty:           stepFrequencyPenalty,
			StopSequences:              stepStopSequences,
			Seed:                       stepSeed,
			Reasoning:                  stepReasoningLevel,
		})
		if prepared.Model != nil {
			stepModel = prepared.Model
		}
		if len(prepared.InstructionMessages) > 0 {
			if err := validateInstructionMessages(prepared.InstructionMessages); err != nil {
				r.fail(err)
				return
			}
			stepInstructionMessages = cloneInstructionMessages(prepared.InstructionMessages)
			stepSystem = ""
		} else if prepared.Instructions != nil {
			stepSystem = *prepared.Instructions
			stepInstructionMessages = nil
		} else if prepared.System != "" {
			stepSystem = prepared.System
			stepInstructionMessages = nil
		} else if prepared.InstructionMessages != nil {
			stepInstructionMessages = nil
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
		if prepared.MaxOutputTokens != nil {
			stepMaxTokens = prepared.MaxOutputTokens
		}
		if prepared.Temperature != nil {
			stepTemperature = prepared.Temperature
		}
		if prepared.TopP != nil {
			stepTopP = prepared.TopP
		}
		if prepared.TopK != nil {
			stepTopK = prepared.TopK
		}
		if prepared.PresencePenalty != nil {
			stepPresencePenalty = prepared.PresencePenalty
		}
		if prepared.FrequencyPenalty != nil {
			stepFrequencyPenalty = prepared.FrequencyPenalty
		}
		if prepared.StopSequences != nil {
			stepStopSequences = prepared.StopSequences
		}
		if prepared.Seed != nil {
			stepSeed = prepared.Seed
		}
		if prepared.Reasoning != nil {
			stepReasoningLevel = prepared.Reasoning
		}
	}

	// Apply deferred tool discovery (ai.ToolSearch) and tool-caller routing
	// (ExperimentalToolCallers). stepTools becomes the model-visible set;
	// stepExecutionTools is used to look up the tool actually invoked.
	stepTools = r.toolSearchState.Apply(stepTools, toolsContext, stepSandbox)
	stepExecutionTools, modelTools, toolCallerMessages := PrepareToolsForToolCallers(stepTools, r.resolvedToolCallers)
	stepTools = modelTools
	if len(toolCallerMessages) > 0 {
		stepMessages = AppendToolCallerMessages(stepMessages, toolCallerMessages)
	}

	stepTools = resolveStepTools(ctx, stepTools, toolsContext, stepSandbox)
	stepTools = orderStepTools(stepTools, stepToolOrder)
	stepExecutionTools = resolveStepTools(ctx, stepExecutionTools, toolsContext, stepSandbox)
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
		Provider:            stepModel.Provider(),
		ModelProvider:       stepModel.Provider(),
		ModelID:             stepModel.ModelID(),
		Instructions:        stepSystem,
		InstructionMessages: stepInstructionMessages,
		System:              stepSystem,
		Messages:            stepMessages,
		Tools:               stepTools,
		ToolChoice:          stepToolChoice,
		ActiveTools:         opts.ActiveTools,
		ToolOrder:           stepToolOrder,
		ProviderOptions:     stepProviderOptions,
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
				r.fail(fmt.Errorf("output.ResponseFormat failed: %w", rfErr))
				return
			}
			responseFormat = rf
		}
	}

	stepPrompt, normErr := promptutils.NormalizePromptWithDownloadSupport(stepCtx, types.Prompt{System: appendSandboxDescription(stepSystem, stepSandbox), Messages: messagesForModel(stepMessages)}, allowSystem, effectiveDownload(opts.ExperimentalDownload), supportedURLChecker(stepModel))
	if normErr != nil {
		if stepCancel != nil {
			stepCancel()
		}
		telemetry.FireOnError(telemetryCtx, telemetry.TelemetryErrorEvent{Settings: telemetrySettings, Error: normErr})
		r.fail(fmt.Errorf("prompt normalization failed: %w", normErr))
		return
	}
	stepPrompt = prependInstructionMessages(stepPrompt, stepInstructionMessages)

	// Build generate options
	genOpts := &provider.GenerateOptions{
		Prompt:                stepPrompt,
		AllowSystemMessages:   allowSystem,
		AllowSystemInMessages: allowSystem,
		Temperature:           stepTemperature,
		MaxTokens:             stepMaxTokens,
		TopP:                  stepTopP,
		TopK:                  stepTopK,
		FrequencyPenalty:      stepFrequencyPenalty,
		PresencePenalty:       stepPresencePenalty,
		StopSequences:         stepStopSequences,
		Seed:                  stepSeed,
		Headers:               opts.Headers,
		Tools:                 stepTools,
		ToolChoice:            stepToolChoice,
		IncludeRawChunks:      includeRawChunksValue(include),
		RuntimeContext:        runtimeContext,
		ToolsContext:          toolsContext,
		ResponseFormat:        responseFormat,
		Reasoning:             stepReasoningLevel,
		SendReasoning:         opts.SendReasoning,
		ProviderOptions:       stepProviderOptions,
		Telemetry:             telemetrySettings,
	}

	Notify(stepCtx, LanguageModelCallStartEvent{
		CallID:              callID,
		Provider:            stepModel.Provider(),
		ModelID:             stepModel.ModelID(),
		Instructions:        stepSystem,
		InstructionMessages: stepInstructionMessages,
		Messages:            stepMessages,
		Tools:               genOpts.Tools,
		MaxOutputTokens:     genOpts.MaxTokens,
		Temperature:         genOpts.Temperature,
		TopP:                genOpts.TopP,
		TopK:                genOpts.TopK,
		PresencePenalty:     genOpts.PresencePenalty,
		FrequencyPenalty:    genOpts.FrequencyPenalty,
		StopSequences:       genOpts.StopSequences,
		Seed:                genOpts.Seed,
		Reasoning:           genOpts.Reasoning,
	}, onLanguageModelCallStart)
	// Scoped to just this call (594029e): the model call runs inside the
	// returned ctx, which embeds the telemetry integration's "chat" span when
	// one is registered, so the provider's own DoStream/HTTP spans become its
	// children. stepCtx itself is unchanged for subsequent chunk processing
	// and tool execution, which are parented under the step span instead.
	modelCallCtx := telemetry.FireOnLanguageModelCallStart(stepCtx, telemetry.LanguageModelCallStartEvent{
		Settings:         telemetrySettings,
		CallID:           callID,
		ModelProvider:    stepModel.Provider(),
		ModelID:          stepModel.ModelID(),
		Prompt:           genOpts.Prompt,
		Tools:            genOpts.Tools,
		System:           stepSystem,
		Temperature:      genOpts.Temperature,
		MaxOutputTokens:  genOpts.MaxTokens,
		TopP:             genOpts.TopP,
		TopK:             genOpts.TopK,
		PresencePenalty:  genOpts.PresencePenalty,
		FrequencyPenalty: genOpts.FrequencyPenalty,
		StopSequences:    genOpts.StopSequences,
		Seed:             genOpts.Seed,
	})

	// Start streaming
	r.stepReopenModel = stepModel
	r.stepReopenGenOpts = genOpts
	r.stepReopenCtx = modelCallCtx
	stream, err := doStreamWithGatewayRetry(modelCallCtx, stepModel, genOpts, opts.MaxRetries)
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
			reason := abortReason(stepCtx, err)
			if onAbort := firstOnAbort(opts.OnAbortEvent, opts.OnAbort); onAbort != nil {
				onAbort(stepCtx, GenerateTextAbortEvent{CallID: callID, Reason: reason})
			}
			telemetry.FireOnAbort(telemetryCtx, telemetry.TelemetryAbortEvent{Settings: telemetrySettings, CallID: callID, Reason: reason})
		} else {
			telemetry.FireOnError(telemetryCtx, telemetry.TelemetryErrorEvent{Settings: telemetrySettings, Error: err})
		}
		r.fail(fmt.Errorf("failed to start stream: %w", err))
		return
	}

	// Create result
	if len(opts.InitialStreamChunks) > 0 || len(resumed.chunks) > 0 {
		prefix := make([]provider.StreamChunk, 0, len(resumed.chunks)+len(opts.InitialStreamChunks))
		prefix = append(prefix, resumed.chunks...)
		prefix = append(prefix, opts.InitialStreamChunks...)
		stream = &prefixedTextStream{prefix: prefix, base: stream}
	}

	// r.stream and r.initialStepCtx/Cancel are set through synchronized
	// setters (not plain field assignment) because Close() may already have
	// been called concurrently, from the caller, while this bootstrap work
	// was still in flight — see setStream/setInitialStep and Close() below.
	r.setStream(stream)
	r.setInitialStep(stepCtx, stepCancel)
	r.telemetryCtx = telemetryCtx
	r.telemetrySettings = telemetrySettings
	r.outputSpec = outputSpec
	// Structured event callbacks
	r.cbCallID = callID
	r.cbOnEnd = onEnd
	r.cbOnStepEnd = onStepEndSimple
	r.cbOnStepFinishEvent = onStepEndEvent
	r.cbOnEndEvent = onEndEvent
	r.cbOnToolCallStart = opts.OnToolExecutionStart
	r.cbOnToolCallFinish = opts.OnToolExecutionEnd
	r.cbFuncID = cbFuncID
	r.cbMeta = cbMeta
	r.cbModelProvider = stepModel.Provider()
	r.cbModelID = stepModel.ModelID()
	r.cbExperimentalCtx = runtimeContext
	r.cbRuntimeCtx = runtimeContext
	r.cbSensitiveRuntimeCtx = opts.SensitiveRuntimeContext
	r.cbToolsCtx = toolsContext
	r.cbToolChoice = stepToolChoice
	r.cbInclude = include
	r.cbMessages = stepMessages
	r.cbTools = stepTools
	r.cbExecutionTools = stepExecutionTools
	r.cbSystem = stepSystem
	r.cbInstructionMessages = stepInstructionMessages
	r.cbInitialInstructionMessages = cloneInstructionMessages(instructionMessages)
	r.cbExperimentalSandbox = stepSandbox
	// Retained for deferred provider tool continuation.
	r.cbModel = stepModel
	r.cbStreamOpts = opts
	// Resumed approval outputs precede the first step.
	r.initialResponseMessages = initialResponseMessages
	r.cbResponseMessages = responseMessagesWithInitial(initialResponseMessages, nil)
	r.resumeChunksRemaining = len(resumed.chunks)

	// Always run the multi-step processing loop, matching the TypeScript SDK:
	// streamText's transform stream (which executes tools and continues the
	// step loop) runs unconditionally, regardless of whether the caller
	// registered any callbacks. Previously, Stream()/Chunks()/ReadAll() read
	// the raw single-step provider stream directly when no callback was set,
	// which meant tool calls were never executed and later steps never ran.
	//
	// chunkBuf/processingDone were already created synchronously in
	// StreamText, before this goroutine started, so Stream()/Chunks()/
	// ReadAll() work correctly even if called before bootstrap reaches this
	// point; they give every consumption path a replayable view of the same
	// fully-processed chunk sequence that processStream produces, so they
	// never race with processStream's own reads of the raw per-step stream.
	userOnChunk := opts.OnChunk
	combinedOnChunk := func(c provider.StreamChunk) {
		r.chunkBuf.push(c)
		if userOnChunk != nil {
			safeInvoke(func() { userOnChunk(c) })
		}
	}
	r.processStream(ctx, combinedOnChunk, onEnd)
}

// fail records a failure that happened before the per-step stream loop ever
// started (prompt normalization, resuming approved tool calls, PrepareStep,
// or making the first provider stream request) and finalizes the result the
// same way a mid-stream failure does (processStream's own deferred cleanup),
// so Err()/ReadAll()/Stream() behave identically regardless of which phase
// produced the error.
func (r *StreamTextResult) fail(err error) {
	r.err = err
	r.mu.Lock()
	r.status = StreamStatusDone
	r.mu.Unlock()
	if r.chunkBuf != nil {
		r.chunkBuf.close(err)
	}
	if r.processingDone != nil {
		close(r.processingDone)
	}
}

// setStream and currentStream synchronize the stream field against Close(),
// which may run concurrently with bootstrapAndStream before it has obtained
// a stream (e.g. Close() called immediately after StreamText returns, while
// the first provider request is still in flight).
func (r *StreamTextResult) setStream(s provider.TextStream) {
	r.mu.Lock()
	r.stream = s
	r.mu.Unlock()
}

func (r *StreamTextResult) currentStream() provider.TextStream {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stream
}

// setInitialStep synchronizes the first step's timeout context/cancel
// against Close() for the same reason as setStream.
func (r *StreamTextResult) setInitialStep(ctx context.Context, cancel context.CancelFunc) {
	r.mu.Lock()
	r.initialStepCtx = ctx
	r.initialStepCancel = cancel
	r.mu.Unlock()
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
	if r.chunkBuf != nil {
		defer func() { r.chunkBuf.close(r.err) }()
	}
	opts := r.cbStreamOpts
	currentMessages := r.cbMessages
	currentTools := append([]types.Tool(nil), r.cbTools...)
	currentExecutionTools := append([]types.Tool(nil), r.cbExecutionTools...)
	repairToolCall := effectiveRepairToolCall(opts.RepairToolCall, opts.ExperimentalRepairToolCall)
	onLanguageModelCallStart := firstLMCallStart(opts.OnLanguageModelCallStart, opts.ExperimentalOnLanguageModelCallStart)
	onLanguageModelCallEnd := firstLMCallEnd(opts.OnLanguageModelCallEnd, opts.ExperimentalOnLanguageModelCallEnd)
	stopConditions := resolveStopConditions(opts.StopWhen, opts.MaxSteps)
	// pendingDeferredToolCalls tracks provider tools (SupportsDeferredResults=true) whose
	// results haven't arrived yet. Key = toolCallID, value = toolName.
	pendingDeferredToolCalls := make(map[string]string)

	var allSteps []types.StepResult
	// currentToolChoice tracks the tool choice actually used to open each
	// step's stream (bootstrapAndStream's resolved value, including any
	// PrepareStep override, for step 1; then whatever PrepareStep resolves
	// for later steps below), so the tool-choice-enforcement check can
	// compare each step's tool calls against the choice that produced them
	// (audit rows 8b6b756/36b3364/ccf98e7, WG3).
	currentToolChoice := r.cbToolChoice
	// lastStepText holds the most recently completed step's own text (not
	// the all-steps concatenation in accumulatedTextParts/r.text), so the
	// final structured-output parse below uses only the step that actually
	// produced it, matching TS stream-text.ts (audit row 2a5ed55 / WG4).
	var lastStepText string
	firstChunkEver := true
	suppressReasoningBoundaries := shouldSuppressReasoningBoundaries(opts.SendReasoning)
	var accumulatedTextParts []string
	// usedTextIDs/usedReasoningIDs track every text/reasoning block ID used
	// across ALL steps of this call, so a later step reusing an ID a prior
	// step already used (many providers restart block IDs like "0" every
	// step) gets remapped to a fresh ID instead of colliding in the merged
	// full stream and in the UI message reducer's per-ID part map (audit row
	// c6d57f3 / WG5 #113). stepTextIDRemap/stepReasoningIDRemap hold the
	// current step's original-ID -> remapped-ID mapping and are reset at the
	// start of every step (a fresh model call may reuse "0" safely from that
	// step's own perspective; only cross-step reuse needs remapping).
	usedTextIDs := make(map[string]bool)
	usedReasoningIDs := make(map[string]bool)
	remapGenerateID := internalGenerateID(opts.Internal)
	pendingStepCtx := r.initialStepCtx
	pendingStepCancel := r.initialStepCancel
	abortFired := false
	fireAbort := func(reason error) {
		if abortFired {
			return
		}
		abortFired = true
		reason = abortReason(ctx, reason)
		if onAbort := firstOnAbort(opts.OnAbortEvent, opts.OnAbort); onAbort != nil {
			onAbort(ctx, GenerateTextAbortEvent{
				CallID: r.cbCallID,
				Steps:  append([]types.StepResult(nil), allSteps...),
				Reason: reason,
			})
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

	// forwardThroughTransformChain feeds chunks through
	// opts.ExperimentalTransform[startIdx:] in order and sinks whatever comes
	// out the end to onChunk/telemetry. A transform's emitter re-enters the
	// chain at startIdx+1 instead of sinking directly, so a chunk emitted
	// early by transform N (e.g. SmoothStream flushing a delayed chunk)
	// still passes through transforms N+1..len-1, matching TS pipeThrough
	// chaining every TransformStream's output into the next stage (hand-off:
	// "transform chaining", core-ai-part00/01 WG12/P1-1 follow-up).
	var forwardThroughTransformChain func(startIdx int, chunks []provider.StreamChunk)
	transformEmitterAt := func(startIdx int) StreamTransformEmitter {
		return func(c provider.StreamChunk) {
			forwardThroughTransformChain(startIdx, []provider.StreamChunk{c})
		}
	}
	forwardThroughTransformChain = func(startIdx int, chunks []provider.StreamChunk) {
		if startIdx >= len(opts.ExperimentalTransform) {
			for _, c := range chunks {
				if onChunk != nil {
					onChunk(c)
				}
				telemetry.FireOnChunk(ctx, telemetry.TelemetryChunkEvent{
					Settings:  r.telemetrySettings,
					ChunkType: string(c.Type),
					Text:      c.Text,
				})
			}
			return
		}
		transform := opts.ExperimentalTransform[startIdx]
		emitCtx := WithStreamTransformEmitter(ctx, transformEmitterAt(startIdx+1))
		var out []provider.StreamChunk
		for _, c := range chunks {
			out = append(out, transform(emitCtx, c)...)
		}
		forwardThroughTransformChain(startIdx+1, out)
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
		stepResponseModelID := stepModelID
		var stepResponseID string
		var stepResponseTimestamp time.Time
		stepInstructions := r.cbSystem
		stepInstructionMessages := r.cbInstructionMessages
		stepTools := append([]types.Tool(nil), currentTools...)
		stepExecutionTools := append([]types.Tool(nil), currentExecutionTools...)
		toolsByName := make(map[string]*types.Tool, len(stepExecutionTools))
		for i := range stepExecutionTools {
			toolsByName[stepExecutionTools[i].Name] = &stepExecutionTools[i]
		}
		// toolInputCallbacks invokes Tool.OnInputStart/OnInputDelta/OnInputAvailable
		// as tool-input-start/delta/tool-call chunks arrive. TS's
		// invokeToolCallbacksFromStream is given `tools: stepExecutionTools`
		// (not stepModelTools), so a caller-only callee's callbacks still
		// fire even though it's hidden from the model. Reset for every step.
		toolInputCallbacks := newStreamToolInputCallbacks(stepExecutionTools, currentMessages, r.cbToolsCtx)
		// preRefinementCalls mirrors stepToolCalls before ExperimentalRefineToolInput
		// runs, for the approval inputSchemaInput diff.
		var preRefinementCalls []types.ToolCall
		// Fire step-start telemetry. OTel implementations create a child step span.
		// PromptMessages reuses r.stepReopenGenOpts, the exact GenerateOptions
		// this step's provider call was dispatched with (set either during
		// bootstrap for step 1, or at the tail of the previous iteration for
		// step N>1 — see the two doStream/DoStream call sites above/below).
		var stepPromptMessages []types.Message
		if r.stepReopenGenOpts != nil {
			stepPromptMessages = r.stepReopenGenOpts.Prompt.Messages
		}
		telemetryStepCtx := telemetry.FireOnStepStart(ctx, telemetry.TelemetryStepStartEvent{
			OperationType:  "ai.streamText",
			Settings:       r.telemetrySettings,
			StepNumber:     stepIndex,
			ModelProvider:  stepProvider,
			ModelID:        stepModelID,
			ToolChoice:     r.cbToolChoice,
			PromptMessages: stepPromptMessages,
			StepTools:      stepTools,
			RuntimeContext: telemetryRuntimeContextWithSensitivity(r.telemetrySettings, r.cbRuntimeCtx, r.cbSensitiveRuntimeCtx),
			ToolsContext:   telemetryToolsContext(r.telemetrySettings, r.cbToolsCtx),
		})

		// Track per-step slices before we accumulate more stream data.
		stepSourcesStart := len(r.sources)
		// Track how many files existed before this step so we can slice per-step files.
		stepFilesStart := len(r.files)
		stepWarningsStart := len(r.warnings)
		// stepTextPartsStart: like the slices above, but for the call-level
		// accumulatedTextParts (used for r.text below) — a retried attempt
		// (see the ChunkTypeError branch) truncates back to this marker too,
		// so a discarded attempt's text is not double-counted in Text().
		stepTextPartsStart := len(accumulatedTextParts)

		// pendingToolCalls accumulates tool call chunks received during this step's stream.
		// All Execute() calls happen after the stream loop ends.
		var stepTextParts []string
		// hasPublishedPartial/stepLastPartialJSON reset every step (audit row
		// 2a5ed55 / WG4: partial output must be parsed from the current
		// step's text only, not all steps concatenated), and
		// hasPublishedPartial replaces relying on stepLastPartialJSON=="" as
		// a "nothing published yet" sentinel, which incorrectly suppressed a
		// genuine first empty-string partial (audit row 84f5d1b / WG4).
		hasPublishedPartial := false
		stepLastPartialJSON := ""
		var stepToolCalls []types.ToolCall
		var stepContent []types.ContentPart
		var stepReasoningBuilder strings.Builder
		var modelCallEndFired bool
		var stepUsage types.Usage
		var stepSawTerminal bool
		var stepSawFinish bool
		var stepSawOutput bool
		// streamedToolResults tracks provider-inline tool results by tool call ID.
		streamedToolResults := make(map[string]types.ToolResult)
		// stepTextIDRemap/stepReasoningIDRemap: see usedTextIDs/usedReasoningIDs above.
		stepTextIDRemap := make(map[string]string)
		stepReasoningIDRemap := make(map[string]string)

		// chunkDeadlineCtx/chunkDeadlineCancel/chunkDeadlineReason implement
		// FirstChunk/PerChunk (audit row 106ea59 / WG-TIMEOUT): FirstChunk is
		// armed fresh for this step and disarmed (replaced by a no-deadline
		// or PerChunk-derived context) as soon as the first semantic output
		// chunk arrives; PerChunk only resets on a semantic output chunk,
		// never on metadata/empty-delta chunks. armStepChunkDeadline/
		// resetChunkDeadlineOnOutput below manage the swap.
		chunkDeadlineCtx, chunkDeadlineCancel, chunkDeadlineReason := armStepChunkDeadline(stepCtx, r.timeout)

		// automaticStreamRetryCount/callbackStreamRetryCount: see the
		// streamRetries handling in the ChunkTypeError branch below (audit
		// row 802af1e / WG8). Reset per step, matching TS stream-text.ts
		// (declared where each step's model call is opened).
		automaticStreamRetryCount := 0
		callbackStreamRetryCount := 0

	stepAttempt:
		for {
			chunk, err := r.nextChunk(chunkDeadlineCtx)
			if err == io.EOF {
				break
			}
			if err != nil {
				if errors.Is(chunkDeadlineCtx.Err(), context.DeadlineExceeded) && chunkDeadlineReason != "" {
					err = wrapTimeoutError(chunkDeadlineReason, chunkDeadlineCtx.Err())
				} else if errors.Is(stepCtx.Err(), context.DeadlineExceeded) && r.timeout != nil && r.timeout.HasPerStep() {
					err = wrapTimeoutError(TimeoutReasonStep, stepCtx.Err())
				}
				r.err = err
				if isAbortErr(ctx, err) {
					fireAbort(err)
				}
				chunkDeadlineCancel()
				break
			}
			if isOutputChunkForTiming(*chunk) {
				chunkDeadlineCtx, chunkDeadlineCancel, chunkDeadlineReason = resetChunkDeadlineOnOutput(stepCtx, r.timeout, chunkDeadlineCancel)
			}
			if r.resumeChunksRemaining > 0 {
				// Outputs of resumed tool approvals: forward, but they belong
				// to the initial response, not to this step's content.
				r.resumeChunksRemaining--
				if onChunk != nil {
					onChunk(*chunk)
				}
				telemetry.FireOnChunk(ctx, telemetry.TelemetryChunkEvent{
					Settings:  r.telemetrySettings,
					ChunkType: string(chunk.Type),
				})
				continue
			}
			remapDuplicateBlockID(chunk, usedTextIDs, stepTextIDRemap, provider.ChunkTypeTextStart, provider.ChunkTypeText, provider.ChunkTypeTextEnd, remapGenerateID)
			remapDuplicateBlockID(chunk, usedReasoningIDs, stepReasoningIDRemap, provider.ChunkTypeReasoningStart, provider.ChunkTypeReasoning, provider.ChunkTypeReasoningEnd, remapGenerateID)
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

			// A text-start boundary: start a new stepContent text part when the
			// trailing part is already a TextContent, so a text-like block
			// immediately following another one (e.g. an Anthropic compaction
			// block followed by plain text) doesn't merge into the previous
			// block's part and inherit its providerMetadata.
			if chunk.Type == provider.ChunkTypeTextStart {
				stepContent = startNewTextPart(stepContent, chunk.ProviderMetadata)
			}

			// Accumulate text
			if chunk.Type == provider.ChunkTypeText {
				stepTextParts = append(stepTextParts, chunk.Text)
				accumulatedTextParts = append(accumulatedTextParts, chunk.Text)
				stepContent = appendTextPart(stepContent, chunk.Text, chunk.ProviderMetadata)

				// Update partial output after each text chunk (with deduplication).
				// Only publishes when the JSON representation of the partial changes,
				// matching the TypeScript SDK's deduplication behavior. Parses from
				// this step's text only (audit row 2a5ed55 / WG4): in a multi-step
				// tool-loop call, only the final step's text is ever a candidate
				// structured-output response.
				if r.outputSpec != nil {
					currentText := strings.Join(stepTextParts, "")
					partial, hasPartial, partialErr := r.outputSpec.parsePartialOutput(ctx, ParsePartialOutputOptions{
						Text: currentText,
					})
					if partialErr != nil {
						r.err = partialErr
						break
					}
					// A nil partial is a legitimate JSON null value, not "no
					// partial yet": hasPartial distinguishes the two so a
					// null value still publishes (audit row 84f5d1b / WG4).
					if hasPartial {
						newJSONStr, ok := partialOutputDedupKey(partial)
						if ok && (!hasPublishedPartial || newJSONStr != stepLastPartialJSON) {
							hasPublishedPartial = true
							stepLastPartialJSON = newJSONStr
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

			// Parse (validate/repair), then refine, and accumulate tool call
			// chunks without executing until the stream is consumed. The
			// chunk (with its parsed/refined ToolCall) is still forwarded to
			// the consumer below. Mirrors TS parseToolCall / RefineToolCalls
			// applied per tool-call chunk rather than batched after the
			// stream ends, so a repaired or invalid call is reflected in the
			// forwarded chunk itself.
			if chunk.Type == provider.ChunkTypeToolCall && chunk.ToolCall != nil {
				enriched := enrichToolCallMetadata([]types.ToolCall{*chunk.ToolCall}, stepTools)[0]
				parsed, parseErr := ParseToolCall(stepCtx, ParseToolCallOptions{
					ToolCall:            enriched,
					Tools:               stepTools,
					RepairToolCall:      repairToolCall,
					Instructions:        stepInstructions,
					InstructionMessages: stepInstructionMessages,
					Messages:            currentMessages,
				})
				if parseErr != nil {
					// Only a context cancellation reaches here (ParseToolCall
					// otherwise returns an Invalid call rather than an error).
					r.err = parseErr
					if isAbortErr(ctx, parseErr) {
						fireAbort(parseErr)
					}
					break
				}
				preRefinementCalls = append(preRefinementCalls, parsed)
				refinedCalls, refErr := RefineToolCalls(stepCtx, []types.ToolCall{parsed}, stepTools, opts.ExperimentalRefineToolInput, r.cbRuntimeCtx, r.cbToolsCtx)
				if refErr != nil {
					r.err = fmt.Errorf("tool input refinement failed at step %d: %w", stepNum, refErr)
					break
				}
				refined := refinedCalls[0]
				chunk.ToolCall = &refined
				stepToolCalls = append(stepToolCalls, refined)
				stepContent = append(stepContent, types.ToolCallContent{
					ToolCallID:       refined.ID,
					ToolName:         refined.ToolName,
					Title:            refined.Title,
					Input:            refined.RawArguments,
					Arguments:        refined.Arguments,
					ProviderExecuted: refined.ProviderExecuted,
					ProviderMetadata: providerMetadataRaw(refined.ProviderMetadata),
					ToolMetadata:     refined.ToolMetadata,
					Dynamic:          refined.Dynamic,
					Invalid:          refined.Invalid,
					Error:            toolCallContentError(refined.Error),
					ThoughtSignature: refined.ThoughtSignature,
				})
			}

			// Track provider-inline tool results for the deferred hasResult check.
			if chunk.Type == provider.ChunkTypeToolResult && chunk.ToolResult != nil {
				enrichedResult, convertErr := enrichStreamedToolResultForModelOutput(stepCtx, *chunk.ToolResult, stepToolCalls, toolsByName, &stepUsage)
				if convertErr != nil {
					r.err = convertErr
					break
				}
				chunk.ToolResult = &enrichedResult
				streamedToolResults[enrichedResult.ToolCallID] = enrichedResult
				stepContent = append(stepContent, toolResultContentFromToolResult(enrichedResult))
			}

			if chunk.Usage != nil {
				stepUsage = *chunk.Usage
			}

			// Update finish reason and context management
			if chunk.Type == provider.ChunkTypeFinish {
				stepSawTerminal = true
				stepSawFinish = true
				r.finishReason = chunk.FinishReason
				if chunk.RawFinishReason != "" {
					r.rawFinishReason = chunk.RawFinishReason
				}
				if chunk.ContextManagement != nil {
					r.contextManagement = chunk.ContextManagement
				}
				if !modelCallEndFired {
					modelCallEndFired = true
					performance := stepPerformance(stepStart, stepUsage, firstTokenAt, outputChunkGapsMs)
					responseID := stepResponseID
					if responseID == "" {
						responseID = responseIDFromMetadata(chunk.ResponseMetadata)
					}
					Notify(stepCtx, LanguageModelCallEndEvent{
						CallID:           r.cbCallID,
						Provider:         stepProvider,
						ModelID:          stepResponseModelID,
						FinishReason:     chunk.FinishReason,
						Usage:            stepUsage,
						Content:          append([]types.ContentPart(nil), stepContent...),
						ResponseID:       responseID,
						ProviderMetadata: decodeProviderMetadataMap(chunk.ProviderMetadata),
						Performance:      languageModelCallPerformance(performance),
					}, onLanguageModelCallEnd)
					telemetry.FireOnLanguageModelCallEnd(ctx, telemetry.LanguageModelCallEndEvent{
						Settings:         r.telemetrySettings,
						CallID:           r.cbCallID,
						ModelProvider:    stepProvider,
						ModelID:          stepModelID,
						FinishReason:     string(chunk.FinishReason),
						Usage:            telemetryUsageFromUsage(stepUsage),
						Content:          append([]types.ContentPart(nil), stepContent...),
						ResponseID:       responseID,
						ProviderMetadata: decodeProviderMetadataMap(chunk.ProviderMetadata),
						Performance:      languageModelCallPerformance(performance),
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
				if chunk.ResponseMetadata.ModelID != "" {
					stepResponseModelID = chunk.ResponseMetadata.ModelID
				}
				if chunk.ResponseMetadata.ID != "" {
					stepResponseID = chunk.ResponseMetadata.ID
				}
				if !chunk.ResponseMetadata.Timestamp.IsZero() {
					stepResponseTimestamp = chunk.ResponseMetadata.Timestamp
				}
			}

			// NOT IMPLEMENTED, intentionally: TS's tool-part buffering
			// (stream-text.ts's `shouldBufferToolParts` /
			// `bufferedAttemptParts`). Once StreamRetries is configured, TS
			// stops forwarding chunks to the consumer as soon as it sees a
			// tool-related part (tool-input-*/tool-call/tool-approval-*/
			// tool-result/tool-error), buffering that part and every part
			// after it (of any type) until the attempt reaches
			// model-call-end (flush) or errors (discard on retry, or flush
			// then forward on a terminal error). Reviewed for WG8 parity
			// (2026-09-27) and judged infeasible to port without a large,
			// risky rewrite of this loop:
			//   - This loop forwards each chunk (OnChunk/fullStream/
			//     telemetry/transform chain) inline as it is read, and
			//     accumulates step state (text, tool calls, usage, content,
			//     IDs) inline too — TS splits these into separate pipeline
			//     stages (buffering stage -> tool-callback stage -> tool-
			//     execution stage -> aggregation stage) that all sit
			//     downstream of the buffer, so a discarded attempt's parts
			//     never reach any of them. Reproducing that would mean
			//     restructuring every per-chunk side effect in this ~600
			//     line loop (chunk deadlines, ID remap tables, tool input
			//     callbacks, StepResponse metadata, transform chaining) to
			//     operate on a buffered/replayed chunk queue instead of the
			//     chunk currently being read.
			//   - The primary risk TS's buffering guards against — a tool
			//     from a discarded attempt getting executed, or double-
			//     executed after a retry — does not apply here: Go always
			//     executes tools once, in a batch, after this loop ends
			//     (see executeTools below), using stepToolCalls, which IS
			//     fully reset on a retry (see the reset block below). So a
			//     retry can never cause a tool to run for the discarded
			//     attempt.
			//   - The residual, accepted gap: if tool-related chunks were
			//     already streamed to the consumer (OnChunk/fullStream)
			//     before the error that triggers a retry, the consumer will
			//     see those "phantom" chunks (and their forwarded text/
			//     reasoning neighbors once buffering would have started)
			//     even though the attempt is discarded, whereas TS hides
			//     them entirely. This is purely a consumer-visible
			//     presentation difference during an active mid-stream
			//     retry (an opt-in, narrow window) — internal state
			//     (Text(), ToolCalls(), Usage(), FinalStep, etc.) is
			//     unaffected because it's rebuilt from the reset step state
			//     below, not from what was forwarded.
			//
			// Call OnError for error chunks before forwarding, and decide
			// whether a retryable mid-stream provider error should reopen
			// this step's model call instead of terminating the stream
			// (streamRetries, audit row 802af1e / 35841f5 / WG8).
			if chunk.Type == provider.ChunkTypeError {
				var rawErr error = errors.New(chunk.Text)
				if chunk.Err != nil {
					rawErr = chunk.Err
				}
				// A ToolChoiceViolationError is never retried, automatically
				// or via OnErrorRetry (TS ToolChoiceViolationError.isInstance
				// check, which normalizeStreamProviderError leaves alone by
				// returning early on any AISDKError) — it reflects the
				// model's own response content, not a transient provider
				// failure. Checked on the RAW error before normalization, so
				// it isn't lost inside a generic *StreamProviderError. Go's
				// tool-choice enforcement currently runs after this loop
				// (checkToolChoiceViolation below) rather than as a chunk
				// here, but the guard is kept so a future refactor that
				// raises it as a chunk can't accidentally make it retryable.
				isToolChoiceViolation := IsToolChoiceViolationError(rawErr)
				normalizedErr := rawErr
				if !isToolChoiceViolation {
					// chunk.Raw is nil here today: no provider populates it
					// on a ChunkTypeError chunk (it's documented for
					// ChunkTypeRaw passthrough only), so
					// NormalizeStreamProviderError's structured-payload
					// extraction (type/code/statusCode/isRetryable) is
					// currently a no-op and it falls back to wrapping
					// chunk.Text. Wiring a provider's raw error object
					// through here (or through chunk.Err) is provider-audit
					// work (WG8's "provider mapping", e.g. Anthropic
					// overloaded_error, Bedrock exceptions, Google, Groq,
					// DeepSeek, HuggingFace, MoonshotAI, Gateway).
					normalizedErr = providererrors.NormalizeStreamProviderError(rawErr, stepProvider, chunk.Raw)
				}
				if opts.OnError != nil {
					safeInvoke(func() { opts.OnError(ctx, normalizedErr) })
				}

				// TS's automatic streamRetries budget (stream-text.ts's
				// `automaticRetry = !isToolChoiceViolation &&
				// automaticStreamRetryCount < streamRetries`) retries ANY
				// mid-stream error chunk while budget remains — it does not
				// gate on StreamProviderError.IsRetryable. isRetryable is
				// exposed to consumers (OnError/OnErrorRetry) to build their
				// own retry heuristics, not consulted by the SDK's own
				// automatic-retry decision.
				streamRetriesLimit := 0
				if opts.StreamRetries != nil {
					streamRetriesLimit = *opts.StreamRetries
				}
				automaticRetry := !isToolChoiceViolation && automaticStreamRetryCount < streamRetriesLimit
				// Callback-directed retry additionally requires StreamRetries
				// to have been explicitly set (even to 0): TS's
				// canRetryStreamViaOnError is `streamRetries !== undefined &&
				// onErrorArg != null`. Omitting StreamRetries entirely
				// disables ALL stream retry behavior, matching TS's "Omit
				// this option to disable all stream retry behavior and
				// preserve incremental tool streaming for existing onError
				// observers."
				callbackRetry := false
				if !isToolChoiceViolation && !automaticRetry && opts.StreamRetries != nil && opts.OnErrorRetry != nil && callbackStreamRetryCount < 1 {
					callbackRetry = safeInvokeBool(func() bool {
						return opts.OnErrorRetry(ctx, StreamTextOnErrorRetryEvent{Error: normalizedErr})
					})
				}

				if (automaticRetry || callbackRetry) && r.stepReopenModel != nil && r.stepReopenGenOpts != nil {
					if automaticRetry {
						automaticStreamRetryCount++
					} else {
						callbackStreamRetryCount++
					}
					if s := r.currentStream(); s != nil {
						_ = s.Close()
					}
					newStream, reopenErr := doStreamWithGatewayRetry(r.stepReopenCtx, r.stepReopenModel, r.stepReopenGenOpts, opts.MaxRetries)
					if reopenErr != nil {
						r.err = reopenErr
						if isAbortErr(ctx, reopenErr) {
							fireAbort(reopenErr)
						}
						break
					}
					r.setStream(newStream)

					// Discard this attempt's buffered step output and start
					// the step's aggregation fresh, so the recovered step
					// reflects only the successful attempt (TS: the
					// attempt-boundary reset in stream-text.ts's transform).
					firstTokenAt = nil
					previousOutputChunkAt = nil
					outputChunkGapsMs = nil
					stepResponseModelID = stepModelID
					stepResponseID = ""
					stepResponseTimestamp = time.Time{}
					toolInputCallbacks = newStreamToolInputCallbacks(stepTools, currentMessages, r.cbToolsCtx)
					preRefinementCalls = nil
					stepTextParts = nil
					hasPublishedPartial = false
					stepLastPartialJSON = ""
					stepToolCalls = nil
					stepContent = nil
					stepReasoningBuilder.Reset()
					modelCallEndFired = false
					stepUsage = types.Usage{}
					stepSawTerminal = false
					stepSawFinish = false
					stepSawOutput = false
					streamedToolResults = make(map[string]types.ToolResult)
					stepTextIDRemap = make(map[string]string)
					stepReasoningIDRemap = make(map[string]string)
					r.mu.Lock()
					r.sources = r.sources[:stepSourcesStart]
					r.files = r.files[:stepFilesStart]
					r.warnings = r.warnings[:stepWarningsStart]
					r.partialOutput = nil
					r.mu.Unlock()
					accumulatedTextParts = accumulatedTextParts[:stepTextPartsStart]
					chunkDeadlineCancel()
					chunkDeadlineCtx, chunkDeadlineCancel, chunkDeadlineReason = armStepChunkDeadline(stepCtx, r.timeout)
					continue stepAttempt
				}

				stepSawTerminal = true
				if r.finishReason == "" {
					r.finishReason = types.FinishReasonError
				}
			}

			// Apply experimental transforms to produce the consumer-facing
			// chunks, chaining every transform's output (including chunks it
			// emits early via the installed emitter) into the next
			// transform, then sink whatever survives to onChunk/telemetry.
			// Forward chunk(s) to consumer before any tool Execute fires.
			if forwardChunk {
				forwardThroughTransformChain(0, []provider.StreamChunk{*chunk})
			}

			// Notify tool input lifecycle callbacks (OnInputStart/OnInputDelta/
			// OnInputAvailable) after the chunk has been forwarded, mirroring
			// TS invokeToolCallbacksFromStream's ordering.
			if err := toolInputCallbacks.handle(stepCtx, *chunk); err != nil {
				r.err = err
				if isAbortErr(ctx, err) {
					fireAbort(err)
				}
				break
			}
		}
		chunkDeadlineCancel()
		if r.err != nil {
			// Close the provider stream (and its underlying HTTP response
			// body) as soon as processStream itself gives up on it, instead
			// of leaving that to a consumer that may never call
			// StreamTextResult.Close() explicitly (hand-off: "processStream
			// closing the TextStream on error"). TextStream.Close() is safe
			// to call more than once, so this doesn't conflict with a later
			// explicit Close() from the caller.
			if s := r.currentStream(); s != nil {
				_ = s.Close()
			}
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
		lastStepText = stepText
		r.text = strings.Join(accumulatedTextParts, "")
		// stepToolCalls were already parsed (with repair) and refined
		// per-chunk above, as each ChunkTypeToolCall arrived.
		stepInputSchemaInputs := inputSchemaInputs(preRefinementCalls, stepToolCalls)
		stepContent = replaceToolCallContentParts(stepContent, stepToolCalls)
		stepContent = replaceToolResultContentParts(stepContent, stepToolCalls)

		if violation := checkToolChoiceViolation(currentToolChoice, stepToolCalls, r.finishReason, stepProvider, stepResponseModelID, stepContent); violation != nil {
			r.err = violation
			cancelStep()
			break
		}

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
				executionBlocked:    !isToolExecutionAllowedFinishReason(r.finishReason),
			}
			usageForTools := r.usage.Add(stepUsage)
			stepToolResults, _ = executeTools(stepCtx, stepToolCalls, stepExecutionTools, r.cbRuntimeCtx, r.cbToolsCtx, opts.ToolApproval, &usageForTools, toolCallbacks)
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
				if input, ok := stepInputSchemaInputs[tr.ToolCallID]; ok {
					request.InputSchemaInput = input
				}
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
		stepContent = append(stepContent, attachInputSchemaInputs(toolResultsToContentParts(stepToolResults, opts.ExperimentalToolApprovalSecret), stepInputSchemaInputs)...)

		// Deferred provider tool tracking, mirroring the TS SDK pendingDeferredToolCalls map.
		// Add tool calls whose results haven't arrived yet.
		// Note: check tool.ProviderExecuted on the definition because some providers
		// (e.g. Anthropic) do not set ProviderExecuted on the ToolCall itself.
		for _, call := range stepToolCalls {
			tool := toolsByName[call.ToolName]
			if tool == nil || !tool.ProviderExecuted || !tool.SupportsDeferredResults {
				continue
			}
			if _, ok := streamedToolResults[call.ID]; !ok {
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
		// a no-op for them. Only streamedToolResults represents actual inline results.
		for callID := range streamedToolResults {
			delete(pendingDeferredToolCalls, callID)
		}
		publicStepToolResults := mergeStreamedToolResults(stepToolResults, streamedToolResults)

		// Accumulate step tool calls and results into the overall result.
		r.mu.Lock()
		r.toolCalls = append(r.toolCalls, stepToolCalls...)
		r.toolResults = append(r.toolResults, publicStepToolResults...)
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

		// Row e6376c2: carry the raw, provider-specific finish/incomplete
		// reason from the chunk stream into the step result, mirroring how
		// r.finishReason (the mapped reason) is read directly.
		stepRawFinishReason := r.rawFinishReason

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
			ToolResults:        publicStepToolResults,
			StaticToolResults:  filterStaticToolResults(publicStepToolResults),
			DynamicToolResults: filterDynamicToolResults(publicStepToolResults),
			FinishReason:       r.finishReason,
			RawFinishReason:    stepRawFinishReason,
			Usage:              stepUsage,
			Performance:        performance,
			Sources:            stepSources,
			Request: types.StepRequest{
				Body:     streamRequestBody(r.currentStream()),
				Messages: includedRequestMessages(r.cbInclude.RequestMessages, currentMessages),
			},
			Response: types.StepResponse{
				ID:        stepResponseID,
				ModelID:   stepResponseModelID,
				Timestamp: stepResponseTimestamp,
				Headers:   stepHeaders,
			},
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
				OperationType:  "ai.streamText",
				StepNumber:     stepIndex,
				FinishReason:   string(r.finishReason),
				Usage:          stepTelUsage,
				Text:           stepText,
				Reasoning:      stepResult.ReasoningText,
				ToolCalls:      stepToolCalls,
				Files:          stepFiles,
				Performance:    languageModelCallPerformance(performance),
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
		r.cbResponseMessages = responseMessagesWithInitial(r.initialResponseMessages, allSteps)
		r.mu.Unlock()
		// Call the simple step-end callback (mirrors GenerateTextOptions.OnStepEnd
		// exactly — see the doc comment on StreamTextOptions.OnStepEnd).
		if r.cbOnStepEnd != nil {
			r.cbOnStepEnd(ctx, stepResult, r.cbRuntimeCtx)
		}
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

		// Log this step's model warnings once per model call (TS
		// stream-text.ts logWarnings, called per step just after the
		// step-finish notify).
		logModelWarnings(stepWarnings, stepProvider, stepModelID)
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
		// Tool calls left unexecuted (unsafe finish reason) stop the loop.
		if hasUnresolvedLocalToolCalls(stepToolCalls, toolsByName, stepToolResults) {
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
		nextInstructionMessages := r.cbInstructionMessages
		// Per-step call-setting overrides (audit row 60f97f6 / WG-STEP); see
		// the analogous comment in generate.go's GenerateText.
		nextMaxTokens := opts.MaxTokens
		nextTemperature := opts.Temperature
		nextTopP := opts.TopP
		nextTopK := opts.TopK
		nextPresencePenalty := opts.PresencePenalty
		nextFrequencyPenalty := opts.FrequencyPenalty
		nextStopSequences := opts.StopSequences
		nextSeed := opts.Seed
		nextReasoningLevel := opts.Reasoning
		if opts.PrepareStep != nil {
			prepared := opts.PrepareStep(ctx, PrepareStepOptions{
				Model:                      nextModel,
				System:                     nextSystem,
				Instructions:               &nextSystem,
				InitialInstructions:        &r.cbSystem,
				InstructionMessages:        cloneInstructionMessages(nextInstructionMessages),
				InitialInstructionMessages: cloneInstructionMessages(r.cbInitialInstructionMessages),
				Messages:                   append([]types.Message(nil), nextMessages...),
				InitialMessages:            append([]types.Message(nil), r.cbMessages...),
				ResponseMessages:           responseMessagesWithInitial(r.initialResponseMessages, allSteps),
				UserContext:                r.cbRuntimeCtx,
				RuntimeContext:             r.cbRuntimeCtx,
				ToolsContext:               r.cbToolsCtx,
				StepNumber:                 stepNum,
				Steps:                      append([]types.StepResult(nil), allSteps...),
				Tools:                      nextTools,
				ToolChoice:                 nextToolChoice,
				ActiveTools:                opts.ActiveTools,
				ToolOrder:                  nextToolOrder,
				ProviderOptions:            nextProviderOptions,
				ExperimentalSandbox:        nextSandbox,
				AccumulatedUsage:           r.usage,
				MaxOutputTokens:            nextMaxTokens,
				Temperature:                nextTemperature,
				TopP:                       nextTopP,
				TopK:                       nextTopK,
				PresencePenalty:            nextPresencePenalty,
				FrequencyPenalty:           nextFrequencyPenalty,
				StopSequences:              nextStopSequences,
				Seed:                       nextSeed,
				Reasoning:                  nextReasoningLevel,
			})
			if prepared.Model != nil {
				nextModel = prepared.Model
			}
			if len(prepared.InstructionMessages) > 0 {
				if err := validateInstructionMessages(prepared.InstructionMessages); err != nil {
					r.err = err
					break
				}
				nextInstructionMessages = cloneInstructionMessages(prepared.InstructionMessages)
				nextSystem = ""
			} else if prepared.Instructions != nil {
				nextSystem = *prepared.Instructions
				nextInstructionMessages = nil
			} else if prepared.System != "" {
				nextSystem = prepared.System
				nextInstructionMessages = nil
			} else if prepared.InstructionMessages != nil {
				nextInstructionMessages = nil
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
			if prepared.MaxOutputTokens != nil {
				nextMaxTokens = prepared.MaxOutputTokens
			}
			if prepared.Temperature != nil {
				nextTemperature = prepared.Temperature
			}
			if prepared.TopP != nil {
				nextTopP = prepared.TopP
			}
			if prepared.TopK != nil {
				nextTopK = prepared.TopK
			}
			if prepared.PresencePenalty != nil {
				nextPresencePenalty = prepared.PresencePenalty
			}
			if prepared.FrequencyPenalty != nil {
				nextFrequencyPenalty = prepared.FrequencyPenalty
			}
			if prepared.StopSequences != nil {
				nextStopSequences = prepared.StopSequences
			}
			if prepared.Seed != nil {
				nextSeed = prepared.Seed
			}
			if prepared.Reasoning != nil {
				nextReasoningLevel = prepared.Reasoning
			}
		}

		nextTools = r.toolSearchState.Apply(nextTools, r.cbToolsCtx, nextSandbox)
		nextExecutionTools, nextModelTools, nextToolCallerMessages := PrepareToolsForToolCallers(nextTools, r.resolvedToolCallers)
		nextTools = nextModelTools
		if len(nextToolCallerMessages) > 0 {
			// Appended only to the prompt sent for this step, not persisted
			// into currentMessages: each step recomputes its own caller
			// announcement (TS appendToolCallerMessages is applied per-step,
			// not accumulated into the conversation history).
			nextMessages = AppendToolCallerMessages(nextMessages, nextToolCallerMessages)
		}

		nextTools = resolveStepTools(ctx, nextTools, r.cbToolsCtx, nextSandbox)
		nextTools = orderStepTools(nextTools, nextToolOrder)
		nextExecutionTools = resolveStepTools(ctx, nextExecutionTools, r.cbToolsCtx, nextSandbox)
		r.cbModel = nextModel
		r.cbModelProvider = nextModel.Provider()
		r.cbModelID = nextModel.ModelID()
		r.cbSystem = nextSystem
		r.cbInstructionMessages = nextInstructionMessages
		r.cbToolChoice = nextToolChoice
		currentTools = append([]types.Tool(nil), nextTools...)
		currentExecutionTools = append([]types.Tool(nil), nextExecutionTools...)
		currentToolChoice = nextToolChoice
		opts.ExperimentalSandbox = nextSandbox
		nextStepCtx := ctx
		nextStepCancel := func() {}
		if r.timeout != nil && r.timeout.HasPerStep() {
			nextStepCtx, nextStepCancel = r.timeout.CreateTimeoutContext(ctx, "step")
		}
		Notify(nextStepCtx, OnStepStartEvent{
			CallID:              r.cbCallID,
			StepNumber:          stepNum,
			Provider:            nextModel.Provider(),
			ModelProvider:       nextModel.Provider(),
			ModelID:             nextModel.ModelID(),
			Instructions:        nextSystem,
			InstructionMessages: nextInstructionMessages,
			System:              nextSystem,
			Messages:            nextMessages,
			Tools:               nextTools,
			ToolChoice:          nextToolChoice,
			ActiveTools:         opts.ActiveTools,
			ToolOrder:           nextToolOrder,
			ProviderOptions:     nextProviderOptions,
			Steps:               allSteps,
			PreviousSteps:       allSteps,
			ExperimentalContext: r.cbExperimentalCtx,
			RuntimeContext:      r.cbRuntimeCtx,
			ToolsContext:        r.cbToolsCtx,
		}, opts.OnStepStart)
		nextPrompt, normErr := promptutils.NormalizePromptWithDownloadSupport(nextStepCtx, types.Prompt{
			Messages: messagesForModel(nextMessages),
			System:   appendSandboxDescription(nextSystem, nextSandbox),
		}, allowSystemMessages(opts.AllowSystemMessages, opts.AllowSystemInMessages), effectiveDownload(opts.ExperimentalDownload), supportedURLChecker(nextModel))
		if normErr != nil {
			nextStepCancel()
			r.err = fmt.Errorf("prompt normalization failed for step %d: %w", stepNum+1, normErr)
			break
		}
		nextPrompt = prependInstructionMessages(nextPrompt, nextInstructionMessages)
		nextGenOpts := &provider.GenerateOptions{
			Prompt:                nextPrompt,
			AllowSystemMessages:   allowSystemMessages(opts.AllowSystemMessages, opts.AllowSystemInMessages),
			AllowSystemInMessages: allowSystemMessages(opts.AllowSystemMessages, opts.AllowSystemInMessages),
			Temperature:           nextTemperature,
			MaxTokens:             nextMaxTokens,
			TopP:                  nextTopP,
			TopK:                  nextTopK,
			FrequencyPenalty:      nextFrequencyPenalty,
			PresencePenalty:       nextPresencePenalty,
			StopSequences:         nextStopSequences,
			Seed:                  nextSeed,
			Headers:               opts.Headers,
			Tools:                 nextTools,
			ToolChoice:            nextToolChoice,
			IncludeRawChunks:      includeRawChunksValue(r.cbInclude),
			RuntimeContext:        r.cbRuntimeCtx,
			ToolsContext:          r.cbToolsCtx,
			ResponseFormat:        responseFormat,
			Reasoning:             nextReasoningLevel,
			SendReasoning:         opts.SendReasoning,
			ProviderOptions:       nextProviderOptions,
			Telemetry:             r.telemetrySettings,
		}
		Notify(nextStepCtx, LanguageModelCallStartEvent{
			CallID:              r.cbCallID,
			Provider:            nextModel.Provider(),
			ModelID:             nextModel.ModelID(),
			Instructions:        nextSystem,
			InstructionMessages: nextInstructionMessages,
			Messages:            nextMessages,
			Tools:               nextGenOpts.Tools,
			MaxOutputTokens:     nextGenOpts.MaxTokens,
			Temperature:         nextGenOpts.Temperature,
			TopP:                nextGenOpts.TopP,
			TopK:                nextGenOpts.TopK,
			PresencePenalty:     nextGenOpts.PresencePenalty,
			FrequencyPenalty:    nextGenOpts.FrequencyPenalty,
			StopSequences:       nextGenOpts.StopSequences,
			Seed:                nextGenOpts.Seed,
			Reasoning:           nextGenOpts.Reasoning,
		}, onLanguageModelCallStart)
		// Scoped to just this call (594029e): see the analogous comment where
		// the first step's stream is started, above.
		nextModelCallCtx := telemetry.FireOnLanguageModelCallStart(nextStepCtx, telemetry.LanguageModelCallStartEvent{
			Settings:         r.telemetrySettings,
			CallID:           r.cbCallID,
			ModelProvider:    nextModel.Provider(),
			ModelID:          nextModel.ModelID(),
			Prompt:           nextGenOpts.Prompt,
			Tools:            nextGenOpts.Tools,
			System:           nextSystem,
			Temperature:      nextGenOpts.Temperature,
			MaxOutputTokens:  nextGenOpts.MaxTokens,
			TopP:             nextGenOpts.TopP,
			TopK:             nextGenOpts.TopK,
			PresencePenalty:  nextGenOpts.PresencePenalty,
			FrequencyPenalty: nextGenOpts.FrequencyPenalty,
			StopSequences:    nextGenOpts.StopSequences,
			Seed:             nextGenOpts.Seed,
		})
		r.stepReopenModel = nextModel
		r.stepReopenGenOpts = nextGenOpts
		r.stepReopenCtx = nextModelCallCtx
		newStream, err := nextModel.DoStream(nextModelCallCtx, nextGenOpts)
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
		r.setStream(newStream)
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
			safeInvoke(func() { opts.OnError(ctx, r.err) })
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

	// Resolve final typed output if spec was provided, from the last step's
	// own text (audit row 2a5ed55 / WG4). Unlike GenerateText, StreamText's
	// TS getOutputPromise() (stream-text.ts) parses the final step's text
	// unconditionally — it has no finishReason gate — so a schema-based
	// Output spec throws NoObjectGeneratedError from output.parseCompleteOutput
	// itself (e.g. on empty text) rather than being silently skipped here.
	if r.outputSpec != nil {
		parsed, parseErr := r.outputSpec.parseCompleteOutput(ctx, ParseCompleteOutputOptions{
			Text:         lastStepText,
			FinishReason: r.finishReason,
			Usage:        &r.usage,
		})
		r.mu.Lock()
		// Only publish a result on success: the generic outputProcessor
		// interface returns the parse method's zero value (e.g. a
		// zero-valued struct, not untyped nil) alongside a non-nil error, so
		// assigning it unconditionally would make Output() return a
		// non-nil-but-empty value instead of nil on a parse failure.
		if parseErr == nil {
			r.outputResult = parsed
		}
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
	streamToolCalls := r.toolCalls
	var streamReasoningText string
	if len(allSteps) > 0 {
		streamReasoningText = allSteps[len(allSteps)-1].ReasoningText
	}
	var streamProviderMeta map[string]interface{}
	if len(r.providerMetadata) > 0 {
		_ = json.Unmarshal(r.providerMetadata, &streamProviderMeta)
	}
	r.mu.Unlock()
	telemetry.FireOnFinish(r.telemetryCtx, telemetry.TelemetryFinishEvent{
		OperationType:    "ai.streamText",
		FinishReason:     string(r.finishReason),
		Usage:            streamTelUsage,
		ModelProvider:    r.cbModelProvider,
		ModelID:          r.cbModelID,
		Text:             r.text,
		Reasoning:        streamReasoningText,
		ToolCalls:        streamToolCalls,
		Files:            streamFiles,
		ProviderMetadata: streamProviderMeta,
		Settings:         r.telemetrySettings,
		RuntimeContext:   telemetryRuntimeContextWithSensitivity(r.telemetrySettings, r.cbRuntimeCtx, r.cbSensitiveRuntimeCtx),
		ToolsContext:     telemetryToolsContext(r.telemetrySettings, r.cbToolsCtx),
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
	// Populate the Request()/Response() accessors from the final step. These
	// were previously only set by ReadAll's now-removed duplicate stream
	// consumption, which left them empty whenever any callback was set.
	r.mu.Lock()
	r.stepRequest = lastStep.Request
	r.stepResponse = lastStep.Response
	r.mu.Unlock()
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
		Output:              r.outputResult,
	}, r.cbOnEndEvent)
}

func enrichStreamedToolResultForModelOutput(ctx context.Context, result types.ToolResult, toolCalls []types.ToolCall, toolsByName map[string]*types.Tool, usage *types.Usage) (types.ToolResult, error) {
	call, hasCall := findToolCallByID(toolCalls, result.ToolCallID)
	if hasCall {
		if result.ToolName == "" {
			result.ToolName = call.ToolName
		}
		if result.Input == nil {
			result.Input = call.Arguments
		}
		if !result.ProviderExecuted {
			if tool := toolsByName[call.ToolName]; tool != nil && tool.ProviderExecuted {
				result.ProviderExecuted = true
			} else {
				result.ProviderExecuted = call.ProviderExecuted
			}
		}
	}
	tool := toolsByName[result.ToolName]
	if tool == nil || tool.ToModelOutput == nil || result.ModelOutput != nil || result.Error != nil {
		return result, nil
	}
	input := result.Input
	if input == nil && hasCall {
		input = call.Arguments
	}
	toolCall := types.ToolCall{
		ID:               result.ToolCallID,
		ToolName:         result.ToolName,
		Title:            result.Title,
		Arguments:        input,
		ProviderExecuted: result.ProviderExecuted,
		ProviderMetadata: result.ProviderMetadata,
		ToolMetadata:     result.ToolMetadata,
		Dynamic:          result.Dynamic,
	}
	if hasCall {
		toolCall = call
		toolCall.Arguments = input
		if toolCall.ToolName == "" {
			toolCall.ToolName = result.ToolName
		}
		if !toolCall.ProviderExecuted {
			toolCall.ProviderExecuted = result.ProviderExecuted
		}
	}
	converted, err := tool.ToModelOutput(ctx, types.ToModelOutputOptions{
		ToolCallID: result.ToolCallID,
		Input:      input,
		Output:     result.Result,
		Result:     result.Result,
		ToolCall:   &toolCall,
		Usage:      usage,
	})
	if err != nil {
		return result, err
	}
	result.ModelOutput = converted
	return result, nil
}

func findToolCallByID(toolCalls []types.ToolCall, toolCallID string) (types.ToolCall, bool) {
	for _, call := range toolCalls {
		if call.ID == toolCallID {
			return call, true
		}
	}
	return types.ToolCall{}, false
}

func mergeStreamedToolResults(executedResults []types.ToolResult, streamedResults map[string]types.ToolResult) []types.ToolResult {
	if len(streamedResults) == 0 {
		return executedResults
	}
	merged := make([]types.ToolResult, 0, len(executedResults)+len(streamedResults))
	seen := make(map[string]bool, len(executedResults))
	for _, result := range executedResults {
		if streamed, ok := streamedResults[result.ToolCallID]; ok && result.ProviderExecuted && result.Result == nil && result.Error == nil {
			merged = append(merged, streamed)
			seen[result.ToolCallID] = true
			continue
		}
		merged = append(merged, result)
		seen[result.ToolCallID] = true
	}
	for id, streamed := range streamedResults {
		if !seen[id] {
			merged = append(merged, streamed)
		}
	}
	return merged
}

// Stream returns the full, processed multi-step chunk stream: every chunk
// forwarded by processStream (tool calls, tool results, later steps, etc.),
// matching the TypeScript SDK's fullStream. Stream() always returns the same
// underlying cursor for the lifetime of the result — calling it repeatedly
// (even once per chunk) resumes from wherever that cursor left off, the same
// contract the raw stream field offered before chunkBuf existed.
//
// StreamTextResult values built directly (bypassing StreamText, e.g. in
// tests exercising the lower-level stream helpers) have no backing buffer
// and fall back to the raw stream field.
func (r *StreamTextResult) Stream() provider.TextStream {
	if r.chunkBuf == nil {
		return r.stream
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.chunkBufReader == nil {
		r.chunkBufReader = r.chunkBuf.reader()
	}
	return r.chunkBufReader
}

// FullStream returns the underlying text stream.
//
// Deprecated: use Stream.
func (r *StreamTextResult) FullStream() provider.TextStream {
	return r.Stream()
}

type prefixedTextStream struct {
	prefix []provider.StreamChunk
	index  int
	base   provider.TextStream
}

func (s *prefixedTextStream) Next() (*provider.StreamChunk, error) {
	if s.index < len(s.prefix) {
		chunk := s.prefix[s.index]
		s.index++
		return &chunk, nil
	}
	if s.base == nil {
		return nil, io.EOF
	}
	return s.base.Next()
}

func (s *prefixedTextStream) Err() error {
	if s.base == nil {
		return nil
	}
	return s.base.Err()
}

func (s *prefixedTextStream) Close() error {
	s.index = len(s.prefix)
	if s.base == nil {
		return nil
	}
	return s.base.Close()
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
	// processStream now always runs in a background goroutine (even with no
	// callbacks registered), so an unsynchronized read here would race with
	// its writes to r.text. ensureConsumed blocks until processStream (or the
	// legacy synchronous path) has fully finished, which — via the
	// processingDone channel close — establishes a happens-before edge for
	// every field processStream wrote, matching the pattern used by Usage().
	_ = r.ensureConsumed()
	return r.text
}

// FinishReason returns the finish reason (only available after stream completes)
func (r *StreamTextResult) FinishReason() types.FinishReason {
	_ = r.ensureConsumed()
	return r.finishReason
}

// StopReason returns the reason from the stop condition that ended the loop.
// It is empty when the stream ended naturally.
func (r *StreamTextResult) StopReason() string {
	_ = r.ensureConsumed()
	return r.stopReason
}

// Usage returns the usage information (only available after stream completes)
func (r *StreamTextResult) Usage() types.Usage {
	_ = r.ensureConsumed()
	return r.usage
}

// TotalUsage returns aggregate token usage across all steps.
//
// Deprecated: use Usage instead (Usage is already the total across all
// steps; TS keeps totalUsage only as an alias of usage).
func (r *StreamTextResult) TotalUsage() types.Usage {
	return r.Usage()
}

// ContextManagement returns context management statistics (Anthropic-specific)
// Only available after stream completes
func (r *StreamTextResult) ContextManagement() interface{} {
	_ = r.ensureConsumed()
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
//
// The mu.Lock below only serializes concurrent callers of this method
// against each other; processStream does not take r.mu when populating
// r.responseHeaders, so it is ensureConsumed — which blocks until
// processStream has fully finished — that actually makes this race free,
// the same as ResponseHeaders().
func (r *StreamTextResult) ResponseHeadersMap() map[string]string {
	_ = r.ensureConsumed()
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.responseHeaders
}

// Request returns metadata about the last request sent to the provider.
//
// Deprecated: use FinalStep().Request instead.
func (r *StreamTextResult) Request() types.StepRequest {
	_ = r.ensureConsumed()
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stepRequest
}

// Response returns metadata about the last response from the provider.
//
// Deprecated: use FinalStep().Response instead.
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
	_ = r.ensureConsumed()
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

// Reasoning returns the reasoning/thinking content parts from the final
// stream step, consuming the stream if needed.
//
// Deprecated: use FinalStep().Reasoning instead.
func (r *StreamTextResult) Reasoning() []types.ReasoningContent {
	return r.FinalStep().Reasoning
}

// ReasoningText returns the concatenated reasoning text from the final
// stream step, consuming the stream if needed.
//
// Deprecated: use FinalStep().ReasoningText instead.
func (r *StreamTextResult) ReasoningText() string {
	return r.FinalStep().ReasoningText
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

// Close closes the stream. It also cancels any in-flight bootstrap work
// (resuming approved tool calls, or making the first provider stream
// request) that hasn't produced a stream yet: Close() may be called
// immediately after StreamText returns, before that background work
// finishes, so r.stream/r.initialStepCancel are read under the same lock
// bootstrapAndStream uses to set them (see setStream/setInitialStep).
func (r *StreamTextResult) Close() error {
	if r.cancelBootstrap != nil {
		r.cancelBootstrap()
	}
	r.mu.Lock()
	cancel := r.initialStepCancel
	s := r.stream
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if s == nil {
		return nil
	}
	return s.Close()
}

// ReadAll returns the complete generated text once streaming (and, for
// results produced by StreamText, the full multi-step tool-calling loop) has
// finished. Tool calls are executed as part of that loop; ReadAll never has
// to execute them itself.
//
// StreamTextResult values produced by StreamText always run processStream in
// the background (see StreamText), so ReadAll only needs to wait for it to
// finish. StreamTextResult values built directly (bypassing StreamText, e.g.
// in tests exercising the lower-level stream helpers) have no background
// processor and fall back to consuming the raw stream directly, without tool
// execution or multi-step continuation — matching their previous behavior.
func (r *StreamTextResult) ReadAll() (string, error) {
	if r.processingDone != nil {
		<-r.processingDone
		return r.text, r.err
	}
	return r.readAllLegacy()
}

// readAllLegacy is the pre-existing single-step stream consumer, kept only
// for StreamTextResult values that were built by hand rather than returned
// from StreamText (which now always populates processingDone).
func (r *StreamTextResult) readAllLegacy() (string, error) {
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
	streamedToolResults := make(map[string]types.ToolResult)
	var stepContent []types.ContentPart
	var stepReasoning []types.ReasoningContent
	var sawTerminal bool
	var sawOutput bool
	toolsByName := make(map[string]*types.Tool, len(r.cbTools))
	for i := range r.cbTools {
		toolsByName[r.cbTools[i].Name] = &r.cbTools[i]
	}

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
					Reason:   abortReason(ctx, err),
					Steps:    append([]types.StepResult(nil), r.cbSteps...),
				})
			}
			return "", err
		}
		if r.resumeChunksRemaining > 0 {
			// Resumed tool approval outputs are not part of the step content.
			r.resumeChunksRemaining--
			continue
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

		// A text-start boundary: see the identical branch in processStream.
		if chunk.Type == provider.ChunkTypeTextStart {
			stepContent = startNewTextPart(stepContent, chunk.ProviderMetadata)
		}

		// Accumulate text
		if chunk.Type == provider.ChunkTypeText {
			r.text += chunk.Text
			stepContent = appendTextPart(stepContent, chunk.Text, chunk.ProviderMetadata)

			// Update partial output after each text chunk (with deduplication).
			if r.outputSpec != nil {
				partial, hasPartial, partialErr := r.outputSpec.parsePartialOutput(ctx, ParsePartialOutputOptions{
					Text: r.text,
				})
				if partialErr != nil {
					r.err = partialErr
					return "", partialErr
				}
				// A nil partial is a legitimate JSON null value, not "no
				// partial yet" (audit row 84f5d1b / WG4).
				if hasPartial {
					newJSONStr, ok := partialOutputDedupKey(partial)
					if ok && (!r.hasPublishedPartialLegacy || newJSONStr != r.lastPartialJSON) {
						r.hasPublishedPartialLegacy = true
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
			enrichedResult, convertErr := enrichStreamedToolResultForModelOutput(stepCtx, *chunk.ToolResult, pendingToolCalls, toolsByName, &r.usage)
			if convertErr != nil {
				r.err = convertErr
				return "", convertErr
			}
			chunk.ToolResult = &enrichedResult
			streamedToolResults[enrichedResult.ToolCallID] = enrichedResult
			stepContent = append(stepContent, toolResultContentFromToolResult(enrichedResult))
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
			if chunk.RawFinishReason != "" {
				r.rawFinishReason = chunk.RawFinishReason
			}
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
	readAllToolResults := mergeStreamedToolResults(nil, streamedToolResults)
	if len(readAllToolResults) > 0 {
		r.mu.Lock()
		r.toolResults = readAllToolResults
		r.mu.Unlock()
	}
	responseMessages := providerutils.ConvertToResponseMessages(
		pendingToolCalls,
		stepContent,
		readAllToolResults,
	)
	step := types.StepResult{
		CallID:             r.cbCallID,
		StepNumber:         0,
		Model:              types.StepModel{Provider: r.cbModelProvider, ModelID: r.cbModelID},
		Text:               r.text,
		Content:            stepContent,
		Reasoning:          stepReasoning,
		ReasoningText:      buildReasoningText(stepReasoning),
		ToolCalls:          pendingToolCalls,
		StaticToolCalls:    filterStaticToolCalls(pendingToolCalls),
		DynamicToolCalls:   filterDynamicToolCalls(pendingToolCalls),
		ToolResults:        readAllToolResults,
		StaticToolResults:  filterStaticToolResults(readAllToolResults),
		DynamicToolResults: filterDynamicToolResults(readAllToolResults),
		FinishReason:       r.finishReason,
		Usage:              r.usage,
		Performance:        stepPerformance(stepStart, r.usage, firstTokenAt, outputChunkGapsMs),
		Sources:            r.sources,
		Files:              r.files,
		Request: types.StepRequest{
			Body:     streamRequestBody(r.currentStream()),
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
	r.cbResponseMessages = responseMessagesWithInitial(r.initialResponseMessages, nil)
	r.cbResponseMessages = append(r.cbResponseMessages, responseMessages...)
	r.stepRequest = step.Request
	r.stepResponse = step.Response
	r.mu.Unlock()

	// Resolve final typed output if spec was provided (audit rows
	// eed7950/9de0baf / WG4). Unconditional, matching TS StreamText's
	// getOutputPromise() — see the comment at the other call site above.
	if r.outputSpec != nil {
		parsed, parseErr := r.outputSpec.parseCompleteOutput(ctx, ParseCompleteOutputOptions{
			Text:         r.text,
			FinishReason: r.finishReason,
			Usage:        &r.usage,
		})
		r.mu.Lock()
		if parseErr == nil {
			r.outputResult = parsed
		}
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
	var readAllProviderMeta map[string]interface{}
	if len(r.providerMetadata) > 0 {
		_ = json.Unmarshal(r.providerMetadata, &readAllProviderMeta)
	}
	r.mu.Unlock()
	telemetry.FireOnFinish(r.telemetryCtx, telemetry.TelemetryFinishEvent{
		OperationType:    "ai.streamText",
		FinishReason:     string(r.finishReason),
		Usage:            readAllTelUsage,
		ModelProvider:    r.cbModelProvider,
		ModelID:          r.cbModelID,
		Text:             r.text,
		Reasoning:        step.ReasoningText,
		ToolCalls:        step.ToolCalls,
		Files:            readAllFiles,
		ProviderMetadata: readAllProviderMeta,
		Settings:         r.telemetrySettings,
		RuntimeContext:   telemetryRuntimeContextWithSensitivity(r.telemetrySettings, r.cbRuntimeCtx, r.cbSensitiveRuntimeCtx),
		ToolsContext:     telemetryToolsContext(r.telemetrySettings, r.cbToolsCtx),
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
		Output:              r.outputResult,
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

// armStepChunkDeadline returns the initial chunk-read deadline context for a
// step: FirstChunk if configured (it always takes priority at the start of a
// step — PerChunk only takes over once FirstChunk disarms, see
// resetChunkDeadlineOnOutput), else PerChunk if configured, else stepCtx
// itself with a no-op cancel. The returned reason identifies which of
// TimeoutReasonFirstChunk/TimeoutReasonChunk a deadline expiring on this
// context should be reported as ("" when there is no deadline at all).
func armStepChunkDeadline(stepCtx context.Context, tc *TimeoutConfig) (context.Context, context.CancelFunc, TimeoutReason) {
	if tc.HasFirstChunk() {
		c, cancel := context.WithTimeout(stepCtx, *tc.FirstChunk)
		return c, cancel, TimeoutReasonFirstChunk
	}
	if tc.HasPerChunk() {
		c, cancel := context.WithTimeout(stepCtx, *tc.PerChunk)
		return c, cancel, TimeoutReasonChunk
	}
	return stepCtx, func() {}, ""
}

// resetChunkDeadlineOnOutput is called once a semantic output chunk
// (isOutputChunkForTiming) has been read. It cancels the previous deadline
// context and, when PerChunk is configured, arms a fresh PerChunk-only
// deadline for the rest of the step (this both disarms FirstChunk — it
// never re-arms within a step — and performs PerChunk's semantic-only
// reset). When PerChunk isn't configured, it returns stepCtx with a no-op
// cancel, so no further chunk-level deadline applies for the rest of the
// step.
func resetChunkDeadlineOnOutput(stepCtx context.Context, tc *TimeoutConfig, prevCancel context.CancelFunc) (context.Context, context.CancelFunc, TimeoutReason) {
	prevCancel()
	if tc.HasPerChunk() {
		c, cancel := context.WithTimeout(stepCtx, *tc.PerChunk)
		return c, cancel, TimeoutReasonChunk
	}
	return stepCtx, func() {}, ""
}

// remapDuplicateBlockID rewrites chunk.ID in place to avoid duplicate
// text/reasoning block IDs across the steps of a single streamText call
// (audit row c6d57f3 / WG5 #113): many providers restart block IDs (e.g.
// "0") on every model call, which is fine within one step but would collide
// in the merged full stream and in the UI message reducer's per-ID part map
// once a second step reuses an ID the first step already used.
//
// used tracks every ID seen so far across all steps (mutated here). remap
// holds the CURRENT step's original-ID -> remapped-ID mapping (the caller
// resets it at the start of each step): on the block's start chunk, a
// colliding ID gets a freshly generated replacement recorded in remap; on
// the block's delta/end chunks, an existing remap entry (if any) is applied
// so every chunk for that block carries the same (possibly remapped) ID
// throughout the step.
//
// generateID mirrors TS's createPartIdReserver: on collision, the
// replacement is a fresh ID from generateID() (the call's configured ID
// generator — TS's generateId, InternalOptions.GenerateID here), not a
// suffix of the original colliding ID. A numeric suffix is appended to that
// FRESH id only in the (extremely unlikely) case that it also collides,
// exactly like TS's `${generatedId}-${++suffix}` loop. Deriving the
// replacement from the original ID instead (e.g. "0" -> "0-2") would let a
// provider's own later block ID collide with an earlier remap's output.
func remapDuplicateBlockID(chunk *provider.StreamChunk, used map[string]bool, remap map[string]string, startType, deltaType, endType provider.ChunkType, generateID IDGenerator) {
	if chunk == nil || chunk.ID == "" {
		return
	}
	switch chunk.Type {
	case startType:
		id := chunk.ID
		if used[id] {
			generatedID := generateID()
			newID := generatedID
			for n := 1; used[newID]; n++ {
				newID = fmt.Sprintf("%s-%d", generatedID, n)
			}
			remap[id] = newID
			chunk.ID = newID
			used[newID] = true
			return
		}
		used[id] = true
	case deltaType, endType:
		if newID, ok := remap[chunk.ID]; ok {
			chunk.ID = newID
		}
	}
}

// streamRequestBody returns the raw request body for s if it implements the
// optional provider.StreamRequestBody capability, or nil otherwise (hand-off:
// "stream request body field"). No current provider implements this yet;
// this is the core-side plumbing for one to opt in.
func streamRequestBody(s provider.TextStream) interface{} {
	if brb, ok := s.(provider.StreamRequestBody); ok {
		return brb.RequestBody()
	}
	return nil
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

// nextChunk reads the next chunk with optional per-chunk timeout.
//
// It reads r.stream once, through currentStream(), rather than referencing
// r.stream directly: bootstrapAndStream/processStream (the only writers) use
// setStream under r.mu, and Close() (callable concurrently by the consumer)
// reads under the same lock, so an unsynchronized read here would race.
func (r *StreamTextResult) nextChunk(ctx context.Context) (*provider.StreamChunk, error) {
	stream := r.currentStream()
	if r.timeout == nil || !r.timeout.HasPerChunk() {
		if ctx == nil || ctx.Done() == nil {
			return stream.Next()
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
			chunk, err := stream.Next()
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
		return stream.Next()
	}

	// Channel to receive the chunk
	type chunkResult struct {
		chunk *provider.StreamChunk
		err   error
	}
	resultCh := make(chan chunkResult, 1)

	// Start goroutine to read chunk
	go func() {
		chunk, err := stream.Next()
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
//
// Deprecated: use FinalStep().ProviderMetadata instead.
func (r *StreamTextResult) ProviderMetadata() json.RawMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.providerMetadata
}

// ResponseHeaders returns the raw HTTP response headers received from the provider.
// Populated once a ChunkTypeResponseMetadata chunk has been processed.
func (r *StreamTextResult) ResponseHeaders() map[string]string {
	_ = r.ensureConsumed()
	return r.responseHeaders
}

// shouldParseFinalOutput mirrors TS generate-text.ts's (non-streaming)
// final-output parse condition: parse on a clean stop, or on any other
// finish reason besides tool-calls as long as the step actually produced
// text (a provider that omits/misreports finishReason but still returned
// the object). Used by GenerateText only — StreamText's TS counterpart
// (stream-text.ts getOutputPromise) has no such gate and always parses the
// final step's text. Mirrors audit rows eed7950/9de0baf, WG4.
func shouldParseFinalOutput(finishReason types.FinishReason, stepText string) bool {
	if finishReason == types.FinishReasonStop {
		return true
	}
	return finishReason != types.FinishReasonToolCalls && stepText != ""
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
// Safe to call concurrently with streaming.
func (r *StreamTextResult) Warnings() []types.Warning {
	_ = r.ensureConsumed()
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.warnings
}

// Files returns model-generated output files accumulated across every step,
// matching the TypeScript SDK's accumulative StreamTextResult.files.
// Safe to call concurrently with streaming.
func (r *StreamTextResult) Files() []types.GeneratedFileContent {
	_ = r.ensureConsumed()
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.files
}

// Chunks returns a channel that streams the full, processed multi-step chunk
// sequence (see Stream). This provides an idiomatic Go way to consume the
// stream.
func (r *StreamTextResult) Chunks() <-chan provider.StreamChunk {
	ch := make(chan provider.StreamChunk, 10)
	stream := r.Stream()

	go func() {
		defer close(ch)
		for {
			chunk, err := stream.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				break
			}

			ch <- *chunk
		}
	}()

	return ch
}
