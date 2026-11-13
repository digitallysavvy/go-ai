package google

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"

	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"

	"github.com/digitallysavvy/go-ai/pkg/providers/gemini"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// googleFilesMaxResponseBytes bounds the metadata and error bodies read from
// the Files API endpoints (the uploaded content itself is the request body).
const googleFilesMaxResponseBytes = 8 << 20

type FilesAPI struct {
	provider *Provider
}

func (f *FilesAPI) UploadFile(ctx context.Context, opts types.UploadFileOptions) (*types.UploadFileResult, error) {
	var displayName *string
	pollIntervalMs := float64(2000)
	pollTimeoutMs := float64(300000)
	if raw, present := providerutils.NormalizeOptionsJSON(opts.ProviderOptions)["google"]; present && raw != nil {
		// TS parseProviderOptions with z.looseObject({displayName: string,
		// pollIntervalMs/pollTimeoutMs: positive number}, all nullish).
		google, ok := raw.(map[string]interface{})
		if !ok {
			return nil, invalidGoogleFilesOptions(fmt.Errorf("expected object, received %T", raw))
		}
		if v := google["displayName"]; v != nil {
			s, isStr := v.(string)
			if !isStr {
				return nil, invalidGoogleFilesOptions(fmt.Errorf("displayName: expected string, received %T", v))
			}
			displayName = &s
		}
		for _, f := range []struct {
			key string
			dst *float64
		}{{"pollIntervalMs", &pollIntervalMs}, {"pollTimeoutMs", &pollTimeoutMs}} {
			v := google[f.key]
			if v == nil {
				continue
			}
			n, isNum := googleFilesNumber(v)
			if !isNum {
				return nil, invalidGoogleFilesOptions(fmt.Errorf("%s: expected number, received %T", f.key, v))
			}
			if n <= 0 {
				return nil, invalidGoogleFilesOptions(fmt.Errorf("%s: too small: expected number to be >0", f.key))
			}
			*f.dst = n
		}
	}

	// TS order: parseProviderOptions, then convertInlineFileDataToUint8Array
	// (a stream is released unread and is unsupported), then getHeaders at
	// the first fetch.
	if opts.Data.Type == types.FileDataTypeStream {
		if closer, ok := opts.Data.Stream.(io.Closer); ok {
			_ = closer.Close()
		}
		return nil, &providererrors.UnsupportedFunctionalityError{Functionality: "streaming file upload"}
	}
	content, err := inlineFileBytes(opts.Data)
	if err != nil {
		return nil, err
	}

	baseURL := f.provider.config.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	baseOrigin := strings.TrimSuffix(baseURL, "/v1beta")
	// The provider's default headers (user-agent suffix, config headers) like
	// TS getHeaders, with the request-time key and the call headers on top.
	authHeaders, err := f.provider.authHeaders(ctx)
	if err != nil {
		return nil, err
	}
	headers := internalhttp.MergeHeaders(authHeaders, opts.Headers)

	initBody := map[string]interface{}{"file": map[string]interface{}{}}
	// TS: `displayName != null` -- an empty string is still sent.
	if displayName != nil {
		initBody["file"] = map[string]interface{}{"display_name": *displayName}
	}
	payload, _ := jsonstr.Marshal(initBody)
	initReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseOrigin+"/upload/v1beta/files", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		initReq.Header.Set(k, v)
	}
	initReq.Header.Set("X-Goog-Upload-Protocol", "resumable")
	initReq.Header.Set("X-Goog-Upload-Command", "start")
	initReq.Header.Set("X-Goog-Upload-Header-Content-Length", fmt.Sprintf("%d", len(content)))
	initReq.Header.Set("X-Goog-Upload-Header-Content-Type", opts.MediaType)
	initReq.Header.Set("Content-Type", "application/json")
	resp, err := internalhttp.WithFetchRedirects(f.provider.client.HTTPClient()).Do(initReq)
	if err != nil {
		// TS google-files.ts calls fetchFn directly (no handleFetchError):
		// the fetch rejection is returned unchanged.
		return nil, googleRawFetchError(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if !googleFilesStatusOK(resp.StatusCode) {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, googleFilesMaxResponseBytes))
		return nil, googleFilesError("GOOGLE_FILES_UPLOAD_ERROR", fmt.Sprintf("Failed to initiate resumable upload: %d %s", resp.StatusCode, string(b)))
	}
	// TS headers.get('x-goog-upload-url') passed to fetch: new URL(text).
	uploadURL, uploadURLErr := providerutils.FetchURLFromHeader(resp.Header, "x-goog-upload-url")
	if uploadURLErr != nil {
		return nil, uploadURLErr
	}
	if uploadURL == "" {
		return nil, googleFilesError("GOOGLE_FILES_UPLOAD_ERROR", "No upload URL returned from initiation request")
	}

	uploadReq, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, bytes.NewReader(content))
	if err != nil {
		return nil, err
	}
	uploadReq.Header.Set("Content-Length", fmt.Sprintf("%d", len(content)))
	uploadReq.Header.Set("X-Goog-Upload-Offset", "0")
	uploadReq.Header.Set("X-Goog-Upload-Command", "upload, finalize")
	uploadResp, err := internalhttp.WithFetchRedirects(f.provider.client.HTTPClient()).Do(uploadReq)
	if err != nil {
		// TS google-files.ts calls fetchFn directly (no handleFetchError):
		// the fetch rejection is returned unchanged.
		return nil, googleRawFetchError(err)
	}
	defer uploadResp.Body.Close() //nolint:errcheck
	if !googleFilesStatusOK(uploadResp.StatusCode) {
		b, _ := io.ReadAll(io.LimitReader(uploadResp.Body, googleFilesMaxResponseBytes))
		return nil, googleFilesError("GOOGLE_FILES_UPLOAD_ERROR", fmt.Sprintf("Failed to upload file data: %d %s", uploadResp.StatusCode, string(b)))
	}
	var uploadResult struct {
		File googleFileResource `json:"file"`
	}
	uploadBody, err := io.ReadAll(io.LimitReader(uploadResp.Body, googleFilesMaxResponseBytes))
	if err != nil {
		return nil, err
	}
	// TS response.text() before JSON.parse.
	uploadText := []byte(textdecode.UTF8(uploadBody))
	if err := googleFilesUploadResultShape(uploadText); err != nil {
		return nil, err
	}
	if err := jsonexact.Unmarshal(uploadText, &uploadResult); err != nil {
		return nil, err
	}
	file := uploadResult.File

	start := time.Now()
	for file.State == "PROCESSING" {
		if float64(time.Since(start))/float64(time.Millisecond) > pollTimeoutMs {
			return nil, googleFilesError("GOOGLE_FILES_UPLOAD_TIMEOUT", fmt.Sprintf("File processing timed out after %sms", providerutils.JSNumberString(pollTimeoutMs)))
		}
		// select on ctx.Done() during the wait (not a plain time.Sleep), so
		// cancellation/timeout is honored promptly instead of being delayed
		// by up to a full poll interval.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(pollIntervalMs * float64(time.Millisecond))):
		}

		pollReq, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/"+googleFilePath(file.Name), nil)
		if err != nil {
			return nil, err
		}
		for k, v := range headers {
			pollReq.Header.Set(k, v)
		}
		pollResp, err := internalhttp.WithFetchRedirects(f.provider.client.HTTPClient()).Do(pollReq)
		if err != nil {
			// TS postToApi/getFromApi handleFetchError: the retryable
			// "Cannot connect to API" APICallError (aborts unchanged).
			return nil, internalhttp.TransportErrorCtx(pollReq.Context(), err)
		}
		if !googleFilesStatusOK(pollResp.StatusCode) {
			b, _ := io.ReadAll(io.LimitReader(pollResp.Body, googleFilesMaxResponseBytes))
			_ = pollResp.Body.Close()
			return nil, f.provider.handleFilesError(pollResp.StatusCode, pollResp.Header, b)
		}
		var polled googleFileResource
		pollBody, err := io.ReadAll(io.LimitReader(pollResp.Body, googleFilesMaxResponseBytes))
		_ = pollResp.Body.Close()
		if err != nil {
			return nil, err
		}
		pollBody = []byte(textdecode.UTF8(pollBody)) // TS response.text() before JSON.parse
		// TS createJsonResponseHandler(googleFileResponseSchema).
		if err := googleValidator(zGoogleFile)(pollResp.StatusCode, pollResp.Header, pollBody); err != nil {
			return nil, err
		}
		if err := jsonexact.Unmarshal(pollBody, &polled); err != nil {
			return nil, err
		}
		file = polled
	}
	if file.State == "FAILED" {
		return nil, googleFilesError("GOOGLE_FILES_UPLOAD_FAILED", fmt.Sprintf("File processing failed for %s", file.Name))
	}

	warnings := []types.Warning{}
	if opts.Filename != "" {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "filename"})
	}

	// TS copies displayName and sizeBytes as they are (an absent key stays
	// absent, null stays null) and the time / hash fields when != null (an
	// empty string included).
	meta := map[string]interface{}{
		"name":     file.Name,
		"mimeType": file.MimeType,
		"state":    file.State,
		"uri":      file.URI,
	}
	for key, v := range map[string]string{"displayName": file.DisplayName, "sizeBytes": file.SizeBytes} {
		if raw, present := file.wire[key]; present || v != "" {
			if string(raw) == "null" {
				meta[key] = nil
			} else {
				meta[key] = v
			}
		}
	}
	for key, v := range map[string]string{"createTime": file.CreateTime, "updateTime": file.UpdateTime, "expirationTime": file.ExpirationTime, "sha256Hash": file.SHA256Hash} {
		if file.nonNull(key) || v != "" {
			meta[key] = v
		}
	}

	// TS file.mimeType ?? options.mediaType: a present "" is kept.
	mediaType := file.MimeType
	if !file.nonNull("mimeType") && mediaType == "" {
		mediaType = opts.MediaType
	}

	return &types.UploadFileResult{
		ProviderReference: map[string]string{"google": file.URI},
		MediaType:         mediaType,
		ProviderMetadata:  map[string]interface{}{"google": meta},
		Warnings:          warnings,
	}, nil
}

