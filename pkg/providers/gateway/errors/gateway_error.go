package errors

import "errors"

// GatewayError matches the common public shape of TS gateway errors.
type GatewayError interface {
	error
	GatewayErrorMarker()
	GetStatusCode() int
	GetType() string
	GetGenerationID() string
	IsRetryable() bool
}

type baseGatewayError struct {
	message      string
	statusCode   int
	errorType    string
	cause        error
	generationID string
}

func (e *baseGatewayError) Error() string {
	if e.generationID != "" {
		return e.message + " [" + e.generationID + "]"
	}
	return e.message
}

func (e *baseGatewayError) Unwrap() error { return e.cause }

func (e *baseGatewayError) GatewayErrorMarker() {}

func (e *baseGatewayError) GetStatusCode() int { return e.statusCode }

func (e *baseGatewayError) GetType() string { return e.errorType }

func (e *baseGatewayError) GetGenerationID() string { return e.generationID }

func (e *baseGatewayError) IsRetryable() bool {
	return e.statusCode == 408 || e.statusCode == 409 || e.statusCode == 429 || e.statusCode >= 500
}

// IsGatewayError reports whether err is a typed gateway error.
func IsGatewayError(err error) bool {
	var gatewayErr GatewayError
	return errors.As(err, &gatewayErr)
}
