package prodia

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ProdiaLanguageModel implements the provider.LanguageModel interface for the
// Prodia img2img inference model (inference.nano-banana.img2img.v2).
//
// It accepts a text prompt (and an optional input image) via multipart
// form-data and returns a generated image together with an optional text
// description.  Because Prodia does not expose SSE for this endpoint,
// DoStream wraps DoGenerate and emits the result as a synchronous chunk
// sequence.
type ProdiaLanguageModel struct {
	prov    *Provider
	modelID string
}

// NewLanguageModel creates a new ProdiaLanguageModel for the given model ID.
func NewLanguageModel(prov *Provider, modelID string) *ProdiaLanguageModel {
	return &ProdiaLanguageModel{prov: prov, modelID: modelID}
}

// SpecificationVersion returns "v4" to match the TypeScript AI SDK specification.
func (m *ProdiaLanguageModel) SpecificationVersion() string { return "v4" }

// Provider returns the provider identifier for this model type.
// Matches the TypeScript SDK's config.provider value: "prodia.language".
func (m *ProdiaLanguageModel) Provider() string { return "prodia.language" }

// ModelID returns the model identifier.
func (m *ProdiaLanguageModel) ModelID() string { return m.modelID }

// SupportsTools returns false — Prodia img2img does not support tool calling.
func (m *ProdiaLanguageModel) SupportsTools() bool { return false }

// SupportsStructuredOutput returns false — the model does not support JSON
// schema output.
func (m *ProdiaLanguageModel) SupportsStructuredOutput() bool { return false }

// SupportsImageInput returns true — the model accepts an image as input.
func (m *ProdiaLanguageModel) SupportsImageInput() bool { return true }

// ProdiaLanguageProviderOptions contains Prodia-specific options for the
// language (img2img) model.
type ProdiaLanguageProviderOptions struct {
	// AspectRatio for the output image.
	// Must be one of: 1:1, 2:3, 3:2, 4:5, 5:4, 4:7, 7:4, 9:16, 16:9, 9:21, 21:9.
	AspectRatio string `json:"aspectRatio,omitempty"`
}

