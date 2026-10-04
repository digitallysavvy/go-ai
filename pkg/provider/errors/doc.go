// Package errors defines the error types returned by the SDK and its providers:
// API call errors, rate limits, invalid arguments, unknown models, invalid
// responses, download failures, retries exhausted, and more.
//
// Each type has an Is function that reports whether an error is that kind, even
// when it is wrapped:
//
//	result, err := ai.GenerateText(ctx, opts)
//	if errors.IsRateLimitError(err) {
//		// wait and retry
//	}
//
// This package is named errors, so import it with an alias when you also need
// the standard library package, for example:
//
//	import (
//		stderrors "errors"
//
//		"github.com/digitallysavvy/go-ai/pkg/provider/errors"
//	)
//
// Guide: https://goaisdk.com/docs/ai-sdk-core/error-handling and
// https://goaisdk.com/docs/reference/types/errors.
package errors
