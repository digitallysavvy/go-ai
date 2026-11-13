package typesafeai

import (
	"net/http"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// typesafeResponseNode is typesafeDecisionResponseSchema
// (typesafe-ai-decision-api.ts).
var typesafeResponseNode = func() zodlite.Node {
	nullishNum := zodlite.Nullish(zodlite.Number)
	probabilities := zodlite.RecordOf(zodlite.Number)
	return zodlite.Object(
		zodlite.F("model", zodlite.Nullish(zodlite.String)),
		zodlite.F("answers", zodlite.RecordOf(zodlite.Disc(map[string]zodlite.Node{
			"choice": zodlite.Typed("choice", zodlite.F("choice", zodlite.String), zodlite.F("probabilities", probabilities), zodlite.F("confidence", nullishNum)),
			"score": zodlite.Typed("score", zodlite.F("score", zodlite.Number), zodlite.F("probabilities", probabilities), zodlite.F("confidence", nullishNum),
				zodlite.F("legend", zodlite.Nullish(zodlite.RecordOf(zodlite.JSON)))),
			"box": zodlite.Typed("box",
				zodlite.F("box", zodlite.Object(
					zodlite.F("top", zodlite.Number), zodlite.F("bottom", zodlite.Number),
					zodlite.F("left", zodlite.Number), zodlite.F("right", zodlite.Number))),
				zodlite.F("confidence", nullishNum)),
			"noul": zodlite.Typed("noul", zodlite.F("noul", zodlite.Number)),
		}))),
		zodlite.F("usage", zodlite.Nullish(zodlite.Object(
			zodlite.F("input_tokens", nullishNum), zodlite.F("output_tokens", nullishNum)))),
	)
}()

// validateDecisionResponse is createJsonResponseHandler(
// typesafeDecisionResponseSchema): a body that does not match is an
// APICallError "Invalid JSON response".
func validateDecisionResponse(statusCode int, headers http.Header, body []byte) error {
	var value interface{}
	err := jsonexact.Unmarshal(body, &value)
	if err == nil {
		if bad := zodlite.Validate(typesafeResponseNode, body, value); bad != nil {
			err = bad
		}
	}
	if err == nil {
		return nil
	}
	return providerutils.InvalidJSONResponseError("typesafe.decision", statusCode, headers, body, err)
}
