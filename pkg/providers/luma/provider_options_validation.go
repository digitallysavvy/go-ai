package luma

import (
	"encoding/json"
	"math"
	"net/http"

	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// lumaImageOptionsSchema is lumaImageModelOptionsSchema (a z.looseObject, so
// unknown keys pass): every member is nullish.
var lumaImageOptionsSchema = providerutils.OptionSchema{
	"referenceType": providerutils.OptNullable(providerutils.OptEnum("image", "style", "character", "modify_image")),
	"images": providerutils.OptNullable(providerutils.OptArray(providerutils.OptObject(providerutils.OptionSchema{
		"weight": providerutils.OptNullable(providerutils.OptRange(0, 1)),
		"id":     providerutils.OptNullable(providerutils.OptString),
	}), 0, -1)),
	"pollIntervalMillis": providerutils.OptNullable(providerutils.OptRange(math.Inf(-1), math.Inf(1))),
	"maxPollAttempts":    providerutils.OptNullable(providerutils.OptRange(math.Inf(-1), math.Inf(1))),
}

// validateLumaImageOptions is parseProviderOptions with
// lumaImageModelOptionsSchema: a mistyped option is an InvalidArgumentError.
func validateLumaImageOptions(providerOptions map[string]interface{}) error {
	_, err := providerutils.ParseProviderOptions("luma", providerOptions, lumaImageOptionsSchema)
	return err
}

// lumaGenerationNode is lumaGenerationResponseSchema.
var lumaGenerationNode = zodlite.Object(
	zodlite.F("id", zodlite.String),
	zodlite.F("state", zodlite.Enum("queued", "dreaming", "completed", "failed")),
	zodlite.F("failure_reason", zodlite.Nullish(zodlite.String)),
	zodlite.F("assets", zodlite.Nullish(zodlite.Object(zodlite.F("image", zodlite.String)))),
)

// validateLumaGeneration is createJsonResponseHandler(
// lumaGenerationResponseSchema): a body that does not match is an
// APICallError "Invalid JSON response".
func validateLumaGeneration(statusCode int, headers http.Header, body []byte) error {
	var value interface{}
	err := json.Unmarshal(body, &value)
	if err == nil {
		if bad := lumaGenerationNode(value, true, nil); bad != nil {
			err = bad
		}
	}
	if err == nil {
		return nil
	}
	return providerutils.InvalidJSONResponseError("luma", statusCode, headers, body, err)
}
