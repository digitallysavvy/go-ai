package elevenlabs

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func TestSpeechProviderOptionsValidatedLikeTS(t *testing.T) {
	for name, o := range map[string]map[string]interface{}{
		"stability":   {"voiceSettings": map[string]interface{}{"stability": 1.5}},
		"seed":        {"seed": -1.0},
		"normalize":   {"applyTextNormalization": "maybe"},
		"request ids": {"previousRequestIds": []interface{}{"a", "b", "c", "d"}},
		"logging":     {"enableLogging": "yes"},
	} {
		err := validateSpeechProviderOptions(map[string]interface{}{"elevenlabs": o})
		var ia *providererrors.InvalidArgumentError
		if !errors.As(err, &ia) || ia.Message != "invalid elevenlabs provider options" {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if err := validateSpeechProviderOptions(map[string]interface{}{"elevenlabs": map[string]interface{}{
		"voiceSettings": map[string]interface{}{"stability": 0.5}, "seed": 12.0, "applyTextNormalization": "on",
	}}); err != nil {
		t.Fatal(err)
	}
}

func TestTranscriptionOptionsValidatedLikeTS(t *testing.T) {
	for name, o := range map[string]map[string]interface{}{
		"numSpeakers":    {"numSpeakers": 40.0},
		"granularity":    {"timestampsGranularity": "sentence"},
		"fileFormat":     {"fileFormat": "wav"},
		"vadThreshold":   {"streaming": map[string]interface{}{"vadThreshold": 0.95}},
		"keyterm length": {"streaming": map[string]interface{}{"keyterms": []interface{}{"this keyterm is far too long"}}},
	} {
		err := validateTranscriptionOptions(map[string]interface{}{"elevenlabs": o})
		var ia *providererrors.InvalidArgumentError
		if !errors.As(err, &ia) || ia.Message != "invalid elevenlabs provider options" {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if err := validateTranscriptionOptions(map[string]interface{}{"elevenlabs": map[string]interface{}{
		"numSpeakers": 2.0, "streaming": map[string]interface{}{"commitStrategy": "vad", "vadThreshold": 0.5},
	}}); err != nil {
		t.Fatal(err)
	}
}
