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
		`{"bridge":{"port":1,"token":"x","lastSeenEventId":0}}`,
		`{"bridge":{"port":1,"token":"x","lastSeenEventId":0,"stateDir":null}}`}
	for _, v := range valid {
		if err := ValidateLifecycleStateObject(json.RawMessage(v), &state{}); err != nil {
			t.Errorf("%q: unexpected error %v", v, err)
		}
	}
	invalid := []string{`[]`, `"x"`, `{"threadId":5}`,
		`{"bridge":{"port":1,"token":"x"}}`, `{"bridge":{"port":"1","token":"x","lastSeenEventId":0}}`,
		`{"bridge":[]}`, `{"bridge":null}`, `{"threadId":null}`,
		`{"bridge":{"port":1,"token":"x","lastSeenEventId":0,"sandboxId":null}}`, `{"bridge":{"token":"x","lastSeenEventId":0}}`}
	for _, v := range invalid {
		if err := ValidateLifecycleStateObject(json.RawMessage(v), &state{}); err == nil {
			t.Errorf("%q: expected an error", v)
		}
	}
}

func TestValidateLifecycleStateObjectRejectsNullCredentialEnvValue(t *testing.T) {
	var dst struct {
		Env map[string]string `json:"sandboxCredentialEnvironment,omitempty"`
	}
	if err := ValidateLifecycleStateObject(json.RawMessage(`{"sandboxCredentialEnvironment":{"A":"x"}}`), &dst); err != nil {
		t.Fatalf("string values must pass: %v", err)
	}
	if err := ValidateLifecycleStateObject(json.RawMessage(`{"sandboxCredentialEnvironment":{"A":null}}`), &dst); err == nil {
		t.Fatal("null record value must be rejected like z.record(z.string(), z.string())")
	}
}
