package providerutils

import (
	"regexp"
	"strings"
)

// CompileSupportedURLPatterns compiles a raw SupportedURLs()-style map
// (regexp pattern source strings keyed by media type, or "*"/"*/*" for a
// wildcard) into a lookup table keyed by lowercased media type, dropping any
// pattern that fails to compile. Returns nil for an empty/nil input or when
// every pattern fails to compile.
//
// This is the single shared implementation of TypeScript's isUrlSupported
// matching semantics (provider-utils/src/is-url-supported.ts): both
// pkg/ai (prompt file-download planning) and pkg/providerutils/prompt
// (Google functionResponse fileData forwarding) need it, but pkg/ai cannot
// depend on pkg/providerutils/prompt without an import cycle, so the matcher
// lives here in the shared low-level package both already import.
func CompileSupportedURLPatterns(patternsByMediaType map[string][]string) map[string][]*regexp.Regexp {
	if len(patternsByMediaType) == 0 {
		return nil
	}
	compiled := make(map[string][]*regexp.Regexp, len(patternsByMediaType))
	for mediaType, patterns := range patternsByMediaType {
		key := strings.ToLower(mediaType)
		for _, pattern := range patterns {
			if re, err := regexp.Compile(pattern); err == nil {
				compiled[key] = append(compiled[key], re)
			}
		}
	}
	if len(compiled) == 0 {
		return nil
	}
	return compiled
}

// MatchesSupportedURL reports whether url is supported for mediaType per the
// compiled pattern table (see CompileSupportedURLPatterns), matching
// TypeScript's isUrlSupported (ai@7.0.118 commit bc49f786f0, which tightened
// the non-wildcard case): a pattern registered under the exact media type
// (equality, not a prefix match, when the key carries no `*`), its `type/*`
// prefix, or the wildcard "*"/"*/*" must match the (lowercased) URL. Before
// that commit, TS (and this port) used a plain prefix match even for
// non-wildcard keys, so e.g. supportedUrls["image/png"] would incorrectly
// also match a URL declared with mediaType "image/png-not-supported".
func MatchesSupportedURL(compiled map[string][]*regexp.Regexp, mediaType, url string) bool {
	if len(compiled) == 0 {
		return false
	}
	mediaType = strings.ToLower(mediaType)
	url = strings.ToLower(url)
	topLevelOnly := !strings.Contains(mediaType, "/")
	for key, regexes := range compiled {
		prefix := strings.ReplaceAll(key, "*", "")
		if key == "*" || key == "*/*" {
			prefix = ""
		}
		if prefix != "" {
			switch {
			case topLevelOnly:
				if mediaType+"/" != prefix {
					continue
				}
			case strings.HasSuffix(prefix, "/"):
				// Wildcard key (e.g. "image/*"): prefix match.
				if !strings.HasPrefix(mediaType, prefix) {
					continue
				}
			default:
				// Exact key (e.g. "image/png"): equality, not a prefix match.
				if mediaType != prefix {
					continue
				}
			}
		}
		for _, re := range regexes {
			if re.MatchString(url) {
				return true
			}
		}
	}
	return false
}
