package telemetry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ---------------------------------------------------------------------------
// Telemetry event types
// ---------------------------------------------------------------------------

// TelemetryStartEvent is passed to TelemetryIntegration.OnStart.
type TelemetryStartEvent struct {
	// OperationType is the canonical AI operation name, e.g. "ai.generateText".
	OperationType string
	ModelProvider string
	ModelID       string
	// Settings holds the caller-supplied telemetry configuration.
	// nil means telemetry was not configured for this call.
	Settings *Settings
	// Prompt and System are only populated when Settings.RecordInputs is true.
	Prompt string
	System string
}

// TelemetryStepStartEvent is passed to TelemetryIntegration.OnStepStart.
type TelemetryStepStartEvent struct {
	StepNumber    int
	ModelProvider string
	ModelID       string
}

// TelemetryToolCallStartEvent is passed to TelemetryIntegration.OnToolCallStart.
type TelemetryToolCallStartEvent struct {
	ToolCallID string
	ToolName   string
	Args       map[string]interface{}
}

// TelemetryToolCallFinishEvent is passed to TelemetryIntegration.OnToolCallFinish.
type TelemetryToolCallFinishEvent struct {
	ToolCallID string
	ToolName   string
	Args       map[string]interface{}
	Result     interface{}
	Error      error
	DurationMs int64
}

// TelemetryChunkEvent is passed to TelemetryIntegration.OnChunk (streaming only).
type TelemetryChunkEvent struct {
	// ChunkType mirrors provider.ChunkType values: "text", "tool-call", "tool-result", etc.
	ChunkType string
	// Text is populated for text-type chunks.
	Text string
}

// TelemetryStepFinishEvent is passed to TelemetryIntegration.OnStepFinish.
type TelemetryStepFinishEvent struct {
	StepNumber   int
	FinishReason string
	Usage        TelemetryUsage
}

// TelemetryFinishEvent is passed to TelemetryIntegration.OnFinish.
type TelemetryFinishEvent struct {
	FinishReason string
	Usage        TelemetryUsage
	// Text is the full generated text. Integrations should check
	// Settings.RecordOutputs before recording this value.
	Text string
	// Files holds any model-generated output files (e.g. images, audio).
	// Integrations should check Settings.RecordOutputs before recording file data.
	Files    []types.GeneratedFileContent
	Settings *Settings
}

// TelemetryErrorEvent is passed to TelemetryIntegration.OnError.
type TelemetryErrorEvent struct {
	Error error
}

// TelemetryUsage carries token counts for telemetry events.
type TelemetryUsage struct {
	InputTokens              *int64
	OutputTokens             *int64
	TotalTokens              *int64
	CacheReadInputTokens     *int64
	CacheCreationInputTokens *int64
	ReasoningTokens          *int64
	// NoCacheInputTokens tracks tokens that bypassed the cache (ai.usage.inputTokenDetails.noCacheTokens).
	NoCacheInputTokens *int64
	// OutputTextTokens tracks text-only output tokens (ai.usage.outputTokenDetails.textTokens).
	OutputTextTokens *int64
}

// ---------------------------------------------------------------------------
// TelemetryIntegration interface
// ---------------------------------------------------------------------------

