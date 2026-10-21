package prodia

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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
// the video model's handler and "image/" parts in the image model's.
func parseMultipartResponseFor(contentType string, body []byte, fallbackPrefix string) (*prodiaJobResponse, []byte, string, error) {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, nil, "", errMissingBoundary(contentType)
	}

	boundary, ok := params["boundary"]
	if !ok {
		return nil, nil, "", errMissingBoundary(contentType)
	}

	mr := multipart.NewReader(bytes.NewReader(body), boundary)
	var jobResp *prodiaJobResponse
	var outputData []byte
	var outputMIME string

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, "", fmt.Errorf("failed to read multipart part: %w", err)
		}

		partData, err := io.ReadAll(part)
		if err != nil {
			return nil, nil, "", fmt.Errorf("failed to read multipart part data: %w", err)
		}

		partCT := part.Header.Get("Content-Type")
		switch part.FormName() {
		case "job":
			j, err := parseJobPart(partData)
			if err != nil {
				return nil, nil, "", err
			}
			jobResp = j
		case "output":
			outputData = partData
			if partCT != "" {
				if mt, _, err := mime.ParseMediaType(partCT); err == nil {
					outputMIME = mt
				}
			}
			// If Content-Type is absent or ambiguous, fall back to the
			// filename extension in Content-Disposition (e.g. "output.txt").
			// Matches TS: contentDisposition.includes('.txt').
			if outputMIME == "" {
				cd := part.Header.Get("Content-Disposition")
				if strings.Contains(cd, ".txt") {
					outputMIME = "text/plain"
				}
			}
		default:
			// Fallback: treat any part whose Content-Type starts with the
			// output prefix as output; as in TS a later such part replaces an
			// earlier output.
			if partCT != "" {
				if mt, _, err2 := mime.ParseMediaType(partCT); err2 == nil {
					if strings.HasPrefix(mt, fallbackPrefix) {
						outputData = partData
						outputMIME = mt
					}
				}
			}
		}
	}

	if jobResp == nil {
		return nil, nil, "", errMissingJobPart
	}

	return jobResp, outputData, outputMIME, nil
}

// parseJobPart decodes the multipart "job" part, applying TS
// prodiaJobResultSchema first: a body that does not match is the typed
// validation (or JSON parse) error itself, as TS parseJSON({schema}) throws it.
func parseJobPart(data []byte) (*prodiaJobResponse, error) {
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
// ({type, config: {prompt, seed, resolution}} for video and
// {prompt, include_messages, aspect_ratio} for the language model).
var prodiaJobBodyOrder = map[string][]string{
	"":       {"type", "config"},
	"config": {"prompt", "seed", "resolution", "include_messages", "aspect_ratio"},
}

// buildMultipartJobRequest creates a multipart/form-data request body with:
//   - a "job" part containing the JSON-encoded jobBody
//   - an optional "input" part with binary data (skipped when inputData is empty)
//
// Returns the body buffer, the full Content-Type header string (including boundary),
// and any error.
func buildMultipartJobRequest(jobBody interface{}, inputData []byte, inputMIME string) (*bytes.Buffer, string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	// Job part (application/json)
	jh := make(textproto.MIMEHeader)
	jh.Set("Content-Disposition", `form-data; name="job"; filename="job.json"`)
	jh.Set("Content-Type", "application/json")
	jobPart, err := mw.CreatePart(jh)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create job part: %w", err)
	}
	// TS JSON.stringify(body): insertion key order, no HTML escaping and no
	// trailing newline (json.Encoder adds one and escapes <, > and &).
	if _, err := jobPart.Write([]byte(providerutils.StringifyOrdered(jobBody, prodiaJobBodyOrder))); err != nil {
		return nil, "", fmt.Errorf("failed to encode job body: %w", err)
	}

	// Input part (optional)
	if len(inputData) > 0 {
		if inputMIME == "" {
			inputMIME = "application/octet-stream"
		}
		ext := mediaTypeToExt(inputMIME)
		ih := make(textproto.MIMEHeader)
		ih.Set("Content-Disposition", `form-data; name="input"; filename="input`+ext+`"`)
		ih.Set("Content-Type", inputMIME)
		inputPart, err := mw.CreatePart(ih)
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
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, nil, errMissingBoundary(contentType)
	}
	boundary, ok := params["boundary"]
	if !ok {
		return nil, nil, errMissingBoundary(contentType)
	}
	mr := multipart.NewReader(bytes.NewReader(body), boundary)
	var jobResp *prodiaJobResponse
	var outputs []prodiaOutput
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read multipart part: %w", err)
		}
		data, err := io.ReadAll(part)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read multipart part data: %w", err)
		}
		partCT := part.Header.Get("Content-Type")
		switch part.FormName() {
		case "job":
			j, err := parseJobPart(data)
			if err != nil {
				return nil, nil, err
			}
			jobResp = j
		case "output":
			switch {
			case strings.HasPrefix(partCT, "text/") || strings.Contains(part.Header.Get("Content-Disposition"), ".txt"):
				outputs = append(outputs, prodiaOutput{mediaType: partCT, data: data, isText: true})
			case strings.HasPrefix(partCT, "image/"):
				outputs = append(outputs, prodiaOutput{mediaType: partCT, data: data})
			}
		}
	}
	if jobResp == nil {
		return nil, nil, errMissingJobPart
	}
	return jobResp, outputs, nil
}
