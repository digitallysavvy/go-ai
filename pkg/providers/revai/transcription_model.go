package revai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// pollTimeout and pollInterval mirror RevaiTranscriptionModel.doGenerate's
// polling loop in the TypeScript SDK.
const (
	pollTimeout  = 60 * time.Second
	pollInterval = 1 * time.Second
)

// TranscriptionModel implements provider.TranscriptionModel for Rev.ai's
// async job flow: submit a job, poll its status until it finishes, then
// fetch the transcript.
type TranscriptionModel struct {
	provider *Provider
	modelID  string
}

// SpecificationVersion returns the specification version.
func (m *TranscriptionModel) SpecificationVersion() string { return "v4" }

// Provider returns the provider name.
func (m *TranscriptionModel) Provider() string { return "revai.transcription" }

// ModelID returns the model ID.
func (m *TranscriptionModel) ModelID() string { return m.modelID }

type revaiJobResponse struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Language string `json:"language"`
}

type revaiTranscriptElement struct {
	Type  string   `json:"type"`
	Value string   `json:"value"`
	TS    *float64 `json:"ts"`
	EndTS *float64 `json:"end_ts"`
}

type revaiTranscriptMonologue struct {
	Elements []revaiTranscriptElement `json:"elements"`
}

type revaiTranscriptResponse struct {
	Monologues []revaiTranscriptMonologue `json:"monologues"`
}

// DoTranscribe submits a transcription job, polls it to completion, and
// fetches the resulting transcript.
func (m *TranscriptionModel) DoTranscribe(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	if opts == nil {
		opts = &provider.TranscriptionOptions{}
	}
	if err := validateOptions(opts.ProviderOptions); err != nil {
		return nil, err
	}
	currentDate := time.Now()

	body, contentType, err := m.buildMultipartBody(opts)
	if err != nil {
		return nil, err
	}

	var submission revaiJobResponse
	_, err = m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		ValidateResponse: providerutils.MediaResponseValidator(m.Provider(), providerutils.SchemaRevAIJob),
		Method:           http.MethodPost,
		Path:             "/speechtotext/v1/jobs",
		Body:             body,
		Headers:          internalhttp.MergeHeaders(opts.Headers, map[string]string{"Content-Type": contentType}),
	}, &submission)
	if err != nil {
		return nil, handleError(err)
	}
	if submission.Status == "failed" {
		// TS AISDKError TranscriptionJobSubmissionFailed (cause: the response).
		failure := providererrors.NewProviderError("revai", 0, "", "Failed to submit transcription job to Rev.ai", nil)
		failure.Data = submission
		return nil, failure
	}

	// TS polls only while the job is not yet "transcribed".
	if submission.Status != "transcribed" {
		if _, err := m.pollJobStatus(ctx, submission.ID, opts.Headers); err != nil {
			return nil, err
		}
	}

	var transcript revaiTranscriptResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		ValidateResponse: providerutils.MediaResponseValidator(m.Provider(), providerutils.SchemaRevAITranscript),
		Method:           http.MethodGet,
		Path:             fmt.Sprintf("/speechtotext/v1/jobs/%s/transcript", submission.ID),
		Headers:          opts.Headers,
	}, &transcript)
	if err != nil {
		return nil, handleError(err)
	}

	text, segments, durationInSeconds := convertRevaiTranscript(transcript)

	return &types.TranscriptionResult{
		Text:       text,
		Segments:   segments,
		Timestamps: segments,
		// TS reports the submission response's language, not the polled one.
		Language:          submission.Language,
		DurationInSeconds: &durationInSeconds,
		Warnings:          []types.Warning{},
		Usage:             types.TranscriptionUsage{DurationSeconds: durationInSeconds},
		Response: &types.ResponseMetadata{
			Timestamp: currentDate,
			ModelID:   m.modelID,
			Headers:   providerutils.ExtractHeaders(httpResp.Headers),
			Body:      providerutils.RawJSONBody(httpResp.Body),
		},
	}, nil
}

