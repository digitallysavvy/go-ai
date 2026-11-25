package http

import "testing"

// Expected strings are Node's new URLSearchParams(...).toString() output.
func TestEncodeQueryMatchesURLSearchParams(t *testing.T) {
	got := encodeQuery(map[string]string{"a b~*": "x~y*z!()é\U0001F600'"})
	want := "a+b%7E*=x%7Ey*z%21%28%29%C3%A9%F0%9F%98%80%27"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
