package githubcopilot

import (
	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
)

// CredentialBrokering builds the request transformations mapping Copilot's
// sandbox-side GitHub / provider credentials back to the host values.
// Mirrors the `credentialBrokering` closure in `createGitHubCopilot()`.
func CredentialBrokering(env, sandboxEnv, headers map[string]string) ([]harness.RequestTransformation, error) {
	var transformations []harness.RequestTransformation

	githubHost := normalizeGitHubHost(firstNonEmpty(env["COPILOT_GH_HOST"], env["GH_HOST"], "github.com"))
	githubHosts := []string{githubHost, "*." + githubHost}
	if githubHost == "github.com" {
		githubHosts = append(githubHosts, "githubcopilot.com", "*.githubcopilot.com")
	}

	for _, host := range githubHosts {
		for _, name := range []string{"COPILOT_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
			hostValue, sandboxValue := env[name], sandboxEnv[name]
			if hostValue == "" || sandboxValue == "" {
				continue
			}
			for _, scheme := range []string{"Bearer", "token"} {
				transformations = append(transformations, harness.RequestTransformation{
					Match: harness.RequestTransformationMatch{
						Host: host,
						Headers: []harness.KeyValueMatcher{{
							Key:   &harness.StringMatcher{Exact: "Authorization"},
							Value: &harness.StringMatcher{Exact: scheme + " " + sandboxValue},
						}},
					},
					Transform: harness.RequestTransformationTransform{
						Headers: map[string]string{"Authorization": scheme + " " + hostValue},
					},
				})
			}
		}
	}

	providerCredential := env["COPILOT_PROVIDER_API_KEY"]
	sandboxProviderCredential := sandboxEnv["COPILOT_PROVIDER_API_KEY"]
	providerBaseURL := env["COPILOT_PROVIDER_BASE_URL"]
	switch {
	case providerCredential != "" && sandboxProviderCredential != "" && providerBaseURL != "":
		transformHeaders := map[string]string{}
		for k, v := range headers {
			transformHeaders[k] = v
		}
		transformHeaders["Authorization"] = "Bearer " + providerCredential
		tr, err := harnessutil.CreateCredentialRequestTransformation(harnessutil.CreateCredentialRequestTransformationOptions{
			MatchURL:         providerBaseURL,
			MatchHeaders:     map[string]string{"Authorization": "Bearer " + sandboxProviderCredential},
			TransformHeaders: transformHeaders,
		})
		if err != nil {
			return nil, err
		}
		transformations = append(transformations, tr)
	case headers != nil:
		copilotHosts := []string{githubHost, "*." + githubHost}
		if githubHost == "github.com" {
			copilotHosts = []string{"githubcopilot.com", "*.githubcopilot.com"}
		}
		for _, host := range copilotHosts {
			transformations = append(transformations, harness.RequestTransformation{
				Match:     harness.RequestTransformationMatch{Host: host},
				Transform: harness.RequestTransformationTransform{Headers: headers},
			})
		}
	}

	return transformations, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
