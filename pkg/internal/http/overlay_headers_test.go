package http

import "testing"

func TestOverlayHeadersReplacesCaseInsensitively(t *testing.T) {
	for i := 0; i < 50; i++ {
		h := map[string]string{"authorization": "Bearer old", "Authorization": "Bearer older", "X-Other": "1"}
		overlayHeaders(h, map[string]string{"Authorization": "Bearer new"})
		if len(h) != 2 || h["Authorization"] != "Bearer new" || h["X-Other"] != "1" {
			t.Fatalf("exact spelling present: %v", h)
		}
	}
	for i := 0; i < 50; i++ {
		h := map[string]string{"authorization": "Bearer old"}
		overlayHeaders(h, map[string]string{"Authorization": "Bearer new"})
		if len(h) != 1 || h["authorization"] != "Bearer new" {
			t.Fatalf("the existing spelling keeps the new value: %v", h)
		}
	}
	h := map[string]string{}
	overlayHeaders(h, map[string]string{"X-Key": "k"})
	if h["X-Key"] != "k" {
		t.Fatalf("new header not added: %v", h)
	}
}
