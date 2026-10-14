package mcp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// mcpEventNow is the clock used for webhook timestamp tolerance checks;
// overridden in tests.
var mcpEventNow = time.Now

var (
	mcpEventSecretBase64 = regexp.MustCompile(`^[A-Za-z0-9+/]+={0,2}$`)
	mcpEventDigits       = regexp.MustCompile(`^\d+$`)
)

// createMCPEventSecret returns a new `whsec_` signing secret with 32 random bytes.
func createMCPEventSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "whsec_" + base64.StdEncoding.EncodeToString(buf), nil
}

// decodeMCPEventSecret validates and decodes a `whsec_` signing secret.
func decodeMCPEventSecret(secret string) ([]byte, error) {
	if !strings.HasPrefix(secret, "whsec_") || !mcpEventSecretBase64.MatchString(secret[6:]) {
		return nil, NewMCPClientError(0, "Invalid MCP event signing secret", nil)
	}
	bytes, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(secret[6:], "="))
	if err != nil {
		return nil, NewMCPClientError(0, "Invalid MCP event signing secret", nil)
	}
	if len(bytes) < 24 || len(bytes) > 64 {
		return nil, NewMCPClientError(0, "MCP event signing secrets must contain 24–64 random bytes", nil)
	}
	return bytes, nil
}

func decodeMCPEventSignatureValue(value string) ([]byte, bool) {
	trimmed := strings.TrimRight(value, "=")
	if b, err := base64.RawStdEncoding.DecodeString(trimmed); err == nil {
		return b, true
	}
	if b, err := base64.RawURLEncoding.DecodeString(trimmed); err == nil {
		return b, true
	}
	return nil, false
}

// verifyMCPEventSignature implements Standard Webhooks v1: it authenticates
// the exact body bytes with HMAC-SHA256 over `id.timestamp.body`, within a
// five minute timestamp tolerance. Multiple space-separated signatures
// support secret rotation; a malformed signature never hides a valid one.
func verifyMCPEventSignature(headers http.Header, body []byte, secret string) (bool, error) {
	id := headers.Get("webhook-id")
	timestamp := headers.Get("webhook-timestamp")
	signatures := headers.Get("webhook-signature")
	if id == "" || timestamp == "" || signatures == "" || !mcpEventDigits.MatchString(timestamp) {
		return false, nil
	}
	ts, err := strconv.ParseFloat(timestamp, 64)
	if err != nil {
		return false, nil
	}
	nowSeconds := float64(mcpEventNow().UnixMilli()) / 1000
	if diff := nowSeconds - ts; diff > 300 || diff < -300 {
		return false, nil
	}

	key, err := decodeMCPEventSecret(secret)
	if err != nil {
		return false, err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + timestamp + ".")) //nolint:errcheck // hash.Hash writes never fail
	mac.Write(body) //nolint:errcheck // hash.Hash writes never fail
	expected := mac.Sum(nil)

	for _, signature := range strings.Split(signatures, " ") {
		parts := strings.Split(signature, ",")
		if len(parts) < 2 || parts[0] != "v1" || parts[1] == "" {
			continue
		}
		got, ok := decodeMCPEventSignatureValue(parts[1])
		if ok && hmac.Equal(got, expected) {
			return true, nil
		}
	}
	return false, nil
}
