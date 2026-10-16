package acp

import "testing"

func TestValidateLifecycleStateDataDeepChecksStartConfigMembers(t *testing.T) {
	h, err := CreateACP(testSettings())
	if err != nil {
		t.Fatal(err)
	}
	v := h.(*acpHarness)
	base := func(extra string) string {
		return `{"version":1,"configurationFingerprint":"f","prompt":[{"type":"text","text":"hi"}],"permissionMode":"allow-all"` + extra + `}`
	}
	ok := map[string]string{
		"tool":            `,"tools":[{"name":"t","description":"d","inputSchema":{"type":"object"},"extra":1}]`,
		"tool minimal":    `,"tools":[{"name":"t"}]`,
		"builtin":         `,"builtinTools":[{"toolName":"x","nativeName":"n","title":"t","toolUseKind":"edit","inputSchema":null}]`,
		"permission map":  `,"permissionModeMapping":{"allow-reads":null,"allow-edits":{"type":"session-mode","modeId":"m"},"allow-all":{"type":"session-config-option","configId":"c","value":true}}`,
		"model":           `,"model":"m","modelMapping":{"type":"session-model","path":"p"}`,
		"debug":           `,"debug":{"enabled":true,"level":"warn","subsystems":["a"]}`,
		"response format": `,"responseFormat":{"type":"json","schema":{"a":1},"name":"n"}`,
		"output mapping":  `,"outputSchemaMapping":{"type":"session-prompt-meta","path":["a"]}`,
		"text format":     `,"responseFormat":{"type":"text"}`,
	}
	for name, extra := range ok {
		if err := v.ValidateLifecycleStateData([]byte(`{"implementationIdentity":"i","turnStartConfig":` + base(extra) + `}`)); err != nil {
			t.Errorf("%s: valid config rejected: %v", name, err)
		}
	}
	bad := map[string]string{
		"tool not object":        `,"tools":["x"]`,
		"tool without name":      `,"tools":[{"description":"d"}]`,
		"tool numeric name":      `,"tools":[{"name":1}]`,
		"tool null description":  `,"tools":[{"name":"t","description":null}]`,
		"tool null inputSchema":  `,"tools":[{"name":"t","inputSchema":null}]`,
		"tool array inputSchema": `,"tools":[{"name":"t","inputSchema":[]}]`,
		"builtin without name":   `,"builtinTools":[{"title":"t"}]`,
		"builtin bad kind":       `,"builtinTools":[{"toolName":"x","toolUseKind":"write"}]`,
		"builtin null title":     `,"builtinTools":[{"toolName":"x","title":null}]`,
		"permission map partial": `,"permissionModeMapping":{"allow-reads":null}`,
		"permission bad target":  `,"permissionModeMapping":{"allow-reads":null,"allow-edits":null,"allow-all":{"type":"session-mode"}}`,
		"model number":           `,"model":1`,
		"model mapping bad type": `,"modelMapping":{"type":"x","path":"p"}`,
		"model mapping empty":    `,"modelMapping":{"type":"session-model","path":""}`,
		"debug bad level":        `,"debug":{"level":"loud"}`,
		"debug bad subsystems":   `,"debug":{"subsystems":[1]}`,
		"format bad type":        `,"responseFormat":{"type":"xml"}`,
		"format bad schema":      `,"responseFormat":{"type":"json","schema":[]}`,
		"output mapping empty":   `,"outputSchemaMapping":{"type":"session-prompt-meta","path":[]}`,
		"output mapping bad":     `,"outputSchemaMapping":{"type":"session-prompt-meta","path":[""]}`,
	}
	for name, extra := range bad {
		if err := v.ValidateLifecycleStateData([]byte(`{"implementationIdentity":"i","turnStartConfig":` + base(extra) + `}`)); err == nil {
			t.Errorf("%s: invalid turnStartConfig accepted", name)
		}
	}
	// coldSession shares the tool checks.
	if err := v.ValidateLifecycleStateData([]byte(`{"implementationIdentity":"i","coldSession":{"version":1,"configurationFingerprint":"f","permissionMode":"allow-all","tools":[{"name":3}]}}`)); err == nil {
		t.Error("coldSession with a malformed tool accepted")
	}
	// coldSession omits model/debug: those keys are not checked.
	if err := v.ValidateLifecycleStateData([]byte(`{"implementationIdentity":"i","coldSession":{"version":1,"configurationFingerprint":"f","permissionMode":"allow-all","model":1,"debug":5}}`)); err != nil {
		t.Errorf("coldSession must ignore omitted keys: %v", err)
	}
}
