package harness

import "testing"

// harnessV1CallWarningSchema is a union of plain zod objects: the member that
// matches strips the keys of the other members whatever their values (node,
// zod 4.6.7). Go decoded every field, so a stray key failed the stream-start
// frame or leaked into the warning.
func TestR99carryStreamStartWarningKeepsVariantKeys(t *testing.T) {
	for _, tc := range []struct {
		warning string
		want    CallWarning
	}{
		{`{"type":"other","message":"m","details":5}`, CallWarning{Type: CallWarningOther, Message: "m"}},
		{`{"type":"other","message":"m","details":"d","setting":"s"}`, CallWarning{Type: CallWarningOther, Message: "m"}},
		{`{"type":"unsupported-setting","setting":"s","tool":5,"message":"x"}`, CallWarning{Type: CallWarningUnsupportedSetting, Setting: "s"}},
		{`{"type":"unsupported-tool","tool":"t","setting":"s","details":"d"}`, CallWarning{Type: CallWarningUnsupportedTool, Tool: "t", Details: "d"}},
	} {
		part, err := DecodeStreamPart([]byte(`{"type":"stream-start","warnings":[` + tc.warning + `]}`))
		if err != nil {
			t.Fatalf("%s: %v", tc.warning, err)
		}
		got := part.(*StreamStartPart).Warnings
		if len(got) != 1 || got[0] != tc.want {
			t.Fatalf("%s: warnings = %+v, want %+v", tc.warning, got, tc.want)
		}
	}
	// A wrongly typed key of the matching member still fails, as in zod.
	if _, err := DecodeStreamPart([]byte(`{"type":"stream-start","warnings":[{"type":"unsupported-setting","setting":"s","details":5}]}`)); err == nil {
		t.Fatal("unsupported-setting with a numeric details: want an error")
	}
}
