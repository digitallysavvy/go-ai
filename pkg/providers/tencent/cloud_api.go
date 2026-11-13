package tencent

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

const (
	tc3Algorithm   = "TC3-HMAC-SHA256"
	tc3ContentType = "application/json; charset=utf-8"
)

// sha256Hex is the lower-case hex SHA-256 of value (TS sha256Hex).
func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(value))
	return mac.Sum(nil)
}

// buildTC3CanonicalRequest builds the TC3 canonical request for a POST to "/"
// with an empty query (TS buildTc3CanonicalRequest): header names are
// lower-cased and sorted, values trimmed.
//
// See https://intl.cloud.tencent.com/document/product/377/30988.
func buildTC3CanonicalRequest(payload string, headers map[string]string) (canonicalRequest, signedHeaders string) {
	type entry struct{ name, value string }
	entries := make([]entry, 0, len(headers))
	for name, value := range headers {
		entries = append(entries, entry{strings.ToLower(name), strings.TrimSpace(value)})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })

	var canonicalHeaders strings.Builder
	names := make([]string, len(entries))
	for i, e := range entries {
		canonicalHeaders.WriteString(e.name + ":" + e.value + "\n")
		names[i] = e.name
	}
	signedHeaders = strings.Join(names, ";")
	canonicalRequest = strings.Join([]string{
		"POST",
		"/",
		"",
		canonicalHeaders.String(),
		signedHeaders,
		sha256Hex(payload),
	}, "\n")
	return canonicalRequest, signedHeaders
}

// createTC3Authorization computes the Authorization header value of a
// Tencent Cloud API 3.0 request signed with TC3-HMAC-SHA256 (TS
// createTc3Authorization). timestamp is the Unix time in seconds and must
// match the X-TC-Timestamp header.
func createTC3Authorization(secretID, secretKey, service string, timestamp int64, payload string, headers map[string]string) string {
	date := time.Unix(timestamp, 0).UTC().Format("2006-01-02")
	credentialScope := date + "/" + service + "/tc3_request"
	canonicalRequest, signedHeaders := buildTC3CanonicalRequest(payload, headers)

	stringToSign := strings.Join([]string{
		tc3Algorithm,
		strconv.FormatInt(timestamp, 10),
		credentialScope,
		sha256Hex(canonicalRequest),
	}, "\n")

	secretDate := hmacSHA256([]byte("TC3"+secretKey), date)
	secretService := hmacSHA256(secretDate, service)
	secretSigning := hmacSHA256(secretService, "tc3_request")
	signature := hex.EncodeToString(hmacSHA256(secretSigning, stringToSign))

	return tc3Algorithm + " Credential=" + secretID + "/" + credentialScope + ", " +
		"SignedHeaders=" + signedHeaders + ", Signature=" + signature
}

// tencentCloudErrorStatusCode maps a Tencent Cloud API 3.0 error code family
// to the HTTP status it stands for (TS getTencentCloudErrorStatusCode): the
// API reports failures with HTTP 200 and a Response.Error, so retry logic
// needs the real failure class.
//
// See https://www.tencentcloud.com/document/api/266/34188.
func tencentCloudErrorStatusCode(code string) int {
	family, _, _ := strings.Cut(code, ".")
	switch family {
	case "AuthFailure":
		return 401
	case "UnauthorizedOperation":
		return 403
	case "ResourceNotFound", "InvalidAction", "NoSuchProduct", "NoSuchVersion":
		return 404
	case "LimitExceeded", "RequestLimitExceeded":
		return 429
	case "InvalidParameter", "InvalidParameterValue", "InvalidRequest", "MissingParameter",
		"RequestSizeLimitExceeded", "UnknownParameter", "UnsupportedOperation",
		"UnsupportedProtocol", "UnsupportedRegion":
		return 400
	case "ResourceInsufficient", "ResourceUnavailable", "ServiceUnavailable":
		return 503
	case "FailedOperation":
		if code == "FailedOperation.InvalidVodUser" {
			return 403
		}
		return 500
	}
	return 500
}

// tencentCloudErrorSchema is TS createTencentCloudErrorSchema().
var tencentCloudErrorSchema = zodlite.Object(
	zodlite.F("Code", zodlite.String),
	zodlite.F("Message", zodlite.String),
)

// tencentCloudEnvelopeSchema is TS tencentCloudEnvelopeSchema.
var tencentCloudEnvelopeSchema = zodlite.Object(
	zodlite.F("Response", zodlite.Object(
		zodlite.F("Error", zodlite.Nullish(tencentCloudErrorSchema)),
		zodlite.F("RequestId", zodlite.Nullish(zodlite.String)),
	)),
)

// tencentCloudErrorEnvelopeSchema is TS tencentCloudErrorEnvelopeSchema.
var tencentCloudErrorEnvelopeSchema = zodlite.Object(
	zodlite.F("Response", zodlite.Object(
		zodlite.F("Error", tencentCloudErrorSchema),
		zodlite.F("RequestId", zodlite.Nullish(zodlite.String)),
	)),
)

func formatTencentCloudErrorMessage(e map[string]interface{}) string {
	code, _ := e["Code"].(string)
	message, _ := e["Message"].(string)
	return code + ": " + message
}

// tencentCloudEnvelopeData is the zod output of the success envelope: the
// top-level object keeps only Response, which is loose (every key kept).
func tencentCloudEnvelopeData(tree map[string]interface{}) map[string]interface{} {
	response, _ := tree["Response"].(map[string]interface{})
	return map[string]interface{}{"Response": response}
}