// TelemetryIntegration receives lifecycle events from AI operations and
// translates them into backend-specific observability records (OTel spans,
// metrics, logs, etc.).
//
// Implementations must be safe for concurrent use — multiple goroutines may
// call any method simultaneously.
//
// The interface mirrors the TypeScript AI SDK's TelemetryIntegration type:
// all methods map 1-to-1, with Go-idiomatic naming.
type TelemetryIntegration interface {
	// OnStart is called once before the first LLM request.
	// Implementations that create a root span should embed it in the returned
	// context (e.g. via trace.ContextWithSpan) so downstream methods can
	// retrieve it with trace.SpanFromContext.
	OnStart(ctx context.Context, e TelemetryStartEvent) context.Context

	// OnStepStart is called at the beginning of each LLM step.
	OnStepStart(ctx context.Context, e TelemetryStepStartEvent)

	// OnToolCallStart is called just before each tool's Execute function runs.
	// Return a (possibly modified) context; OTel implementations may start a
	// child span and embed it for OnToolCallFinish.
	OnToolCallStart(ctx context.Context, e TelemetryToolCallStartEvent) context.Context

	// OnToolCallFinish is called after each tool's Execute function returns,
	// whether the execution succeeded or failed.
	OnToolCallFinish(ctx context.Context, e TelemetryToolCallFinishEvent)

	// OnChunk is called for each stream chunk during streaming generation.
	OnChunk(ctx context.Context, e TelemetryChunkEvent)

	// OnStepFinish is called after each LLM step completes.
	OnStepFinish(ctx context.Context, e TelemetryStepFinishEvent)

	// OnFinish is called once when the AI operation completes successfully.
	// OTel implementations should end the root span here.
	OnFinish(ctx context.Context, e TelemetryFinishEvent)

	// OnError is called when the AI operation fails with an error.
	// OTel implementations should record the error on the span and end it.
	OnError(ctx context.Context, e TelemetryErrorEvent)

	// ExecuteTool wraps tool execution, enabling integrations to create nested
	// child spans for tool→generateText chains.
	// Implementations MUST call execute and return its result unchanged.
	// The default (NoopTelemetryIntegration) delegates directly to execute.
	ExecuteTool(
		ctx context.Context,
		toolName string,
		args map[string]interface{},
		execute func(ctx context.Context, args map[string]interface{}) (interface{}, error),
	) (interface{}, error)
}

// ---------------------------------------------------------------------------
// NoopTelemetryIntegration
// ---------------------------------------------------------------------------

// NoopTelemetryIntegration implements TelemetryIntegration with all no-ops.
// It is used as the default when no integration has been registered, and by
// callers that want to temporarily suppress telemetry.
type NoopTelemetryIntegration struct{}

func (NoopTelemetryIntegration) OnStart(ctx context.Context, _ TelemetryStartEvent) context.Context {
	return ctx
}
func (NoopTelemetryIntegration) OnStepStart(_ context.Context, _ TelemetryStepStartEvent) {}
func (NoopTelemetryIntegration) OnToolCallStart(ctx context.Context, _ TelemetryToolCallStartEvent) context.Context {
	return ctx
}
func (NoopTelemetryIntegration) OnToolCallFinish(_ context.Context, _ TelemetryToolCallFinishEvent) {
}
func (NoopTelemetryIntegration) OnChunk(_ context.Context, _ TelemetryChunkEvent)           {}
func (NoopTelemetryIntegration) OnStepFinish(_ context.Context, _ TelemetryStepFinishEvent) {}
func (NoopTelemetryIntegration) OnFinish(_ context.Context, _ TelemetryFinishEvent)         {}
func (NoopTelemetryIntegration) OnError(_ context.Context, _ TelemetryErrorEvent)           {}
func (NoopTelemetryIntegration) ExecuteTool(
	ctx context.Context,
	_ string,
	args map[string]interface{},
	execute func(context.Context, map[string]interface{}) (interface{}, error),
) (interface{}, error) {
	return execute(ctx, args)
}

// ---------------------------------------------------------------------------
// OTelTelemetryIntegration
// ---------------------------------------------------------------------------

// OTelTelemetryIntegration translates TelemetryIntegration events into
// OpenTelemetry spans.  Register it to enable OTel tracing:
//
//	telemetry.RegisterTelemetryIntegration(telemetry.OTelTelemetryIntegration{})
type OTelTelemetryIntegration struct{}

