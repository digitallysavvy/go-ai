package harness

import "testing"

func TestDecodedErrorPartKeepsWireKeyOrder(t *testing.T) {
	part, err := DecodeStreamPart([]byte(`{"type":"error","error":{"z":1,"a":{"y":2.0,"b":3},"7":true,"name":"x"}}`))
	if err != nil {
		t.Fatal(err)
	}
	ep := part.(*ErrorPart)
	want := `{"7":true,"z":1,"a":{"y":2,"b":3},"name":"x"}`
	if got := ep.Message(); got != want {
		t.Fatalf("Message = %s, want %s", got, want)
	}
	if got := ep.AsError().Error(); got != want {
		t.Fatalf("AsError = %s, want %s", got, want)
	}
	ep.Error = map[string]any{"b": 1, "a": 2}
	if got := ep.Message(); got != `{"a":2,"b":1}` {
		t.Fatalf("replaced Error should not use stale text, got %s", got)
	}
}