// googleFilesStatusOK is fetch's response.ok (a 2xx status), which both the
// raw fetch steps and getFromApi check: an unfollowed 3xx fails too.
func googleFilesStatusOK(code int) bool {
	return code >= 200 && code < 300
}

// googleFilesUploadResultShape is the TypeError TS throws reading
// `uploadResult.file.state` when the upload body has no file object: a null
// body fails on `.file`, a missing or null file on `.state`.
func googleFilesUploadResultShape(body []byte) error {
	var top interface{}
	if err := jsonexact.Unmarshal(body, &top); err != nil {
		return err
	}
	if top == nil {
		return errors.New("Cannot read properties of null (reading 'file')") //nolint:staticcheck // TS V8 TypeError text
	}
	var file interface{}
	present := false
	if m, ok := top.(map[string]interface{}); ok {
		file, present = m["file"]
	}
	switch {
	case !present:
		return errors.New("Cannot read properties of undefined (reading 'state')") //nolint:staticcheck // TS V8 TypeError text
	case file == nil:
		return errors.New("Cannot read properties of null (reading 'state')") //nolint:staticcheck // TS V8 TypeError text
	}
	return nil
}

// googleRawFetchError is a failed raw fetch (TS google-files.ts and the
// realtime client secret call fetchFn directly, and the rejection propagates
// as is): a rejected custom fetch keeps its own error, any
// other transport failure is the net/http error.
func googleRawFetchError(err error) error {
	if cause, ok := internalhttp.FetchRejectionCause(err); ok {
		return cause
	}
	return err
}

