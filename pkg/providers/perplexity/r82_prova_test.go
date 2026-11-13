package perplexity

import (
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r82provaPNGBadTail is a PNG signature in standard base64 followed by a tail
// that is not base64: TS detectMediaType decodes only the sniffed prefix
// (decodePrefix: ceil(18/3)*4 characters), so the type is still detected and
// the string is forwarded as is (convertToBase64).
const r82provaPNGBadTail = "iVBORw0KGgoAAAAAAAAAAAAAAAAAAAAA!!not-base64!!"

// r82provaBadPrefix has a prefix atob rejects: TS detectMediaType throws.
const r82provaBadPrefix = "!!!!iVBORw0KGgoAAAAAAAAAAAAAAAA"

func r82provaDataFile(mediaType, data string) types.FileContent {
	return types.FileContent{MediaType: mediaType, FileData: types.FileData{Type: types.FileDataTypeData, MediaType: mediaType, DataString: data}}
}

func r82provaIsDecodeError(err error) bool {
	var unsupported *providererrors.UnsupportedFunctionalityError
	return err != nil && !errors.As(err, &unsupported)
}

func TestR82provaPerplexitySniffDecodesOnlyPrefix(t *testing.T) {
	got, err := convertPerplexityUserPart(r82provaDataFile("image", r82provaPNGBadTail))
	if err != nil || got["image_url"] != "data:image/png;base64,"+r82provaPNGBadTail {
		t.Fatalf("part = %v, %v", got, err)
	}
	if _, err := convertPerplexityUserPart(r82provaDataFile("image", r82provaBadPrefix)); !r82provaIsDecodeError(err) {
		t.Fatalf("bad prefix: err = %v", err)
	}
}

func TestR82provaPerplexityDecisionSniffDecodesOnlyPrefix(t *testing.T) {
	got, err := convertToPerplexityDecisionState([]provider.DecisionStatePart{imageState("image", r82provaPNGBadTail)})
	want := map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "data:image/png;base64," + r82provaPNGBadTail}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, %v", got, err)
	}
	if _, err := convertToPerplexityDecisionState([]provider.DecisionStatePart{imageState("image", r82provaBadPrefix)}); !r82provaIsDecodeError(err) {
		t.Fatalf("bad prefix: err = %v", err)
	}
}

// TS keeps a single string/object/array as native state (`typeof single ===
// 'object'`); Go matched only map[string]interface{} and []interface{}, so a
// typed map, struct or json.RawMessage was wrapped in an array.
func TestR82provaPerplexityDecisionTypedObjectIsNativeState(t *testing.T) {
	type review struct {
		Review string `json:"review"`
	}
	for _, tc := range []struct {
		name   string
		value  interface{}
		native bool
	}{
		{"typed map", map[string]string{"review": "ok"}, true},
		{"struct", review{Review: "ok"}, true},
		{"raw object", json.RawMessage(`{"review":"ok"}`), true},
		{"typed slice", []string{"a"}, true},
		{"number", 3, false},
		{"bool", true, false},
		{"nil", nil, false},
	} {
		got, err := convertToPerplexityDecisionState([]provider.DecisionStatePart{{Type: "json", Value: tc.value}})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		_, wrapped := got.([]interface{})
		if wrapped == tc.native {
			t.Fatalf("%s: state = %#v, native %v", tc.name, got, tc.native)
		}
	}
	// Images inside a typed value are counted (TS countImages walks every
	// object): a non-data-URL image is rejected.
	bad := map[string]map[string]interface{}{"x": {"type": "image_url", "image_url": map[string]string{"url": "https://example.com/a.png"}}}
	if _, err := convertToPerplexityDecisionState([]provider.DecisionStatePart{{Type: "json", Value: bad}}); err == nil {
		t.Fatal("typed value with a URL image: want an error")
	}
}

// TS emitOutputSources reads results only from search_results items
// (getSearchResults) and contents only from fetch_url_results items; a
// finance_results item's `results` carry no source (TS wire replay "doStream
// > preserves native tool traces in raw chunks ..."). Go emitted a url-less
// source for it.
func TestR82provaPerplexityStreamFinanceResultsNoSource(t *testing.T) {
	body := "data: " + `{"type":"response.output_item.done","output_index":0,"item":{"type":"finance_results","results":[{"category":"quote","content":"AAPL"}],"contents":[{"url":"https://example.com/x"}]}}` + "\n\n" +
		"data: " + `{"type":"response.completed","response":{"id":"r","created_at":1,"model":"m","object":"response","output":[],"status":"completed"}}` + "\n\n"
	s := newPerplexityAgentStream(io.NopCloser(strings.NewReader(body)), nil, false, nil)
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeSource {
			t.Fatalf("unexpected source %+v", c.SourceContent)
		}
	}
}

// TS rejects a non-data decision file (`Perplexity decision file input:
// url`); a Go part using the legacy URL field was sniffed as empty data and
// failed with the image media type error instead.
func TestR82provaPerplexityDecisionLegacyURLFile(t *testing.T) {
	_, err := convertToPerplexityDecisionState([]provider.DecisionStatePart{provider.DecisionFilePart(types.FileContent{MediaType: "image/png", URL: "https://example.com/a.png"})})
	var unsupported *providererrors.UnsupportedFunctionalityError
	if !errors.As(err, &unsupported) || unsupported.Functionality != "Perplexity decision file input: url" {
		t.Fatalf("err = %v", err)
	}
}
