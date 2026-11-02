package gemini

import (
	"bytes"
	"encoding/json"
)

// metaShape is the declared shape of a provider-metadata value TS parses with
// a plain z.object schema (google-language-model.ts responseSchema /
// chunkSchema). zod strips undeclared object keys, so TS
// providerMetadata.google.{promptFeedback, groundingMetadata,
// urlContextMetadata, safetyRatings} hold the declared keys only, in schema
// order. A nil *metaShape is a leaf kept as sent.
type metaShape struct {
	keys   []string              // object keys in schema order
	fields map[string]*metaShape // child shapes (nil child = leaf)
	elem   *metaShape            // array element shape
	isArr  bool
	// union lists object members tried in order (z.union): the first whose
	// required keys are present (and numbers) wins.
	union    []*metaShape
	required []string // number-typed keys a union member requires
}

func metaObj(pairs ...interface{}) *metaShape {
	s := &metaShape{fields: map[string]*metaShape{}}
	for i := 0; i+1 < len(pairs); i += 2 {
		k := pairs[i].(string)
		s.keys = append(s.keys, k)
		child, _ := pairs[i+1].(*metaShape)
		s.fields[k] = child
	}
	return s
}

func metaArr(elem *metaShape) *metaShape { return &metaShape{isArr: true, elem: elem} }

var (
	metaSafetyRating = metaObj("category", nil, "probability", nil, "probabilityScore", nil, "severity", nil, "severityScore", nil, "blocked", nil)

	metaSafetyRatings = metaArr(metaSafetyRating)

	metaPromptFeedback = metaObj("blockReason", nil, "safetyRatings", metaSafetyRatings)

	metaURLContext = metaObj("urlMetadata", metaArr(metaObj("retrievedUrl", nil, "urlRetrievalStatus", nil)))

	metaRetrievalMetadata = func() *metaShape {
		withScore := metaObj("webDynamicRetrievalScore", nil)
		withScore.required = []string{"webDynamicRetrievalScore"}
		return &metaShape{union: []*metaShape{withScore, metaObj()}}
	}()

	metaGrounding = metaObj(
		"webSearchQueries", nil,
		"imageSearchQueries", nil,
		"retrievalQueries", nil,
		"searchEntryPoint", metaObj("renderedContent", nil),
		"groundingChunks", metaArr(metaObj(
			"web", metaObj("uri", nil, "title", nil),
			"image", metaObj("sourceUri", nil, "imageUri", nil, "title", nil, "domain", nil),
			"retrievedContext", metaObj(
				"uri", nil, "title", nil, "text", nil, "fileSearchStore", nil,
				"customMetadata", metaArr(metaObj(
					"key", nil, "stringValue", nil, "numericValue", nil,
					"stringListValue", metaObj("values", nil),
				)),
			),
			"maps", metaObj("uri", nil, "title", nil, "text", nil, "placeId", nil),
		)),
		"groundingSupports", metaArr(metaObj(
			"segment", metaObj("startIndex", nil, "endIndex", nil, "text", nil),
			"segment_text", nil,
			"groundingChunkIndices", nil,
			"supportChunkIndices", nil,
			"confidenceScores", nil,
			"confidenceScore", nil,
		)),
		"retrievalMetadata", metaRetrievalMetadata,
	)
)

// stripMetadata returns raw with the object keys shape does not declare
// removed (recursively, in schema key order), as TS zod parsing leaves the
// value. null, absent and values whose JSON type does not match the shape
// are returned unchanged.
func stripMetadata(raw json.RawMessage, shape *metaShape) json.RawMessage {
	trimmed := bytes.TrimSpace(raw)
	if shape == nil || len(trimmed) == 0 || string(trimmed) == "null" {
		return raw
	}
	switch {
	case shape.isArr:
		var items []json.RawMessage
		if trimmed[0] != '[' || json.Unmarshal(trimmed, &items) != nil {
			return raw
		}
		var b bytes.Buffer
		b.WriteByte('[')
		for i, it := range items {
			if i > 0 {
				b.WriteByte(',')
			}
			b.Write(stripMetadata(it, shape.elem))
		}
		b.WriteByte(']')
		return b.Bytes()
	case shape.union != nil:
		var obj map[string]json.RawMessage
		if trimmed[0] != '{' || json.Unmarshal(trimmed, &obj) != nil {
			return raw
		}
		for _, member := range shape.union {
			if metaMatches(obj, member) {
				return stripMetadata(trimmed, member)
			}
		}
		return raw
	default:
		var obj map[string]json.RawMessage
		if trimmed[0] != '{' || json.Unmarshal(trimmed, &obj) != nil {
			return raw
		}
		var b bytes.Buffer
		b.WriteByte('{')
		n := 0
		for _, k := range shape.keys {
			v, ok := obj[k]
			if !ok {
				continue
			}
			if n > 0 {
				b.WriteByte(',')
			}
			n++
			kb, _ := json.Marshal(k)
			b.Write(kb)
			b.WriteByte(':')
			b.Write(stripMetadata(v, shape.fields[k]))
		}
		b.WriteByte('}')
		return b.Bytes()
	}
}

// metaMatches reports whether obj has every number key member requires.
func metaMatches(obj map[string]json.RawMessage, member *metaShape) bool {
	for _, k := range member.required {
		var f float64
		v, ok := obj[k]
		if !ok || string(bytes.TrimSpace(v)) == "null" || json.Unmarshal(v, &f) != nil {
			return false
		}
	}
	return true
}
