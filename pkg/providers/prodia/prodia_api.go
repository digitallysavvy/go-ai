package prodia

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
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

// parseMultipartResponse parses a multipart/form-data response from the Prodia API.
// It extracts the "job" JSON metadata part and the "output" binary data part.
// Returns the job metadata, output bytes, output MIME type, and any error.
func parseMultipartResponse(contentType string, body []byte) (*prodiaJobResponse, []byte, string, error) {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to parse response Content-Type %q: %w", contentType, err)
	}

	boundary, ok := params["boundary"]
	if !ok {
		return nil, nil, "", fmt.Errorf("multipart response missing boundary in Content-Type %q", contentType)
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
			var j prodiaJobResponse
			if err := json.Unmarshal(partData, &j); err != nil {
				return nil, nil, "", fmt.Errorf("failed to parse job metadata: %w", err)
			}
			jobResp = &j
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
			// Fallback: treat any part whose Content-Type starts with "video/" as output.
			if outputData == nil && partCT != "" {
				if mt, _, err2 := mime.ParseMediaType(partCT); err2 == nil {
					if len(mt) >= 6 && mt[:6] == "video/" {
						outputData = partData
						outputMIME = mt
					}
				}
			}
		}
	}

	if jobResp == nil {
		return nil, nil, "", fmt.Errorf("multipart response missing 'job' part")
	}

	return jobResp, outputData, outputMIME, nil
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
	if job.CreatedAt != "" {
		m["createdAt"] = job.CreatedAt
	}
	if job.UpdatedAt != "" {
		m["updatedAt"] = job.UpdatedAt
	}
	if job.Price != nil && job.Price.Dollars != nil {
		m["dollars"] = *job.Price.Dollars
	}
	return m
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
	if err := json.NewEncoder(jobPart).Encode(jobBody); err != nil {
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

// postMultipartToProdia sends a multipart/form-data POST to the Prodia API.
// acceptHeader is the value for the Accept request header (e.g.
// "multipart/form-data" for the language model, or
// "multipart/form-data; video/mp4" for the video model).
// Returns the response body bytes, response headers, and any error.
func postMultipartToProdia(ctx context.Context, reqURL, apiKey string, body *bytes.Buffer, contentType, acceptHeader string) ([]byte, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create multipart request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", acceptHeader)

	resp, err := internalhttp.DefaultHTTPClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read response body: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, nil, fmt.Errorf("prodia API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, resp.Header, nil
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
