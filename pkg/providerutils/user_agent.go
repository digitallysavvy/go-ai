package providerutils

import (
	"runtime"
	"sort"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/intsafe"
)

// WithUserAgentSuffix appends suffixParts to the "user-agent" header,
// creating the header if it does not already exist. Mirrors the TS SDK's
// `withUserAgentSuffix` (packages/provider-utils/src/with-user-agent-suffix.ts):
//
//   - Header keys are normalized to lowercase. TS achieves this by round
//     tripping the input through the `Headers` API (which always lowercases
//     header names); Go has no such implicit normalization for a plain map,
//     so this function does it explicitly. Keys are visited in sorted order
//     so that two differently-cased duplicate keys (e.g. both "User-Agent"
//     and "user-agent" present in the input map — something a JS `Headers`
//     object cannot represent but a Go map can) collapse deterministically
//     instead of depending on Go's randomized map iteration order.
//   - Empty suffix parts are dropped (TS: `.filter(Boolean)`), and an empty
//     existing "user-agent" value is dropped the same way, so a suffix-only
//     call still produces a clean value with no leading space.
//   - The current "user-agent" value (if any) is kept first, then each
//     non-empty suffixPart is appended, space separated.
//
// Unlike TS's `HeadersInit`, Go's `map[string]string` cannot represent an
// "unset" entry distinct from an empty string, so there is no equivalent of
// TS normalizeHeaders' `undefined`/`null` filtering here: every key in
// headers is kept (lowercased), matching what a Go caller can actually
// express.
func WithUserAgentSuffix(headers map[string]string, suffixParts ...string) map[string]string {
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	normalized := make(map[string]string, intsafe.AddCap(len(headers), 1))
	for _, k := range keys {
		normalized[strings.ToLower(k)] = headers[k]
	}

	parts := make([]string, 0, intsafe.AddCap(len(suffixParts), 1))
	if current := normalized["user-agent"]; current != "" {
		parts = append(parts, current)
	}
	for _, p := range suffixParts {
		if p != "" {
			parts = append(parts, p)
		}
	}
	normalized["user-agent"] = strings.Join(parts, " ")

	return normalized
}

// HasUserAgent reports whether headers already has a "user-agent" key,
// case-insensitively. Providers that are implemented by wrapping another
// provider's constructor (e.g. Go's anthropicaws/minimax reusing
// pkg/providers/anthropic, or azure reusing pkg/providers/openai and
// pkg/providers/deepseek) use this to detect that the wrapping caller
// already tagged the request with its own `ai-sdk/<name>/VERSION`, so the
// reused constructor's own default tag should not also be appended.
func HasUserAgent(headers map[string]string) bool {
	for k := range headers {
		if strings.EqualFold(k, "user-agent") {
			return true
		}
	}
	return false
}

// RuntimeEnvironmentUserAgent returns the runtime-environment user-agent
// suffix, mirroring TS provider-utils' `getRuntimeEnvironmentUserAgent`.
// The TS helper branches on the detected JS runtime (browser, Deno/Bun/modern
// Node via navigator.userAgent, older Node.js, Vercel Edge), producing
// strings like "runtime/browser" or "runtime/node.js/v20.11.0". Go has
// exactly one runtime, so this always returns
// "runtime/go/<runtime.Version()>" (e.g. "runtime/go/go1.25.1").
//
// The "go/" segment is kept even though runtime.Version() already starts
// with "go" (so the result reads "go/go1.25.1", not "go1.25.1") so the
// value keeps TS's "runtime/<ecosystem>/<version>" shape — matching
// "runtime/node.js/v20.11.0" token for token — rather than collapsing to a
// two-segment "runtime/go1.25.1" that would read as ecosystem-less.
func RuntimeEnvironmentUserAgent() string {
	return "runtime/go/" + runtime.Version()
}
