package gateway

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS gateway-decision-model.ts parseProviderOptions (parse-provider-options.ts):
// InvalidArgumentError{argument: providerOptions, message: "invalid gateway
// provider options", cause: the schema failure}.
func TestR89provgGatewayDecisionOptionErrorText(t *testing.T) {
	err := validateGatewayDecisionProviderOptions(map[string]interface{}{"models": "not-a-list"})
	var ae *providererrors.InvalidArgumentError
	if !errors.As(err, &ae) || ae.Field != "providerOptions" || ae.Message != "invalid gateway provider options" || ae.Cause == nil {
		t.Fatalf("error = %#v", err)
	}
}
