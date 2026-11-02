package ai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Owner ruling 2026-11-02 (pending decision 1, option b): TS uiMessagesSchema
// under zod 4.4.3 rejects a data part without `data` (`data: z.unknown()` is
// nonoptional: "Invalid input: expected nonoptional, received undefined");
// any present value, null included, passes.
func TestR78integValidateUIMessagesDataPartRequiresData(t *testing.T) {
	for _, tc := range []struct {
		name, part string
		wantErr    string
	}{
		{"missing data", `{"type":"data-weather","id":"a"}`, "[0].parts[0]: Invalid input: expected nonoptional, received undefined for data"},
		{"missing data and id", `{"type":"data-weather"}`, "[0].parts[0]: Invalid input: expected nonoptional, received undefined for data"},
		{"bad id first", `{"type":"data-weather","id":1}`, "[0].parts[0]: Invalid input: expected string for id"},
		{"null data", `{"type":"data-weather","data":null}`, ""},
		{"object data", `{"type":"data-weather","id":"a","data":{"city":"Tokyo"}}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
				Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[` + tc.part + `]}]`),
			})
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.HasSuffix(err.Error(), "].\nError message: "+tc.wantErr) || !strings.HasPrefix(err.Error(), "Type validation failed: Value: [") {
				t.Fatalf("error = %q, want \"Type validation failed: Value: ….\\nError message: %s\"", err, tc.wantErr)
			}
		})
	}
}
