package mcp

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// oauthPKCEVerifierBytes is the number of random bytes used to build the PKCE
// code_verifier. base64url-encoding 32 bytes without padding yields a
// 43-character string, matching the default length used by the TS
// pkce-challenge package (the RFC 7636 minimum recommended length).
const oauthPKCEVerifierBytes = 32

// GenerateOAuthPKCE generates a PKCE (RFC 7636) code_verifier and its S256
// code_challenge, matching TS startAuthorization's use of pkce-challenge
// (oauth.ts, part of MCP-OAUTH-FLOW). The verifier uses the unreserved
// URL-safe alphabet [A-Za-z0-9-._~] via unpadded base64url encoding.
func GenerateOAuthPKCE() (codeVerifier string, codeChallenge string, err error) {
	raw := make([]byte, oauthPKCEVerifierBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	codeVerifier = base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(codeVerifier))
	codeChallenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return codeVerifier, codeChallenge, nil
}
