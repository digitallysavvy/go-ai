// Package posixpath mirrors the subset of Node's `path.posix` and
// `String.prototype.localeCompare` semantics the TypeScript harness relies on.
//
// Go's path package cleans trailing slashes, while Node's posix.normalize and
// posix.join preserve them. Harness identities hash these normalized strings,
// so the port must reproduce Node exactly to keep hashes equal to TS.
package posixpath

import (
	"sort"
	"strings"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// IsAbs mirrors path.posix.isAbsolute.
func IsAbs(p string) bool {
	return strings.HasPrefix(p, "/")
}

// Normalize mirrors path.posix.normalize (including trailing-slash retention).
func Normalize(p string) string {
	if p == "" {
		return "."
	}
	isAbsolute := strings.HasPrefix(p, "/")
	trailingSeparator := strings.HasSuffix(p, "/")

	normalized := normalizeString(p, !isAbsolute)
	if normalized == "" {
		if isAbsolute {
			return "/"
		}
		if trailingSeparator {
			return "./"
		}
		return "."
	}
	if trailingSeparator {
		normalized += "/"
	}
	if isAbsolute {
		return "/" + normalized
	}
	return normalized
}

// normalizeString resolves "." and ".." segments like Node's internal helper.
func normalizeString(p string, allowAboveRoot bool) string {
	segments := make([]string, 0)
	for _, segment := range strings.Split(p, "/") {
		switch segment {
		case "", ".":
			continue
		case "..":
			if len(segments) > 0 && segments[len(segments)-1] != ".." {
				segments = segments[:len(segments)-1]
			} else if allowAboveRoot {
				segments = append(segments, "..")
			}
		default:
			segments = append(segments, segment)
		}
	}
	return strings.Join(segments, "/")
}

// Join mirrors path.posix.join.
func Join(parts ...string) string {
	nonEmpty := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			nonEmpty = append(nonEmpty, part)
		}
	}
	if len(nonEmpty) == 0 {
		return "."
	}
	return Normalize(strings.Join(nonEmpty, "/"))
}

// Resolve mirrors path.posix.resolve(base, p) for an absolute base: absolute p
// is normalized as-is, relative p is joined onto base. Trailing slashes are
// removed like Node's resolve.
func Resolve(base, p string) string {
	joined := p
	if !IsAbs(p) {
		joined = base + "/" + p
	}
	resolved := "/" + normalizeString(joined, false)
	return resolved
}

// Dirname mirrors path.posix.dirname.
func Dirname(p string) string {
	if p == "" {
		return "."
	}
	trimmed := p
	for len(trimmed) > 1 && strings.HasSuffix(trimmed, "/") {
		trimmed = trimmed[:len(trimmed)-1]
	}
	idx := strings.LastIndex(trimmed, "/")
	switch {
	case idx < 0:
		return "."
	case idx == 0:
		return "/"
	default:
		return trimmed[:idx]
	}
}

// Basename mirrors path.posix.basename (without suffix stripping).
func Basename(p string) string {
	trimmed := p
	for len(trimmed) > 1 && strings.HasSuffix(trimmed, "/") {
		trimmed = trimmed[:len(trimmed)-1]
	}
	if trimmed == "/" {
		return ""
	}
	return trimmed[strings.LastIndex(trimmed, "/")+1:]
}

// IsWin32Abs mirrors path.win32.isAbsolute.
func IsWin32Abs(p string) bool {
	if p == "" {
		return false
	}
	if p[0] == '/' || p[0] == '\\' {
		return true
	}
	if len(p) > 2 && isLetter(p[0]) && p[1] == ':' && (p[2] == '/' || p[2] == '\\') {
		return true
	}
	return false
}

func isLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// LocaleCompare mirrors `a.localeCompare(b)` in Node (ICU root collation),
// returning -1, 0 or 1. It exists so that orderings which feed hashes are
// byte-identical to the TypeScript implementation.
func LocaleCompare(a, b string) int {
	return collate.New(language.Und).CompareString(a, b)
}

// SortStrings sorts in place using LocaleCompare.
func SortStrings(values []string) {
	c := collate.New(language.Und)
	sort.SliceStable(values, func(i, j int) bool {
		return c.CompareString(values[i], values[j]) < 0
	})
}

// SortFunc stably sorts values by the locale-compared key.
func SortFunc[T any](values []T, key func(T) string) {
	c := collate.New(language.Und)
	sort.SliceStable(values, func(i, j int) bool {
		return c.CompareString(key(values[i]), key(values[j])) < 0
	})
}
