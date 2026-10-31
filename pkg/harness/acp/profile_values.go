package acp

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// gatewayValues are the AI Gateway values a provider-authentication profile
// resolves against. Mirrors TS `ACPGatewayValues`.
type gatewayValues struct {
	APIKey           string
	BaseURL          string
	ClientAppName    string
	ClientAppVersion string
}

var trailingSlashes = regexp.MustCompile(`/+$`)

// resolveProfileValue mirrors TS `resolveACPProfileValue`: it resolves the
// `$source` tokens of a provider-authentication profile against the gateway
// values.
func resolveProfileValue(value any, gw gatewayValues) any {
	switch v := value.(type) {
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = resolveProfileValue(item, gw)
		}
		return out
	case []string:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = item
		}
		return out
	case map[string]string:
		out := make(map[string]any, len(v))
		for k, item := range v {
			out[k] = item
		}
		return out
	case map[string]any:
		if source, ok := v["$source"].(string); ok {
			prefix, _ := v["prefix"].(string)
			suffix, _ := v["suffix"].(string)
			resolved := prefix + resolveProfileSource(source, gw)
			if ensure, ok := v["ensureSuffix"].(string); ok {
				normalized := trailingSlashes.ReplaceAllString(resolved, "")
				if strings.HasSuffix(normalized, ensure) {
					return normalized
				}
				return normalized + ensure
			}
			return resolved + suffix
		}
		out := make(map[string]any, len(v))
		for k, item := range v {
			out[k] = resolveProfileValue(item, gw)
		}
		return out
	}
	return value
}

func resolveProfileSource(source string, gw gatewayValues) string {
	switch source {
	case "gateway-api-key":
		return gw.APIKey
	case "gateway-base-url":
		return gw.BaseURL
	case "gateway-authorization":
		return "Bearer " + gw.APIKey
	case "client-app":
		return gw.ClientAppName + "/" + gw.ClientAppVersion
	case "client-app-name":
		return gw.ClientAppName
	case "client-app-version":
		return gw.ClientAppVersion
	}
	return ""
}

// resolveLaunchEnvironment mirrors TS `resolveACPLaunchEnvironment` for an
// AI Gateway profile: the profile resolves to an object whose values become
// environment variables (non-strings are JSON-encoded).
func resolveLaunchEnvironment(profile map[string]any, gw gatewayValues) (map[string]string, error) {
	resolved, ok := resolveProfileValue(profile, gw).(map[string]any)
	if !ok {
		return nil, errors.New("ACP profile data must resolve to an object.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	out := make(map[string]string, len(resolved))
	for k, v := range resolved {
		if s, ok := v.(string); ok {
			out[k] = s
			continue
		}
		data, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		out[k] = string(data)
	}
	return out, nil
}

// resolveProviderEnvironment mirrors TS `resolveProviderEnvironment`: the
// concrete launch environment for an AI Gateway provider profile, or an empty
// map for any other authentication type.
func resolveProviderEnvironment(resolved resolvedProviderAuthentication, clientApp ClientApp) (map[string]string, error) {
	if resolved.Type != "ai-gateway" {
		return map[string]string{}, nil
	}
	apiKey, ok := resolved.Env["AI_SDK_ACP_GATEWAY_API_KEY"]
	if !ok {
		return nil, fmt.Errorf("ACP resolved environment value %s is unavailable.", "AI_SDK_ACP_GATEWAY_API_KEY") //nolint:staticcheck // matches TS SDK's exact error text
	}
	baseURL, ok := resolved.Env["AI_SDK_ACP_GATEWAY_BASE_URL"]
	if !ok {
		return nil, fmt.Errorf("ACP resolved environment value %s is unavailable.", "AI_SDK_ACP_GATEWAY_BASE_URL") //nolint:staticcheck // matches TS SDK's exact error text
	}
	return resolveLaunchEnvironment(resolved.GatewayEnv, gatewayValues{
		APIKey: apiKey, BaseURL: baseURL, ClientAppName: clientApp.Name, ClientAppVersion: clientApp.Version,
	})
}

// providerCredentialEnvironmentVariables mirrors TS
// `resolveACPProviderCredentialEnvironmentVariables`: the profile keys whose
// resolved value carries the gateway credential.
func providerCredentialEnvironmentVariables(resolved resolvedProviderAuthentication) []string {
	if resolved.Type != "ai-gateway" {
		return nil
	}
	var names []string
	for name, value := range resolved.GatewayEnv {
		if containsProviderCredential(value) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func containsProviderCredential(value any) bool {
	switch v := value.(type) {
	case []any:
		for _, item := range v {
			if containsProviderCredential(item) {
				return true
			}
		}
	case map[string]any:
		if source, ok := v["$source"]; ok {
			return source == "gateway-api-key" || source == "gateway-authorization"
		}
		for _, item := range v {
			if containsProviderCredential(item) {
				return true
			}
		}
	}
	return false
}
