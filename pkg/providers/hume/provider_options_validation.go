package hume

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// humeVoiceProvider is z.enum(['HUME_AI', 'CUSTOM_VOICE']).optional().
var humeVoiceProvider = providerutils.OptEnum("HUME_AI", "CUSTOM_VOICE")

// optHumeVoice is the utterance voice union: {id, provider?} or {name, provider?}.
func optHumeVoice(path string, v interface{}) error {
	byID := providerutils.OptObject(providerutils.OptionSchema{"id": providerutils.OptString, "provider": humeVoiceProvider})
	byName := providerutils.OptObject(providerutils.OptionSchema{"name": providerutils.OptString, "provider": humeVoiceProvider})
	obj, _ := v.(map[string]interface{})
	_, hasID := obj["id"]
	_, hasName := obj["name"]
	switch {
	case hasID:
		if err := byID(path, v); err == nil {
			return nil
		}
	}
	if hasName {
		if err := byName(path, v); err == nil {
			return nil
		}
	}
	if obj != nil && !hasID && !hasName {
		return fmt.Errorf("invalid input: expected voice with id or name (at %s)", path)
	}
	// Report the member that matched best.
	if hasID {
		return byID(path, v)
	}
	if hasName {
		return byName(path, v)
	}
	return providerutils.OptObject(nil)(path, v)
}

// optHumeContext is context: {generationId} or {utterances: [...]}.
func optHumeContext(path string, v interface{}) error {
	obj, ok := v.(map[string]interface{})
	if !ok {
		return providerutils.OptObject(nil)(path, v)
	}
	utterance := providerutils.OptObject(providerutils.OptionSchema{
		"text":            providerutils.OptString,
		"description":     providerutils.OptString,
		"speed":           providerutils.OptNumber,
		"trailingSilence": providerutils.OptNumber,
		"voice":           optHumeVoice,
	})
	if g, has := obj["generationId"]; has {
		if err := providerutils.OptString(path+".generationId", g); err == nil {
			return nil
		}
	}
	if u, has := obj["utterances"]; has {
		// utterances items require text.
		for i, item := range asSlice(u) {
			m, _ := item.(map[string]interface{})
			if _, hasText := m["text"]; !hasText {
				return fmt.Errorf("invalid input: expected string, received undefined (at %s.utterances[%d].text)", path, i)
			}
		}
		return providerutils.OptArray(utterance, 0, -1)(path+".utterances", u)
	}
	return fmt.Errorf("invalid input: expected context with generationId or utterances (at %s)", path)
}

func asSlice(v interface{}) []interface{} {
	if s, ok := v.([]interface{}); ok {
		return s
	}
	return nil
}

// humeSpeechOptionsSchema is humeSpeechModelOptionsSchema.
var humeSpeechOptionsSchema = providerutils.OptionSchema{
	"context": providerutils.OptNullable(optHumeContext),
}

// validateSpeechOptions is parseProviderOptions with
// humeSpeechModelOptionsSchema: a mistyped option is an InvalidArgumentError.
func validateSpeechOptions(providerOptions map[string]interface{}) error {
	if raw, ok := providerOptions["hume"]; ok {
		switch raw.(type) {
		case SpeechModelOptions, *SpeechModelOptions:
			return nil // typed options are well-formed by construction
		}
	}
	_, err := providerutils.ParseProviderOptions("hume", providerOptions, humeSpeechOptionsSchema)
	return err
}
