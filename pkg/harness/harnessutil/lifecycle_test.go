package harnessutil

import (
	"encoding/json"
	"testing"
)

func TestValidateLifecycleStateObject(t *testing.T) {
	type coords struct {
		Port int `json:"port"`
	}
	type state struct {
		ThreadID string  `json:"threadId,omitempty"`
		Bridge   *coords `json:"bridge,omitempty"`
	}
	valid := []string{``, `null`, `{}`, `{"threadId":"t","extra":1}`,
		`{"bridge":{"port":1,"token":"x","lastSeenEventId":0}}`}
	for _, v := range valid {
		if err := ValidateLifecycleStateObject(json.RawMessage(v), &state{}); err != nil {
			t.Errorf("%q: unexpected error %v", v, err)
		}
	}
	invalid := []string{`[]`, `"x"`, `{"threadId":5}`,
		`{"bridge":{"port":1,"token":"x"}}`, `{"bridge":{"port":"1","token":"x","lastSeenEventId":0}}`,
		`{"bridge":[]}`, `{"bridge":{"token":"x","lastSeenEventId":0}}`}
	for _, v := range invalid {
		if err := ValidateLifecycleStateObject(json.RawMessage(v), &state{}); err == nil {
			t.Errorf("%q: expected an error", v)
		}
	}
}
