package jsonexact

import (
	"encoding/json"
	"testing"
)

type r94carryInner struct {
	Message string `json:"message"`
	Code    *int   `json:"code,omitempty"`
}

type r94carryEmbedded struct {
	ID string `json:"id"`
}

type r94carryBody struct {
	r94carryEmbedded
	Error    *r94carryInner           `json:"error"`
	Items    []r94carryInner          `json:"items"`
	ByName   map[string]r94carryInner `json:"byName"`
	Raw      json.RawMessage          `json:"raw"`
	Any      interface{}              `json:"any"`
	Untagged string
}

func decode(t *testing.T, body string) r94carryBody {
	t.Helper()
	var out r94carryBody
	if err := json.Unmarshal(Filter([]byte(body), &out), &out); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return out
}

// zod matches keys exactly: {"Error":{"Message":...}} does not fill
// error.message, at any depth (struct, slice, map, embedded field).
func TestR94carryFilterDropsCaseInsensitiveMatches(t *testing.T) {
	if got := decode(t, `{"Error":{"Message":"m"}}`); got.Error != nil {
		t.Errorf("Error key filled error: %+v", got.Error)
	}
	if got := decode(t, `{"error":{"Message":"m","message":"x","MESSAGE":"y"}}`); got.Error == nil || got.Error.Message != "x" {
		t.Errorf("error = %+v, want message x", got.Error)
	}
	got := decode(t, `{"ID":"no","items":[{"Message":"a"},{"message":"b"}],"byName":{"k":{"MESSAGE":"c"}},"untagged":"no","Untagged":"yes"}`)
	if got.ID != "" || len(got.Items) != 2 || got.Items[0].Message != "" || got.Items[1].Message != "b" || got.ByName["k"].Message != "" || got.Untagged != "yes" {
		t.Errorf("got %+v", got)
	}
}

// Values the decoder does not match itself (RawMessage, interface{}),
// unknown keys and number spellings are left as sent; invalid JSON is
// returned unchanged so the caller's error text is the usual one.
func TestR94carryFilterKeepsOtherValues(t *testing.T) {
	body := `{"raw":{"Message":1e400},"any":{"Message":1},"other":{"Message":1},"Error":null,"error":{"message":"m","code":1}}`
	got := decode(t, body)
	if string(got.Raw) != `{"Message":1e400}` || got.Any.(map[string]interface{})["Message"] != 1.0 || got.Error == nil || got.Error.Message != "m" {
		t.Errorf("got %+v raw=%s", got, got.Raw)
	}
	if out := Filter([]byte(`{"error":{"message":"m"}}`), &r94carryBody{}); string(out) != `{"error":{"message":"m"}}` {
		t.Errorf("clean body rewritten: %s", out)
	}
	for _, bad := range []string{`{"Error":1`, `{"Error":1} x`, ``} {
		if out := Filter([]byte(bad), &r94carryBody{}); string(out) != bad {
			t.Errorf("invalid %q rewritten to %q", bad, out)
		}
	}
	// A plain map or interface target is untouched.
	var m map[string]interface{}
	if out := Filter([]byte(`{"Error":1}`), &m); string(out) != `{"Error":1}` {
		t.Errorf("map target rewritten: %s", out)
	}
}

// Fold matching is EqualFold's (the Kelvin sign folds to k).
func TestR94carryFilterUnicodeFold(t *testing.T) {
	type kv struct {
		K string `json:"k"`
	}
	var out kv
	if err := json.Unmarshal(Filter([]byte("{\"K\":\"v\"}"), &out), &out); err != nil || out.K != "" {
		t.Errorf("Kelvin key filled k: %+v %v", out, err)
	}
}
