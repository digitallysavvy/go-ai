package ai

import (
	"errors"
	"os"

	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
)

const unauthenticatedGatewayInfoURL = "https://ai-sdk.dev/unauthenticated-ai-gateway"

// wrappedGatewayError is the error TS wrapGatewayError returns in place of a
// GatewayAuthenticationError. The original stays reachable through Unwrap, so
// errors.As on the gateway error keeps working for Go callers.
type wrappedGatewayError struct {
	name    string
	message string
	cause   error
}

func (e *wrappedGatewayError) Error() string { return e.message }
func (e *wrappedGatewayError) Unwrap() error { return e.cause }

// wrapGatewayError mirrors TS prompt/wrap-gateway-error.ts, used by
// generateObject, streamObject and the batch cancel and list calls: an
// unauthenticated AI Gateway request becomes an error that tells the caller
// how to authenticate (a short one when NODE_ENV is production).
func wrapGatewayError(err error) error {
	if err == nil {
		return nil
	}
	var auth *gatewayerrors.GatewayAuthenticationError
	if !errors.As(err, &auth) {
		return err
	}
	if os.Getenv("NODE_ENV") == "production" {
		return &wrappedGatewayError{
			name:    "GatewayError",
			message: "Unauthenticated. Configure AI_GATEWAY_API_KEY or use a provider module. Learn more: " + unauthenticatedGatewayInfoURL,
			cause:   err,
		}
	}
	return &wrappedGatewayError{
		name: "GatewayAuthenticationError",
		message: "\u001b[1m\u001b[31mUnauthenticated request to AI Gateway.\u001b[0m\n\n" +
			"To authenticate, set the \u001b[33mAI_GATEWAY_API_KEY\u001b[0m environment variable with your API key.\n\n" +
			"Alternatively, you can use a provider module instead of the AI Gateway.\n\n" +
			"Learn more: \u001b[34m" + unauthenticatedGatewayInfoURL + "\u001b[0m\n\n",
		cause: err,
	}
}
