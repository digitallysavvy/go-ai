package deepgram

import (
	"fmt"
	"net/http"
	"reflect"

	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// optStringOrStringArray is z.union([z.string(), z.array(z.string())]).
func optStringOrStringArray(path string, v interface{}) error {
	if providerutils.OptString(path, v) == nil {
		return nil
	}
	return providerutils.OptArray(providerutils.OptString, 0, -1)(path, v)
}

// optSummarize is z.union([z.literal('v2'), z.literal(false)]).
func optSummarize(path string, v interface{}) error {
	if b, ok := v.(bool); ok && !b {
		return nil
	}
	if s, ok := v.(string); ok && s == "v2" {
		return nil
	}
	return fmt.Errorf("invalid input: expected \"v2\" or false (at %s)", path)
}

// optNumberOrString is z.union([z.number(), z.string()]).
func optNumberOrString(path string, v interface{}) error {
	if providerutils.OptNumber(path, v) == nil || (v != nil && reflect.ValueOf(v).Kind() == reflect.String) {
		return nil
	}
	return fmt.Errorf("invalid input: expected number or string (at %s)", path)
}

var (
	nullBool = providerutils.OptNullable(providerutils.OptBool)
	nullStr  = providerutils.OptNullable(providerutils.OptString)
	nullNum  = providerutils.OptNullable(providerutils.OptNumber)
)

// deepgramSpeechOptionsSchema is deepgramSpeechModelOptionsSchema.
var deepgramSpeechOptionsSchema = providerutils.OptionSchema{
	"bitRate":        providerutils.OptNullable(optNumberOrString),
	"container":      nullStr,
	"encoding":       nullStr,
	"sampleRate":     nullNum,
	"callback":       providerutils.OptNullable(providerutils.OptURL(false)),
	"callbackMethod": providerutils.OptNullable(providerutils.OptEnum("POST", "PUT")),
	"mipOptOut":      nullBool,
	"tag":            providerutils.OptNullable(optStringOrStringArray),
}

// deepgramTranscriptionOptionsSchema is deepgramTranscriptionModelOptionsSchema.
var deepgramTranscriptionOptionsSchema = providerutils.OptionSchema{
	"language":       nullStr,
	"detectLanguage": nullBool,
	"smartFormat":    nullBool,
	"punctuate":      nullBool,
	"paragraphs":     nullBool,
	"summarize":      providerutils.OptNullable(optSummarize),
	"topics":         nullBool,
	"intents":        nullBool,
	"sentiment":      nullBool,
	"detectEntities": nullBool,
	"redact":         providerutils.OptNullable(optStringOrStringArray),
	"replace":        nullStr,
	"search":         nullStr,
	"keyterm":        nullStr,
	"diarize":        nullBool,
	"utterances":     nullBool,
	"uttSplit":       nullNum,
	"fillerWords":    nullBool,
	// TS 5b89b2560c: streaming members are .optional() (null rejected).
	"streaming": providerutils.OptObject(providerutils.OptionSchema{
		"interimResults": providerutils.OptBool,
		"endpointing":    optEndpointing,
		"channels":       providerutils.OptPositive(true),
		"multichannel":   providerutils.OptBool,
		"vadEvents":      providerutils.OptBool,
		"utteranceEndMs": providerutils.OptInt(intPtr(1000), nil),
	}),
}

func intPtr(v int) *int { return &v }

// optEndpointing is z.union([z.number().int().nonnegative(), z.literal(false)]).
func optEndpointing(path string, v interface{}) error {
	if b, ok := v.(bool); ok && !b {
		return nil
	}
	return providerutils.OptInt(intPtr(0), nil)(path, v)
}

// validateSpeechOptions / validateTranscriptionOptions are parseProviderOptions
// with the TS option schemas: a mistyped option is an InvalidArgumentError.
func validateSpeechOptions(providerOptions map[string]interface{}) error {
	_, err := providerutils.ParseProviderOptions("deepgram", providerOptions, deepgramSpeechOptionsSchema)
	return err
}

func validateTranscriptionOptions(providerOptions map[string]interface{}) error {
	_, err := providerutils.ParseProviderOptions("deepgram", providerOptions, deepgramTranscriptionOptionsSchema)
	return err
}

// deepgramTranscriptionResponseNode is deepgramTranscriptionResponseSchema.
var deepgramTranscriptionResponseNode = zodlite.Object(
	zodlite.F("metadata", zodlite.Nullish(zodlite.Object(zodlite.F("duration", zodlite.Number)))),
	zodlite.F("results", zodlite.Nullish(zodlite.Object(
		zodlite.F("channels", zodlite.Array(zodlite.Object(
			zodlite.F("detected_language", zodlite.Nullish(zodlite.String)),
			zodlite.F("alternatives", zodlite.Array(zodlite.Object(
				zodlite.F("transcript", zodlite.String),
				zodlite.F("words", zodlite.Array(zodlite.Object(
					zodlite.F("word", zodlite.String), zodlite.F("start", zodlite.Number), zodlite.F("end", zodlite.Number),
				))),
			))),
		))),
	))),
)

// validateTranscriptionResponse is createJsonResponseHandler(
// deepgramTranscriptionResponseSchema): a body that does not match is an
// APICallError "Invalid JSON response". The body is parsed like JSON.parse (a
// number past the double range reads as Infinity, ruling 4) with the
// secureJsonParse prototype check.
func validateTranscriptionResponse(statusCode int, headers http.Header, body []byte) error {
	return providerutils.NodeResponseValidator("deepgram", deepgramTranscriptionResponseNode)(statusCode, headers, body)
}
