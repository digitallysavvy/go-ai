package fileutil

import (
	"net/url"
	"strings"
)

var urlExtensionToMediaType = map[string]string{
	"jpg":  "image/jpeg",
	"jpeg": "image/jpeg",
	"png":  "image/png",
	"gif":  "image/gif",
	"webp": "image/webp",
	"svg":  "image/svg+xml",
	"avif": "image/avif",
	"heic": "image/heic",
	"bmp":  "image/bmp",
	"tiff": "image/tiff",
	"tif":  "image/tiff",
	"pdf":  "application/pdf",
	"mp4":  "video/mp4",
	"webm": "video/webm",
	"mp3":  "audio/mpeg",
	"wav":  "audio/wav",
	"ogg":  "audio/ogg",
}

// InferMediaTypeFromURL infers media type from URL pathname extension.
// Returns fallback when the URL cannot be parsed, has no extension, or the
// extension is unknown.
func InferMediaTypeFromURL(rawURL, fallback string) string {
	if fallback == "" {
		fallback = "application/octet-stream"
	}
	pathname, ok := urlPathname(rawURL)
	if !ok {
		return fallback
	}
	lastDot := strings.LastIndex(pathname, ".")
	if lastDot < 0 {
		return fallback
	}
	ext := strings.ToLower(pathname[lastDot+1:])
	if ext == "" {
		return fallback
	}
	if mediaType, ok := urlExtensionToMediaType[ext]; ok {
		return mediaType
	}
	return fallback
}

func urlPathname(rawURL string) (string, bool) {
	parsed, err := url.Parse(rawURL)
	if err == nil && parsed.Scheme != "" {
		if parsed.Host == "" && isSpecialScheme(parsed.Scheme) && parsed.Opaque != "" {
			return specialSchemeOpaquePathname(parsed.Scheme, strings.ReplaceAll(parsed.Opaque, "\\", "/"))
		}
		if parsed.Host == "" && parsed.Opaque != "" {
			return trimPathQueryFragment(parsed.Opaque), true
		}
		return parsed.EscapedPath(), true
	}

	colon := strings.Index(rawURL, ":")
	if colon <= 0 {
		return "", false
	}
	scheme := strings.ToLower(rawURL[:colon])
	rest := rawURL[colon+1:]
	if isSpecialScheme(scheme) {
		rest = strings.ReplaceAll(rest, "\\", "/")
	}
	if strings.HasPrefix(rest, "//") {
		afterAuthority := rest[2:]
		pathStart := strings.IndexAny(afterAuthority, "/?#")
		if pathStart < 0 || afterAuthority[pathStart] != '/' {
			if !validURLAuthority(scheme, afterAuthority[:authorityEnd(afterAuthority)]) {
				return "", false
			}
			return "/", true
		}
		if !validURLAuthority(scheme, afterAuthority[:pathStart]) {
			return "", false
		}
		return trimPathQueryFragment(afterAuthority[pathStart:]), true
	}
	if isSpecialScheme(scheme) {
		return specialSchemeOpaquePathname(scheme, rest)
	}
	return trimPathQueryFragment(rest), true
}

func isSpecialScheme(scheme string) bool {
	switch strings.ToLower(scheme) {
	case "ftp", "file", "http", "https", "ws", "wss":
		return true
	default:
		return false
	}
}

func specialSchemeOpaquePathname(scheme, rest string) (string, bool) {
	rest = strings.TrimLeft(rest, "/")
	pathStart := strings.Index(rest, "/")
	if pathStart < 0 {
		if !validURLAuthority(scheme, rest[:authorityEnd(rest)]) {
			return "", false
		}
		return "/", true
	}
	if !validURLAuthority(scheme, rest[:pathStart]) {
		return "", false
	}
	return trimPathQueryFragment(rest[pathStart:]), true
}

func trimPathQueryFragment(path string) string {
	if path == "" {
		return ""
	}
	if end := strings.IndexAny(path, "?#"); end >= 0 {
		return path[:end]
	}
	return path
}

func authorityEnd(value string) int {
	if end := strings.IndexAny(value, "?#"); end >= 0 {
		return end
	}
	return len(value)
}

func validURLAuthority(scheme, authority string) bool {
	if authority == "" {
		return false
	}
	hostport := authority
	if at := strings.LastIndex(hostport, "@"); at >= 0 {
		hostport = hostport[at+1:]
	}
	if hostport == "" {
		return false
	}
	_, err := url.Parse(strings.ToLower(scheme) + "://" + hostport + "/")
	return err == nil
}
