package http

import (
	"strings"
	"testing"
)

// A fractional count is accepted (TS z.number()), so when the body also
// violates the schema elsewhere, the error must name that violation, not the
// count the retry accepts.
func TestR80carryDecodeReportsRealViolationAfterFractionalCount(t *testing.T) {
	var v struct {
		Usage struct {
			Tokens int `json:"tokens"`
		} `json:"usage"`
		Done bool `json:"done"`
	}
	err := decodeJSONBody([]byte(`{"usage":{"tokens":1.5},"done":"yes"}`), &v)
	if err == nil || !strings.Contains(err.Error(), "string") || !strings.Contains(err.Error(), "done") {
		t.Fatalf("got %v, want the string-into-bool error on done", err)
	}
	if err := decodeJSONBody([]byte(`{"usage":{"tokens":1.5},"done":true}`), &v); err != nil || v.Usage.Tokens != 1 || !v.Done {
		t.Fatalf("got %v %+v", err, v)
	}
}
