package bedrock

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestAWSSignerCanonicalHelpers(t *testing.T) {
	s := NewAWSSigner("AKID", "SECRET", "", "us-east-1")

	u, _ := url.Parse("https://bedrock-runtime.us-east-1.amazonaws.com/model/test?z=9&a=1&a=2")
	req := &http.Request{Method: http.MethodPost, URL: u, Header: http.Header{}}
	req.Header.Set("X-Amz-Date", "20260512T000000Z")
	req.Header.Set("Host", u.Host)
	req.Header.Set("Content-Type", " application/json ")

	if got := s.buildCanonicalQueryString(req); got != "a=1&a=2&z=9" {
		t.Fatalf("canonical query = %q", got)
	}

	canonicalHeaders, signedHeaders := s.buildCanonicalHeaders(req)
	if !strings.Contains(canonicalHeaders, "content-type:application/json\n") {
		t.Fatalf("canonical headers missing content-type normalization: %q", canonicalHeaders)
	}
	if signedHeaders != "content-type;host;x-amz-date" {
		t.Fatalf("signed headers = %q", signedHeaders)
	}

	if got := s.hashPayload([]byte("hello")); got != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatalf("payload hash = %q", got)
	}

	ts := time.Date(2026, 5, 12, 10, 30, 0, 0, time.UTC)
	if got := s.getCredentialScope(ts); got != "20260512/us-east-1/bedrock/aws4_request" {
		t.Fatalf("credential scope = %q", got)
	}

	canonicalReq := s.buildCanonicalRequest(req, []byte(`{"x":1}`))
	stringToSign := s.buildStringToSign(ts, s.getCredentialScope(ts), canonicalReq)
	if !strings.Contains(stringToSign, "AWS4-HMAC-SHA256\n20260512T103000Z\n20260512/us-east-1/bedrock/aws4_request\n") {
		t.Fatalf("unexpected string to sign: %q", stringToSign)
	}
}

func TestAWSSignerSignRequestSetsHeaders(t *testing.T) {
	s := NewAWSSigner("AKID", "SECRET", "SESSION", "us-east-1")
	req, err := http.NewRequest(http.MethodPost, "https://bedrock-runtime.us-east-1.amazonaws.com/model/test", strings.NewReader(`{"a":1}`))
	if err != nil {
		t.Fatalf("NewRequest error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if err := s.SignRequest(req, []byte(`{"a":1}`)); err != nil {
		t.Fatalf("SignRequest error = %v", err)
	}

	if req.Header.Get("Host") == "" || req.Header.Get("X-Amz-Date") == "" {
		t.Fatalf("missing required signed headers: %#v", req.Header)
	}
	if req.Header.Get("X-Amz-Security-Token") != "SESSION" {
		t.Fatalf("session token header = %q", req.Header.Get("X-Amz-Security-Token"))
	}
	auth := req.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=AKID/") {
		t.Fatalf("authorization prefix mismatch: %q", auth)
	}
	if !strings.Contains(auth, "SignedHeaders=") || !strings.Contains(auth, "Signature=") {
		t.Fatalf("authorization malformed: %q", auth)
	}
}

func TestAWSSignerAuthorizationHeaderIncludesSortedSignedHeaders(t *testing.T) {
	s := NewAWSSigner("AKID", "SECRET", "", "us-east-1")
	headers := http.Header{}
	headers.Set("X-Amz-Date", "20260512T010203Z")
	headers.Set("Host", "example.com")
	headers.Set("Content-Type", "application/json")

	scope := "20260512/us-east-1/bedrock/aws4_request"
	got := s.buildAuthorizationHeader(time.Date(2026, 5, 12, 1, 2, 3, 0, time.UTC), scope, headers, "abc123")
	if !strings.Contains(got, "SignedHeaders=content-type;host;x-amz-date") {
		t.Fatalf("signed headers ordering mismatch: %q", got)
	}
	if !strings.HasSuffix(got, "Signature=abc123") {
		t.Fatalf("signature suffix mismatch: %q", got)
	}
}