// pollJobStatus polls the job status endpoint until it reaches "transcribed"
// or "failed", or pollTimeout elapses. The URL is built from this provider's
// own configured base URL with a job ID substituted into a fixed path
// template (not taken verbatim from a provider response), but polling
// through fileutil.PollJSON with a trusted-origin validator still guards
// against a compromised or misbehaving redirect hop.
func (m *TranscriptionModel) pollJobStatus(ctx context.Context, jobID string, headers map[string]string) (*revaiJobResponse, error) {
	pollOpts := fileutil.TrustedOriginDownloadOptions(m.provider.config.BaseURL, m.provider.client.HTTPClient().Transport)
	pollOpts.Headers = version.WithUserAgentSuffix(internalhttp.MergeHeaders(map[string]string{"Authorization": "Bearer " + providerutils.APIKeyOrEnv(m.provider.config.APIKey, "REVAI_API_KEY")}, m.provider.config.Headers, headers), version.ProviderUserAgent("revai"))

	pollURL := m.provider.config.BaseURL + "/speechtotext/v1/jobs/" + jobID
	start := time.Now()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		if time.Since(start) > pollTimeout {
			// TS AISDKError TranscriptionJobPollingTimedOut.
			return nil, providererrors.NewProviderError("revai", 0, "TranscriptionJobPollingTimedOut", "Transcription job polling timed out", nil)
		}

		download, err := fileutil.DownloadWithMetadata(ctx, pollURL, pollOpts)
		if err != nil {
			return nil, handleError(err)
		}
		// TS createJsonResponseHandler(revaiTranscriptionJobResponseSchema).
		if err := providerutils.MediaResponseValidator("revai.transcription", providerutils.SchemaRevAIJob)(http.StatusOK, http.Header(download.Headers), download.Data); err != nil {
			return nil, err
		}
		var status revaiJobResponse
		if err := json.Unmarshal(download.Data, &status); err != nil {
			return nil, fmt.Errorf("revai: failed to decode job status: %w", err)
		}

		switch status.Status {
		case "transcribed":
			return &status, nil
		case "failed":
			// TS AISDKError TranscriptionJobFailed (cause: the job response).
			failure := providererrors.NewProviderError("revai", 0, "", "Transcription job failed", nil)
			failure.Data = status
			return nil, failure
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

func (m *TranscriptionModel) buildMultipartBody(opts *provider.TranscriptionOptions) (*bytes.Buffer, string, error) {
	revaiOpts := extractOptions(opts.ProviderOptions)

	audio := opts.Audio
	if len(audio) == 0 && opts.AudioBase64 != "" {
		decoded, err := types.DecodeFileDataString(opts.AudioBase64)
		if err != nil {
			return nil, "", fmt.Errorf("revai: failed to decode base64 audio: %w", err)
		}
		audio = decoded
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := providerutils.CreateTypedFormFile(writer, "media", "audio."+revaiAudioExtension(opts.MimeType), opts.MimeType)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(audio); err != nil {
		return nil, "", err
	}

	config := map[string]interface{}{"transcriber": m.modelID}
	if revaiOpts != nil {
		b, err := json.Marshal(revaiOpts)
		if err != nil {
			return nil, "", err
		}
		var fields map[string]interface{}
		if err := json.Unmarshal(b, &fields); err != nil {
			return nil, "", err
		}
		for k, v := range fields {
			config[k] = v
		}
	}
	configBytes, err := json.Marshal(config)
	if err != nil {
		return nil, "", err
	}
	if err := writer.WriteField("config", string(configBytes)); err != nil {
		return nil, "", err
	}

	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return &buf, writer.FormDataContentType(), nil
}

func revaiAudioExtension(mimeType string) string {
	return providerutils.MediaTypeToExtension(mimeType)
}

// convertRevaiTranscript mirrors RevaiTranscriptionModel.doGenerate's
// monologue -> text/segments/duration conversion.
func convertRevaiTranscript(transcript revaiTranscriptResponse) (string, []types.TranscriptionTimestamp, float64) {
	var durationInSeconds float64
	segments := make([]types.TranscriptionTimestamp, 0)
	textParts := make([]string, 0, len(transcript.Monologues))

	for _, monologue := range transcript.Monologues {
		var monologueText string
		for _, el := range monologue.Elements {
			monologueText += el.Value
		}
		textParts = append(textParts, monologueText)

		var currentSegmentText string
		var segmentStart float64
		hasStartedSegment := false

		for _, el := range monologue.Elements {
			currentSegmentText += el.Value

			if el.Type != "text" {
				continue
			}
			if el.EndTS != nil && *el.EndTS > durationInSeconds {
				durationInSeconds = *el.EndTS
			}
			if !hasStartedSegment && el.TS != nil {
				segmentStart = *el.TS
				hasStartedSegment = true
			}
			if el.EndTS != nil && hasStartedSegment {
				if trimmed := strings.TrimSpace(currentSegmentText); trimmed != "" {
					segments = append(segments, types.TranscriptionTimestamp{
						Text:  trimmed,
						Start: segmentStart,
						End:   *el.EndTS,
					})
				}
				currentSegmentText = ""
				hasStartedSegment = false
			}
		}

		if hasStartedSegment {
			if trimmed := strings.TrimSpace(currentSegmentText); trimmed != "" {
				end := segmentStart + 1
				if durationInSeconds > segmentStart {
					end = durationInSeconds
				}
				segments = append(segments, types.TranscriptionTimestamp{
					Text:  trimmed,
					Start: segmentStart,
					End:   end,
				})
			}
		}
	}

	return strings.Join(textParts, " "), segments, durationInSeconds
}
