package tencent

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Ported from tencent-cloud-api.test.ts.

// Worked example from Tencent's "Signature v3" documentation.
const tc3DocPayload = `{"Limit": 1, "Filters": [{"Values": ["unnamed"], "Name": "instance-name"}]}`

func TestTC3HashesThePayloadAsDocumented(t *testing.T) {
	if got := sha256Hex(tc3DocPayload); got != "99d58dfbc6745f6747f36bfca17dee5e6881dc0428a0a36f96199342bc5b4907" {
		t.Errorf("sha256Hex = %s", got)
	}
}

func TestTC3ProducesTheDocumentedCanonicalRequestHash(t *testing.T) {
	canonical, signed := buildTC3CanonicalRequest(tc3DocPayload, map[string]string{
		"Host":         "cvm.tencentcloudapi.com",
		"Content-Type": "application/json; charset=utf-8",
	})
	if signed != "content-type;host" {
		t.Errorf("signedHeaders = %q", signed)
	}
	if got := sha256Hex(canonical); got != "2815843035062fffda5fd6f2a44ea8a34818b0dc46f024b8b3786976a3adda7a" {
		t.Errorf("canonical hash = %s", got)
	}
}

const tc3VodAuthorization = "TC3-HMAC-SHA256 Credential=AKIDEXAMPLEEXAMPLEEXAMPLE/2026-11-08/vod/tc3_request, " +
	"SignedHeaders=content-type;host;x-tc-action, " +
	"Signature=20bd5603a92b091b44b53c95c55b2e5f7687cf779aff39688d83229b7252409f"

func TestTC3SignsAVodRequest(t *testing.T) {
	got := createTC3Authorization("AKIDEXAMPLEEXAMPLEEXAMPLE", "EXAMPLESECRETKEYEXAMPLESECRETKEY", "vod", 1794120000,
		`{"SubAppId":1500000000,"ModelName":"VS","ModelVersion":"2.0","Prompt":"A cat"}`,
		map[string]string{
			"content-type": "application/json; charset=utf-8",
			"host":         "vod.intl.tencentcloudapi.com",
			"x-tc-action":  "createaigcvideotask",
		})
	if got != tc3VodAuthorization {
		t.Errorf("authorization = %s", got)
	}
}

func TestTencentCloudErrorStatusCode(t *testing.T) {
	for code, want := range map[string]int{
		"AuthFailure.SignatureFailure":   401,
		"AuthFailure.SecretIdNotFound":   401,
		"UnauthorizedOperation":          403,
		"FailedOperation.InvalidVodUser": 403,
		"ResourceNotFound":               404,
		"InvalidParameterValue.SubAppId": 400,
		"MissingParameter":               400,
		"RequestLimitExceeded":           429,
		"LimitExceeded":                  429,
		"ResourceUnavailable":            503,
		"InternalError":                  500,
		"FailedOperation":                500,
		"SomethingNew":                   500,
	} {
		if got := tencentCloudErrorStatusCode(code); got != want {
			t.Errorf("%s -> %d, want %d", code, got, want)
		}
	}
}

type cloudCall struct {
	headers http.Header
	body    string
	path    string
}

// cloudServer answers every POST with body and records the calls.
func cloudServer(t *testing.T, body string) (*httptest.Server, func() []cloudCall) {
	t.Helper()
	var mu sync.Mutex
	var calls []cloudCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		calls = append(calls, cloudCall{headers: r.Header.Clone(), body: string(b), path: r.URL.Path})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []cloudCall {
		mu.Lock()
		defer mu.Unlock()
		return append([]cloudCall(nil), calls...)
	}
}

func cloudBody() *jsonorder.Object {
	return newOrderedBody("SubAppId", int64(1500000000), "ModelName", "VS", "ModelVersion", "2.0", "Prompt", "A cat")
}

var cloudCreds = vodCredentials{secretID: "AKIDEXAMPLEEXAMPLEEXAMPLE", secretKey: "EXAMPLESECRETKEYEXAMPLESECRETKEY"}

