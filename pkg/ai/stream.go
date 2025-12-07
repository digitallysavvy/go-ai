package ai

import (
	"context"
	"fmt"
	"io"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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

	// Error that occurred during streaming
	err error
}

// StreamText performs streaming text generation
func StreamText(ctx context.Context, opts StreamTextOptions) (*StreamTextResult, error) {
	// Validate options
	if opts.Model == nil {
		return nil, fmt.Errorf("model is required")
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
	}

	// Start streaming
	stream, err := opts.Model.DoStream(ctx, genOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to start stream: %w", err)
	}

	// Create result
	result := &StreamTextResult{
		stream: stream,
	}

	// Start goroutine to process chunks and call callbacks
	if opts.OnChunk != nil || opts.OnFinish != nil {
		go result.processStream(opts.OnChunk, opts.OnFinish)
	}

	return result, nil
}

// processStream processes the stream and calls callbacks
func (r *StreamTextResult) processStream(onChunk func(provider.StreamChunk), onFinish func(*StreamTextResult)) {
	for {
		chunk, err := r.stream.Next()
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

		// Update finish reason and usage
		if chunk.Type == provider.ChunkTypeFinish {
			r.finishReason = chunk.FinishReason
		}
		if chunk.Usage != nil {
			r.usage = *chunk.Usage
		}

		// Call chunk callback
		if onChunk != nil {
			onChunk(*chunk)
		}
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
	for {
		chunk, err := r.stream.Next()
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

		// Update finish reason and usage
		if chunk.Type == provider.ChunkTypeFinish {
			r.finishReason = chunk.FinishReason
		}
		if chunk.Usage != nil {
			r.usage = *chunk.Usage
		}
	}

	return r.text, nil
}

// Chunks returns a channel that streams chunks
// This provides an idiomatic Go way to consume the stream
func (r *StreamTextResult) Chunks() <-chan provider.StreamChunk {
	ch := make(chan provider.StreamChunk, 10)

	go func() {
		defer close(ch)
		for {
			chunk, err := r.stream.Next()
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
