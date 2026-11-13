package fal

import (
	"testing"
)

// TS createJsonResponseHandler: JSON.parse reads 1e999 as Infinity and zod
// strips the undeclared key (ruling 4).
func TestR98provcFalVideoResponseOutOfRangeUndeclared(t *testing.T) {
	body := []byte(`{"video":{"url":"https://x/v.mp4","bitrate":1e999},"seed":1}`)
	if err := validateVideoStatus(200, nil, body); err != nil {
		t.Fatalf("status: want valid, got %v", err)
	}
	job := []byte(`{"request_id":"r1","queue_position":1e999}`)
	if err := validateVideoJob(200, nil, job); err != nil {
		t.Fatalf("job: want valid, got %v", err)
	}
}
