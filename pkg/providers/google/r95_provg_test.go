package google

import "testing"

type r95provgLooseTarget struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// looseDecode (the interactions unknownEvent fallback) reads the payload
// with exact keys, as the strict decode and zod do: "Name" is not name.
func TestR95provgLooseDecodeKeysMatchExactly(t *testing.T) {
	var v r95provgLooseTarget
	looseDecode([]byte(`{"Name":"wrong case","count":2}`), &v)
	if v.Name != "" || v.Count != 2 {
		t.Fatalf("decoded %+v, want name empty and count 2", v)
	}
}
