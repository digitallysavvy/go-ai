package cohere

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

// mapCohereCitationToSource ports TS mapCohereCitationToSource
// (cohere/src/cohere-citations.ts, 3aad18a417): the first source whose
// document is non-null provides the title (`document.title || 'Document'`);
// a citation with no document source (tool output only) maps to nil and is
// skipped. Provider metadata `cohere` carries start, end, text, sources (the
// parsed sources: the loose document keeps its extra keys, tool_output is
// kept, other unknown source keys are stripped) and citationType, each only
// when non-null. raw is a citation already checked against the TS schema.
func mapCohereCitationToSource(raw json.RawMessage) *types.SourceContent {
	parsed, ok := jsonorder.Parse(raw)
	if !ok {
		return nil
	}
	citation, ok := parsed.(*jsonorder.Object)
	if !ok {
		return nil
	}
	rawSources, _ := citation.Get("sources")
	sources, _ := rawSources.([]interface{})

	var document *jsonorder.Object
	for _, s := range sources {
		src, ok := s.(*jsonorder.Object)
		if !ok {
			continue
		}
		if d, ok := src.Get("document"); ok && d != nil {
			document, _ = d.(*jsonorder.Object)
			if document != nil {
				break
			}
		}
	}
	if document == nil {
		return nil
	}

	title := "Document"
	if t, ok := document.Get("title"); ok {
		if s, ok := t.(string); ok && s != "" {
			title = s
		}
	}

	meta := jsonorder.New()
	for _, key := range []string{"start", "end", "text"} {
		if v, ok := citation.Get(key); ok && v != nil {
			meta.Set(key, v)
		}
	}
	if rawSources != nil {
		meta.Set("sources", cohereParsedSources(sources))
	}
	if v, ok := citation.Get("type"); ok && v != nil {
		meta.Set("citationType", v)
	}
	outer := jsonorder.New()
	outer.Set("cohere", meta)
	// Stringify, not json.Marshal: no HTML escaping, as JSON.stringify.
	metaJSON := json.RawMessage(jsonorder.Stringify(outer))
	return &types.SourceContent{
		SourceType:       "document",
		ID:               streaming.GenerateID(),
		MediaType:        "text/plain",
		Title:            title,
		ProviderMetadata: metaJSON,
	}
}

// cohereParsedSources is the zod output of the citation `sources` array: each
// source keeps type, id, document and tool_output (in schema order, only the
// keys the input has); the loose document lists its known keys (id, text,
// title) first, then its other keys in input order.
func cohereParsedSources(sources []interface{}) []interface{} {
	out := make([]interface{}, 0, len(sources))
	for _, s := range sources {
		src, ok := s.(*jsonorder.Object)
		if !ok {
			out = append(out, s)
			continue
		}
		parsed := jsonorder.New()
		for _, key := range []string{"type", "id", "document", "tool_output"} {
			v, ok := src.Get(key)
			if !ok {
				continue
			}
			if key == "document" {
				if doc, isObj := v.(*jsonorder.Object); isObj {
					v = cohereLooseObject(doc, "id", "text", "title")
				}
			}
			parsed.Set(key, v)
		}
		out = append(out, parsed)
	}
	return out
}

// cohereLooseObject is a zod looseObject output: the shape keys present in
// the input first (shape order), then the remaining keys in input order.
func cohereLooseObject(in *jsonorder.Object, known ...string) *jsonorder.Object {
	out := jsonorder.New()
	isKnown := map[string]bool{}
	for _, k := range known {
		isKnown[k] = true
		if v, ok := in.Get(k); ok {
			out.Set(k, v)
		}
	}
	for _, k := range in.Keys() {
		if isKnown[k] {
			continue
		}
		v, _ := in.Get(k)
		out.Set(k, v)
	}
	return out
}
