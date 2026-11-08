package responses

import (
	"encoding/json"
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
)

// TS 7a905226b6 (mcp.ts refines; texts from zod v4 run on the TS schema):
// a tunnel alone is valid and sent as tunnel_id; combined with serverUrl or
// connectorId it fails the second refine.
func TestR88provbMCPTunnel(t *testing.T) {
	defs, err := PrepareToolsWithError([]types.Tool{openaitool.MCP(openaitool.MCPConfig{ServerLabel: "x", TunnelID: "tun_1"})})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(defs)
	if want := `[{"require_approval":"never","server_label":"x","tunnel_id":"tun_1","type":"mcp"}]`; string(raw) != want {
		t.Fatalf("defs = %s, want %s", raw, want)
	}
	want := "Type validation failed: Value: {\"serverLabel\":\"x\",\"serverUrl\":\"u\",\"tunnelId\":\"t\"}.\nError message: [\n  {\n    \"code\": \"custom\",\n    \"path\": [],\n    \"message\": \"tunnelId cannot be combined with serverUrl or connectorId.\"\n  }\n]"
	_, err = PrepareToolsWithError([]types.Tool{openaitool.MCP(openaitool.MCPConfig{ServerLabel: "x", TunnelID: "t", ServerURL: "u"})})
	var tv *providererrors.TypeValidationError
	if !errors.As(err, &tv) || err.Error() != want {
		t.Fatalf("err =\n%v\nwant\n%s", err, want)
	}
}

// TS 7bb309d132: typed code interpreter container limits.
func TestR88provbCodeInterpreterContainerLimits(t *testing.T) {
	for _, tc := range []struct {
		container openaitool.CodeInterpreterContainer
		want      string
	}{
		{openaitool.CodeInterpreterContainer{MemoryLimit: "4g", NetworkPolicy: &openaitool.CodeInterpreterNetworkPolicy{Type: "disabled"}},
			`{"container":{"memory_limit":"4g","network_policy":{"type":"disabled"},"type":"auto"},"type":"code_interpreter"}`},
		{openaitool.CodeInterpreterContainer{FileIDs: []string{"f"}, NetworkPolicy: &openaitool.CodeInterpreterNetworkPolicy{Type: "allowlist", AllowedDomains: []string{"a.com"},
			DomainSecrets: []openaitool.CodeInterpreterDomainSecret{{Domain: "a.com", Name: "K", Value: "v"}}}},
			`{"container":{"file_ids":["f"],"network_policy":{"allowed_domains":["a.com"],"domain_secrets":[{"domain":"a.com","name":"K","value":"v"}],"type":"allowlist"},"type":"auto"},"type":"code_interpreter"}`},
		{openaitool.CodeInterpreterContainer{NetworkPolicy: &openaitool.CodeInterpreterNetworkPolicy{Type: "allowlist"}},
			`{"container":{"network_policy":{"allowed_domains":[],"type":"allowlist"},"type":"auto"},"type":"code_interpreter"}`},
	} {
		defs, err := PrepareToolsWithError([]types.Tool{openaitool.CodeInterpreter(openaitool.CodeInterpreterConfig{Container: tc.container})})
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(defs[0])
		if string(raw) != tc.want {
			t.Errorf("def = %s, want %s", raw, tc.want)
		}
	}
	_, err := PrepareToolsWithError([]types.Tool{openaitool.CodeInterpreter(openaitool.CodeInterpreterConfig{Container: openaitool.CodeInterpreterContainer{MemoryLimit: "2g"}})})
	if err == nil {
		t.Fatal("memoryLimit 2g: want a validation error")
	}
}
