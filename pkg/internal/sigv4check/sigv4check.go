// Package sigv4check recomputes an AWS Signature Version 4 from a received
// request, the way the AWS front door does. It exists for provider tests that
// want to know whether a real service would accept the signature.
package sigv4check

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

// Verify recomputes the signature the way the AWS front door does, from
// the request exactly as it arrived (raw path, signed headers, body), and
// returns a non-empty reason when it does not match.
func Verify(r *http.Request, body []byte, secret, region, service string) string {
	auth := r.Header.Get("Authorization")
	m := regexp.MustCompile(`^AWS4-HMAC-SHA256 Credential=([^/]+)/(\d{8})/([^/]+)/([^/]+)/aws4_request, SignedHeaders=([^,]+), Signature=([0-9a-f]+)$`).FindStringSubmatch(auth)
	if m == nil {
		return "malformed Authorization: " + auth
	}
	date, gotRegion, gotService, signedHeaders, sig := m[2], m[3], m[4], m[5], m[6]
	if gotRegion != region || gotService != service {
		return fmt.Sprintf("scope region/service = %s/%s, want %s/%s", gotRegion, gotService, region, service)
	}
	var canonHeaders strings.Builder
	names := strings.Split(signedHeaders, ";")
	if !sort.StringsAreSorted(names) {
		return "SignedHeaders not sorted: " + signedHeaders
	}
	for _, n := range names {
		v := r.Header.Get(n)
		if n == "host" {
			v = r.Host
		}
		canonHeaders.WriteString(n + ":" + strings.Join(strings.Fields(v), " ") + "\n")
	}
	// Non-S3 services: the canonical path is the request path URI-encoded once
	// more ('%' of an existing escape becomes %25).
	rawPath := r.URL.EscapedPath()
	var canonPath strings.Builder
	for i := 0; i < len(rawPath); i++ {
		c := rawPath[i]
		if c == '/' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == '~' {
			canonPath.WriteByte(c)
		} else {
			fmt.Fprintf(&canonPath, "%%%02X", c)
		}
	}
	bodyHash := sha256.Sum256(body)
	canonical := strings.Join([]string{r.Method, canonPath.String(), r.URL.RawQuery, canonHeaders.String(), signedHeaders, hex.EncodeToString(bodyHash[:])}, "\n")
	ch := sha256.Sum256([]byte(canonical))
	amzDate := r.Header.Get("X-Amz-Date")
	if !strings.HasPrefix(amzDate, date) {
		return "x-amz-date does not match credential date"
	}
	sts := strings.Join([]string{"AWS4-HMAC-SHA256", amzDate, date + "/" + region + "/" + service + "/aws4_request", hex.EncodeToString(ch[:])}, "\n")
	h := func(k, d []byte) []byte { x := hmac.New(sha256.New, k); x.Write(d); return x.Sum(nil) }
	key := h(h(h(h([]byte("AWS4"+secret), []byte(date)), []byte(region)), []byte(service)), []byte("aws4_request"))
	if want := hex.EncodeToString(h(key, []byte(sts))); want != sig {
		return fmt.Sprintf("signature mismatch for path %q (canonical path %q)", rawPath, canonPath.String())
	}
	return ""
}
