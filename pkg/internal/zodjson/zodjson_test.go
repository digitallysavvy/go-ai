package zodjson

import "testing"

func TestStringifyKeepsSchemaOrderAndDropsUnknownKeys(t *testing.T) {
	spec := Obj([]string{"id", "data"}, map[string]*Node{
		"data": Obj([]string{"a", "list"}, map[string]*Node{"list": Array(Obj([]string{"x"}, nil))}),
	})
	got := Stringify([]byte(`{"z":1,"data":{"list":[{"y":2,"x":1}],"a":null,"q":3},"id":"i"}`), spec)
	if want := `{"id":"i","data":{"a":null,"list":[{"x":1}]}}`; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	if got := Stringify([]byte(`not json`), spec); got != "not json" {
		t.Fatalf("invalid JSON must pass through, got %q", got)
	}
}
