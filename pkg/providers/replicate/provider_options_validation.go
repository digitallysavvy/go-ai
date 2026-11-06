package replicate

import (
	"encoding/json"
	"net/http"

	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

var (
	posNumber = providerutils.OptNullable(providerutils.OptAnd(providerutils.OptNumber, providerutils.OptPositive(false)))
	posInt    = providerutils.OptNullable(providerutils.OptPositive(true))
	nullNum   = providerutils.OptNullable(providerutils.OptNumber)
	nullStr   = providerutils.OptNullable(providerutils.OptString)
	nullBool  = providerutils.OptNullable(providerutils.OptBool)
)

// replicateImageOptionsSchema is replicateImageModelOptionsSchema (a
// z.looseObject: model-specific keys pass through).
var replicateImageOptionsSchema = providerutils.OptionSchema{
	"maxWaitTimeInSeconds": posNumber,
	"pollIntervalMillis":   posInt,
	"maxPollAttempts":      posInt,
	"guidance_scale":       nullNum,
	"num_inference_steps":  nullNum,
	"negative_prompt":      nullStr,
	"output_format":        providerutils.OptNullable(providerutils.OptEnum("png", "jpg", "webp")),
	"output_quality":       providerutils.OptNullable(providerutils.OptRange(1, 100)),
	"strength":             providerutils.OptNullable(providerutils.OptRange(0, 1)),
}

// replicateVideoOptionsSchema is replicateVideoModelOptionsSchema (loose).
var replicateVideoOptionsSchema = providerutils.OptionSchema{
	"pollIntervalMs":       posNumber,
	"pollTimeoutMs":        posNumber,
	"maxWaitTimeInSeconds": posNumber,
	"guidance_scale":       nullNum,
	"num_inference_steps":  nullNum,
	"motion_bucket_id":     nullNum,
	"cond_aug":             nullNum,
	"decoding_t":           nullNum,
	"video_length":         nullStr,
	"sizing_strategy":      nullStr,
	"frames_per_second":    nullNum,
	"prompt_optimizer":     nullBool,
}

// validateImageOptions / validateVideoOptions are parseProviderOptions with
// the TS option schemas: a mistyped option is an InvalidArgumentError.
func validateImageOptions(providerOptions map[string]interface{}) error {
	_, err := providerutils.ParseProviderOptions("replicate", providerOptions, replicateImageOptionsSchema)
	return err
}

func validateVideoOptions(providerOptions map[string]interface{}) error {
	_, err := providerutils.ParseProviderOptions("replicate", providerOptions, replicateVideoOptionsSchema)
	return err
}

var replicateStatus = zodlite.Enum("starting", "processing", "succeeded", "failed", "canceled")

var replicateURLs = zodlite.Object(zodlite.F("get", zodlite.String))

// replicateImagePredictionNode is replicateImageResponseSchema.
var replicateImagePredictionNode = zodlite.Object(
	zodlite.F("status", replicateStatus),
	zodlite.F("output", zodlite.Nullish(zodlite.Union(zodlite.Array(zodlite.String), zodlite.String))),
	zodlite.F("error", zodlite.Nullish(zodlite.String)),
	zodlite.F("urls", replicateURLs),
)

// replicatePredictionNode is replicatePredictionSchema (video).
var replicatePredictionNode = zodlite.Object(
	zodlite.F("id", zodlite.String),
	zodlite.F("status", replicateStatus),
	zodlite.F("output", zodlite.Nullish(zodlite.String)),
	zodlite.F("error", zodlite.Nullish(zodlite.String)),
	zodlite.F("urls", replicateURLs),
	zodlite.F("metrics", zodlite.Nullish(zodlite.Object(zodlite.F("predict_time", zodlite.Nullish(zodlite.Number))))),
)

// checkPrediction is createJsonResponseHandler(schema): a body that does not
// match is an APICallError "Invalid JSON response".
func checkPrediction(node zodlite.Node, statusCode int, headers http.Header, body []byte) error {
	var value interface{}
	err := json.Unmarshal(body, &value)
	if err == nil {
		if bad := node(value, true, nil); bad != nil {
			err = bad
		}
	}
	if err == nil {
		return nil
	}
	return providerutils.InvalidJSONResponseError("replicate", statusCode, headers, body, err)
}

func validateImagePrediction(statusCode int, headers http.Header, body []byte) error {
	return checkPrediction(replicateImagePredictionNode, statusCode, headers, body)
}

func validateVideoPrediction(statusCode int, headers http.Header, body []byte) error {
	return checkPrediction(replicatePredictionNode, statusCode, headers, body)
}
