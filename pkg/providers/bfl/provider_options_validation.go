package bfl

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"reflect"

	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// This file ports the zod provider-option schemas of the TS Black Forest Labs
// package (blackForestLabsImageModelOptionsSchema,
// blackForestLabsFlux3ImageModelOptionsSchema and
// blackForestLabsVideoModelOptionsSchema): TS parseProviderOptions throws an
// InvalidArgumentError "invalid blackForestLabs provider options" for a
// mistyped or out-of-range option instead of ignoring it.

func bflImageOptionsSchema(maxSafetyTolerance int) providerutils.OptionSchema {
	zero, min256, max1920 := 0, 256, 1920
	schema := providerutils.OptionSchema{
		"resolution":          providerutils.OptEnum("768sq", "1k", "1.5k", "2k", "4k"),
		"grounding":           providerutils.OptBool,
		"version":             providerutils.OptLiteral("latest"),
		"imagePrompt":         providerutils.OptString,
		"imagePromptStrength": providerutils.OptRange(0, 1),
		"steps":               providerutils.OptPositive(true),
		"guidance":            providerutils.OptRange(0, math.MaxFloat64),
		"width":               providerutils.OptInt(&min256, &max1920),
		"height":              providerutils.OptInt(&min256, &max1920),
		"outputFormat":        providerutils.OptEnum("jpeg", "png"),
		"promptUpsampling":    providerutils.OptBool,
		"raw":                 providerutils.OptBool,
		"safetyTolerance":     providerutils.OptInt(&zero, &maxSafetyTolerance),
		"webhookSecret":       providerutils.OptString,
		"webhookUrl":          providerutils.OptURL(false),
		"pollIntervalMillis":  providerutils.OptPositive(true),
		"pollTimeoutMillis":   providerutils.OptPositive(true),
	}
	schema["inputImage"] = providerutils.OptString
	for i := 2; i <= 10; i++ {
		schema[fmt.Sprintf("inputImage%d", i)] = providerutils.OptString
	}
	return schema
}

// validateBFLImageOptions is parseProviderOptions for the image model: the
// FLUX 3 schema caps safetyTolerance at 4, the others at 6.
func validateBFLImageOptions(modelID string, providerOptions map[string]interface{}) error {
	maxSafety := 6
	if modelID == flux3ImageModelID {
		maxSafety = 4
	}
	_, err := providerutils.ParseProviderOptions("blackForestLabs", providerOptions, bflImageOptionsSchema(maxSafety))
	return err
}

// optKeyframes is blackForestLabsVideoKeyframesSchema: 1 to 10 plain
// strings, or 1 to 10 [seconds, string] pairs with strictly increasing
// seconds in 0..20.
func optKeyframes(path string, v interface{}) error {
	list, ok := v.([]interface{})
	if !ok {
		if strs, isStrs := v.([]string); isStrs {
			list = make([]interface{}, len(strs))
			for i, s := range strs {
				list[i] = s
			}
		} else if pairs, isPairs := v.([][]interface{}); isPairs {
			list = make([]interface{}, len(pairs))
			for i, p := range pairs {
				list[i] = p
			}
		} else {
			return fmt.Errorf("invalid input: expected array of keyframes (at %s)", path)
		}
	}
	if len(list) < 1 || len(list) > 10 {
		return fmt.Errorf("invalid input: expected between 1 and 10 keyframes (at %s)", path)
	}
	allStrings, allPairs := true, true
	for _, item := range list {
		if _, isStr := item.(string); !isStr {
			allStrings = false
		}
		if _, isPair := bflTimedKeyframe(item); !isPair {
			allPairs = false
		}
	}
	switch {
	case allStrings:
		return nil
	case allPairs:
		prev := math.Inf(-1)
		for _, item := range list {
			pair, _ := bflTimedKeyframe(item)
			if pair <= prev {
				return fmt.Errorf("timed keyframes must be in chronological order (at %s)", path)
			}
			prev = pair
		}
		return nil
	}
	return fmt.Errorf("invalid input: expected an array of strings or of [seconds, string] pairs (at %s)", path)
}

