package tsfixture

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// RawFieldOrder returns the key order of a recorded doExtract call's fields
// (a TS object keeps insertion order; the Go map does not).
func (c WireCall) RawFieldOrder() []string {
	if len(c.argsRaw) == 0 {
		return nil
	}
	tree, ok := jsonorder.Parse(c.argsRaw)
	if !ok {
		return nil
	}
	args, _ := tree.([]interface{})
	if len(args) == 0 {
		return nil
	}
	fs, ok := jsonorder.Get(args[0], "fields")
	if !ok {
		return nil
	}
	fo, ok := fs.(*jsonorder.Object)
	if !ok {
		return nil
	}
	return fo.Keys()
}

// wireExtract replays a recorded doExtract call (port wave 2 recordings)
// through the Go extraction model.
func wireExtract(ctx context.Context, m provider.ExtractionModel, arg map[string]interface{}, c WireCall) (map[string]interface{}, error) {
	opts := provider.ExtractionCallOptions{
		Headers:         strMap(arg["headers"]),
		ProviderOptions: objMap(arg["providerOptions"]),
		Fields:          map[string]provider.ExtractionField{},
		FieldOrder:      c.RawFieldOrder(),
	}
	if v, ok := arg["referenceDate"].(string); ok {
		opts.ReferenceDate = v
	}
	if v, ok := arg["maxOutputTokens"].(float64); ok {
		n := int(v)
		opts.MaxOutputTokens = &n
	}
	if v, ok := arg["reasoning"].(string); ok {
		r := types.ReasoningLevel(v)
		opts.Reasoning = &r
	}
	switch st := arg["state"].(type) {
	case []interface{}:
		var parts []provider.DecisionStatePart
		rawValues := decideStateValues(c)
		for i, pv := range st {
			pm, _ := pv.(map[string]interface{})
			switch pm["type"] {
			case "text":
				txt, _ := pm["text"].(string)
				part := provider.DecisionTextPart(txt)
				part.ProviderOptions = objMap(pm["providerOptions"])
				parts = append(parts, part)
			case "json":
				value := pm["value"]
				if i < len(rawValues) && len(rawValues[i]) > 0 {
					value = providerutils.OrderLike(value, rawValues[i])
				}
				parts = append(parts, provider.DecisionJSONPart(value))
			case "file":
				fd, ok := fileDataFromTS(pm["data"])
				if !ok {
					return map[string]interface{}{"skip": "extraction file part data"}, errWireSkip
				}
				mt, _ := pm["mediaType"].(string)
				fd.MediaType = mt
				fname, _ := pm["filename"].(string)
				fc := types.FileContent{FileData: fd, MediaType: mt, Filename: fname}
				fc.ProviderOptions = objMap(pm["providerOptions"])
				parts = append(parts, provider.DecisionFilePart(fc))
			default:
				return map[string]interface{}{"skip": "extraction state part type"}, errWireSkip
			}
		}
		opts.State = parts
	default:
		opts.State = st
	}
	if fs, ok := arg["fields"].(map[string]interface{}); ok {
		for id, fv := range fs {
			fm, _ := fv.(map[string]interface{})
			f := provider.ExtractionField{Instructions: fm["instructions"]}
			f.Type, _ = fm["type"].(string)
			f.Unit, _ = fm["unit"].(string)
			f.Items, _ = fm["items"].(string)
			if b, ok := fm["integer"].(bool); ok {
				f.Integer = &b
			}
			if d, ok := fm["decimals"].(float64); ok {
				n := int(d)
				f.Decimals = &n
			}
			opts.Fields[id] = f
		}
	}
	res, err := m.DoExtract(ctx, opts)
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{
		"answers":          res.Answers,
		"warnings":         WireWarnings(res.Warnings),
		"providerMetadata": res.ProviderMetadata,
		"usage":            res.Usage,
	}
	if r := res.Response; r != nil {
		out["response"] = map[string]interface{}{"id": r.ID, "modelId": r.ModelID, "headers": wirePruneHeaders(r.Headers), "body": r.Body}
	}
	return out, nil
}
