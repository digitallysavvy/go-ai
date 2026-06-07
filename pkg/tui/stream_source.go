package tui

import (
	"context"
	"io"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// StreamRenderSource wraps StreamTextResult for renderer consumption.
type StreamRenderSource struct {
	result           *ai.StreamTextResult
	stream           provider.TextStream
	responseMessages []types.Message
	finalStep        types.StepResult
	usage            types.Usage
}

func NewStreamRenderSource(result *ai.StreamTextResult) *StreamRenderSource {
	return &StreamRenderSource{result: result}
}

func NewStreamRenderSourceFromTextStream(stream provider.TextStream, responseMessages []types.Message, finalStep types.StepResult) *StreamRenderSource {
	return &StreamRenderSource{stream: stream, responseMessages: responseMessages, finalStep: finalStep, usage: finalStep.Usage}
}

func (s *StreamRenderSource) Next(ctx context.Context) (*provider.StreamChunk, error) {
	if s == nil {
		return nil, io.EOF
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	stream := s.stream
	if stream == nil && s.result != nil {
		stream = s.result.Stream()
	}
	if stream == nil {
		return nil, io.EOF
	}
	type nextResult struct {
		chunk *provider.StreamChunk
		err   error
	}
	ch := make(chan nextResult, 1)
	go func() {
		chunk, err := stream.Next()
		ch <- nextResult{chunk: chunk, err: err}
	}()
	select {
	case <-ctx.Done():
		_ = stream.Close()
		return nil, ctx.Err()
	case result := <-ch:
		return result.chunk, result.err
	}
}

func (s *StreamRenderSource) Close() error {
	if s == nil {
		return nil
	}
	if s.stream != nil {
		return s.stream.Close()
	}
	if s.result == nil {
		return nil
	}
	return s.result.Close()
}

func (s *StreamRenderSource) ResponseMessages() []types.Message {
	if s == nil {
		return nil
	}
	if s.result == nil {
		return append([]types.Message(nil), s.responseMessages...)
	}
	return s.result.ResponseMessages()
}

func (s *StreamRenderSource) Usage() types.Usage {
	if s == nil {
		return types.Usage{}
	}
	if s.result == nil {
		return s.usage
	}
	return s.result.Usage()
}

func (s *StreamRenderSource) FinalStep() types.StepResult {
	if s == nil {
		return types.StepResult{}
	}
	if s.result == nil {
		return s.finalStep
	}
	return s.result.FinalStep()
}
