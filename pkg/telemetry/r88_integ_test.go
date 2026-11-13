package telemetry

import (
	"encoding/json"
	"strings"
	"testing"

	openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
)

// cr-r88-provb-to-carry (TS 7bb309d132): the typed openai.code_interpreter
// container network policy (provb r88) reaches ProviderArgs; its domain
// secret values are redacted in the telemetry tool definition, and
// ProviderArgs keeps the real values for the request.
func TestR88integCodeInterpreterDomainSecretsRedacted(t *testing.T) {
	tool := openaitool.CodeInterpreter(openaitool.CodeInterpreterConfig{Container: openaitool.CodeInterpreterContainer{
		NetworkPolicy: &openaitool.CodeInterpreterNetworkPolicy{
			Type: "allowlist", AllowedDomains: []string{"a.com"},
			DomainSecrets: []openaitool.CodeInterpreterDomainSecret{{Domain: "a.com", Name: "K", Value: "r88integ-secret"}},
		},
	}})
	b, err := json.Marshal(languageModelToolDefinition(tool))
	if err != nil {
		t.Fatal(err)
	}
	def := string(b)
	if strings.Contains(def, "r88integ-secret") || !strings.Contains(def, `"value":"[redacted]"`) || !strings.Contains(def, `"name":"K"`) {
		t.Fatalf("definition = %s", def)
	}
	raw, _ := json.Marshal(tool.ProviderArgs)
	if !strings.Contains(string(raw), "r88integ-secret") {
		t.Fatalf("ProviderArgs lost the secret: %s", raw)
	}
}
