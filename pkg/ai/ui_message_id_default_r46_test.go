package ai

import (
	"context"
	"regexp"
	"testing"
)

// TS createUIMessageStream defaults generateId to the built-in generateId:
// 16 alphanumerics without a prefix (not a "call-" callId).
func TestCreateUIMessageStream_DefaultMessageIDLikeTS(t *testing.T) {
	var id string
	stream, errCh := CreateUIMessageStreamWithOptions(context.Background(), UIMessageStreamOptions{
		Execute: func(writer UIMessageStreamWriter) {
			writer.Write(UIMessageChunk{"type": "start"})
		},
		OnEnd: func(event map[string]interface{}) {
			if msg, ok := event["responseMessage"].(UIMessageChunk); ok {
				id, _ = msg["id"].(string)
			}
		},
	})
	var startID string
	for chunk := range stream {
		if chunk["type"] == "start" {
			startID, _ = chunk["messageId"].(string)
		}
	}
	if err := <-errCh; err != nil {
		t.Fatalf("stream error = %v", err)
	}
	if !regexp.MustCompile(`^[0-9A-Za-z]{16}$`).MatchString(id) || startID != id {
		t.Fatalf("response message id = %q, start messageId = %q, want one 16-character id", id, startID)
	}
}
