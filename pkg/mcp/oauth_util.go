package mcp

import (
	"net/url"
	"strings"
)

// resourceURLFromServerURL converts an MCP server URL to an RFC 8707 resource
// URI by removing the fragment (RFC 8707 §2: resource URIs "MUST NOT include
// a fragment component"). Everything else (scheme, host, port, path, query)
// is unchanged, matching TS resourceUrlFromServerUrl (util/oauth-util.ts).
func resourceURLFromServerURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	parsed.Fragment = ""
	parsed.RawFragment = ""
	return parsed, nil
}

// resourceURLStripSlash serializes a resource URL, stripping the trailing
// slash that a pathless URL otherwise carries. Per the MCP spec,
// implementations SHOULD omit the trailing slash for interoperability.
// Matches TS resourceUrlStripSlash (util/oauth-util.ts, hash 1e89d62).
func resourceURLStripSlash(resource *url.URL) string {
	href := resource.String()
	if resource.Path == "/" && strings.HasSuffix(href, "/") {
		return href[:len(href)-1]
	}
	return href
}

// checkOAuthResourceAllowed reports whether requestedResource matches
// configuredResource: same origin (scheme+host+port), and requestedResource's
// path is at or below configuredResource's path. Matches TS
// checkResourceAllowed (util/oauth-util.ts).
func checkOAuthResourceAllowed(requestedResource, configuredResource string) (bool, error) {
	requested, err := url.Parse(requestedResource)
	if err != nil {
		return false, err
	}
	configured, err := url.Parse(configuredResource)
	if err != nil {
		return false, err
	}
	if origin(requested) != origin(configured) {
		return false, nil
	}
	requestedPath := normalizedURLPathname(requested)
	configuredPath := normalizedURLPathname(configured)
	if len(requestedPath) < len(configuredPath) {
		return false, nil
	}
	if !strings.HasSuffix(requestedPath, "/") {
		requestedPath += "/"
	}
	if !strings.HasSuffix(configuredPath, "/") {
		configuredPath += "/"
	}
	return strings.HasPrefix(requestedPath, configuredPath), nil
}

// normalizedURLPathname mirrors the WHATWG URL `pathname` the TS check
// compares: the still-percent-encoded path with "." / ".." segments
// (including %2e forms) resolved, and "/" for an empty path. Comparing the
// decoded, un-normalized Go Path would let "/api/../admin" or "/api%2f.."
// appear to sit below "/api".
func normalizedURLPathname(u *url.URL) string {
	p := u.EscapedPath()
	if p == "" {
		return "/"
	}
	segments := strings.Split(p, "/")
	out := make([]string, 0, len(segments))
	for i, seg := range segments {
		switch strings.ToLower(seg) {
		case ".", "%2e":
			if i == len(segments)-1 {
				out = append(out, "")
			}
		case "..", ".%2e", "%2e.", "%2e%2e":
			if len(out) > 1 {
				out = out[:len(out)-1]
			}
			if i == len(segments)-1 {
				out = append(out, "")
			}
		default:
			out = append(out, seg)
		}
	}
	res := strings.Join(out, "/")
	if !strings.HasPrefix(res, "/") {
		res = "/" + res
	}
	return res
}
