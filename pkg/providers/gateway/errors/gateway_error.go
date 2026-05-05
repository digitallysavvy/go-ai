package errors

import "errors"

// GatewayError matches the common public shape of TS gateway errors.
type GatewayError interface {
	error
	GatewayErrorMarker()
	GetStatusCode() int
	GetType() string
	GetGenerationID() string
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

// IsGatewayError reports whether err is a typed gateway error.
func IsGatewayError(err error) bool {
	var gatewayErr GatewayError
	return errors.As(err, &gatewayErr)
}
