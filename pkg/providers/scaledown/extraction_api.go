package scaledown

import (
	"net/http"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// nonEmptyArray is z.array(n).min(1).
func nonEmptyArray(n zodlite.Node) zodlite.Node {
	arr := zodlite.Array(n)
	return func(v interface{}, present bool, path []interface{}) *zodlite.Issue {
		if bad := arr(v, present, path); bad != nil {
			return bad
		}
		if a, ok := v.([]interface{}); ok && len(a) == 0 {
			return &zodlite.Issue{Path: path, Code: "too_small", Origin: "array", Minimum: 1, Continue: true, Message: "Too small: expected array to have >=1 items"}
		}
		return nil
	}
}

// scaledownExtractionResponseNode is TS scaledownExtractionResponseSchema
// (scaledown-extraction-api.ts). Extraction is not in ScaleDown's published
// OpenAPI specification.
var scaledownExtractionResponseNode = func() zodlite.Node {
	nullishNumber := zodlite.Nullish(zodlite.Number)
	return zodlite.Object(
		zodlite.F("id", zodlite.Nullish(zodlite.String)),
		zodlite.F("created", nullishNumber),
		zodlite.F("model", zodlite.Nullish(zodlite.String)),
		zodlite.F("choices", nonEmptyArray(zodlite.Object(
			zodlite.F("message", zodlite.Object(zodlite.F("content", zodlite.String)))))),
		zodlite.F("usage", zodlite.Nullish(zodlite.Object(
			zodlite.F("prompt_tokens", nullishNumber),
			zodlite.F("completion_tokens", nullishNumber)))),
	)
}()

// nonNegativeInt is z.number().int().nonnegative().
var nonNegativeInt = zodlite.Min(zodlite.Int, 0)

// scaledownExtractionContentNode is TS scaledownExtractionContentSchema, the
// schema of the JSON text in choices[0].message.content.
var scaledownExtractionContentNode = func() zodlite.Node {
	return zodlite.Object(
		zodlite.F("entities", zodlite.Array(zodlite.Object(
			zodlite.F("text", zodlite.String),
			zodlite.F("type", zodlite.String),
			zodlite.F("confidence", zodlite.Number),
			zodlite.F("start", zodlite.Nullish(nonNegativeInt)),
			zodlite.F("end", zodlite.Nullish(nonNegativeInt))))),
		zodlite.F("input_tokens", zodlite.Nullish(zodlite.Number)),
		zodlite.F("ocr_text", zodlite.Nullish(zodlite.String)),
	)
}()

// validateScaleDownExtractionResponse is createJsonResponseHandler(
// scaledownExtractionResponseSchema).
func validateScaleDownExtractionResponse(statusCode int, headers http.Header, body []byte) error {
	var value interface{}
	err := jsonexact.Unmarshal(body, &value)
	if err == nil {
		if bad := zodlite.Validate(scaledownExtractionResponseNode, body, value); bad != nil {
			err = bad
		}
	}
	if err == nil {
		return nil
	}
	return providerutils.InvalidJSONResponseError(extractionProviderName, statusCode, headers, body, err)
}

// scaledownExtractionResponse is the decoded successful response.
type scaledownExtractionResponse struct {
	ID      *string  `json:"id"`
	Created *float64 `json:"created"`
	Model   *string  `json:"model"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     *float64 `json:"prompt_tokens"`
		CompletionTokens *float64 `json:"completion_tokens"`
	} `json:"usage"`
}

type scaledownExtractionEntity struct {
	Text       string   `json:"text"`
	Type       string   `json:"type"`
	Confidence float64  `json:"confidence"`
	Start      *float64 `json:"start"`
	End        *float64 `json:"end"`
}

// scaledownExtractionContent is the decoded inner extraction content.
type scaledownExtractionContent struct {
	Entities    []scaledownExtractionEntity `json:"entities"`
	InputTokens *float64                    `json:"input_tokens"`
	OcrText     *string                     `json:"ocr_text"`
}

// parseExtractionContent is safeParseJSON({text, schema}): the text must be
// secure JSON that matches the content schema.
func parseExtractionContent(text string) (*scaledownExtractionContent, bool) {
	var value interface{}
	if err := jsonexact.Unmarshal([]byte(text), &value); err != nil {
		return nil, false
	}
	if bad := zodlite.Validate(scaledownExtractionContentNode, []byte(text), value); bad != nil {
		return nil, false
	}
	var content scaledownExtractionContent
	if err := jsonexact.Unmarshal([]byte(text), &content); err != nil {
		return nil, false
	}
	return &content, true
}
