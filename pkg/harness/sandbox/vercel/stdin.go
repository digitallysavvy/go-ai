package vercel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"os"
	"sync"
	"time"

	httpinternal "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// Writable stdin for detached commands, ported from @vercel/sandbox 3.9.0
// (dist/api-client/api-client.js `writeCommandStdin`, dist/command.js
// `writeStdin` / `closeStdin`) and packages/sandbox-vercel
// vercel-sandbox-session.ts `createStdinStream`.

const (
	// stdinMaxRetries is TS STDIN_MAX_RETRIES.
	stdinMaxRetries = 5
	// stdinMaxStalledTimeouts is TS STDIN_MAX_STALLED_TIMEOUTS.
	stdinMaxStalledTimeouts = 2
	// stdinChunkBytes is TS STDIN_CHUNK_BYTES: bytes sent per stdin request,
	// keeping the base64 JSON body under the API's 1 MB request limit.
	stdinChunkBytes = 512 * 1024
)

// stdinBackoff is TS `stdinBackoff`: 400 ms * 2^attempt.
var stdinBackoff = func(attempt int) time.Duration {
	return time.Duration(400*math.Pow(2, float64(attempt))) * time.Millisecond
}

// writeStdinRequest is the POST .../cmd/{id}/stdin body. JSON.stringify
// drops the undefined members: data when there is nothing to send, offset
// when it is unknown, close unless true.
type writeStdinRequest struct {
	Data   string `json:"data,omitempty"`
	Offset *int64 `json:"offset,omitempty"`
	Close  bool   `json:"close,omitempty"`
}

// writeStdinParams are the inputs of writeCommandStdin.
type writeStdinParams struct {
	data   []byte
	offset *int64
	close  bool
}

// stdinWriteError marks an error after which the request may already have
// been applied (TS `stdinMaybeWrittenErrors`).
type stdinWriteError struct {
	err          error
	maybeApplied bool
}

func (e *stdinWriteError) Error() string { return e.err.Error() }
func (e *stdinWriteError) Unwrap() error { return e.err }

// waitBeforeStdinRetry is TS `waitBeforeStdinRetry`.
func waitBeforeStdinRetry(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// writeCommandStdin mirrors TS `APIClient.writeCommandStdin`. With an
// offset the request is safe to resend (the server skips bytes it already
// has), so it is retried across connection failures, 502, 503 and 504.
// Without it only rate limited requests are retried, since any other failure
// may have written the bytes already. It returns the reported bytesWritten
// (nil when the response has none).
func (c *APIClient) writeCommandStdin(ctx context.Context, sessionID, cmdID string, p writeStdinParams) (*int64, error) {
	body := writeStdinRequest{Offset: p.offset, Close: p.close}
	if len(p.data) > 0 {
		body.Data = base64.StdEncoding.EncodeToString(p.data)
	}
	resendable := p.offset != nil || (len(p.data) == 0 && !p.close)
	maybeApplied := false
	stalledTimeouts := 0
	var lastWritten int64
	if p.offset != nil {
		lastWritten = *p.offset
	}
	path := "/v2/sandboxes/sessions/" + pathEscape(sessionID) + "/cmd/" + pathEscape(cmdID) + "/stdin"
	for attempt := 0; ; attempt++ {
		canRetry := attempt < stdinMaxRetries
		resp, err := c.http.Do(withoutRetry(ctx), httpinternal.Request{
			Method: http.MethodPost, Path: path, Headers: c.headers(nil), Query: c.query(nil), Body: body,
		})
		if err != nil {
			if !resendable || !canRetry || ctx.Err() != nil {
				return nil, err
			}
			maybeApplied = true
			if err := waitBeforeStdinRetry(ctx, stdinBackoff(attempt)); err != nil {
				return nil, err
			}
			continue
		}
		status := resp.StatusCode
		if status == http.StatusNotFound && maybeApplied && p.close && len(p.data) == 0 {
			return nil, nil
		}
		if status == http.StatusTooManyRequests && canRetry {
			retryAfter := jsNumber(resp.Headers.Get("Retry-After"))
			if !(retryAfter > 20) {
				wait := stdinBackoff(attempt)
				if retryAfter > 0 {
					wait = time.Duration(retryAfter * float64(time.Second))
				}
				if err := waitBeforeStdinRetry(ctx, wait); err != nil {
					return nil, err
				}
				continue
			}
		}
		if resendable && canRetry && (status == http.StatusBadGateway || status == http.StatusServiceUnavailable) {
			maybeApplied = true
			if err := waitBeforeStdinRetry(ctx, stdinBackoff(attempt)); err != nil {
				return nil, err
			}
			continue
		}
		if resendable && canRetry && status == http.StatusGatewayTimeout {
			maybeApplied = true
			if written, ok := stdinTimeoutBytesWritten(resp.Body); ok && written > lastWritten {
				lastWritten = written
				stalledTimeouts = 0
			} else if stalledTimeouts++; stalledTimeouts >= stdinMaxStalledTimeouts {
				return parseStdinResponse(resp, maybeApplied)
			}
			continue
		}
		return parseStdinResponse(resp, maybeApplied)
	}
}

// parseStdinResponse is TS `parseStdinResponse` (parseOrThrow with
// CommandStdinResponse): an error status is an *APIError; bytesWritten must
// be a non-negative integer when present.
func parseStdinResponse(resp *httpinternal.Response, maybeApplied bool) (*int64, error) {
	if resp.StatusCode >= 400 {
		err := toAPIError(&httpinternal.HTTPStatusError{StatusCode: resp.StatusCode, Headers: resp.Headers, Body: resp.Body})
		return nil, &stdinWriteError{err: err, maybeApplied: maybeApplied}
	}
	var out struct {
		BytesWritten *float64 `json:"bytesWritten"`
	}
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return nil, &stdinWriteError{err: err, maybeApplied: maybeApplied}
	}
	if out.BytesWritten == nil {
		return nil, nil
	}
	v := *out.BytesWritten
	if v < 0 || v != math.Trunc(v) || v > float64(math.MaxInt64) {
		return nil, &stdinWriteError{err: errors.New("Response JSON is not valid: bytesWritten must be a non-negative integer"), maybeApplied: maybeApplied} //nolint:staticcheck // TS parse error text prefix
	}
	n := int64(v)
	return &n, nil
}

