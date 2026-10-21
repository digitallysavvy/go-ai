package bridge

import "testing"

// zod `.optional()` accepts an absent key but rejects an explicit null, for
// stream parts and control frames alike.
func TestDecodeOutboundRejectsNullOptionalKeys(t *testing.T) {
	bad := []string{
		`{"type":"text-delta","id":"a","delta":"x","harnessMetadata":null}`,
		`{"type":"tool-input-start","id":"a","toolName":"t","title":null}`,
		`{"type":"tool-call","toolCallId":"a","toolName":"t","input":"{}","providerExecuted":null}`,
		`{"type":"tool-result","toolCallId":"a","toolName":"t","result":1,"isError":null}`,
		`{"type":"compaction","trigger":"auto","summary":"s","tokensBefore":null}`,
		`{"type":"stream-start","modelId":null}`,
		`{"type":"bridge-hello","state":null}`,
		`{"type":"bridge-hello","capabilities":{"experimental_userMessageResponses":null}}`,
		`{"type":"user-message-response","messageId":"m","accepted":true,"error":null}`,
		`{"type":"user-message-response","messageId":"m","accepted":false,"error":{}}`,
		`{"type":"debug-event","level":"info","subsystem":"s","message":"m","error":null}`,
		`{"type":"debug-event","level":"info","subsystem":"s","message":"m","attrs":null}`,
		`{"type":"debug-event","level":"info","subsystem":"s","message":"m","error":{"message":"x","stack":null}}`,
	}
	for _, in := range bad {
		if _, _, err := DecodeOutbound([]byte(in)); err == nil {
			t.Errorf("accepted %s", in)
		}
	}
	good := []string{
		`{"type":"text-delta","id":"a","delta":"x"}`,
		`{"type":"bridge-hello"}`,
		`{"type":"user-message-response","messageId":"m","accepted":false,"error":{"message":"x"}}`,
		`{"type":"debug-event","level":"info","subsystem":"s","message":"m","error":{"message":"x"}}`,
	}
	for _, in := range good {
		if _, _, err := DecodeOutbound([]byte(in)); err != nil {
			t.Errorf("rejected %s: %v", in, err)
		}
	}
}
