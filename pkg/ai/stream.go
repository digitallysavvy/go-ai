package ai

import (
	"context"
	"fmt"
	"io"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// StreamTextOptions contains options for streaming text generation
type StreamTextOptions struct {
	// Model to use for generation
	Model provider.LanguageModel

	// Prompt can be a simple string or a list of messages
	Prompt string
	Messages []types.Message
	System string

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
	Tools []types.Tool
	ToolChoice types.ToolChoice

	// Response format (for structured output)
	ResponseFormat *provider.ResponseFormat

	// Timeout provides granular timeout controls
	// Supports total timeout, per-step timeout, and per-chunk timeout
	Timeout *TimeoutConfig

	// ExperimentalRetention controls what data is retained from LLM requests/responses.
	// Useful for reducing memory consumption with images or large contexts.
	// Default (nil) retains everything for backwards compatibility.
	ExperimentalRetention *types.RetentionSettings

	// ProviderOptions allows passing provider-specific options
	ProviderOptions map[string]interface{}

	// Telemetry configuration for observability
	ExperimentalTelemetry *TelemetrySettings

	// Callbacks
	OnChunk  func(chunk provider.StreamChunk)
	OnFinish func(result *StreamTextResult)
}

// StreamTextResult contains the result of streaming text generation
type StreamTextResult struct {
	// Stream of chunks
	stream provider.TextStream

	// Accumulated text (built as chunks arrive)
	text string

	// Finish reason (set when stream completes)
	finishReason types.FinishReason

	// Usage information (set when stream completes)
	usage types.Usage

	// Context management information (Anthropic-specific)
	// Contains statistics about automatic conversation history cleanup
	contextManagement interface{}

	// Error that occurred during streaming
	err error

	// Timeout configuration for per-chunk timeouts
	timeout *TimeoutConfig

	// Telemetry span (closed when stream completes)
	telemetrySpan trace.Span
	telemetryOpts *TelemetrySettings
}

// StreamText performs streaming text generation
func StreamText(ctx context.Context, opts StreamTextOptions) (*StreamTextResult, error) {
	// Validate options
	if opts.Model == nil {
		return nil, fmt.Errorf("model is required")
	}

	// Create telemetry span if enabled
	var span trace.Span
	if opts.ExperimentalTelemetry != nil && opts.ExperimentalTelemetry.IsEnabled {
		tracer := telemetry.GetTracer(opts.ExperimentalTelemetry)

		// Create top-level ai.streamText span
		spanName := "ai.streamText"
		if opts.ExperimentalTelemetry.FunctionID != "" {
			spanName = spanName + "." + opts.ExperimentalTelemetry.FunctionID
		}

		ctx, span = tracer.Start(ctx, spanName)
		// Note: span.End() is deferred in processStream when stream completes

		// Add base telemetry attributes
		span.SetAttributes(
			attribute.String("ai.operationId", "ai.streamText"),
			attribute.String("ai.model.provider", opts.Model.Provider()),
			attribute.String("ai.model.id", opts.Model.ModelID()),
		)

		// Add function ID if present
		if opts.ExperimentalTelemetry.FunctionID != "" {
			span.SetAttributes(attribute.String("ai.telemetry.functionId", opts.ExperimentalTelemetry.FunctionID))
		}

		// Add custom metadata
		for key, value := range opts.ExperimentalTelemetry.Metadata {
			span.SetAttributes(attribute.KeyValue{
				Key:   attribute.Key("ai.telemetry.metadata." + key),
				Value: value,
			})
		}

		// Record prompt if enabled
		if opts.ExperimentalTelemetry.RecordInputs && opts.Prompt != "" {
			span.SetAttributes(attribute.String("ai.prompt", opts.Prompt))
		}
	}

	// Apply total timeout if configured
	if opts.Timeout != nil && opts.Timeout.HasTotal() {
		var cancel context.CancelFunc
		ctx, cancel = opts.Timeout.CreateTimeoutContext(ctx, "total")
		defer cancel()
	}

	// Build prompt
	prompt := buildPrompt(opts.Prompt, opts.Messages, opts.System)

	// Build generate options
	genOpts := &provider.GenerateOptions{
		Prompt:           prompt,
		Temperature:      opts.Temperature,
		MaxTokens:        opts.MaxTokens,
		TopP:             opts.TopP,
		TopK:             opts.TopK,
		FrequencyPenalty: opts.FrequencyPenalty,
		PresencePenalty:  opts.PresencePenalty,
		StopSequences:    opts.StopSequences,
		Seed:             opts.Seed,
		Tools:            opts.Tools,
		ToolChoice:       opts.ToolChoice,
		ResponseFormat:   opts.ResponseFormat,
		ProviderOptions:  opts.ProviderOptions,
		Telemetry:        opts.ExperimentalTelemetry,
	}

	// Start streaming
	stream, err := opts.Model.DoStream(ctx, genOpts)
	if err != nil {
		if span != nil {
			span.End()
		}
		return nil, fmt.Errorf("failed to start stream: %w", err)
	}

	// Create result
	result := &StreamTextResult{
		stream:        stream,
		timeout:       opts.Timeout,
		telemetrySpan: span,
		telemetryOpts: opts.ExperimentalTelemetry,
	}

	// Start goroutine to process chunks and call callbacks
	if opts.OnChunk != nil || opts.OnFinish != nil {
		go result.processStream(opts.OnChunk, opts.OnFinish)
	}

	return result, nil
}

// processStream processes the stream and calls callbacks
func (r *StreamTextResult) processStream(onChunk func(provider.StreamChunk), onFinish func(*StreamTextResult)) {
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

		// Accumulate text
		if chunk.Type == provider.ChunkTypeText {
			r.text += chunk.Text
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

		// Call chunk callback
		if onChunk != nil {
			onChunk(*chunk)
		}
	}

	// Record telemetry output attributes if span exists
	if r.telemetrySpan != nil {
		// Record output if enabled
		if r.telemetryOpts != nil && r.telemetryOpts.RecordOutputs {
			r.telemetrySpan.SetAttributes(attribute.String("ai.response.text", r.text))
		}

		// Record finish reason
		r.telemetrySpan.SetAttributes(attribute.String("ai.response.finishReason", string(r.finishReason)))

		// Record usage information
		if r.usage.InputTokens != nil {
			r.telemetrySpan.SetAttributes(attribute.Int64("ai.usage.promptTokens", *r.usage.InputTokens))
		}
		if r.usage.OutputTokens != nil {
			r.telemetrySpan.SetAttributes(attribute.Int64("ai.usage.completionTokens", *r.usage.OutputTokens))
		}
		if r.usage.TotalTokens != nil {
			r.telemetrySpan.SetAttributes(attribute.Int64("ai.usage.totalTokens", *r.usage.TotalTokens))
		}

		// End the telemetry span
		r.telemetrySpan.End()
	}

	// Call finish callback
	if onFinish != nil {
		onFinish(r)
	}
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

// Usage returns the usage information (only available after stream completes)
func (r *StreamTextResult) Usage() types.Usage {
	return r.usage
}

// ContextManagement returns context management statistics (Anthropic-specific)
// Only available after stream completes
func (r *StreamTextResult) ContextManagement() interface{} {
	return r.contextManagement
}

// Err returns any error that occurred during streaming
func (r *StreamTextResult) Err() error {
	return r.err
}

// Close closes the stream
func (r *StreamTextResult) Close() error {
	return r.stream.Close()
}

// ReadAll reads all chunks from the stream and returns the complete text
func (r *StreamTextResult) ReadAll() (string, error) {
	ctx := context.Background()
	for {
		chunk, err := r.nextChunk(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		// Accumulate text
		if chunk.Type == provider.ChunkTypeText {
			r.text += chunk.Text
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
	}

	// Record telemetry output attributes if span exists
	if r.telemetrySpan != nil {
		// Record output if enabled
		if r.telemetryOpts != nil && r.telemetryOpts.RecordOutputs {
			r.telemetrySpan.SetAttributes(attribute.String("ai.response.text", r.text))
		}

		// Record finish reason
		r.telemetrySpan.SetAttributes(attribute.String("ai.response.finishReason", string(r.finishReason)))

		// Record usage information
		if r.usage.InputTokens != nil {
			r.telemetrySpan.SetAttributes(attribute.Int64("ai.usage.promptTokens", *r.usage.InputTokens))
		}
		if r.usage.OutputTokens != nil {
			r.telemetrySpan.SetAttributes(attribute.Int64("ai.usage.completionTokens", *r.usage.OutputTokens))
		}
		if r.usage.TotalTokens != nil {
			r.telemetrySpan.SetAttributes(attribute.Int64("ai.usage.totalTokens", *r.usage.TotalTokens))
		}

		// End the telemetry span
		r.telemetrySpan.End()
	}

	return r.text, nil
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
		return nil, fmt.Errorf("chunk timeout exceeded: %w", chunkCtx.Err())
	}
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
