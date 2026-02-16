package klingai

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// generateJWTToken generates a JWT token for KlingAI API authentication
// Uses HS256 (HMAC-SHA256) signing matching the TypeScript implementation
// The token is valid for 30 minutes
func generateJWTToken(accessKey, secretKey string) (string, error) {
	if accessKey == "" {
		return "", fmt.Errorf("access key is required")
	}
	if secretKey == "" {
		return "", fmt.Errorf("secret key is required")
	}

	now := time.Now().Unix()

	// Create JWT header
	header := map[string]string{
		"alg": "HS256",
		"typ": "JWT",
	}

	// Create JWT payload with claims
	payload := map[string]interface{}{
		"iss": accessKey,
		"exp": now + 1800, // Valid for 30 minutes
		"nbf": now - 5,    // Valid 5 seconds before current time
	}

	// Encode header and payload to base64url
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", fmt.Errorf("failed to marshal header: %w", err)
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payload: %w", err)
	}

	headerB64 := base64urlEncode(headerJSON)
	payloadB64 := base64urlEncode(payloadJSON)

	// Create signing input
	signingInput := headerB64 + "." + payloadB64

	// Sign with HMAC-SHA256
	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write([]byte(signingInput))
	signature := mac.Sum(nil)

	// Encode signature to base64url
	signatureB64 := base64urlEncode(signature)

	// Return complete JWT
	return signingInput + "." + signatureB64, nil
}

// base64urlEncode encodes data to base64url format (URL-safe base64 without padding)
func base64urlEncode(data []byte) string {
	encoded := base64.StdEncoding.EncodeToString(data)
	// Convert to base64url: replace + with -, / with _, and remove =
	encoded = strings.ReplaceAll(encoded, "+", "-")
	encoded = strings.ReplaceAll(encoded, "/", "_")
	encoded = strings.TrimRight(encoded, "=")
	return encoded
}
