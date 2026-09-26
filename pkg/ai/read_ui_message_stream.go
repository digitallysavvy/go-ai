package ai

import (
	"context"
	"errors"
	"strings"
)

// ReadUIMessagesOptions configures ReadUIMessages.
type ReadUIMessagesOptions struct {
	// Message is the last assistant message to continue when a conversation
	// is resumed. Optional.
	Message *UIMessage
	// Stream is the UI message chunk stream to read.
	Stream <-chan UIMessageChunk
	// OnError is called for processing errors (e.g. a delta for a missing
	// part). Optional.
	OnError func(error)
	// TerminateOnError stops reading and reports the first error on the
	// error channel.
	TerminateOnError bool
}

// ReadUIMessages transforms a stream of UI message chunks into a stream of
// UIMessage snapshots: each value is a new state of the same assistant
// message as it is being completed. Mirrors TS readUIMessageStream (the
// SSE-level ReadUIMessageStream reads raw chunks from an io.Reader).
//
// The error channel receives at most one error (TerminateOnError, or a
// snapshot conversion failure) and is closed with the message channel.
func ReadUIMessages(ctx context.Context, opts ReadUIMessagesOptions) (<-chan UIMessage, <-chan error) {
	out := make(chan UIMessage)
	errCh := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errCh)
		if ctx == nil {
			ctx = context.Background()
		}
		if opts.Stream == nil {
			errCh <- errors.New("stream is required")
			return
		}

		var original []UIMessageChunk
		messageID := ""
		if opts.Message != nil {
			messageID = opts.Message.ID
			chunk, err := opts.Message.ToChunk()
			if err != nil {
				errCh <- err
				return
			}
			original = []UIMessageChunk{chunk}
		}
		// Only an assistant message is continued; otherwise a new message
		// with the given ID is started.
		state := newUIMessageCallbackState(original, messageID)

		for {
			select {
			case <-ctx.Done():
				return
			case chunk, ok := <-opts.Stream:
				if !ok {
					return
				}
				var processingErr error
				state.apply(chunk, func(err error) string {
					if processingErr == nil {
						processingErr = err
					}
					if opts.OnError != nil {
						opts.OnError(err)
					}
					return ""
				})
				if processingErr != nil {
					if opts.TerminateOnError {
						errCh <- processingErr
						return
					}
					// An `error` chunk is reported and reading continues; any
					// other processing failure ends the stream (TS errors the
					// transform stream, which closes the output).
					if stringValue(chunk["type"]) != "error" {
						return
					}
					continue
				}
				if !uiChunkWritesMessage(chunk) {
					continue
				}
				snapshot, err := UIMessageFromChunk(state.responseMessage())
				if err != nil {
					errCh <- err
					return
				}
				select {
				case out <- snapshot:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, errCh
}

// uiChunkWritesMessage reports whether TS processUIMessageStream calls
// write() (emits a new message snapshot) for the chunk.
func uiChunkWritesMessage(chunk UIMessageChunk) bool {
	chunkType := stringValue(chunk["type"])
	switch chunkType {
	case "start":
		return chunk["messageId"] != nil || chunk["messageMetadata"] != nil
	case "finish", "message-metadata":
		return chunk["messageMetadata"] != nil
	case "start-step", "finish-step", "error", "abort":
		return false
	}
	if strings.HasPrefix(chunkType, "data-") {
		transient, _ := chunk["transient"].(bool)
		return !transient
	}
	return true
}
