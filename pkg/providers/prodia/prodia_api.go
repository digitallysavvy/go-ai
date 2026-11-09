package prodia

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"regexp"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	promptutil "github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
)

// prodiaJobResponse represents the Prodia job API response metadata.
// Field names match the Prodia v2 API response schema exactly.
type prodiaJobResponse struct {
	ID        string           `json:"id"`
	CreatedAt string           `json:"created_at,omitempty"`
	UpdatedAt string           `json:"updated_at,omitempty"`
	ExpiresAt string           `json:"expires_at,omitempty"`
	State     *prodiaJobState  `json:"state,omitempty"`
	Config    *prodiaJobConfig `json:"config,omitempty"`
	Metrics   *prodiaMetrics   `json:"metrics,omitempty"`
	Price     *prodiaPrice     `json:"price,omitempty"`
	ImageURL  string           `json:"imageUrl,omitempty"` // legacy field

	// createdAtSet / updatedAtSet record that the wire carried the key (even
	// as ""): TS spreads `created_at != null`, so an empty string is kept.
	createdAtSet bool
	updatedAtSet bool
}

// UnmarshalJSON decodes the job metadata and notes which optional string
// fields were present.
func (j *prodiaJobResponse) UnmarshalJSON(data []byte) error {
	type plain prodiaJobResponse
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	var keys map[string]json.RawMessage
	_ = json.Unmarshal(data, &keys)
	isString := func(key string) bool {
		raw, ok := keys[key]
		return ok && len(raw) > 0 && raw[0] == '"'
	}
	p.createdAtSet = isString("created_at")
	p.updatedAtSet = isString("updated_at")
	*j = prodiaJobResponse(p)
	return nil
}

// prodiaJobState is the state object returned by the Prodia API.
type prodiaJobState struct {
	Current string `json:"current"`
}

// prodiaJobConfig is the config object returned in the Prodia job response.
// It contains the resolved generation parameters, including the effective seed.
type prodiaJobConfig struct {
	Seed *float64 `json:"seed,omitempty"`
}

// prodiaMetrics holds performance metrics from the Prodia job response.
type prodiaMetrics struct {
	Elapsed *float64 `json:"elapsed,omitempty"`
	IPS     *float64 `json:"ips,omitempty"`
}

// prodiaPrice holds pricing information from the Prodia job response.
type prodiaPrice struct {
	Product string   `json:"product,omitempty"`
	Dollars *float64 `json:"dollars,omitempty"`
}

// errMissingJobPart and errMissingBoundary carry the TS multipart handler's
// error texts (prodia-image-model.ts and siblings).
var errMissingJobPart = errors.New("Prodia multipart response missing job part") //nolint:staticcheck // matches TS SDK's exact error text

func errMissingBoundary(contentType string) error {
	return fmt.Errorf("Prodia response missing multipart boundary in content-type: %s", contentType) //nolint:staticcheck // matches TS SDK's exact error text
}

// parseMultipartResponse parses a multipart/form-data response from the Prodia API.
// It extracts the "job" JSON metadata part and the "output" binary data part.
// Returns the job metadata, output bytes, output MIME type, and any error.
func parseMultipartResponse(contentType string, body []byte) (*prodiaJobResponse, []byte, string, error) {
	return parseMultipartResponseFor(contentType, body, "video/")
}

