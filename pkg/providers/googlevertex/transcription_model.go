package googlevertex

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/gemini"
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
	vertexOpts, err := vertexTranscriptionOptionsChecked(opts.ProviderOptions)
	if err != nil {
		return nil, err
	}
	region := vertexOpts.Region
	regionArg := "location"
	if vertexOpts.hasRegion {
		regionArg = "region"
	} else {
		region = m.provider.location()
	}
	if region == "" && !vertexOpts.hasRegion {
		region = "global"
	}
	// region is interpolated directly into the Cloud Speech-to-Text request
	// host (https://{region}-speech.googleapis.com/...), so only a single
	// DNS label is accepted. Ports TS google-vertex-transcription-model.ts
	// (TS #21842).
	if !providerutils.IsValidHostnamePart(region) {
		return nil, &providererrors.InvalidArgumentError{
			Field:   regionArg,
			Message: "Invalid Google Cloud Speech-to-Text region. Expected a single DNS label (letters, digits, and hyphens).",
		}
	}
	languages := vertexOpts.LanguageCodes
	if languages == nil {
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, vertexSpeechRecognizeURL(m.provider.project(), region), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	headers := vertexTranscriptionHeaders(m.provider.config, opts.Headers)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := m.provider.mainClient().HTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, gemini.GoogleHTTPError(m.Provider(), resp.StatusCode, resp.Header, respBody, nil)
	}

	// TS createJsonResponseHandler(googleVertexTranscriptionResponseSchema).
	if err := vertexValidator(m.Provider(), zVertexTranscription)(resp.StatusCode, resp.Header, respBody); err != nil {
		return nil, err
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
	hasRegion                  bool
}

// vertexTranscriptionOptionsChecked is the zod-validating form of
// googleVertexTranscriptionProviderOptionsSchema: a present value of the wrong
// type fails with an InvalidArgumentError. Empty languageCodes stay non-nil
// because TS forwards `languageCodes: []`.
func vertexTranscriptionOptionsChecked(providerOptions map[string]interface{}) (vertexTranscriptionProviderOptions, error) {
	invalid := func(cause error) error {
		return &providererrors.InvalidArgumentError{Field: "providerOptions", Message: "invalid google provider options", Cause: cause}
	}
	for _, key := range []string{"googleVertex", "vertex", "google"} {
		rawAny, present := providerOptions[key]
		if !present || rawAny == nil {
			continue
		}
		raw, ok := rawAny.(map[string]interface{})
		if !ok {
			return vertexTranscriptionProviderOptions{}, invalid(fmt.Errorf("%s: expected object, received %T", key, rawAny))
		}
		var out vertexTranscriptionProviderOptions
		if v, has := raw["region"]; has && v != nil {
			region, isStr := v.(string)
			if !isStr {
				return out, invalid(fmt.Errorf("region: expected string, received %T", v))
			}
			out.Region = region
			out.hasRegion = true
		}
		if v, has := raw["languageCodes"]; has && v != nil {
			switch values := v.(type) {
			case []string:
				out.LanguageCodes = append([]string{}, values...)
			case []interface{}:
				out.LanguageCodes = make([]string, 0, len(values))
				for i, item := range values {
					str, isStr := item.(string)
					if !isStr {
						return out, invalid(fmt.Errorf("languageCodes[%d]: expected string, received %T", i, item))
					}
					out.LanguageCodes = append(out.LanguageCodes, str)
				}
			default:
				return out, invalid(fmt.Errorf("languageCodes: expected array, received %T", v))
			}
		}
		for _, name := range []string{"enableWordTimeOffsets", "enableAutomaticPunctuation"} {
			v, has := raw[name]
			if !has || v == nil {
				continue
			}
			b, isBool := v.(bool)
			if !isBool {
				return out, invalid(fmt.Errorf("%s: expected boolean, received %T", name, v))
			}
			if name == "enableWordTimeOffsets" {
				out.EnableWordTimeOffsets = &b
			} else {
				out.EnableAutomaticPunctuation = &b
			}
		}
		return out, nil
	}
	return vertexTranscriptionProviderOptions{}, nil
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
	if len(resp.Results) > 0 {
		// TS: convertBcp47ToIso6391(results[0]?.languageCode).
		language = iso6391(resp.Results[0].LanguageCode)
	}
	for _, result := range resp.Results {
		// TS joins every result's primary transcript ('' when absent), so an
		// empty one still contributes a separator.
		if len(result.Alternatives) == 0 {
			texts = append(texts, "")
			continue
		}
		alt := result.Alternatives[0]
		texts = append(texts, alt.Transcript)
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
		Text:              providerutils.JSTrim(strings.Join(texts, " ")),
		Segments:          segments,
		Timestamps:        segments,
		Language:          language,
		DurationInSeconds: duration,
		// TS always returns `warnings: []` (an empty array is declared and
		// never populated in GoogleVertexTranscriptionModel.doGenerate) --
		// matched explicitly here rather than leaving Warnings nil, since
		// the shared Go type's `omitempty` tag would otherwise drop the key
		// entirely instead of emitting an empty array like TS does.
		Warnings: []types.Warning{},
		Response: &types.ResponseMetadata{
			Timestamp: time.Now(),
			ModelID:   modelID,
			Headers:   providerutils.ExtractHeaders(headers),
			Body:      responseBody,
		},
	}
}

// parseGoogleDuration is TS Number.parseFloat for a Speech-to-Text duration
// such as "1.200s": the longest leading decimal prefix, finite only.
func parseGoogleDuration(value string) (float64, bool) {
	m := leadingFloatRE.FindString(strings.TrimLeft(value, " \t\r\n"))
	if m == "" {
		return 0, false
	}
	seconds, err := strconv.ParseFloat(m, 64)
	if err != nil || math.IsInf(seconds, 0) || math.IsNaN(seconds) {
		return 0, false
	}
	return seconds, true
}

var leadingFloatRE = regexp.MustCompile(`^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?`)

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
