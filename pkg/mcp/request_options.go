package mcp

import (
	"context"
	"errors"
	"time"
)

// RequestOptions are per-request limits, the Go form of TS RequestOptions
// ({ signal, timeout, maxTotalTimeout }). The signal is the call's context.
//
// When both limits are set the smaller applies (TS getEffectiveTimeout). When
// neither is set the client's RequestTimeoutMS applies.
type RequestOptions struct {
	// TimeoutMS is the per-request timeout in milliseconds.
	TimeoutMS int
	// MaxTotalTimeoutMS is the overall time limit in milliseconds.
	MaxTotalTimeoutMS int
}

type requestOptionsKey struct{}

// WithRequestOptions returns a context that carries per-request options for
// the MCP client calls made with it (ListTools, CallTool, ReadResource, ...).
func WithRequestOptions(ctx context.Context, opts RequestOptions) context.Context {
	return context.WithValue(ctx, requestOptionsKey{}, opts)
}

func requestOptionsFrom(ctx context.Context) (RequestOptions, bool) {
	opts, ok := ctx.Value(requestOptionsKey{}).(RequestOptions)
	return opts, ok
}

// effectiveTimeout mirrors TS getEffectiveTimeout; zero means no limit set.
func (o RequestOptions) effectiveTimeout() time.Duration {
	t, m := o.TimeoutMS, o.MaxTotalTimeoutMS
	switch {
	case t <= 0 && m <= 0:
		return 0
	case t <= 0:
		return time.Duration(m) * time.Millisecond
	case m <= 0:
		return time.Duration(t) * time.Millisecond
	case t < m:
		return time.Duration(t) * time.Millisecond
	default:
		return time.Duration(m) * time.Millisecond
	}
}

var errInitTimeout = errors.New("mcp initialization timed out")