// parseMultipartResponseFor is parseMultipartResponse with the media type
// prefix that identifies an unnamed output part: TS accepts "video/" parts in
// the video model's handler and "image/" parts in the image model's. The
// returned media type is the output part's Content-Type exactly as sent
// (TS partContentType), "" when it has none.
func parseMultipartResponseFor(contentType string, body []byte, fallbackPrefix string) (*prodiaJobResponse, []byte, string, error) {
	boundary, ok := prodiaMultipartBoundary(contentType)
	if !ok {
		return nil, nil, "", errMissingBoundary(contentType)
	}

	var jobResp *prodiaJobResponse
	var outputData []byte
	var outputMIME string
	for _, part := range parseProdiaMultipart(body, boundary) {
		disposition := part.headers["content-disposition"]
		partCT := part.headers["content-type"]
		switch {
		case strings.Contains(disposition, `name="job"`):
			j, err := parseJobPart(part.body)
			if err != nil {
				return nil, nil, "", err
			}
			jobResp = j
		case strings.Contains(disposition, `name="output"`):
			outputData = part.body
			outputMIME = partCT
		case strings.HasPrefix(partCT, fallbackPrefix):
			// As in TS a later such part replaces an earlier output.
			outputData = part.body
			outputMIME = partCT
		}
	}

	if jobResp == nil {
		return nil, nil, "", errMissingJobPart
	}

	return jobResp, outputData, outputMIME, nil
}

// prodiaBoundaryPattern is TS /boundary=([^\s;]+)/ with JS \s (a quoted
// boundary keeps its quotes, as in TS). The callers pass the Content-Type as
// TS reads it (providerutils.FetchHeaderText), so a raw 0xA0 ends the
// boundary.
var prodiaBoundaryPattern = regexp.MustCompile(`boundary=([^` + providerutils.JSRegexpSpaceChars + `;]+)`)

// prodiaMultipartBoundary extracts the boundary like TS
// contentType.match(/boundary=([^\s;]+)/).
func prodiaMultipartBoundary(contentType string) (string, bool) {
	m := prodiaBoundaryPattern.FindStringSubmatch(contentType)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// prodiaMultipartPart is TS MultipartPart: lower-cased header names to
// trimmed values, and the raw body.
type prodiaMultipartPart struct {
	headers map[string]string
	body    []byte
}

// parseProdiaMultipart is TS parseMultipart (prodia-api.ts), ported byte for
// byte: every occurrence of "--boundary" splits the body, CRLF or LF around
// a delimiter is dropped, the header block ends at the first CRLFCRLF or
// LFLF, a part without one is skipped, and the closing "--boundary--" is
// never a part. Unlike mime/multipart it does not require delimiters at
// line starts or reject malformed headers.
func parseProdiaMultipart(data []byte, boundary string) []prodiaMultipartPart {
	delimiter := []byte("--" + boundary)
	endDelimiter := []byte("--" + boundary + "--")

	var positions []int
	for i := 0; i <= len(data)-len(delimiter); i++ {
		if bytes.Equal(data[i:i+len(delimiter)], delimiter) {
			positions = append(positions, i)
		}
	}

	at := func(i int) int { // JS data[i]: undefined outside the array
		if i < 0 || i >= len(data) {
			return -1
		}
		return int(data[i])
	}

	var parts []prodiaMultipartPart
	for i := 0; i < len(positions)-1; i++ {
		start := positions[i] + len(delimiter)
		end := positions[i+1]

		if positions[i]+len(endDelimiter) <= len(data) && bytes.Equal(data[positions[i]:positions[i]+len(endDelimiter)], endDelimiter) {
			continue
		}

		partStart := start
		if at(partStart) == '\r' && at(partStart+1) == '\n' {
			partStart += 2
		} else if at(partStart) == '\n' {
			partStart++
		}
		partEnd := end
		if at(partEnd-2) == '\r' && at(partEnd-1) == '\n' {
			partEnd -= 2
		} else if at(partEnd-1) == '\n' {
			partEnd--
		}
		// JS slice(start, end) with end < start is empty.
		var partData []byte
		if partEnd > partStart {
			partData = data[partStart:partEnd]
		}

		headerEnd := -1
		for j := 0; j < len(partData)-3; j++ {
			if partData[j] == '\r' && partData[j+1] == '\n' && partData[j+2] == '\r' && partData[j+3] == '\n' {
				headerEnd = j
				break
			}
			if partData[j] == '\n' && partData[j+1] == '\n' {
				headerEnd = j
				break
			}
		}
		if headerEnd == -1 {
			continue
		}

		headers := map[string]string{}
		for _, line := range prodiaLineBreak.Split(textdecode.UTF8(partData[:headerEnd]), -1) {
			if colon := strings.Index(line, ":"); colon > 0 {
				headers[providerutils.JSToLower(providerutils.JSTrim(line[:colon]))] = providerutils.JSTrim(line[colon+1:])
			}
		}

		bodyStart := headerEnd + 2
		if partData[headerEnd] == '\r' {
			bodyStart = headerEnd + 4
		}
		var body []byte
		if bodyStart < len(partData) {
			body = append([]byte(nil), partData[bodyStart:]...)
		} else {
			body = []byte{}
		}
		parts = append(parts, prodiaMultipartPart{headers: headers, body: body})
	}
	return parts
}

// prodiaLineBreak is TS /\r?\n/.
var prodiaLineBreak = regexp.MustCompile(`\r?\n`)

// parseJobPart decodes the multipart "job" part, applying TS
// prodiaJobResultSchema first: a body that does not match is the typed
// validation (or JSON parse) error itself, as TS parseJSON({schema}) throws it.
func parseJobPart(data []byte) (*prodiaJobResponse, error) {
	// TS new TextDecoder().decode(part.body) before parseJSON.
	data = []byte(promptutil.DecodeUTF8Text(data))
	if err := providerutils.MediaSchemaParser(providerutils.SchemaProdiaJob)(0, nil, data); err != nil {
		return nil, err
	}
	var j prodiaJobResponse
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, fmt.Errorf("failed to parse job metadata: %w", err)
	}
	return &j, nil
}

