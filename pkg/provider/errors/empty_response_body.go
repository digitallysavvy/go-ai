package errors

import "errors"

// FailedToProcessSuccessfulResponseMessage is the message of the TS
// APICallError postToApi / getFromApi throw when the successful-response
// handler fails with an error that is not itself an APICallError (for
// example the EmptyResponseBodyError of a stream response without a body).
const FailedToProcessSuccessfulResponseMessage = "Failed to process successful response"

// EmptyResponseBodyError is TS AI_EmptyResponseBodyError: a successful
// response that has no body (fetch gives a 204 or 205 response a null body),
// where the response handler needs one to stream from.
type EmptyResponseBodyError struct {
	// Message defaults to "Empty response body".
	Message string
}

func (e *EmptyResponseBodyError) Error() string {
	if e == nil || e.Message == "" {
		return "Empty response body"
	}
	return e.Message
}

// IsEmptyResponseBodyError reports whether err is (or wraps) an
// EmptyResponseBodyError.
func IsEmptyResponseBodyError(err error) bool {
	var target *EmptyResponseBodyError
	return errors.As(err, &target)
}

// isFinalResponseError reports whether inner is a ProviderError a
// successful-response handler built (TS APICallError "Invalid JSON response"
// or "Failed to process successful response"): provider error handlers
// return it as is instead of wrapping it again.
func isFinalResponseError(inner *ProviderError) bool {
	return inner.Message == InvalidJSONResponseMessage ||
		(inner.Message == FailedToProcessSuccessfulResponseMessage && IsEmptyResponseBodyError(inner.Cause))
}