// tencentCloudErrorData is the zod output of the error envelope:
// {Response: {Error: {Code, Message}, RequestId?}}, undeclared keys stripped.
func tencentCloudErrorData(tree map[string]interface{}) map[string]interface{} {
	response, _ := tree["Response"].(map[string]interface{})
	e, _ := response["Error"].(map[string]interface{})
	outResponse := map[string]interface{}{
		"Error": map[string]interface{}{"Code": e["Code"], "Message": e["Message"]},
	}
	if v, ok := response["RequestId"]; ok {
		outResponse["RequestId"] = v
	}
	return map[string]interface{}{"Response": outResponse}
}

// tencentCloudResult is a validated Tencent Cloud API response: the
// Response object and the HTTP response headers.
type tencentCloudResult struct {
	response map[string]interface{}
	headers  map[string]string
}

// postTencentCloudAPI calls a Tencent Cloud API 3.0 action with a TC3-signed
// JSON POST and returns the validated Response object (TS
// postTencentCloudApi). body is sent exactly as signed, in its key order.
// headers are the caller headers; a name that matches a signed protocol
// header (any case) is dropped so the request never carries two values.
func (p *Provider) postTencentCloudAPI(ctx context.Context, service, action, apiVersion string, creds vodCredentials, body *jsonorder.Object, responseSchema zodlite.Node, headers map[string]string, timestamp int64) (*tencentCloudResult, error) {
	payload, err := jsonstr.Marshal(body)
	if err != nil {
		return nil, err
	}
	host := ""
	if u, parseErr := url.Parse(p.vodBaseURL); parseErr == nil {
		host = u.Host
	}
	authorization := createTC3Authorization(creds.secretID, creds.secretKey, service, timestamp, string(payload), map[string]string{
		"content-type": tc3ContentType,
		"host":         host,
		"x-tc-action":  strings.ToLower(action),
	})

	protocolHeaders := map[string]string{
		"authorization":  authorization,
		"content-type":   tc3ContentType,
		"x-tc-action":    action,
		"x-tc-timestamp": strconv.FormatInt(timestamp, 10),
		"x-tc-version":   apiVersion,
	}
	if p.config.Region != "" {
		protocolHeaders["x-tc-region"] = p.config.Region
	}
	if creds.sessionToken != "" {
		protocolHeaders["x-tc-token"] = creds.sessionToken
	}
	requestHeaders := make(map[string]string, len(headers)+len(protocolHeaders))
	for name, value := range headers {
		if _, signed := protocolHeaders[strings.ToLower(name)]; !signed {
			requestHeaders[name] = value
		}
	}
	for name, value := range protocolHeaders {
		requestHeaders[name] = value
	}

	resp, err := p.vod.Do(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Body:    payload,
		Headers: requestHeaders,
	})
	if err != nil {
		return nil, internalhttp.UnwrapFetchRejection(err) // TS postToApi lets a transport failure propagate unchanged
	}
	resp.Body = []byte(textdecode.UTF8(resp.Body)) // TS reads the body as text (TextDecoder)
	responseHeaders := providerutils.ExtractHeaders(resp.Headers)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, tencentCloudFailedResponse(resp)
	}

	// TS createJsonResponseHandler(tencentCloudEnvelopeSchema).
	var tree interface{}
	if err := securejson.Unmarshal(resp.Body, &tree); err != nil {
		return nil, providerutils.InvalidJSONResponseError("tencent", resp.StatusCode, resp.Headers, resp.Body, err)
	}
	if issue := zodlite.Validate(tencentCloudEnvelopeSchema, resp.Body, tree); issue != nil {
		return nil, providerutils.InvalidJSONResponseError("tencent", resp.StatusCode, resp.Headers, resp.Body, issue)
	}
	envelope := tree.(map[string]interface{})
	response := envelope["Response"].(map[string]interface{})
	if e, ok := response["Error"].(map[string]interface{}); ok {
		perr := &providererrors.ProviderError{
			Provider:        "tencent",
			StatusCode:      tencentCloudErrorStatusCode(e["Code"].(string)),
			Message:         formatTencentCloudErrorMessage(e),
			ResponseHeaders: responseHeaders,
			ResponseBody:    jsonorder.Text(resp.Body, envelope),
			Data:            tencentCloudEnvelopeData(envelope),
		}
		return nil, perr
	}

	if issue := zodlite.ValidateWithin(responseSchema, resp.Body, tree, response); issue != nil {
		return nil, &providererrors.ProviderError{
			Provider:        "tencent",
			StatusCode:      resp.StatusCode,
			Message:         providererrors.InvalidJSONResponseMessage,
			Cause:           issue,
			ResponseHeaders: responseHeaders,
			ResponseBody:    jsonorder.Text(resp.Body, envelope),
		}
	}
	return &tencentCloudResult{response: response, headers: responseHeaders}, nil
}

// tencentCloudFailedResponse is TS tencentCloudFailedResponseHandler for a
// non-2xx response: the message is "<Code>: <Message>" of a body matching
// the error envelope, else the HTTP status text.
func tencentCloudFailedResponse(resp *internalhttp.Response) error {
	cause := &internalhttp.HTTPStatusError{StatusCode: resp.StatusCode, Headers: resp.Headers, Body: resp.Body}
	message := http.StatusText(resp.StatusCode)
	var data interface{}
	var tree interface{}
	if securejson.Unmarshal(resp.Body, &tree) == nil && tencentCloudErrorEnvelopeSchema(tree, true, nil) == nil {
		envelope := tree.(map[string]interface{})
		e := envelope["Response"].(map[string]interface{})["Error"].(map[string]interface{})
		message = formatTencentCloudErrorMessage(e)
		data = tencentCloudErrorData(envelope)
	}
	perr := providererrors.NewProviderErrorFromResponse("tencent", 0, "", message, cause)
	perr.Data = data
	return perr
}
