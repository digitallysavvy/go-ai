package deepgram

import (
	"net/http"
	"reflect"
	"testing"
)

// cr-r86b-tools-to-integrator 5: TS reads the dg-* response headers from
// extractResponseHeaders: repeated lines joined with ", ", each byte one
// character (node fetch reads `\xc3\xa9` as "Ã©").
func TestR86bintegDeepgramHeadersReadAsFetch(t *testing.T) {
	got := buildSpeechProviderMetadata(http.Header{
		"Dg-Request-Id":             {"r\xc3\xa9"},
		"Dg-Model-Name":             {"a", "b"},
		"Dg-Additional-Model-Uuids": {"u1", "u2"},
		"Dg-Char-Count":             {"\xa042"},
	})
	want := map[string]interface{}{
		"requestId":            "rÃ©",
		"modelName":            "a, b",
		"additionalModelUuids": []string{"u1", " u2"},
		"charCount":            42,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("metadata = %#v, want %#v", got, want)
	}
}