// buildProdiaProviderMetadata converts a prodiaJobResponse to a provider
// metadata map.  Field names and structure match the TypeScript SDK's
// buildProdiaProviderMetadata function exactly.
func buildProdiaProviderMetadata(job *prodiaJobResponse) map[string]interface{} {
	if job == nil {
		return nil
	}
	m := map[string]interface{}{
		"jobId": job.ID,
	}
	// seed lives in config, not metrics (matches TS: jobResult.config?.seed)
	if job.Config != nil && job.Config.Seed != nil {
		m["seed"] = *job.Config.Seed
	}
	if job.Metrics != nil {
		if job.Metrics.Elapsed != nil {
			m["elapsed"] = *job.Metrics.Elapsed
		}
		if job.Metrics.IPS != nil {
			m["iterationsPerSecond"] = *job.Metrics.IPS
		}
	}
	if job.CreatedAt != "" || job.createdAtSet {
		m["createdAt"] = job.CreatedAt
	}
	if job.UpdatedAt != "" || job.updatedAtSet {
		m["updatedAt"] = job.UpdatedAt
	}
	if job.Price != nil && job.Price.Dollars != nil {
		m["dollars"] = *job.Price.Dollars
	}
	return m
}

// prodiaJobBodyOrder is the key order of the TS job body literals
// ({type, config: {prompt, seed, resolution}} for video, {type, config:
// {prompt, width, height, seed, steps, style_preset, loras, progressive}}
// for images and
// {prompt, include_messages, aspect_ratio} for the language model).
var prodiaJobBodyOrder = map[string][]string{
	"":       {"type", "config"},
	"config": {"prompt", "width", "height", "seed", "steps", "style_preset", "loras", "progressive", "resolution", "include_messages", "aspect_ratio"},
}