// OnStart starts the root OTel span and embeds it in the returned context.
// Returns ctx unchanged when settings is nil or disabled.
func (OTelTelemetryIntegration) OnStart(ctx context.Context, e TelemetryStartEvent) context.Context {
	if e.Settings == nil || !e.Settings.IsEnabled {
		return ctx
	}
	tracer := GetTracer(e.Settings)
	spanName := e.OperationType
	if e.Settings.FunctionID != "" {
		spanName += "." + e.Settings.FunctionID
	}
	ctx, span := tracer.Start(ctx, spanName)
	span.SetAttributes(
		attribute.String("ai.operationId", e.OperationType),
		attribute.String("gen_ai.system", e.ModelProvider),
		attribute.String("gen_ai.request.model", e.ModelID),
	)
	if e.Settings.FunctionID != "" {
		span.SetAttributes(attribute.String("ai.telemetry.functionId", e.Settings.FunctionID))
	}
	for key, value := range e.Settings.Metadata {
		span.SetAttributes(attribute.KeyValue{
			Key:   attribute.Key("ai.telemetry.metadata." + key),
			Value: value,
		})
	}
	if e.Settings.RecordInputs && e.Prompt != "" {
		span.SetAttributes(attribute.String("ai.prompt", e.Prompt))
	}
	return ctx // span is embedded via OTel context propagation
}

func (OTelTelemetryIntegration) OnStepStart(_ context.Context, _ TelemetryStepStartEvent) {}

// OnToolCallStart starts a child span for tool execution and embeds it.
func (OTelTelemetryIntegration) OnToolCallStart(ctx context.Context, e TelemetryToolCallStartEvent) context.Context {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return ctx
	}
	tracer := span.TracerProvider().Tracer("go-ai")
	ctx, child := tracer.Start(ctx, "ai.toolCall."+e.ToolName)
	child.SetAttributes(
		attribute.String("ai.toolCall.id", e.ToolCallID),
		attribute.String("ai.toolCall.name", e.ToolName),
	)
	return ctx
}

// OnToolCallFinish ends the tool-call child span.
func (OTelTelemetryIntegration) OnToolCallFinish(ctx context.Context, e TelemetryToolCallFinishEvent) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	span.SetAttributes(attribute.Int64("ai.toolCall.durationMs", e.DurationMs))
	if e.Error != nil {
		span.RecordError(e.Error)
		span.SetStatus(codes.Error, e.Error.Error())
	}
	span.End()
}

func (OTelTelemetryIntegration) OnChunk(_ context.Context, _ TelemetryChunkEvent) {}
func (OTelTelemetryIntegration) OnStepFinish(_ context.Context, _ TelemetryStepFinishEvent) {}

