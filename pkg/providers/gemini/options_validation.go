package gemini

import (
	"fmt"
	"math"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

var (
	harmCategories = []string{
		"HARM_CATEGORY_UNSPECIFIED", "HARM_CATEGORY_HATE_SPEECH", "HARM_CATEGORY_DANGEROUS_CONTENT",
		"HARM_CATEGORY_HARASSMENT", "HARM_CATEGORY_SEXUALLY_EXPLICIT", "HARM_CATEGORY_CIVIC_INTEGRITY",
	}
	harmThresholds = []string{
		"HARM_BLOCK_THRESHOLD_UNSPECIFIED", "BLOCK_LOW_AND_ABOVE", "BLOCK_MEDIUM_AND_ABOVE",
		"BLOCK_ONLY_HIGH", "BLOCK_NONE", "OFF",
	}
	mediaResolutions = []string{
		"MEDIA_RESOLUTION_UNSPECIFIED", "MEDIA_RESOLUTION_LOW", "MEDIA_RESOLUTION_MEDIUM", "MEDIA_RESOLUTION_HIGH",
	}
	imageAspectRatios = []string{"1:1", "2:3", "3:2", "3:4", "4:3", "4:5", "5:4", "9:16", "16:9", "21:9", "1:8", "8:1", "1:4", "4:1"}
)

// validateLanguageModelOptions mirrors the zod validation TS runs on
// providerOptions.<name> with googleLanguageModelOptions: a value of the wrong
// type or outside an enum fails the call with an InvalidArgumentError (unknown
// keys are ignored, like z.object).
func validateLanguageModelOptions(name string, opts map[string]interface{}) error {
	if err := checkLanguageModelOptions(opts); err != nil {
		return &providererrors.InvalidArgumentError{
			Field:   "providerOptions",
			Message: fmt.Sprintf("invalid %s provider options", name),
			Cause:   err,
		}
	}
	return nil
}

func checkLanguageModelOptions(opts map[string]interface{}) error {
	if v, ok := opts["responseModalities"]; ok && v != nil {
		list, isList := toInterfaceSlice(v)
		if !isList {
			return fmt.Errorf("responseModalities: expected array, received %T", v)
		}
		for i, e := range list {
			if err := enumValue(fmt.Sprintf("responseModalities[%d]", i), e, "TEXT", "IMAGE"); err != nil {
				return err
			}
		}
	}
	if v, ok := opts["thinkingConfig"]; ok && v != nil {
		m, isMap := v.(map[string]interface{})
		if !isMap {
			return fmt.Errorf("thinkingConfig: expected object, received %T", v)
		}
		if err := numberField("thinkingConfig.thinkingBudget", m["thinkingBudget"]); err != nil {
			return err
		}
		if err := boolField("thinkingConfig.includeThoughts", m["includeThoughts"]); err != nil {
			return err
		}
		if err := enumValue("thinkingConfig.thinkingLevel", m["thinkingLevel"], "minimal", "low", "medium", "high"); err != nil {
			return err
		}
	}
	if err := stringField("cachedContent", opts["cachedContent"]); err != nil {
		return err
	}
	if err := boolField("structuredOutputs", opts["structuredOutputs"]); err != nil {
		return err
	}
	if v, ok := opts["safetySettings"]; ok && v != nil {
		list, isList := toInterfaceSlice(v)
		if !isList {
			return fmt.Errorf("safetySettings: expected array, received %T", v)
		}
		for i, e := range list {
			m, isMap := e.(map[string]interface{})
			if !isMap {
				return fmt.Errorf("safetySettings[%d]: expected object, received %T", i, e)
			}
			if m["category"] == nil {
				return fmt.Errorf("safetySettings[%d].category: required", i)
			}
			if m["threshold"] == nil {
				return fmt.Errorf("safetySettings[%d].threshold: required", i)
			}
			if err := enumValue(fmt.Sprintf("safetySettings[%d].category", i), m["category"], harmCategories...); err != nil {
				return err
			}
			if err := enumValue(fmt.Sprintf("safetySettings[%d].threshold", i), m["threshold"], harmThresholds...); err != nil {
				return err
			}
		}
	}
	if err := enumValue("threshold", opts["threshold"], harmThresholds...); err != nil {
		return err
	}
	if err := boolField("audioTimestamp", opts["audioTimestamp"]); err != nil {
		return err
	}
	if v, ok := opts["labels"]; ok && v != nil {
		m, isMap := v.(map[string]interface{})
		if !isMap {
			if sm, isStrMap := v.(map[string]string); isStrMap {
				_ = sm
				m = nil
			} else {
				return fmt.Errorf("labels: expected object, received %T", v)
			}
		}
		for k, val := range m {
			if _, isStr := val.(string); !isStr {
				return fmt.Errorf("labels.%s: expected string, received %T", k, val)
			}
		}
	}
	if err := enumValue("mediaResolution", opts["mediaResolution"], mediaResolutions...); err != nil {
		return err
	}
	if v, ok := opts["imageConfig"]; ok && v != nil {
		m, isMap := v.(map[string]interface{})
		if !isMap {
			return fmt.Errorf("imageConfig: expected object, received %T", v)
		}
		if err := enumValue("imageConfig.aspectRatio", m["aspectRatio"], imageAspectRatios...); err != nil {
			return err
		}
		if err := enumValue("imageConfig.imageSize", m["imageSize"], "1K", "2K", "4K", "512"); err != nil {
			return err
		}
		if err := enumValue("imageConfig.personGeneration", m["personGeneration"],
			"PERSON_GENERATION_UNSPECIFIED", "ALLOW_ALL", "ALLOW_ADULT", "ALLOW_NONE"); err != nil {
			return err
		}
		if err := enumValue("imageConfig.prominentPeople", m["prominentPeople"],
			"PROMINENT_PEOPLE_UNSPECIFIED", "ALLOW_PROMINENT_PEOPLE", "BLOCK_PROMINENT_PEOPLE"); err != nil {
			return err
		}
		if o, ok := m["imageOutputOptions"]; ok && o != nil {
			om, isMap := o.(map[string]interface{})
			if !isMap {
				return fmt.Errorf("imageConfig.imageOutputOptions: expected object, received %T", o)
			}
			if err := enumValue("imageConfig.imageOutputOptions.mimeType", om["mimeType"], "image/jpeg", "image/png"); err != nil {
				return err
			}
			if err := numberField("imageConfig.imageOutputOptions.compressionQuality", om["compressionQuality"]); err != nil {
				return err
			}
		}
	}
	if v, ok := opts["retrievalConfig"]; ok && v != nil {
		m, isMap := v.(map[string]interface{})
		if !isMap {
			return fmt.Errorf("retrievalConfig: expected object, received %T", v)
		}
		if ll, ok := m["latLng"]; ok && ll != nil {
			lm, isMap := ll.(map[string]interface{})
			if !isMap {
				return fmt.Errorf("retrievalConfig.latLng: expected object, received %T", ll)
			}
			for _, k := range []string{"latitude", "longitude"} {
				if lm[k] == nil {
					return fmt.Errorf("retrievalConfig.latLng.%s: required", k)
				}
				if err := numberField("retrievalConfig.latLng."+k, lm[k]); err != nil {
					return err
				}
			}
		}
	}
	if err := boolField("streamFunctionCallArguments", opts["streamFunctionCallArguments"]); err != nil {
		return err
	}
	if err := enumValue("serviceTier", opts["serviceTier"], "standard", "flex", "priority"); err != nil {
		return err
	}
	if err := stringField("continuationToken", opts["continuationToken"]); err != nil {
		return err
	}
	if err := enumValue("sharedRequestType", opts["sharedRequestType"], "priority", "flex", "standard"); err != nil {
		return err
	}
	return enumValue("requestType", opts["requestType"], "shared")
}

func toInterfaceSlice(v interface{}) ([]interface{}, bool) {
	switch x := v.(type) {
	case []interface{}:
		return x, true
	case []string:
		out := make([]interface{}, len(x))
		for i, s := range x {
			out[i] = s
		}
		return out, true
	}
	return nil, false
}

func enumValue(field string, v interface{}, allowed ...string) error {
	if v == nil {
		return nil
	}
	if s, ok := v.(string); ok {
		for _, a := range allowed {
			if s == a {
				return nil
			}
		}
	}
	return fmt.Errorf("%s: invalid option %v, expected one of %v", field, v, allowed)
}

func boolField(field string, v interface{}) error {
	if v == nil {
		return nil
	}
	if _, ok := v.(bool); !ok {
		return fmt.Errorf("%s: expected boolean, received %T", field, v)
	}
	return nil
}

func stringField(field string, v interface{}) error {
	if v == nil {
		return nil
	}
	if _, ok := v.(string); !ok {
		return fmt.Errorf("%s: expected string, received %T", field, v)
	}
	return nil
}

func numberField(field string, v interface{}) error {
	if v == nil {
		return nil
	}
	switch x := v.(type) {
	case int, int32, int64, uint, uint32, uint64, float32:
		return nil
	case float64:
		if !math.IsNaN(x) {
			return nil
		}
	}
	return fmt.Errorf("%s: expected number, received %T", field, v)
}
