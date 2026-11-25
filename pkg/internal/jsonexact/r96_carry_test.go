package jsonexact

import (
	"encoding/json"
	"math"
	"testing"
)

type r96carryTarget struct {
	Value    interface{}            `json:"value"`
	Metadata map[string]interface{} `json:"metadata"`
	Raw      json.RawMessage        `json:"raw"`
	Text     string                 `json:"text"`
	List     []interface{}          `json:"list"`
	Nested   *r96carryTarget        `json:"nested"`
}

// JSON.parse('{"value":1e400}').value is Infinity; encoding/json fails the
// whole decode. Every interface position takes +-Inf.
func TestR96carryDecodeOutOfRangeInInterfacePositions(t *testing.T) {
	data := []byte(`{"value":1e400,"metadata":{"p":{"score":-1e999,"ok":1}},"raw":{"n":1e400},"text":"1e400","list":[1,2e308],"nested":{"value":-1E+400}}`)
	var got r96carryTarget
	if err := Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if f, _ := got.Value.(float64); !math.IsInf(f, 1) {
		t.Errorf("value = %#v, want +Inf", got.Value)
	}
	p, _ := got.Metadata["p"].(map[string]interface{})
	if f, _ := p["score"].(float64); !math.IsInf(f, -1) || p["ok"] != 1.0 {
		t.Errorf("metadata.p = %#v, want score -Inf, ok 1", p)
	}
	if string(got.Raw) != `{"n":1e400}` {
		t.Errorf("raw = %s, want the literal kept byte for byte", got.Raw)
	}
	if got.Text != "1e400" {
		t.Errorf("text = %q", got.Text)
	}
	if len(got.List) != 2 || got.List[0] != 1.0 || !math.IsInf(got.List[1].(float64), 1) {
		t.Errorf("list = %#v", got.List)
	}
	if got.Nested == nil {
		t.Fatal("nested missing")
	}
	if f, _ := got.Nested.Value.(float64); !math.IsInf(f, -1) {
		t.Errorf("nested.value = %#v, want -Inf", got.Nested.Value)
	}
}

func TestR96carryDecodeOutOfRangeMapAndInterfaceTargets(t *testing.T) {
	var m map[string]interface{}
	if err := Unmarshal([]byte(`{"a":[{"b":1e400}],"c":"x"}`), &m); err != nil {
		t.Fatalf("map: %v", err)
	}
	inner := m["a"].([]interface{})[0].(map[string]interface{})
	if f, _ := inner["b"].(float64); !math.IsInf(f, 1) || m["c"] != "x" {
		t.Errorf("map = %#v", m)
	}
	var v interface{}
	if err := Unmarshal([]byte(`-1e400`), &v); err != nil {
		t.Fatalf("interface: %v", err)
	}
	if f, _ := v.(float64); !math.IsInf(f, -1) {
		t.Errorf("interface = %#v", v)
	}
	var arr []interface{}
	if err := Unmarshal([]byte(`[1e400, "s", 1e-400]`), &arr); err != nil {
		t.Fatalf("slice: %v", err)
	}
	if !math.IsInf(arr[0].(float64), 1) || arr[1] != "s" || arr[2] != 0.0 {
		t.Errorf("slice = %#v", arr)
	}
	var typed map[string]map[string]interface{}
	if err := Unmarshal([]byte(`{"x":{"y":1e400}}`), &typed); err != nil || !math.IsInf(typed["x"]["y"].(float64), 1) {
		t.Errorf("nested map = %#v, %v", typed, err)
	}
}

// A typed numeric field cannot hold Infinity (z.number() rejects it in TS);
// the decode fails with encoding/json's own error.
func TestR96carryDecodeOutOfRangeTypedFieldFails(t *testing.T) {
	type typed struct {
		F float64     `json:"f"`
		V interface{} `json:"v"`
	}
	data := []byte(`{"v":1e400,"f":1e400}`)
	var want typed
	wantErr := json.Unmarshal(data, &want)
	var got typed
	err := Unmarshal(data, &got)
	if err == nil || wantErr == nil || err.Error() != wantErr.Error() {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	var s struct {
		S map[string]string `json:"s"`
	}
	if err := Unmarshal([]byte(`{"s":{"k":1e400}}`), &s); err == nil {
		t.Fatal("a string map value took the number")
	}
}

// A fractional number in an integer field that the overflow hid comes back
// as the decode's number error, so UnmarshalChunk's truncation retry sees it.
func TestR96carryDecodeOutOfRangeThenNumberError(t *testing.T) {
	var got struct {
		N int         `json:"n"`
		V interface{} `json:"v"`
	}
	err := Decode([]byte(`{"v":1e400,"n":1.5}`), &got)
	var typeErr *json.UnmarshalTypeError
	if err == nil || !jsonAs(err, &typeErr) || typeErr.Value != "number 1.5" {
		t.Fatalf("err = %v, want the 1.5 type error", err)
	}
}

func jsonAs(err error, target **json.UnmarshalTypeError) bool {
	te, ok := err.(*json.UnmarshalTypeError)
	if ok {
		*target = te
	}
	return ok
}
