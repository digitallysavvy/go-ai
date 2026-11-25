package youcom

import (
	"strings"
	"testing"
)

// TestR97integDecodeKeepsPrototypeKey: @youdotcom-oss/ai-sdk-plugin reads
// the search, research and contents bodies with `await response.json()`
// (plain JSON.parse), so a __proto__ key is an ordinary property.
func TestR97integDecodeKeepsPrototypeKey(t *testing.T) {
	var v map[string]interface{}
	if err := decodeYouJSON(strings.NewReader(`{"__proto__":{"a":1},"constructor":{"prototype":{}},"ok":true}`), &v); err != nil {
		t.Fatal(err)
	}
	if v["ok"] != true {
		t.Fatalf("v = %v", v)
	}
}
