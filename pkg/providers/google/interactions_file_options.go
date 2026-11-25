package google

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// interactionsFileOptionsSchema mirrors TS googleInteractionsFileOptionsSchema
// (google-interactions-file-options.ts, afb4e69924): the per-file inspection
// options of an Interactions file part, read from providerOptions.google.
var interactionsFileOptionsSchema = func() zodlite.Node {
	positiveInt := zodlite.Min(zodlite.Int, 1)
	nonNegative := zodlite.Min(zodlite.Number, 0)
	fps := func(v interface{}, present bool, path []interface{}) *zodlite.Issue {
		if bad := zodlite.Max(zodlite.Number, 24)(v, present, path); bad != nil {
			return bad
		}
		if f, ok := v.(float64); ok && f <= 0 {
			return &zodlite.Issue{Path: path, Code: "too_small", Origin: "number", Minimum: 0, Exclusive: true, Continue: true, Message: "Too small: expected number to be >0"}
		}
		return nil
	}
	pageOptions := zodlite.Object(
		zodlite.F("startPage", zodlite.Optional(positiveInt)),
		zodlite.F("endPage", zodlite.Optional(positiveInt)),
		zodlite.F("contentLayers", zodlite.Optional(zodlite.Array(zodlite.Enum("page_image", "page_text")))),
	)
	spreadsheetOptions := zodlite.Object(
		zodlite.F("sheetIndex", zodlite.Optional(positiveInt)),
		zodlite.F("visibleCells", zodlite.Optional(zodlite.String)),
		zodlite.F("contentLayers", zodlite.Optional(zodlite.Array(zodlite.Enum("data", "formulas", "visual")))),
	)
	return zodlite.Object(
		zodlite.F("resolution", zodlite.Optional(zodlite.Enum("low", "medium", "high"))),
		zodlite.F("processing", zodlite.Optional(zodlite.Union(
			zodlite.Enum("agentic", "static"),
			zodlite.Object(
				zodlite.F("type", zodlite.Literal("static")),
				zodlite.F("pageOptions", zodlite.Optional(pageOptions)),
				zodlite.F("spreadsheetOptions", zodlite.Optional(spreadsheetOptions)),
				zodlite.F("startOffset", zodlite.Optional(nonNegative)),
				zodlite.F("endOffset", zodlite.Optional(nonNegative)),
				zodlite.F("fps", zodlite.Optional(fps)),
				zodlite.F("contentLayers", zodlite.Optional(zodlite.Array(zodlite.Enum("frames", "audio", "transcript")))),
			),
		))),
	)
}()

// interactionsFileOptions is the parsed per-file inspection options.
type interactionsFileOptions struct {
	// Resolution is "low", "medium" or "high" ("" when unset).
	Resolution string
	// Processing is nil, the string "agentic" / "static", or the static
	// configuration object.
	Processing interface{}
}

// parseInteractionsFileOptions is TS googleInteractionsFileOptionsSchema.parse
// of the part's providerOptions.google with its null values dropped. An
// invalid value is a validation error (TS throws the ZodError), so a
// malformed selection never silently sends the whole file.
func parseInteractionsFileOptions(providerOptions map[string]interface{}) (interactionsFileOptions, error) {
	// Typed-option ruling: part options are read in their JSON form.
	google, _ := providerutils.NormalizeOptionsJSON(providerOptions)["google"].(map[string]interface{})
	filtered := make(map[string]interface{}, len(google))
	for key, value := range google {
		if value != nil {
			filtered[key] = value
		}
	}
	data := jsonStringify(filtered)
	// TS schema.parse of the caller's object: no secureJsonParse.
	if message := zodlite.ValidationMessagePlain(interactionsFileOptionsSchema, data); message != "" {
		return interactionsFileOptions{}, providererrors.NewValidationErrorWithContext(zodlite.CompactJSON(data), zodlite.IssueText(message), nil, &providererrors.ValidationContext{})
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(data), &parsed); err != nil {
		return interactionsFileOptions{}, err
	}
	out := interactionsFileOptions{}
	out.Resolution, _ = parsed["resolution"].(string)
	out.Processing = interactionsFileProcessing(parsed["processing"])
	return out, nil
}

// interactionsFileProcessing is TS getFileProcessingField: "agentic" and
// "static" pass through; a static configuration becomes the wire object with
// offsets in seconds as duration strings. Keys follow the TS literal order.
func interactionsFileProcessing(processing interface{}) interface{} {
	switch p := processing.(type) {
	case string:
		return p
	case map[string]interface{}:
		out := providerutils.OrderedMap{Keys: []string{"type"}, Vals: map[string]interface{}{"type": "static"}}
		set := func(key string, value interface{}) {
			out.Keys = append(out.Keys, key)
			out.Vals[key] = value
		}
		if v := videoOffsetString(p["startOffset"]); v != nil {
			set("start_offset", v)
		}
		if v := videoOffsetString(p["endOffset"]); v != nil {
			set("end_offset", v)
		}
		if v, ok := p["fps"]; ok {
			set("fps", v)
		}
		if v, ok := p["contentLayers"]; ok {
			set("content_layers", v)
		}
		nested := func(key, wire string, fields [][2]string) {
			options, ok := p[key].(map[string]interface{})
			if !ok {
				return
			}
			object := providerutils.OrderedMap{Vals: map[string]interface{}{}}
			for _, field := range fields {
				if v, ok := options[field[0]]; ok {
					object.Keys = append(object.Keys, field[1])
					object.Vals[field[1]] = v
				}
			}
			set(wire, object)
		}
		nested("pageOptions", "page_options", [][2]string{{"startPage", "start_page"}, {"endPage", "end_page"}, {"contentLayers", "content_layers"}})
		nested("spreadsheetOptions", "spreadsheet_options", [][2]string{{"sheetIndex", "sheet_index"}, {"visibleCells", "visible_cells"}, {"contentLayers", "content_layers"}})
		return out
	}
	return nil
}
