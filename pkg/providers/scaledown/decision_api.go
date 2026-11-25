package scaledown

import (
	"net/http"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// scaledownResponseNode is TS scaledownDecisionResponseSchema
// (scaledown-decision-api.ts; OpenAPI 1.0.0 plus the "classify" model alias).
var scaledownResponseNode = func() zodlite.Node {
	probabilities := zodlite.RecordOf(zodlite.Number)
	nullishNumber := zodlite.Nullish(zodlite.Number)
	return zodlite.Object(
		zodlite.F("model", zodlite.Nullish(zodlite.String)),
		zodlite.F("answers", zodlite.RecordOf(zodlite.Disc(map[string]zodlite.Node{
			"noul": zodlite.Typed("noul", zodlite.F("noul", zodlite.Number)),
			"choice": zodlite.Typed("choice",
				zodlite.F("choice", zodlite.String), zodlite.F("probabilities", probabilities), zodlite.F("confidence", nullishNumber)),
			"score": zodlite.Typed("score",
				zodlite.F("score", zodlite.Number), zodlite.F("probabilities", probabilities),
				zodlite.F("confidence", nullishNumber), zodlite.F("legend", zodlite.Nullish(zodlite.RecordOf(zodlite.String)))),
		}))),
		zodlite.F("usage", zodlite.Nullish(zodlite.Object(
			zodlite.F("input_tokens", nullishNumber),
			zodlite.F("output_tokens", nullishNumber),
			zodlite.F("cost", nullishNumber)))),
	)
}()

// validateScaleDownDecisionResponse is createJsonResponseHandler(
// scaledownDecisionResponseSchema): a body that does not match is an
// APICallError "Invalid JSON response".
func validateScaleDownDecisionResponse(statusCode int, headers http.Header, body []byte) error {
	var value interface{}
	err := jsonexact.Unmarshal(body, &value)
	if err == nil {
		if bad := zodlite.Validate(scaledownResponseNode, body, value); bad != nil {
			err = bad
		}
	}
	if err == nil {
		return nil
	}
	return providerutils.InvalidJSONResponseError(decisionProviderName, statusCode, headers, body, err)
}
