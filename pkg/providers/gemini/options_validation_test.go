package gemini

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS validates providerOptions.google / googleVertex / vertex with zod and
// throws InvalidArgumentError for a value of the wrong type or outside an enum.
func TestInvalidLanguageModelOptionsFailTheRequest(t *testing.T) {
	bad := map[string]map[string]interface{}{
		"serviceTier":           {"serviceTier": "SERVICE_TIER_FLEX"},
		"mediaResolution":       {"mediaResolution": "HIGH"},
		"thinkingLevel":         {"thinkingConfig": map[string]interface{}{"thinkingLevel": "extreme"}},
		"thinkingBudget":        {"thinkingConfig": map[string]interface{}{"thinkingBudget": "lots"}},
		"safety category":       {"safetySettings": []interface{}{map[string]interface{}{"category": "X", "threshold": "BLOCK_NONE"}}},
		"safety threshold":      {"safetySettings": []interface{}{map[string]interface{}{"category": "HARM_CATEGORY_HATE_SPEECH", "threshold": "Y"}}},
		"threshold":             {"threshold": "NOPE"},
		"responseModalities":    {"responseModalities": []interface{}{"VIDEO"}},
		"cachedContent":         {"cachedContent": 12},
		"labels":                {"labels": map[string]interface{}{"a": 1}},
		"aspectRatio":           {"imageConfig": map[string]interface{}{"aspectRatio": "7:5"}},
		"retrievalConfig":       {"retrievalConfig": map[string]interface{}{"latLng": map[string]interface{}{"latitude": 1}}},
		"requestType":           {"requestType": "dedicated"},
		"streamFunctionCallArg": {"streamFunctionCallArguments": "yes"},
	}
	m := makeTestModel("gemini-2.5-flash")
	for name, g := range bad {
		t.Run(name, func(t *testing.T) {
			_, _, _, err := m.buildRequest(context.Background(), &provider.GenerateOptions{
				ProviderOptions: map[string]interface{}{"google": g},
			}, false)
			var ia *providererrors.InvalidArgumentError
			if !errors.As(err, &ia) || ia.Field != "providerOptions" {
				t.Fatalf("err = %v, want InvalidArgumentError for providerOptions", err)
			}
		})
	}
}

func TestValidLanguageModelOptionsPass(t *testing.T) {
	err := checkLanguageModelOptions(map[string]interface{}{
		"responseModalities": []string{"TEXT", "IMAGE", "AUDIO"},
		"thinkingConfig":     map[string]interface{}{"thinkingLevel": "low", "thinkingBudget": 10, "includeThoughts": true},
		"serviceTier":        "flex",
		"labels":             map[string]string{"a": "b"},
		"imageConfig":        map[string]interface{}{"aspectRatio": "16:9", "imageSize": "2K"},
		"unknownKey":         "ignored like z.object",
	})
	if err != nil {
		t.Fatal(err)
	}
}
