package bridge

import "testing"

// TS openBridgeWebSocket ignores a message that fails harnessV1BridgeHelloSchema
// and keeps waiting: optional keys must not be null and must have the right
// type.
func TestR43ParseHelloAppliesHelloSchema(t *testing.T) {
	for frame, want := range map[string]bool{
		`{"type":"bridge-hello"}`:                                                           true,
		`{"type":"bridge-hello","state":"idle","lastSeq":3}`:                                true,
		`{"type":"bridge-hello","capabilities":{"experimental_userMessageResponses":true}}`: true,
		`{"type":"bridge-hello","state":null}`:                                              false,
		`{"type":"bridge-hello","lastSeq":null}`:                                            false,
		`{"type":"bridge-hello","capabilities":null}`:                                       false,
		`{"type":"bridge-hello","capabilities":{"experimental_userMessageResponses":null}}`: false,
		`{"type":"bridge-hello","state":1}`:                                                 false,
		`{"type":"thread","threadId":"t"}`:                                                  false,
	} {
		if _, got := parseHello([]byte(frame)); got != want {
			t.Errorf("parseHello(%s) = %v, want %v", frame, got, want)
		}
	}
}
