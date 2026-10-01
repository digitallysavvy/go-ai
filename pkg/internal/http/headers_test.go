package http

import "testing"

func TestMergeHeadersLaterMapsOverrideEarlierMaps(t *testing.T) {
	got := MergeHeaders(
		map[string]string{
			"Authorization": "Bearer default",
			"Content-Type":  "application/json",
		},
		nil,
		map[string]string{
			"Authorization": "Bearer custom",
			"X-Test":        "1",
		},
	)

	if got["Authorization"] != "Bearer custom" {
		t.Fatalf("Authorization = %q, want custom override", got["Authorization"])
	}
	if got["Content-Type"] != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got["Content-Type"])
	}
	if got["X-Test"] != "1" {
		t.Fatalf("X-Test = %q, want 1", got["X-Test"])
	}
}

func TestMergeHeadersReturnsIndependentMap(t *testing.T) {
	base := map[string]string{"A": "1"}
	got := MergeHeaders(base)
	got["A"] = "2"

	if base["A"] != "1" {
		t.Fatalf("base header mutated: %q", base["A"])
	}
}