// googleFilesError is the AISDKError TS throws for a failed upload; Name is
// carried as the error code.
func googleFilesError(name, message string) error {
	return providererrors.NewProviderError("google", 0, name, message, nil)
}

type googleFileResource struct {
	Name           string `json:"name"`
	DisplayName    string `json:"displayName"`
	MimeType       string `json:"mimeType"`
	SizeBytes      string `json:"sizeBytes"`
	CreateTime     string `json:"createTime"`
	UpdateTime     string `json:"updateTime"`
	ExpirationTime string `json:"expirationTime"`
	SHA256Hash     string `json:"sha256Hash"`
	URI            string `json:"uri"`
	State          string `json:"state"`

	// wire holds the keys the API sent (presence and null checks).
	wire map[string]json.RawMessage
}

// UnmarshalJSON decodes the resource and records which keys were sent.
func (r *googleFileResource) UnmarshalJSON(b []byte) error {
	type plain googleFileResource
	var p plain
	if err := jsonexact.Unmarshal(b, &p); err != nil {
		return err
	}
	*r = googleFileResource(p)
	_ = json.Unmarshal(b, &r.wire)
	return nil
}

// nonNull reports TS `field != null` for a key the API sent.
func (r *googleFileResource) nonNull(key string) bool {
	raw, ok := r.wire[key]
	return ok && string(raw) != "null"
}

func inlineFileBytes(data types.FileData) ([]byte, error) {
	switch data.Type {
	case types.FileDataTypeData:
		if data.DataString != "" {
			return types.DecodeFileDataString(data.DataString)
		}
		return data.Data, nil
	case types.FileDataTypeText:
		return []byte(data.Text), nil
	default:
		return nil, fmt.Errorf("unsupported file data type %q", data.Type)
	}
}

// googleFilePath path-encodes a provider-returned file resource name before it
// is reused in the credentialed polling URL (TS google-files.ts, 7de3612):
// "files/<id>" keeps the collection segment and encodes only the id.
func googleFilePath(name string) string {
	if rest, ok := strings.CutPrefix(name, "files/"); ok && rest != "" && !strings.Contains(rest, "/") {
		return "files/" + providerutils.EncodePathSegment(rest)
	}
	return providerutils.EncodePathSegment(name)
}

// googleFilesNumber reads a numeric provider option given as any Go number
// type, like TS z.number(): fractions are kept and NaN / Infinity are not
// numbers.
func googleFilesNumber(v interface{}) (float64, bool) {
	var f float64
	switch n := v.(type) {
	case json.Number:
		parsed, err := n.Float64()
		if err != nil {
			return 0, false
		}
		f = parsed
	default:
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			f = float64(rv.Int())
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			f = float64(rv.Uint())
		case reflect.Float32, reflect.Float64:
			f = rv.Float()
		default:
			return 0, false
		}
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

// handleFilesError builds the ProviderError for a failed Files API response
// (TS googleFailedResponseHandler: error.message, status as the code).
func (p *Provider) handleFilesError(status int, headers http.Header, body []byte) error {
	return gemini.GoogleHTTPError("google", status, headers, body, nil)
}

func invalidGoogleFilesOptions(cause error) error {
	return providererrors.NewArgumentError("providerOptions", "invalid google provider options", cause)
}
