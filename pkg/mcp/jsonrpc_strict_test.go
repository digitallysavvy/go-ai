package mcp

import "testing"

// TS JSONRPCMessageSchema is a union of zod .strict() objects.
func TestValidateJSONRPCMessageStrictParity(t *testing.T) {
	valid := []string{
		`{"jsonrpc":"2.0","id":"a","method":"x"}`,
		`{"jsonrpc":"2.0","id":1.0,"method":"x","params":{"_meta":{}}}`,
		`{"jsonrpc":"2.0","id":1e3,"method":""}`,
		`{"jsonrpc":"2.0","method":"x","params":{}}`,
		`{"jsonrpc":"2.0","id":1,"result":{}}`,
		`{"jsonrpc":"2.0","id":1,"result":{"resultType":"complete","extra":1}}`,
		`{"jsonrpc":"2.0","error":{"code":-1,"message":"","extra":true}}`,
		`{"jsonrpc":"2.0","id":2,"error":{"code":1,"message":"m","data":[1]}}`,
	}
	for _, in := range valid {
		if _, err := ValidateJSONRPCMessage([]byte(in)); err != nil {
			t.Errorf("%s: unexpected error %v", in, err)
		}
	}
	invalid := []string{
		`null`, `[]`, `1`,
		`{"jsonrpc":"2.0","id":1,"method":"x","extra":1}`,
		`{"jsonrpc":"2.0","method":"x","extra":1}`,
		`{"jsonrpc":"2.0","id":1,"result":{},"extra":1}`,
		`{"jsonrpc":"2.0","id":1,"error":{"code":1,"message":"m"},"extra":1}`,
		`{"jsonrpc":"2.0","id":1,"method":"x","result":{}}`,
		`{"jsonrpc":"2.0","id":1,"result":{},"error":{"code":1,"message":"m"}}`,
		`{"JSONRPC":"2.0","id":1,"method":"x"}`,
		`{"jsonrpc":"2.0","Method":"x"}`,
		`{"jsonrpc":"2.0","id":null,"method":"x"}`,
		`{"jsonrpc":"2.0","id":null,"error":{"code":1,"message":"m"}}`,
		`{"jsonrpc":"2.0","id":null,"result":{}}`,
		`{"jsonrpc":"2.0","id":true,"method":"x"}`,
		`{"jsonrpc":"2.0","id":9007199254740993,"method":"x"}`,
		`{"jsonrpc":"2.0","id":1,"method":1}`,
		`{"jsonrpc":"2.0","id":1,"method":"x","params":null}`,
		`{"jsonrpc":"2.0","id":1,"method":"x","params":[]}`,
		`{"jsonrpc":"2.0","id":1,"method":"x","params":{"_meta":null}}`,
		`{"jsonrpc":"2.0","id":1,"result":null}`,
		`{"jsonrpc":"2.0","id":1,"result":[]}`,
		`{"jsonrpc":"2.0","id":1,"result":5}`,
		`{"jsonrpc":"2.0","id":1,"result":{"_meta":1}}`,
		`{"jsonrpc":"2.0","id":1,"result":{"resultType":1}}`,
		`{"jsonrpc":"2.0","result":{}}`,
		`{"jsonrpc":"2.0","error":{"message":"m"}}`,
		`{"jsonrpc":"2.0","error":{"code":1.5,"message":"m"}}`,
		`{"jsonrpc":"2.0","error":{"code":1}}`,
		`{"jsonrpc":"2.0","error":"x"}`,
		`{"jsonrpc":"2.0","error":null}`,
	}
	for _, in := range invalid {
		if _, err := ValidateJSONRPCMessage([]byte(in)); err == nil {
			t.Errorf("%s: expected error", in)
		}
	}
}
