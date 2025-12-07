package bedrock

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	algorithm       = "AWS4-HMAC-SHA256"
	serviceName     = "bedrock"
	requestType     = "aws4_request"
	timeFormat      = "20060102T150405Z"
	shortTimeFormat = "20060102"
)

// AWSSigner handles AWS Signature V4 signing for Bedrock requests
type AWSSigner struct {
	accessKeyID     string
	secretAccessKey string
	sessionToken    string
	region          string
}

// NewAWSSigner creates a new AWS signer
func NewAWSSigner(accessKeyID, secretAccessKey, sessionToken, region string) *AWSSigner {
	return &AWSSigner{
		accessKeyID:     accessKeyID,
		secretAccessKey: secretAccessKey,
		sessionToken:    sessionToken,
		region:          region,
	}
}

// SignRequest signs an HTTP request with AWS Signature V4
func (s *AWSSigner) SignRequest(req *http.Request, payload []byte) error {
	now := time.Now().UTC()

	// Set required headers
	req.Header.Set("Host", req.URL.Host)
	req.Header.Set("X-Amz-Date", now.Format(timeFormat))

	if s.sessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", s.sessionToken)
	}

	// Create canonical request
	canonicalRequest := s.buildCanonicalRequest(req, payload)

	// Create string to sign
	credentialScope := s.getCredentialScope(now)
	stringToSign := s.buildStringToSign(now, credentialScope, canonicalRequest)

	// Calculate signature
	signature := s.calculateSignature(now, stringToSign)

	// Build authorization header
	authHeader := s.buildAuthorizationHeader(now, credentialScope, req.Header, signature)
	req.Header.Set("Authorization", authHeader)

	return nil
}

func (s *AWSSigner) buildCanonicalRequest(req *http.Request, payload []byte) string {
	// Canonical URI
	canonicalURI := req.URL.Path
	if canonicalURI == "" {
		canonicalURI = "/"
	}

	// Canonical query string
	canonicalQueryString := s.buildCanonicalQueryString(req)

	// Canonical headers
	canonicalHeaders, signedHeaders := s.buildCanonicalHeaders(req)

	// Payload hash
	payloadHash := s.hashPayload(payload)

	return fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		req.Method,
		canonicalURI,
		canonicalQueryString,
		canonicalHeaders,
		signedHeaders,
		payloadHash)
}

func (s *AWSSigner) buildCanonicalQueryString(req *http.Request) string {
	if req.URL.RawQuery == "" {
		return ""
	}

	params := req.URL.Query()
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		for _, v := range params[k] {
			parts = append(parts, fmt.Sprintf("%s=%s", k, v))
		}
	}

	return strings.Join(parts, "&")
}

func (s *AWSSigner) buildCanonicalHeaders(req *http.Request) (string, string) {
	headers := make(map[string]string)
	for k, v := range req.Header {
		lowerKey := strings.ToLower(k)
		headers[lowerKey] = strings.TrimSpace(v[0])
	}

	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var canonicalHeaderParts []string
	for _, k := range keys {
		canonicalHeaderParts = append(canonicalHeaderParts, fmt.Sprintf("%s:%s", k, headers[k]))
	}

	canonicalHeaders := strings.Join(canonicalHeaderParts, "\n") + "\n"
	signedHeaders := strings.Join(keys, ";")

	return canonicalHeaders, signedHeaders
}

func (s *AWSSigner) hashPayload(payload []byte) string {
	hash := sha256.Sum256(payload)
	return hex.EncodeToString(hash[:])
}

func (s *AWSSigner) buildStringToSign(t time.Time, credentialScope, canonicalRequest string) string {
	hash := sha256.Sum256([]byte(canonicalRequest))
	hashedCanonicalRequest := hex.EncodeToString(hash[:])

	return fmt.Sprintf("%s\n%s\n%s\n%s",
		algorithm,
		t.Format(timeFormat),
		credentialScope,
		hashedCanonicalRequest)
}

func (s *AWSSigner) getCredentialScope(t time.Time) string {
	return fmt.Sprintf("%s/%s/%s/%s",
		t.Format(shortTimeFormat),
		s.region,
		serviceName,
		requestType)
}

func (s *AWSSigner) calculateSignature(t time.Time, stringToSign string) string {
	kDate := s.hmac([]byte("AWS4"+s.secretAccessKey), []byte(t.Format(shortTimeFormat)))
	kRegion := s.hmac(kDate, []byte(s.region))
	kService := s.hmac(kRegion, []byte(serviceName))
	kSigning := s.hmac(kService, []byte(requestType))
	signature := s.hmac(kSigning, []byte(stringToSign))

	return hex.EncodeToString(signature)
}

func (s *AWSSigner) hmac(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func (s *AWSSigner) buildAuthorizationHeader(t time.Time, credentialScope string, headers http.Header, signature string) string {
	credential := fmt.Sprintf("%s/%s", s.accessKeyID, credentialScope)

	// Get signed headers
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, strings.ToLower(k))
	}
	sort.Strings(keys)
	signedHeaders := strings.Join(keys, ";")

	return fmt.Sprintf("%s Credential=%s, SignedHeaders=%s, Signature=%s",
		algorithm,
		credential,
		signedHeaders,
		signature)
}
