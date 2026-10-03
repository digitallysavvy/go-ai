// Package errors defines the error types that the Vercel AI Gateway provider
// returns: authentication, rate limit, invalid request, model not found,
// timeout and internal server errors, among others. Every type implements
// GatewayError.
//
// Check for a gateway error with IsGatewayError, or use the standard library:
//
//	result, err := ai.GenerateText(ctx, opts)
//	if err != nil {
//		var authErr *gatewayerrors.GatewayAuthenticationError
//		if stderrors.As(err, &authErr) {
//			log.Fatal("check AI_GATEWAY_API_KEY")
//		}
//	}
//
// Guide: https://goaisdk.com/docs/providers/gateway.
package errors
