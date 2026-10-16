package jsstr

import "testing"

func TestMarshalIndentKeepsLineSeparatorsRaw(t *testing.T) {
	b, err := MarshalIndent(map[string]any{"a": "x y", "b": []int{}})
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"a\": \"x y\",\n  \"b\": []\n}"
	if string(b) != want {
		t.Fatalf("got %q want %q", b, want)
	}
}
