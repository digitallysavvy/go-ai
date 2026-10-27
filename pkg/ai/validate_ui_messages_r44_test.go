package ai

import (
	"context"
	"testing"
)

// TS dynamic: z.literal(false).optional() rejects a present null (zod 4.4,
// checked with node); an absent key and false are accepted.
func TestValidateUIMessages_DynamicToolPartRejectsNullDynamic(t *testing.T) {
	for _, tc := range []struct {
		dynamic string
		ok      bool
	}{
		{``, true},
		{`,"dynamic":false`, true},
		{`,"dynamic":null`, false},
		{`,"dynamic":true`, false},
	} {
		raw := `[{"id":"m1","role":"assistant","parts":[{"type":"dynamic-tool","toolName":"t","toolCallId":"c1","state":"input-available","input":{}` + tc.dynamic + `}]}]`
		res := SafeValidateUIMessages(context.Background(), ValidateUIMessagesOptions{Messages: raw})
		if res.Success != tc.ok {
			t.Fatalf("dynamic%q: success = %v (%v), want %v", tc.dynamic, res.Success, res.Error, tc.ok)
		}
	}
}