// buildMultipartJobRequest creates a multipart/form-data request body with:
//   - a "job" part containing the JSON-encoded jobBody
//   - an optional "input" part with binary data (skipped when inputData is nil)
//
// Returns the body buffer, the full Content-Type header string (including boundary),
// and any error.
func buildMultipartJobRequest(jobBody interface{}, inputData []byte, inputMIME string) (*bytes.Buffer, string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	// Job part (application/json)
	jobPart, err := providerutils.CreateFormDataFile(mw, "job", "job.json", "application/json")
	if err != nil {
		return nil, "", fmt.Errorf("failed to create job part: %w", err)
	}
	// TS JSON.stringify(body): insertion key order, no HTML escaping and no
	// trailing newline (json.Encoder adds one and escapes <, > and &).
	if _, err := jobPart.Write([]byte(providerutils.StringifyOrdered(jobBody, prodiaJobBodyOrder))); err != nil {
		return nil, "", fmt.Errorf("failed to encode job body: %w", err)
	}

	// Input part (optional): TS appends it for any image, even an empty
	// one (`if (imageBytes)` is true for an empty Uint8Array); nil means
	// no image.
	if inputData != nil {
		if inputMIME == "" {
			inputMIME = "application/octet-stream"
		}
		ext := mediaTypeToExt(inputMIME)
		inputPart, err := providerutils.CreateFormDataFile(mw, "input", "input"+ext, inputMIME)
		if err != nil {
			return nil, "", fmt.Errorf("failed to create input part: %w", err)
		}
		if _, err := inputPart.Write(inputData); err != nil {
			return nil, "", fmt.Errorf("failed to write input data: %w", err)
		}
	}

	if err := mw.Close(); err != nil {
		return nil, "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	return &buf, mw.FormDataContentType(), nil
}

// postMultipartToProdia sends a multipart/form-data POST to /job?price=true
// through the provider's HTTP client, so it carries the provider headers (the
// bearer token, custom headers and the "ai-sdk-prodia/<version>" user agent,
// TS getHeaders) and reports a transport failure like every other call.
// acceptHeader is the value for the Accept request header (e.g.
// "multipart/form-data" for the language model, or
// "multipart/form-data; video/mp4" for the video model).
// Returns the response body bytes, response headers, and any error.
func postMultipartToProdia(ctx context.Context, client *internalhttp.Client, body *bytes.Buffer, contentType, acceptHeader string, callHeaders map[string]string) ([]byte, http.Header, error) {
	resp, err := client.Do(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/job",
		Query:   map[string]string{"price": "true"},
		Body:    body,
		Headers: internalhttp.MergeHeaders(callHeaders, map[string]string{"Content-Type": contentType, "Accept": acceptHeader}),
	})
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, newProdiaProviderError(resp.StatusCode, resp.Body, resp.Headers)
	}
	return resp.Body, resp.Headers, nil
}

// mediaTypeToExt returns the canonical file extension for a given MIME type.
func mediaTypeToExt(mimeType string) string {
	switch mimeType {
	case "image/png":
		return ".png"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	default:
		return ""
	}
}

// prodiaOutput is one "output" part of a multipart job response.
type prodiaOutput struct {
	mediaType string
	data      []byte
	isText    bool
}

// parseMultipartOutputs reads every "output" part like the TS language model:
// text parts (text/* or a .txt filename) and image parts are kept in order,
// anything else is ignored.
func parseMultipartOutputs(contentType string, body []byte) (*prodiaJobResponse, []prodiaOutput, error) {
	boundary, ok := prodiaMultipartBoundary(contentType)
	if !ok {
		return nil, nil, errMissingBoundary(contentType)
	}
	var jobResp *prodiaJobResponse
	var outputs []prodiaOutput
	for _, part := range parseProdiaMultipart(body, boundary) {
		disposition := part.headers["content-disposition"]
		partCT := part.headers["content-type"]
		switch {
		case strings.Contains(disposition, `name="job"`):
			j, err := parseJobPart(part.body)
			if err != nil {
				return nil, nil, err
			}
			jobResp = j
		case strings.Contains(disposition, `name="output"`):
			switch {
			case strings.HasPrefix(partCT, "text/") || strings.Contains(disposition, ".txt"):
				outputs = append(outputs, prodiaOutput{mediaType: partCT, data: part.body, isText: true})
			case strings.HasPrefix(partCT, "image/"):
				outputs = append(outputs, prodiaOutput{mediaType: partCT, data: part.body})
			}
		}
	}
	if jobResp == nil {
		return nil, nil, errMissingJobPart
	}
	return jobResp, outputs, nil
}
