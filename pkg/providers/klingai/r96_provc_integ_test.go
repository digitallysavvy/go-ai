package klingai

import (
	"errors"
	"testing"
)

// TestR96provcErrorCodeOutOfRange: klingaiErrorDataSchema's code is
// z.number(), which rejects Infinity (JSON.parse of 1e400): the body does not
// match, so the message is the HTTP status text and there is no data.
func TestR96provcErrorCodeOutOfRange(t *testing.T) {
	body := []byte(`{"code":1e400,"message":"m"}`)
	if data := klingaiErrorData(body); data != nil {
		t.Fatalf("data = %#v, want nil", data)
	}
	var e *Error
	if err := parseKlingAIErrorBody(500, nil, body); !errors.As(err, &e) || e.Message != "Internal Server Error" {
		t.Fatalf("err = %v, want the status text", err)
	}
}
