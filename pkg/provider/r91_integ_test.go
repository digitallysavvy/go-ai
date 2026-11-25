package provider

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// core2-16 (ECMAScript OrdinaryOwnPropertyKeys, node Object.keys): array
// index keys come first in numeric order.
func TestR91integOrderedKeysArrayIndexFirst(t *testing.T) {
	m := map[string]int{"2": 0, "b": 0, "10": 0, "1": 0, "a": 0, "01": 0}
	if got := OrderedKeys(m, []string{"b", "10"}); !reflect.DeepEqual(got, []string{"1", "2", "10", "b", "01", "a"}) {
		t.Fatalf("OrderedKeys = %v", got)
	}
}

// core2-17 (TS stringify-for-telemetry / convert-to-language-model-prompt
// 637-643): a file state part is type, mediaType, filename, data.
func TestR91integDecisionFilePartKeyOrder(t *testing.T) {
	b, err := json.Marshal(DecisionFilePart(types.FileContent{MediaType: "image/png", Filename: "a.png", FileData: types.FileData{Type: types.FileDataTypeData, Data: []byte("x")}}))
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"type":"file","mediaType":"image/png","filename":"a.png","data":"eA=="}`; string(b) != want {
		t.Fatalf("json = %s, want %s", b, want)
	}
}

// core2-18 (TS convertBase64ToUint8Array): unpadded and base64url data
// strings decode.
func TestR91integDecisionInlineDataLenient(t *testing.T) {
	for _, s := range []string{"iVBORw0KGgo", "iVBORw0KGgo=", "_-8"} {
		part := DecisionFilePart(types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeData, DataString: s}})
		if _, data, err := part.InlineData(); err != nil || len(data) == 0 {
			t.Fatalf("InlineData(%q) = %v, %v", s, data, err)
		}
	}
}