func TestPostTencentCloudAPISendsTheSignedPayloadAndProtocolHeaders(t *testing.T) {
	srv, calls := cloudServer(t, `{"Response":{"TaskId":"task-1","RequestId":"req-1"}}`)
	p := New(Config{VodBaseURL: srv.URL})
	result, err := p.postTencentCloudAPI(context.Background(), "vod", "CreateAigcVideoTask", "2018-07-17", cloudCreds, cloudBody(),
		vodCreateTaskResponseSchema, map[string]string{"Content-Type": "text/plain", "x-custom": "value"}, 1794120000)
	if err != nil {
		t.Fatal(err)
	}
	if result.response["TaskId"] != "task-1" {
		t.Errorf("response = %v", result.response)
	}
	c := calls()[0]
	if c.body != `{"SubAppId":1500000000,"ModelName":"VS","ModelVersion":"2.0","Prompt":"A cat"}` {
		t.Errorf("body = %s", c.body)
	}
	for name, want := range map[string]string{
		"Content-Type":   "application/json; charset=utf-8",
		"X-Tc-Action":    "CreateAigcVideoTask",
		"X-Tc-Version":   "2018-07-17",
		"X-Tc-Timestamp": "1794120000",
		"X-Custom":       "value",
	} {
		if got := c.headers.Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if got := c.headers.Get("X-Tc-Region"); got != "" {
		t.Errorf("X-TC-Region = %q, want none", got)
	}
	if len(c.headers.Values("Content-Type")) != 1 {
		t.Errorf("Content-Type values = %v", c.headers.Values("Content-Type"))
	}
	// The signature covers the test server's host, so check its shape.
	if !strings.HasPrefix(c.headers.Get("Authorization"), "TC3-HMAC-SHA256 Credential=AKIDEXAMPLEEXAMPLEEXAMPLE/2026-11-08/vod/tc3_request, SignedHeaders=content-type;host;x-tc-action, Signature=") {
		t.Errorf("Authorization = %q", c.headers.Get("Authorization"))
	}
}

func TestPostTencentCloudAPISendsRegionAndSessionToken(t *testing.T) {
	srv, calls := cloudServer(t, `{"Response":{"TaskId":"task-1"}}`)
	p := New(Config{VodBaseURL: srv.URL, Region: "ap-singapore"})
	creds := cloudCreds
	creds.sessionToken = "session-token"
	if _, err := p.postTencentCloudAPI(context.Background(), "vod", "DescribeTaskDetail", "2018-07-17", creds,
		newOrderedBody("TaskId", "task-1"), vodCreateTaskResponseSchema, nil, 1794120000); err != nil {
		t.Fatal(err)
	}
	c := calls()[0]
	if c.headers.Get("X-Tc-Region") != "ap-singapore" || c.headers.Get("X-Tc-Token") != "session-token" {
		t.Errorf("headers = %v", c.headers)
	}
}

func TestPostTencentCloudAPIResponseErrorWithHTTP200(t *testing.T) {
	srv, _ := cloudServer(t, `{"Response":{"Error":{"Code":"AuthFailure.SignatureFailure","Message":"The provided credentials could not be validated."},"RequestId":"req-1"}}`)
	p := New(Config{VodBaseURL: srv.URL})
	_, err := p.postTencentCloudAPI(context.Background(), "vod", "CreateAigcVideoTask", "2018-07-17", cloudCreds, cloudBody(), vodCreateTaskResponseSchema, nil, 1794120000)
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) {
		t.Fatalf("err = %T %v", err, err)
	}
	if perr.StatusCode != 401 || perr.IsRetryable() || perr.Message != "AuthFailure.SignatureFailure: The provided credentials could not be validated." {
		t.Errorf("error = %+v retryable=%v", perr, perr.IsRetryable())
	}
}

func TestPostTencentCloudAPIRateLimitIsRetryable(t *testing.T) {
	srv, _ := cloudServer(t, `{"Response":{"Error":{"Code":"RequestLimitExceeded","Message":"Too many requests."}}}`)
	p := New(Config{VodBaseURL: srv.URL})
	_, err := p.postTencentCloudAPI(context.Background(), "vod", "CreateAigcVideoTask", "2018-07-17", cloudCreds, cloudBody(), vodCreateTaskResponseSchema, nil, 1794120000)
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.StatusCode != 429 || !perr.IsRetryable() {
		t.Fatalf("err = %v", err)
	}
}

// The response must match its schema (TS safeValidateTypes): a failure is
// "Invalid JSON response" with the HTTP status.
func TestPostTencentCloudAPIInvalidResponse(t *testing.T) {
	srv, _ := cloudServer(t, `{"Response":{"RequestId":"req-1"}}`)
	p := New(Config{VodBaseURL: srv.URL})
	_, err := p.postTencentCloudAPI(context.Background(), "vod", "CreateAigcVideoTask", "2018-07-17", cloudCreds, cloudBody(), vodCreateTaskResponseSchema, nil, 1794120000)
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.Message != providererrors.InvalidJSONResponseMessage || perr.StatusCode != 200 {
		t.Fatalf("err = %v", err)
	}
}
