package xai

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func TestImageProviderOptionsViolationsAreInvalidArgumentErrors(t *testing.T) {
	for name, o := range map[string]map[string]interface{}{
		"resolution": {"resolution": "4k"},
		"quality":    {"quality": "ultra"},
		"sync_mode":  {"sync_mode": "yes"},
	} {
		_, err := extractImageProviderOptions(map[string]interface{}{"xai": o})
		var ia *providererrors.InvalidArgumentError
		if !errors.As(err, &ia) || ia.Field != "providerOptions" || ia.Message != "invalid xai provider options" {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestVideoProviderOptionsEnumsValidatedAsInvalidArgument(t *testing.T) {
	for name, o := range map[string]map[string]interface{}{
		"mode":       {"mode": "remix"},
		"resolution": {"resolution": "4k"},
		"type":       {"pollIntervalMs": "fast"},
	} {
		_, _, err := extractVideoProviderOptions(map[string]interface{}{"xai": o})
		var ia *providererrors.InvalidArgumentError
		if !errors.As(err, &ia) || ia.Field != "providerOptions" {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}
