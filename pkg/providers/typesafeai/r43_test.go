package typesafeai

import "testing"

// TS errorToMessage is JSON.stringify(error.detail): numbers read through a
// double and strings use JS escaping, not the source text.
func TestR43_DetailObjectIsStringifiedLikeJS(t *testing.T) {
	got := typesafeErrorMessage(422, []byte(`{"detail":{"a":1.0,"b":1e2,"c":"é<>","10":0,"2":1}}`))
	want := `{"2":1,"10":0,"a":1,"b":100,"c":"é<>"}`
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}