// extractLanguageProviderOptions reads Prodia-specific options from the
// provider options map supplied in GenerateOptions.
func extractLanguageProviderOptions(opts *provider.GenerateOptions) *ProdiaLanguageProviderOptions {
	if opts.ProviderOptions == nil {
		return nil
	}
	raw, ok := opts.ProviderOptions["prodia"]
	if !ok || raw == nil {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var o ProdiaLanguageProviderOptions
	if err := json.Unmarshal(b, &o); err != nil {
		return nil
	}
	return &o
}

// DoGenerate sends the prompt (and optional image) to the Prodia API using
// multipart form-data and returns the generated content.
func (m *ProdiaLanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	var warnings []types.Warning

	// Warn about features this provider does not support.
	if opts.Temperature != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Message: "temperature is not supported"})
	}
	if opts.TopP != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Message: "topP is not supported"})
	}
	if opts.TopK != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Message: "topK is not supported"})
	}
	if opts.MaxTokens != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Message: "maxOutputTokens is not supported"})
	}
	if len(opts.StopSequences) > 0 {
		warnings = append(warnings, types.Warning{Type: "unsupported", Message: "stopSequences is not supported"})
	}
	if opts.PresencePenalty != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Message: "presencePenalty is not supported"})
	}
	if opts.FrequencyPenalty != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Message: "frequencyPenalty is not supported"})
	}
	if len(opts.Tools) > 0 {
		warnings = append(warnings, types.Warning{Type: "unsupported", Message: "tools are not supported"})
	}
	if opts.ToolChoice.Type != "" {
		warnings = append(warnings, types.Warning{Type: "unsupported", Message: "toolChoice is not supported"})
	}
	if opts.ResponseFormat != nil && opts.ResponseFormat.Type != "text" {
		warnings = append(warnings, types.Warning{Type: "unsupported", Message: "responseFormat is not supported"})
	}
	if opts.Reasoning != nil && *opts.Reasoning != types.ReasoningDefault {
		warnings = append(warnings, types.Warning{Type: "unsupported", Message: "reasoning is not supported"})
	}

	provOpts := extractLanguageProviderOptions(opts)

	// Validate aspect ratio if provided.
	if provOpts != nil && provOpts.AspectRatio != "" {
		if !validAspectRatios[provOpts.AspectRatio] {
			return nil, fmt.Errorf("prodia: unsupported aspectRatio %q; valid values: 1:1, 2:3, 3:2, 4:5, 5:4, 4:7, 7:4, 9:16, 16:9, 9:21, 21:9", provOpts.AspectRatio)
		}
	}

	// Extract text prompt and optional image from the message history.
	prompt, imageData, imageMIME := extractPromptAndImage(opts.Prompt)

	jobConfig := map[string]interface{}{
		"prompt":           prompt,
		"include_messages": true,
	}
	if provOpts != nil && provOpts.AspectRatio != "" {
		jobConfig["aspect_ratio"] = provOpts.AspectRatio
	}

	reqBody := map[string]interface{}{
		"type":   m.modelID,
		"config": jobConfig,
	}

	// Build the multipart request (img2img always uses multipart/form-data).
	buf, contentType, err := buildMultipartJobRequest(reqBody, imageData, imageMIME)
	if err != nil {
		return nil, fmt.Errorf("prodia: failed to build multipart request: %w", err)
	}

	reqURL := fmt.Sprintf("%s/job?price=true", m.prov.effectiveBaseURL())
	respBody, respHeaders, err := postMultipartToProdia(ctx, reqURL, m.prov.effectiveAPIKey(), buf, contentType, "multipart/form-data")
	if err != nil {
		return nil, fmt.Errorf("prodia: %w", err)
	}

	// Derive the Content-Type with boundary from the response header.
	// Fall back to scanning the body when the header is absent.
	respCT := respHeaders.Get("Content-Type")
	if respCT == "" {
		respCT, err = detectMultipartContentType(respBody)
		if err != nil {
			return nil, fmt.Errorf("prodia: cannot detect multipart boundary: %w", err)
		}
	}

	jobResp, outputData, outputMIME, err := parseMultipartResponse(respCT, respBody)
	if err != nil {
		return nil, fmt.Errorf("prodia: %w", err)
	}

	result := &types.GenerateResult{
		FinishReason: "stop",
		Warnings:     warnings,
		ProviderMetadata: map[string]interface{}{
			"prodia": buildProdiaProviderMetadata(jobResp),
		},
	}

	if len(outputData) > 0 {
		if strings.HasPrefix(outputMIME, "text/") {
			result.Text = string(outputData)
		} else {
			if outputMIME == "" {
				outputMIME = "image/png"
			}
			result.Content = []types.ContentPart{
				types.GeneratedFileContent{
					MediaType: outputMIME,
					Data:      outputData,
				},
			}
		}
	}

	return result, nil
}

// DoStream wraps DoGenerate and emits its result as a synchronous stream.
// Prodia does not expose SSE for this model.
func (m *ProdiaLanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	gr, err := m.DoGenerate(ctx, opts)
	if err != nil {
		return nil, err
	}

	var chunks []*provider.StreamChunk

	// stream-start carries any warnings.
	chunks = append(chunks, &provider.StreamChunk{
		Type:     provider.ChunkTypeStreamStart,
		Warnings: gr.Warnings,
	})

	// Text block.
	if gr.Text != "" {
		const blockID = "text-0"
		chunks = append(chunks,
			&provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: blockID},
			&provider.StreamChunk{Type: provider.ChunkTypeText, ID: blockID, Text: gr.Text},
			&provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: blockID},
		)
	}

	// File blocks (generated images).
	for _, part := range gr.Content {
		if fc, ok := part.(types.GeneratedFileContent); ok {
			cp := fc
			chunks = append(chunks, &provider.StreamChunk{
				Type:                 provider.ChunkTypeFile,
				GeneratedFileContent: &cp,
			})
		}
	}

	// Finish chunk — mirror TS: include providerMetadata on the finish event.
	var finishMeta json.RawMessage
	if gr.ProviderMetadata != nil {
		if b, err := json.Marshal(gr.ProviderMetadata); err == nil {
			finishMeta = b
		}
	}
	chunks = append(chunks, &provider.StreamChunk{
		Type:             provider.ChunkTypeFinish,
		FinishReason:     gr.FinishReason,
		Usage:            &gr.Usage,
		ProviderMetadata: finishMeta,
	})

	return &prodiaTextStream{chunks: chunks}, nil
}

