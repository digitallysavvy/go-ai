package klingai

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// resolveKlingAIAuthToken resolves the bearer token to use for a KlingAI API
// request (TS resolveKlingAIAuthToken).
//
// KlingAI supports two authentication schemes:
//   - A single API key sent directly as a bearer token (recommended).
//   - A legacy access key / secret key pair that signs a short-lived JWT.
//
// Explicit values take precedence over environment variables, and the API
// key takes precedence over the access key / secret key pair:
//
//  1. apiKey (Config.APIKey)
//  2. accessKey + secretKey (Config.AccessKey / Config.SecretKey, both set)
//  3. KLINGAI_API_KEY environment variable
//  4. KLINGAI_ACCESS_KEY + KLINGAI_SECRET_KEY environment variables
//
// See https://kling.ai/document-api/guides/get-started/quick-start.
func resolveKlingAIAuthToken(apiKey, accessKey, secretKey string) (string, error) {
	if explicit := strings.TrimSpace(apiKey); explicit != "" {
		return explicit, nil
	}

	if accessKey != "" && secretKey != "" {
		return generateJWTToken(accessKey, secretKey)
	}

	if envAPIKey := strings.TrimSpace(os.Getenv("KLINGAI_API_KEY")); envAPIKey != "" {
		return envAPIKey, nil
	}

	hasLegacyCredentials := trimmedSettingPresent(accessKey, "KLINGAI_ACCESS_KEY") ||
		trimmedSettingPresent(secretKey, "KLINGAI_SECRET_KEY")

	if !hasLegacyCredentials {
		return "", fmt.Errorf("KlingAI API key is missing. Pass it using the 'apiKey' parameter " + //nolint:staticcheck // matches TS SDK's exact error text
			"or the KLINGAI_API_KEY environment variable. Alternatively, pass the " +
			"legacy 'accessKey' and 'secretKey' parameters or the " +
			"KLINGAI_ACCESS_KEY and KLINGAI_SECRET_KEY environment variables.")
	}

	return generateJWTToken(accessKey, secretKey)
}

// trimmedSettingPresent reports whether settingValue (trimmed) is non-empty,
// or, when it is empty, whether the named environment variable (trimmed) is.
func trimmedSettingPresent(settingValue, envVar string) bool {
	if strings.TrimSpace(settingValue) != "" {
		return true
	}
	return strings.TrimSpace(os.Getenv(envVar)) != ""
}

// generateJWTToken generates a JWT token for KlingAI API authentication from
// an access key / secret key pair (TS generateKlingAIAuthToken), falling
// back to the KLINGAI_ACCESS_KEY / KLINGAI_SECRET_KEY environment variables
// for whichever value is empty.
// Uses HS256 (HMAC-SHA256) signing matching the TypeScript implementation.
// The token is valid for 30 minutes.
func generateJWTToken(accessKey, secretKey string) (string, error) {
	if accessKey == "" {
		accessKey = os.Getenv("KLINGAI_ACCESS_KEY")
	}
	if accessKey == "" {
		return "", fmt.Errorf("KlingAI access key is required (set KLINGAI_ACCESS_KEY or provide Config.AccessKey)")
	}
	if secretKey == "" {
		secretKey = os.Getenv("KLINGAI_SECRET_KEY")
	}
	if secretKey == "" {
		return "", fmt.Errorf("KlingAI secret key is required (set KLINGAI_SECRET_KEY or provide Config.SecretKey)")
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
