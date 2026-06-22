//go:build ignore

package main

import (
	"errors"
	"fmt"

	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
)

func main() {
	err := gatewayerrors.CreateGatewayErrorFromResponse(
		[]byte(`{"error":{"message":"upstream timed out","type":"upstream_timeout","code":"timeout"},"generationId":"gen_123"}`),
		504,
		"Gateway request failed",
		nil,
		"api-key",
	)

	var gatewayErr gatewayerrors.GatewayError
	if errors.As(err, &gatewayErr) {
		fmt.Printf("type=%s status=%d retryable=%t generation=%s\n",
			gatewayErr.GetType(), gatewayErr.GetStatusCode(), gatewayErr.IsRetryable(), gatewayErr.GetGenerationID())
	}

	var details gatewayerrors.GatewayErrorDetails
	if errors.As(err, &details) {
		fmt.Printf("rawType=%s code=%v param=%v\n", details.GetRawType(), details.GetCode(), details.GetParam())
	}
}
