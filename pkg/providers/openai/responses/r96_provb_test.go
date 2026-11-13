package responses

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestR96provbAnnotationJSONStringify: a provider annotation replayed or
// carried in metadata is written like JSON.stringify: <, > and & stay
// literal (main ruling 4, write side).
func TestR96provbAnnotationJSONStringify(t *testing.T) {
	for _, a := range []TextAnnotation{
		{Type: "url_citation", URL: "https://x.test/?a=1&b=2", Title: "<t>"},
		{Type: "file_citation", FileID: "f", Filename: "<a&b>.txt"},
	} {
		b, err := a.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), `\u00`) {
			t.Errorf("%s: HTML characters escaped", b)
		}
		var back TextAnnotation
		if err := json.Unmarshal(b, &back); err != nil || back != a {
			t.Errorf("round trip %s = %+v (%v)", b, back, err)
		}
	}
}
