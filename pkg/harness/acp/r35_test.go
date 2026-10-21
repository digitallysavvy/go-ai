package acp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
)

func TestValidateEnvironmentReportsKeyDeterministically(t *testing.T) {
	env := map[string]string{"1bad": "x", "2bad": "y", "3bad": "z", "4bad": "w", "OK": "v"}
	for i := 0; i < 50; i++ {
		err := validateEnvironment(env)
		if err == nil || err.Error() != `ACP environment variable name is invalid: "1bad".` {
			t.Fatalf("err = %v", err)
		}
	}
}

func TestStartMessageOmitsToolsWhenUndefined(t *testing.T) {
	var m StartMessage
	m.Prompt = []TextContentBlock{}
	m.OmitTools = true
	b, err := bridge.MarshalInbound(m)
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		t.Fatal(err)
	}
	if _, ok := top["tools"]; ok {
		t.Fatalf("tools key present: %s", b)
	}
	m.OmitTools = false
	b, _ = bridge.MarshalInbound(m)
	if !strings.Contains(string(b), `"tools":[]`) {
		t.Fatalf("tools missing: %s", b)
	}
	m.Tools = []harness.ToolSpec{{Name: "x"}}
	m.OmitTools = false
	b, _ = bridge.MarshalInbound(m)
	if !strings.Contains(string(b), `"name":"x"`) {
		t.Fatalf("tools lost: %s", b)
	}
}
