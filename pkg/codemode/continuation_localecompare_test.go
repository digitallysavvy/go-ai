package codemode

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

// Fixture produced by Node (canonicalJson with localeCompare key sort and
// createHmac('sha256', 'k').digest('base64url')), so a TS-signed continuation
// verifies in Go and the other way round.
func TestCanonicalJSONKeySortMatchesLocaleCompare(t *testing.T) {
	in := map[string]interface{}{
		"Z": 1, "a": 2,
		"A": map[string]interface{}{"_x": 1, "-a": 2, "B": 3, "b": 4, "10": 5, "2": 6, "é": 7, "e": 8, "Id": 9, "id": 10, "ID": 11},
		"a b": []interface{}{map[string]interface{}{"c": 1, "C": 2}},
		"ab":  3, "aB": 4,
	}
	want := `{"a":2,"A":{"_x":1,"-a":2,"10":5,"2":6,"b":4,"B":3,"e":8,"é":7,"id":10,"Id":9,"ID":11},"a b":[{"c":1,"C":2}],"ab":3,"aB":4,"Z":1}`
	got, err := canonicalJSON(in)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("canonicalJSON =\n%s\nwant\n%s", got, want)
	}
	mac := hmac.New(sha256.New, []byte("k"))
	mac.Write([]byte(got))
	if sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil)); sig != "iOXGN03kT4hwxlrJDqaMdWMhaSmYE5fpYNbk1k8u164" {
		t.Fatalf("signature = %s", sig)
	}
}
