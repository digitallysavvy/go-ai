package quiverai

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// quiverReferenceImage is z.union([z.object({url: z.string().min(1)}).strict(),
// z.object({base64: z.string().min(1).max(16_777_216)}).strict()]).
func quiverReferenceImage(path string, v interface{}) error {
	obj, ok := v.(map[string]interface{})
	if !ok {
		return providerutils.OptObject(nil)(path, v)
	}
	byURL := func() error {
		if len(obj) != 1 {
			return fmt.Errorf("unrecognized key (at %s)", path)
		}
		return providerutils.OptStringLen(1, -1)(path+".url", obj["url"])
	}
	byBase64 := func() error {
		if len(obj) != 1 {
			return fmt.Errorf("unrecognized key (at %s)", path)
		}
		return providerutils.OptStringLen(1, 16_777_216)(path+".base64", obj["base64"])
	}
	_, hasURL := obj["url"]
	_, hasBase64 := obj["base64"]
	switch {
	case hasURL && byURL() == nil:
		return nil
	case hasBase64 && byBase64() == nil:
		return nil
	}
	return fmt.Errorf("invalid input: expected {url} or {base64} (at %s)", path)
}

// quiveraiImageOptionsSchema is quiveraiImageModelOptionsSchema
// (quiverai-image-model-options.ts): every member is .optional() (null
// fails), except presencePenalty which is also nullable.
var quiveraiImageOptionsSchema = func() providerutils.OptionSchema {
	zero, five, one, maxOut, maxOrch, lo, hi := 0, 5, 1, 131072, 65536, 128, 4096
	return providerutils.OptionSchema{
		"operation":       providerutils.OptEnum("generate", "vectorize", "animate", "edit"),
		"instructions":    providerutils.OptStringLen(1, -1),
		"reasoningEffort": providerutils.OptEnum("low", "medium", "high", "xhigh"),
		"referenceImages": providerutils.OptArray(quiverReferenceImage, 0, 4),
		"maxReviewSteps":  providerutils.OptInt(&zero, &five),
		"attributes": providerutils.OptObject(providerutils.OptionSchema{
			"viewBox": providerutils.OptObjectWith(providerutils.OptionSchema{
				"minX": providerutils.OptNumber, "minY": providerutils.OptNumber,
				"width": providerutils.OptPositive(false), "height": providerutils.OptPositive(false),
			}, "minX", "minY", "width", "height"),
		}),
		"temperature":                 providerutils.OptRange(0, 2),
		"topP":                        providerutils.OptRange(0, 1),
		"presencePenalty":             providerutils.OptNullable(providerutils.OptRange(-2, 2)),
		"maxOutputTokens":             providerutils.OptInt(&one, &maxOut),
		"orchestratorMaxOutputTokens": providerutils.OptInt(&one, &maxOrch),
		"shallowMaxOutputTokens":      providerutils.OptInt(&one, &maxOrch),
		"autoCrop":                    providerutils.OptBool,
		"targetSize":                  providerutils.OptInt(&lo, &hi),
	}
}()

func validateImageOptions(providerOptions map[string]interface{}) error {
	return providerutils.ValidateMapProviderOptions("quiverai", providerOptions, quiveraiImageOptionsSchema)
}
