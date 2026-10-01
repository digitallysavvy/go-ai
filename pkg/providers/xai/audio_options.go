package xai

import (
	"fmt"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func invalidXAIProviderOptions(cause error) error {
	return &providererrors.InvalidArgumentError{
		Field:   "providerOptions",
		Message: "invalid xai provider options",
		Cause:   cause,
	}
}

func xaiIntIn(value int, allowed ...int) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func xaiStringIn(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func validateXAIKeyterm(raw interface{}) error {
	switch v := raw.(type) {
	case nil, string:
		return nil
	case []string:
		return nil
	case []interface{}:
		for _, item := range v {
			if _, ok := item.(string); !ok {
				return fmt.Errorf("keyterm entries must be strings")
			}
		}
		return nil
	default:
		return fmt.Errorf("keyterm must be a string or array of strings")
	}
}