// bflTimedKeyframe reports the seconds of a [number 0..20, string] pair.
func bflTimedKeyframe(item interface{}) (float64, bool) {
	var seconds, image interface{}
	switch pair := item.(type) {
	case []interface{}:
		if len(pair) != 2 {
			return 0, false
		}
		seconds, image = pair[0], pair[1]
	default:
		return 0, false
	}
	if _, isStr := image.(string); !isStr {
		return 0, false
	}
	if providerutils.OptNumber("", seconds) != nil {
		return 0, false
	}
	n := reflect.ValueOf(seconds)
	var f float64
	switch n.Kind() {
	case reflect.Float32, reflect.Float64:
		f = n.Float()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		f = float64(n.Int())
	default:
		f = float64(n.Uint())
	}
	if f < 0 || f > 20 {
		return 0, false
	}
	return f, true
}

var bflVideoOptionsSchema = providerutils.OptionSchema{
	"resolution":      providerutils.OptEnum("hd", "fhd"),
	"aspectRatio":     providerutils.OptEnum("21:9", "2:1", "16:9", "4:3", "1:1", "3:4", "9:16", "auto"),
	"keyframes":       optKeyframes,
	"safetyTolerance": providerutils.OptInt(intPtr(0), intPtr(4)),
	"draft":           providerutils.OptBool,
	"draftCache":      providerutils.OptString,
	"version":         providerutils.OptLiteral("latest"),
}

func intPtr(i int) *int { return &i }

func validateBFLVideoOptions(providerOptions map[string]interface{}) error {
	_, err := providerutils.ParseProviderOptions("blackForestLabs", providerOptions, bflVideoOptionsSchema)
	return err
}

// bflURLNode is z.url().
func bflURLNode(v interface{}, present bool, path []interface{}) *zodlite.Issue {
	if bad := zodlite.String(v, present, path); bad != nil {
		return bad
	}
	if u, err := url.Parse(v.(string)); err != nil || u.Scheme == "" || (u.Host == "" && u.Opaque == "") {
		return &zodlite.Issue{Path: path, Code: "invalid_format", Message: "Invalid URL"}
	}
	return nil
}

// bflSubmitNode is bflSubmitSchema / bflVideoSubmitSchema.
var bflSubmitNode = zodlite.Object(
	zodlite.F("id", zodlite.String),
	zodlite.F("polling_url", bflURLNode),
	zodlite.F("cost", zodlite.Nullish(zodlite.Number)),
	zodlite.F("input_mp", zodlite.Nullish(zodlite.Number)),
	zodlite.F("output_mp", zodlite.Nullish(zodlite.Number)),
)

// validateBFLSubmitBody checks a submit response like bflSubmitSchema: nil
// when it matches, a *providerutils.ChunkSchemaError for a schema mismatch, or
// the JSON syntax error.
func validateBFLSubmitBody(body []byte) error {
	var value interface{}
	if err := json.Unmarshal(body, &value); err != nil {
		return err
	}
	if bad := bflSubmitNode(value, true, nil); bad != nil {
		return bad
	}
	return nil
}

// bflPollNode is bflPollSchema (black-forest-labs-image-model.ts): status or
// state must be one of the known task states, and a result needs a URL sample.
var bflPollNode = func() zodlite.Node {
	statusNode := zodlite.Optional(zodlite.Enum("Content Moderated", "Generating", "Pending", "Reasoning", "Ready", "Error", "Failed", "Request Moderated", "Task not found"))
	optNum := zodlite.Optional(zodlite.Number)
	sampleURL := bflURLNode
	return zodlite.Object(
		zodlite.F("status", statusNode),
		zodlite.F("state", statusNode),
		zodlite.F("details", zodlite.Unknown),
		zodlite.F("result", zodlite.Nullish(zodlite.Object(
			zodlite.F("sample", sampleURL),
			zodlite.F("seed", optNum), zodlite.F("start_time", optNum),
			zodlite.F("end_time", optNum), zodlite.F("duration", optNum),
		))),
	)
}()

// validateBFLPollBody checks a poll response like bflPollSchema: nil when it
// matches, a *providerutils.ChunkSchemaError for a schema mismatch, or the
// JSON syntax error.
func validateBFLPollBody(body []byte) error {
	var value interface{}
	if err := json.Unmarshal(body, &value); err != nil {
		return err
	}
	if bad := bflPollNode(value, true, nil); bad != nil {
		return bad
	}
	obj, _ := value.(map[string]interface{})
	if obj["status"] == nil && obj["state"] == nil {
		return &providerutils.ChunkSchemaError{Code: "custom", Message: "Missing status in Black Forest Labs poll response"}
	}
	return nil
}
