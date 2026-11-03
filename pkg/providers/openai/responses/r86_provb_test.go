package responses

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR86provbShellEnvironmentGoTypedArgs: TS-shaped openai.shell args built
// with Go-typed values ([]map[string]interface{} skills and domain secrets,
// a types.ProviderReference) map like their JSON form; the skills and
// secrets were dropped and the reference failed to resolve.
func TestR86provbShellEnvironmentGoTypedArgs(t *testing.T) {
	typed := map[string]interface{}{"environment": map[string]interface{}{
		"type": "containerAuto",
		"networkPolicy": map[string]interface{}{
			"type":           "allowlist",
			"allowedDomains": []string{"example.com"},
			"domainSecrets":  []map[string]interface{}{{"domain": "example.com", "name": "TOKEN", "value": "v"}},
		},
		"skills": []map[string]interface{}{
			{"type": "skillReference", "providerReference": types.ProviderReference{"openai": "skill_1"}},
			{"type": "inline", "name": "n", "description": "d", "source": map[string]interface{}{"type": "base64", "mediaType": "application/zip", "data": "AA=="}},
		},
	}}
	var jsonForm map[string]interface{}
	b, _ := json.Marshal(typed)
	if err := json.Unmarshal(b, &jsonForm); err != nil {
		t.Fatal(err)
	}
	encode := func(args map[string]interface{}) string {
		env, err := shellEnvironmentFromArgs(args)
		if err != nil {
			t.Fatal(err)
		}
		out, _ := json.Marshal(env)
		return string(out)
	}
	got, want := encode(typed), encode(jsonForm)
	if got != want {
		t.Fatalf("typed = %s\njson  = %s", got, want)
	}
	const wire = `{"type":"container_auto","network_policy":{"type":"allowlist","allowed_domains":["example.com"],"domain_secrets":[{"domain":"example.com","name":"TOKEN","value":"v"}]},"skills":[{"type":"skill_reference","skill_id":"skill_1"},{"type":"inline","name":"n","description":"d","source":{"type":"base64","media_type":"application/zip","data":"AA=="}}]}`
	if got != wire {
		t.Fatalf("env = %s\nwant  %s", got, wire)
	}
}