// OnFinish sets output attributes on the root span and ends it.
func (OTelTelemetryIntegration) OnFinish(ctx context.Context, e TelemetryFinishEvent) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	if e.Settings != nil && e.Settings.RecordOutputs && e.Text != "" {
		span.SetAttributes(attribute.String("ai.response.text", e.Text))
	}
	if e.Settings != nil && e.Settings.RecordOutputs && len(e.Files) > 0 {
		type fileEntry struct {
			Type      string `json:"type"`
			MediaType string `json:"mediaType"`
			Data      string `json:"data"`
		}
		entries := make([]fileEntry, len(e.Files))
		for i, f := range e.Files {
			entries[i] = fileEntry{
				Type:      "file",
				MediaType: f.MediaType,
				Data:      base64.StdEncoding.EncodeToString(f.Data),
			}
		}
		if b, err := json.Marshal(entries); err == nil {
			span.SetAttributes(attribute.String("ai.response.files", string(b)))
		}
	}
	span.SetAttributes(attribute.String("ai.response.finishReason", e.FinishReason))
	// Gen AI semantic convention attributes (OpenTelemetry Gen AI spec).
	if e.Usage.InputTokens != nil {
		span.SetAttributes(attribute.Int64("gen_ai.usage.input_tokens", *e.Usage.InputTokens))
	}
	if e.Usage.OutputTokens != nil {
		span.SetAttributes(attribute.Int64("gen_ai.usage.output_tokens", *e.Usage.OutputTokens))
	}

	// Legacy ai.usage.* attributes — TS SDK emits both namespaces for backward compat.
	if e.Usage.InputTokens != nil {
		span.SetAttributes(attribute.Int64("ai.usage.inputTokens", *e.Usage.InputTokens))
	}
	if e.Usage.OutputTokens != nil {
		span.SetAttributes(attribute.Int64("ai.usage.outputTokens", *e.Usage.OutputTokens))
	}
	if e.Usage.TotalTokens != nil {
		span.SetAttributes(attribute.Int64("ai.usage.totalTokens", *e.Usage.TotalTokens))
	}
	if e.Usage.ReasoningTokens != nil {
		span.SetAttributes(attribute.Int64("ai.usage.reasoningTokens", *e.Usage.ReasoningTokens))
	}
	// ai.usage.cachedInputTokens is a legacy flat alias for cacheReadTokens.
	if e.Usage.CacheReadInputTokens != nil {
		span.SetAttributes(attribute.Int64("ai.usage.cachedInputTokens", *e.Usage.CacheReadInputTokens))
	}
	if e.Usage.NoCacheInputTokens != nil {
		span.SetAttributes(attribute.Int64("ai.usage.inputTokenDetails.noCacheTokens", *e.Usage.NoCacheInputTokens))
	}
	if e.Usage.CacheReadInputTokens != nil {
		span.SetAttributes(attribute.Int64("ai.usage.inputTokenDetails.cacheReadTokens", *e.Usage.CacheReadInputTokens))
	}
	if e.Usage.CacheCreationInputTokens != nil {
		span.SetAttributes(attribute.Int64("ai.usage.inputTokenDetails.cacheWriteTokens", *e.Usage.CacheCreationInputTokens))
	}
	if e.Usage.OutputTextTokens != nil {
		span.SetAttributes(attribute.Int64("ai.usage.outputTokenDetails.textTokens", *e.Usage.OutputTextTokens))
	}
	if e.Usage.ReasoningTokens != nil {
		span.SetAttributes(attribute.Int64("ai.usage.outputTokenDetails.reasoningTokens", *e.Usage.ReasoningTokens))
	}
	span.End()
}

// OnError records the error on the root span and ends it.
func (OTelTelemetryIntegration) OnError(ctx context.Context, e TelemetryErrorEvent) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	if e.Error != nil {
		span.RecordError(e.Error)
		span.SetStatus(codes.Error, e.Error.Error())
	}
	span.End()
}

// ExecuteTool delegates directly to execute. Nested span support can be added here.
func (OTelTelemetryIntegration) ExecuteTool(
	ctx context.Context,
	_ string,
	args map[string]interface{},
	execute func(context.Context, map[string]interface{}) (interface{}, error),
) (interface{}, error) {
	return execute(ctx, args)
}

// ---------------------------------------------------------------------------
// Registry — slice-based for composite fan-out (Gap 2)
// ---------------------------------------------------------------------------

var (
	mu           sync.RWMutex
	integrations []TelemetryIntegration
)

// RegisterTelemetryIntegration replaces all registered integrations with i.
// Pass NoopTelemetryIntegration{} to reset to the quiet default.
// Safe to call concurrently with fire functions.
func RegisterTelemetryIntegration(i TelemetryIntegration) {
	mu.Lock()
	defer mu.Unlock()
	integrations = []TelemetryIntegration{i}
}

// AddTelemetryIntegration appends i to the list of registered integrations.
// All registered integrations receive every event (fan-out).
// Safe to call concurrently with fire functions.
func AddTelemetryIntegration(i TelemetryIntegration) {
	mu.Lock()
	defer mu.Unlock()
	integrations = append(integrations, i)
}

// ClearTelemetryIntegrations removes all registered integrations.
// After this call, telemetry events are silently discarded.
func ClearTelemetryIntegrations() {
	mu.Lock()
	defer mu.Unlock()
	integrations = nil
}

