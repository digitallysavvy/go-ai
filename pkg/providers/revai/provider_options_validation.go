package revai

import "github.com/digitallysavvy/go-ai/pkg/providerutils"

// revaiOptionsSchema is revaiTranscriptionModelOptionsSchema
// (revai-transcription-model-options.ts).
var revaiOptionsSchema = func() providerutils.OptionSchema {
	null := providerutils.OptNullable
	boolean := null(providerutils.OptBool)
	number := null(providerutils.OptNumber)
	str := null(providerutils.OptString)
	tier := null(providerutils.OptEnum("standard", "premium"))
	return providerutils.OptionSchema{
		"metadata": str,
		"notification_config": null(providerutils.OptObjectWith(providerutils.OptionSchema{
			"url": providerutils.OptString,
			"auth_headers": null(providerutils.OptObjectWith(providerutils.OptionSchema{
				"Authorization": providerutils.OptString,
			}, "Authorization")),
		}, "url")),
		"delete_after_seconds": number,
		"verbatim":             providerutils.OptBool,
		"rush":                 boolean,
		"test_mode":            boolean,
		"segments_to_transcribe": null(providerutils.OptArray(providerutils.OptObjectWith(providerutils.OptionSchema{
			"start": providerutils.OptNumber, "end": providerutils.OptNumber,
		}, "start", "end"), 0, -1)),
		"speaker_names": null(providerutils.OptArray(providerutils.OptObjectWith(providerutils.OptionSchema{
			"display_name": providerutils.OptString,
		}, "display_name"), 0, -1)),
		"skip_diarization":         boolean,
		"skip_postprocessing":      boolean,
		"skip_punctuation":         boolean,
		"remove_disfluencies":      boolean,
		"remove_atmospherics":      boolean,
		"filter_profanity":         boolean,
		"speaker_channels_count":   number,
		"speakers_count":           number,
		"diarization_type":         tier,
		"custom_vocabulary_id":     str,
		"custom_vocabularies":      providerutils.OptArray(providerutils.OptObject(providerutils.OptionSchema{}), 0, -1),
		"strict_custom_vocabulary": providerutils.OptBool,
		"summarization_config": null(providerutils.OptObject(providerutils.OptionSchema{
			"model":  tier,
			"type":   null(providerutils.OptEnum("paragraph", "bullets")),
			"prompt": str,
		})),
		"translation_config": null(providerutils.OptObjectWith(providerutils.OptionSchema{
			"target_languages": providerutils.OptArray(providerutils.OptObjectWith(providerutils.OptionSchema{
				"language": providerutils.OptEnum("en", "en-us", "en-gb", "ar", "pt", "pt-br", "pt-pt", "fr", "fr-ca",
					"es", "es-es", "es-la", "it", "ja", "ko", "de", "ru"),
			}, "language"), 0, -1),
			"model": tier,
		}, "target_languages")),
		"language":         str,
		"forced_alignment": boolean,
	}
}()

// validateOptions is TS parseProviderOptions with the revai schema; typed
// option structs are valid by construction.
func validateOptions(providerOptions map[string]interface{}) error {
	return providerutils.ValidateMapProviderOptions("revai", providerOptions, revaiOptionsSchema)
}