// extractPromptAndImage walks the message prompt and returns:
//   - the combined text prompt (system message prepended to last user text)
//   - the raw bytes of the first image found in the last user message (nil if none)
//   - the MIME type of that image (empty if none)
func extractPromptAndImage(prompt types.Prompt) (string, []byte, string) {
	if prompt.IsSimple() {
		return prompt.Text, nil, ""
	}

	var systemMsg string
	var userText string
	var imageData []byte
	var imageMIME string

	// Collect the system message.
	for _, msg := range prompt.Messages {
		if msg.Role == types.RoleSystem {
			for _, part := range msg.Content {
				if tc, ok := part.(types.TextContent); ok {
					systemMsg = tc.Text
				}
			}
		}
	}

	// Find the last user message and extract text + image.
	for i := len(prompt.Messages) - 1; i >= 0; i-- {
		msg := prompt.Messages[i]
		if msg.Role != types.RoleUser {
			continue
		}
		for _, part := range msg.Content {
			switch p := part.(type) {
			case types.TextContent:
				if userText != "" {
					userText += "\n"
				}
				userText += p.Text
			case types.FileContent:
				if imageData == nil && strings.HasPrefix(p.MimeType, "image/") {
					imageData = p.Data
					imageMIME = p.MimeType
				}
			case types.ImageContent:
				if imageData == nil {
					imageData = p.Image
					imageMIME = p.MimeType
					if imageMIME == "" {
						imageMIME = "image/png"
					}
				}
			}
		}
		break
	}

	combined := userText
	if systemMsg != "" {
		combined = systemMsg + "\n" + userText
	}
	return combined, imageData, imageMIME
}

// detectMultipartContentType scans the raw body for the multipart boundary
// marker and synthesises a Content-Type header value that parseMultipartResponse
// can consume.  Used as a fallback when the response Content-Type header is
// absent.
func detectMultipartContentType(body []byte) (string, error) {
	// The first line of a well-formed multipart body begins with "--<boundary>".
	if len(body) < 4 || body[0] != '-' || body[1] != '-' {
		return "", fmt.Errorf("body does not start with multipart boundary marker")
	}
	end := 2
	for end < len(body) && body[end] != '\r' && body[end] != '\n' {
		end++
	}
	boundary := string(body[2:end])
	if boundary == "" {
		return "", fmt.Errorf("empty boundary in multipart body")
	}
	return "multipart/form-data; boundary=" + boundary, nil
}

// prodiaTextStream is a synchronous provider.TextStream backed by a pre-built
// slice of StreamChunks.  Used by ProdiaLanguageModel.DoStream.
type prodiaTextStream struct {
	chunks []*provider.StreamChunk
	pos    int
	closed bool
}

func (s *prodiaTextStream) Next() (*provider.StreamChunk, error) {
	if s.closed || s.pos >= len(s.chunks) {
		return nil, io.EOF
	}
	chunk := s.chunks[s.pos]
	s.pos++
	return chunk, nil
}

func (s *prodiaTextStream) Err() error   { return nil }
func (s *prodiaTextStream) Close() error { s.closed = true; return nil }
