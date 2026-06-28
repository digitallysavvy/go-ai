package googlevertex

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

type TranscriptionModel struct {
	provider *Provider
	modelID  string
}

func NewTranscriptionModel(p *Provider, modelID string) *TranscriptionModel {
	return &TranscriptionModel{provider: p, modelID: modelID}
}

func (m *TranscriptionModel) SpecificationVersion() string { return "v4" }
func (m *TranscriptionModel) Provider() string             { return "google.vertex.transcription" }
func (m *TranscriptionModel) ModelID() string              { return m.modelID }

func (m *TranscriptionModel) DoTranscribe(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	if opts == nil {
		opts = &provider.TranscriptionOptions{}
	}
	vertexOpts := vertexTranscriptionOptions(opts.ProviderOptions)
	region := vertexOpts.Region
	if region == "" {
		region = m.provider.config.Location
	}
	if region == "" {
		region = "global"
	}
	languages := vertexOpts.LanguageCodes
	if len(languages) == 0 {
		if opts.Language != "" {
			languages = []string{opts.Language}
		} else {
			languages = []string{"auto"}
		}
	}
	enableWordTimes := true
	if vertexOpts.EnableWordTimeOffsets != nil {
		enableWordTimes = *vertexOpts.EnableWordTimeOffsets
	}
	enablePunctuation := true
	if vertexOpts.EnableAutomaticPunctuation != nil {
		enablePunctuation = *vertexOpts.EnableAutomaticPunctuation
	}

	content := opts.AudioBase64
	if content == "" {
		content = base64.StdEncoding.EncodeToString(opts.Audio)
	}

	body := map[string]interface{}{
		"config": map[string]interface{}{
			"model":              m.modelID,
			"languageCodes":      languages,
			"autoDecodingConfig": map[string]interface{}{},
			"features": map[string]interface{}{
				"enableWordTimeOffsets":      enableWordTimes,
				"enableAutomaticPunctuation": enablePunctuation,
			},
		},
		"content": content,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, vertexSpeechRecognizeURL(m.provider.config.Project, region), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	headers := vertexTranscriptionHeaders(m.provider.config, opts.Headers)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := m.provider.client.HTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("google vertex transcription request failed: %d %s", resp.StatusCode, string(respBody))
	}

	var parsed vertexTranscriptionResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode vertex transcription response: %w", err)
	}
	return convertVertexTranscriptionResponse(parsed, m.modelID, resp.Header, respBody), nil
}

type vertexTranscriptionProviderOptions struct {
	Region                     string
	LanguageCodes              []string
	EnableWordTimeOffsets      *bool
	EnableAutomaticPunctuation *bool
}

func vertexTranscriptionOptions(providerOptions map[string]interface{}) vertexTranscriptionProviderOptions {
	for _, key := range []string{"googleVertex", "vertex", "google"} {
		raw, ok := providerOptions[key].(map[string]interface{})
		if !ok {
			continue
		}
		var out vertexTranscriptionProviderOptions
		if region, ok := raw["region"].(string); ok {
			out.Region = region
		}
		if values, ok := raw["languageCodes"].([]string); ok {
			out.LanguageCodes = values
		} else if values, ok := raw["languageCodes"].([]interface{}); ok {
			for _, v := range values {
				if s, ok := v.(string); ok {
					out.LanguageCodes = append(out.LanguageCodes, s)
				}
			}
		}
		if v, ok := raw["enableWordTimeOffsets"].(bool); ok {
			out.EnableWordTimeOffsets = &v
		}
		if v, ok := raw["enableAutomaticPunctuation"].(bool); ok {
			out.EnableAutomaticPunctuation = &v
		}
		return out
	}
	return vertexTranscriptionProviderOptions{}
}

func vertexTranscriptionHeaders(cfg Config, extra map[string]string) map[string]string {
	headers := map[string]string{"Content-Type": "application/json"}
	if cfg.APIKey != "" {
		headers["x-goog-api-key"] = cfg.APIKey
	}
	headers = version.WithUserAgentSuffix(internalhttp.MergeHeaders(headers, cfg.Headers, extra), version.ProviderUserAgent("google-vertex"))
	return headers
}

func vertexSpeechRecognizeURL(project, region string) string {
	host := "speech.googleapis.com"
	if region != "global" {
		host = region + "-speech.googleapis.com"
	}
	return fmt.Sprintf("https://%s/v2/projects/%s/locations/%s/recognizers/_:recognize", host, project, region)
}

type vertexTranscriptionResponse struct {
	Results []struct {
		Alternatives []struct {
			Transcript string `json:"transcript"`
			Words      []struct {
				Word        string `json:"word"`
				StartOffset string `json:"startOffset"`
				EndOffset   string `json:"endOffset"`
			} `json:"words"`
		} `json:"alternatives"`
		LanguageCode string `json:"languageCode"`
	} `json:"results"`
	Metadata struct {
		TotalBilledDuration string `json:"totalBilledDuration"`
	} `json:"metadata"`
}

func convertVertexTranscriptionResponse(resp vertexTranscriptionResponse, modelID string, headers http.Header, rawBody []byte) *types.TranscriptionResult {
	var texts []string
	var segments []types.TranscriptionTimestamp
	language := ""
	for _, result := range resp.Results {
		if language == "" {
			language = iso6391(result.LanguageCode)
		}
		if len(result.Alternatives) == 0 {
			continue
		}
		alt := result.Alternatives[0]
		if alt.Transcript != "" {
			texts = append(texts, alt.Transcript)
		}
		for _, word := range alt.Words {
			start, okStart := parseGoogleDuration(word.StartOffset)
			end, okEnd := parseGoogleDuration(word.EndOffset)
			if word.Word != "" && okStart && okEnd {
				segments = append(segments, types.TranscriptionTimestamp{Text: word.Word, Start: start, End: end})
			}
		}
	}
	var duration *float64
	if d, ok := parseGoogleDuration(resp.Metadata.TotalBilledDuration); ok {
		duration = &d
	}
	var responseBody interface{}
	if len(rawBody) > 0 {
		if err := json.Unmarshal(rawBody, &responseBody); err != nil {
			responseBody = string(rawBody)
		}
	}
	return &types.TranscriptionResult{
		Text:              strings.TrimSpace(strings.Join(texts, " ")),
		Segments:          segments,
		Timestamps:        segments,
		Language:          language,
		DurationInSeconds: duration,
		Response: &types.ResponseMetadata{
			Timestamp: time.Now(),
			ModelID:   modelID,
			Headers:   providerutils.ExtractHeaders(headers),
			Body:      responseBody,
		},
	}
}

func parseGoogleDuration(value string) (float64, bool) {
	value = strings.TrimSuffix(value, "s")
	if value == "" {
		return 0, false
	}
	seconds, err := strconv.ParseFloat(value, 64)
	return seconds, err == nil
}

func iso6391(value string) string {
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "cmn-") {
		return "zh"
	}
	parts := strings.Split(value, "-")
	if len(parts[0]) == 2 {
		return parts[0]
	}
	return ""
}
