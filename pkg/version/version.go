package version

import "strings"

// Version is the Go AI SDK release version.
const Version = "0.5.0"

// UserAgent returns the Go AI SDK user-agent value used by operation wrappers.
func UserAgent() string {
	return "go-ai/" + Version
}

// ProviderUserAgent returns the Go AI SDK provider package user-agent value.
func ProviderUserAgent(provider string) string {
	return "go-ai/" + provider + "/" + Version
}

// WithUserAgentSuffix normalizes headers and appends suffix to user-agent.
func WithUserAgentSuffix(headers map[string]string, suffix string) map[string]string {
	out := make(map[string]string, len(headers)+1)
	for k, v := range headers {
		out[strings.ToLower(k)] = v
	}
	currentUserAgent := out["user-agent"]
	if currentUserAgent == "" {
		out["user-agent"] = suffix
	} else {
		out["user-agent"] = currentUserAgent + " " + suffix
	}
	return out
}
