package version

import "github.com/digitallysavvy/go-ai/pkg/providerutils"

// Version is the Go AI SDK release version. The TS SDK versions each
// provider package independently (openai's VERSION, anthropic's VERSION,
// etc. are all different); Go ships as a single module, so this one
// constant stands in for every TS package's own VERSION import below.
const Version = "0.5.0"

// UserAgent returns the user-agent suffix used by the pkg/ai operation
// wrappers (GenerateText, StreamText, Embed, Transcribe, Batch, GenerateVideo,
// GenerateSpeech, Evaluate, ...), mirroring the TS `ai` package's own
// `ai/${VERSION}` tag (see e.g. packages/ai/src/generate-text/generate-text.ts).
// It intentionally does not include a runtime suffix: in TS that is added
// once, downstream, by provider-utils' postToApi/getFromApi; in Go the
// equivalent point is pkg/internal/http.Client, which appends
// providerutils.RuntimeEnvironmentUserAgent() to every request.
func UserAgent() string {
	return "ai/" + Version
}

// SDKUserAgent returns the user-agent suffix used by SDK-wide (non
// provider-specific) call sites that TS tags with `ai-sdk/${VERSION}`
// — the URL-download helper (packages/ai/src/util/download/download.ts) and
// the MCP HTTP/SSE transports (packages/mcp/src/tool/mcp-*-transport.ts).
// Unlike UserAgent, these TS call sites also append
// getRuntimeEnvironmentUserAgent() themselves (they do not funnel through
// provider-utils' postToApi), so Go callers should pair this with
// providerutils.RuntimeEnvironmentUserAgent() at the same call site.
func SDKUserAgent() string {
	return "ai-sdk/" + Version
}

// ProviderUserAgent returns the user-agent suffix for a given provider
// package name, matching every TS provider's own
// `ai-sdk-<name>/${VERSION}` tag (e.g. `ai-sdk-openai/${VERSION}` in
// packages/openai/src/openai-provider.ts). TS versions each provider
// package independently; Go uses the single SDK Version for all of them.
//
// TS #21344 ("use standards-compliant User-Agent header") changed this
// from `ai-sdk/<name>/${VERSION}` to `ai-sdk-<name>/${VERSION}`: an RFC
// 9110 product identifier is `token ["/" version]`, which allows only one
// "/", so the old two-slash form (e.g. "ai-sdk/openai/0.5.0") was invalid
// and rejected by some servers that validate User-Agent strictly (e.g.
// Azure). The hyphenated name keeps the "ai-sdk" identity visible while
// leaving exactly one "/" before the version.
func ProviderUserAgent(provider string) string {
	return "ai-sdk-" + provider + "/" + Version
}

// WithUserAgentSuffix appends suffix to the "user-agent" header (creating it
// if absent), matching TS's `withUserAgentSuffix` semantics for the common
// single-suffix-part call shape used throughout the provider packages. See
// providerutils.WithUserAgentSuffix for the full (variadic, tested)
// implementation this delegates to.
func WithUserAgentSuffix(headers map[string]string, suffix string) map[string]string {
	return providerutils.WithUserAgentSuffix(headers, suffix)
}