// stdinTimeoutBytesWritten is TS `readStdinTimeout`: the 504 body's
// error.bytesWritten when it is a safe non-negative integer.
func stdinTimeoutBytesWritten(body []byte) (int64, bool) {
	var out struct {
		Error struct {
			BytesWritten *float64 `json:"bytesWritten"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &out) != nil || out.Error.BytesWritten == nil {
		return 0, false
	}
	v := *out.Error.BytesWritten
	if v < 0 || v != math.Trunc(v) || v > 9007199254740991 {
		return 0, false
	}
	return int64(v), true
}

// jsNumber is JS `Number(s)` (NaN when not numeric).
func jsNumber(s string) float64 {
	if v, ok := providerutils.JSNumber(s); ok {
		return v
	}
	return math.NaN()
}

// isRejectedBeforeWrite is TS `isRejectedBeforeWrite`: the API rejected
// the write with a client error before applying it.
func isRejectedBeforeWrite(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode < 400 || apiErr.StatusCode >= 500 {
		return false
	}
	var w *stdinWriteError
	return !errors.As(err, &w) || !w.maybeApplied
}

// errStdinOffsetUnknown is TS "The API did not report the bytes written to
// stdin.".
var errStdinOffsetUnknown = errors.New("The API did not report the bytes written to stdin.") //nolint:staticcheck // matches TS SDK's exact error text

// commandStdin is the writable stdin of a detached command started with
// attachStdin. Writes are delivered in order (TS `enqueueStdin`) and return
// once the process has accepted the data. Like the TS WritableStream from
// `createStdinStream`, a failed write (or the spawn context ending) errors
// the stream: later writes and Close return that error and stdin is not
// closed, because EOF after a partial write could make the process treat
// truncated input as complete.
type commandStdin struct {
	ctx       context.Context
	client    *APIClient
	sessionID string
	cmdID     string

	mu        sync.Mutex
	offset    *int64
	streamErr error
	closed    bool
}

// resolveOffset is TS `resolveStdinOffset`.
func (s *commandStdin) resolveOffset() error {
	if s.offset != nil {
		return nil
	}
	written, err := s.client.writeCommandStdin(s.ctx, s.sessionID, s.cmdID, writeStdinParams{})
	if err != nil {
		return err
	}
	if written == nil {
		return errStdinOffsetUnknown
	}
	s.offset = written
	return nil
}

func (s *commandStdin) usable() error {
	if s.closed {
		return os.ErrClosed
	}
	if s.streamErr != nil {
		return s.streamErr
	}
	if err := s.ctx.Err(); err != nil {
		s.streamErr = err
		return err
	}
	return nil
}

// Write is TS `Command.writeStdin` behind the WritableStream sink.
func (s *commandStdin) Write(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(); err != nil {
		return 0, err
	}
	if len(b) == 0 {
		return 0, nil
	}
	if err := s.resolveOffset(); err != nil {
		s.streamErr = err
		return 0, err
	}
	sent := 0
	for i := 0; i < len(b); i += stdinChunkBytes {
		end := i + stdinChunkBytes
		if end > len(b) {
			end = len(b)
		}
		chunk := b[i:end]
		offset := *s.offset
		written, err := s.client.writeCommandStdin(s.ctx, s.sessionID, s.cmdID, writeStdinParams{data: chunk, offset: &offset})
		if err != nil {
			if sent > 0 || !isRejectedBeforeWrite(err) {
				s.offset = nil
			}
			s.streamErr = err
			return sent, err
		}
		if written != nil {
			s.offset = written
		} else {
			next := offset + int64(len(chunk))
			s.offset = &next
		}
		sent = end
	}
	return sent, nil
}

// Close is TS `Command.closeStdin`: the process reads EOF once it has
// consumed any pending data.
func (s *commandStdin) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(); err != nil {
		return err
	}
	s.closed = true
	if err := s.resolveOffset(); err != nil {
		return err
	}
	offset := *s.offset
	_, err := s.client.writeCommandStdin(s.ctx, s.sessionID, s.cmdID, writeStdinParams{offset: &offset, close: true})
	return err
}