// GetTelemetryIntegration returns the first registered integration, or
// NoopTelemetryIntegration if none has been registered.
// Provided for backward compatibility; prefer the Fire* functions.
func GetTelemetryIntegration() TelemetryIntegration {
	mu.RLock()
	defer mu.RUnlock()
	if len(integrations) == 0 {
		return NoopTelemetryIntegration{}
	}
	return integrations[0]
}

// snapshot returns a copy of the integrations slice under read-lock.
func snapshot() []TelemetryIntegration {
	mu.RLock()
	defer mu.RUnlock()
	return integrations
}

// ---------------------------------------------------------------------------
// Fire functions — fan-out to all registered integrations
// ---------------------------------------------------------------------------

// FireOnStart calls OnStart on every registered integration, threading the
// returned context through the chain so each integration can inject spans.
func FireOnStart(ctx context.Context, e TelemetryStartEvent) context.Context {
	for _, i := range snapshot() {
		ctx = i.OnStart(ctx, e)
	}
	return ctx
}

// FireOnStepStart calls OnStepStart on every registered integration.
func FireOnStepStart(ctx context.Context, e TelemetryStepStartEvent) {
	for _, i := range snapshot() {
		i.OnStepStart(ctx, e)
	}
}

// FireOnToolCallStart calls OnToolCallStart on every registered integration,
// threading the returned context through the chain.
func FireOnToolCallStart(ctx context.Context, e TelemetryToolCallStartEvent) context.Context {
	for _, i := range snapshot() {
		ctx = i.OnToolCallStart(ctx, e)
	}
	return ctx
}

// FireOnToolCallFinish calls OnToolCallFinish on every registered integration.
func FireOnToolCallFinish(ctx context.Context, e TelemetryToolCallFinishEvent) {
	for _, i := range snapshot() {
		i.OnToolCallFinish(ctx, e)
	}
}

// FireOnChunk calls OnChunk on every registered integration.
func FireOnChunk(ctx context.Context, e TelemetryChunkEvent) {
	for _, i := range snapshot() {
		i.OnChunk(ctx, e)
	}
}

// FireOnStepFinish calls OnStepFinish on every registered integration.
func FireOnStepFinish(ctx context.Context, e TelemetryStepFinishEvent) {
	for _, i := range snapshot() {
		i.OnStepFinish(ctx, e)
	}
}

// FireOnFinish calls OnFinish on every registered integration.
func FireOnFinish(ctx context.Context, e TelemetryFinishEvent) {
	for _, i := range snapshot() {
		i.OnFinish(ctx, e)
	}
}

// FireOnError calls OnError on every registered integration.
func FireOnError(ctx context.Context, e TelemetryErrorEvent) {
	for _, i := range snapshot() {
		i.OnError(ctx, e)
	}
}

// FireExecuteTool chains ExecuteTool across all registered integrations (Gap 4).
// Each integration wraps the next; the actual tool function is at the innermost level.
func FireExecuteTool(
	ctx context.Context,
	toolName string,
	args map[string]interface{},
	execute func(ctx context.Context, args map[string]interface{}) (interface{}, error),
) (interface{}, error) {
	is := snapshot()
	if len(is) == 0 {
		return execute(ctx, args)
	}
	// Build chain from innermost (execute) outward.
	fn := execute
	for i := len(is) - 1; i >= 0; i-- {
		fn = makeToolFn(is[i], toolName, fn)
	}
	return fn(ctx, args)
}

// makeToolFn avoids loop-variable capture issues when building the ExecuteTool chain.
func makeToolFn(
	integration TelemetryIntegration,
	toolName string,
	next func(context.Context, map[string]interface{}) (interface{}, error),
) func(context.Context, map[string]interface{}) (interface{}, error) {
	return func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		return integration.ExecuteTool(ctx, toolName, args, next)
	}
}
