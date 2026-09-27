package alibaba

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/imageutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/polling"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// VideoModel implements the provider.VideoModelV3 interface for Alibaba Wan
// video models (TS AlibabaVideoModel), including the legacy (wan2.6 and
// earlier), wan2.7, and wan3 request protocols.
type VideoModel struct {
	prov    *Provider
	modelID string
}

// NewVideoModel creates a new Alibaba video generation model
func NewVideoModel(prov *Provider, modelID string) *VideoModel {
	return &VideoModel{
		prov:    prov,
		modelID: modelID,
	}
}

// SpecificationVersion returns the specification version
func (m *VideoModel) SpecificationVersion() string {
	return "v4"
}

// Provider returns the provider name
func (m *VideoModel) Provider() string {
	return "alibaba.video"
}

// ModelID returns the model ID
func (m *VideoModel) ModelID() string {
	return m.modelID
}

// MaxVideosPerCall returns 1 (Alibaba generates one video per call)
func (m *VideoModel) MaxVideosPerCall() *int {
	one := 1
	return &one
}

// detectMode determines the model mode from the model ID. Only meaningful
// for ids that name their mode (wan2.6/wan2.7); wan3 ships a single
// all-in-one id, so its mode comes from the media the request carries.
func (m *VideoModel) detectMode() string {
	if strings.Contains(m.modelID, "-i2v") {
		return "i2v"
	}
	if strings.Contains(m.modelID, "-r2v") {
		return "r2v"
	}
	return "t2v"
}

// alibabaVideoProtocol selects the request shape by model id:
//   - legacy (wan2.6 and earlier): parameters.size, input.img_url, and
//     input.reference_urls.
//   - wan27: resolution tiers and ratio instead of size, input.media instead
//     of input.reference_urls, no shot_type, audio always on.
//   - wan3: like wan27, plus a real last_frame slot, an audio toggle, a 480P
//     tier, and one id covering every mode.
type alibabaVideoProtocol string

const (
	alibabaProtocolLegacy alibabaVideoProtocol = "legacy"
	alibabaProtocolWan27  alibabaVideoProtocol = "wan27"
	alibabaProtocolWan3   alibabaVideoProtocol = "wan3"
)

func detectAlibabaVideoProtocol(modelID string) alibabaVideoProtocol {
	if strings.HasPrefix(modelID, "wan3") {
		return alibabaProtocolWan3
	}
	if strings.HasPrefix(modelID, "wan2.7") {
		return alibabaProtocolWan27
	}
	return alibabaProtocolLegacy
}

// alibabaResolutionTierMap maps SDK "WIDTHxHEIGHT" resolutions to Alibaba
// resolution tiers.
var alibabaResolutionTierMap = map[string]string{
	"1280x720":  "720P",
	"720x1280":  "720P",
	"960x960":   "720P",
	"1088x832":  "720P",
	"832x1088":  "720P",
	"1920x1080": "1080P",
	"1080x1920": "1080P",
	"1440x1440": "1080P",
	"1632x1248": "1080P",
	"1248x1632": "1080P",
	"832x480":   "480P",
	"480x832":   "480P",
	"624x624":   "480P",
}

var alibabaSupportedRatios = map[string]bool{
	"16:9": true, "9:16": true, "1:1": true, "4:3": true, "3:4": true,
}

func deriveAlibabaRatioFromResolution(resolution string) (string, bool) {
	parts := strings.SplitN(resolution, "x", 2)
	if len(parts) != 2 {
		return "", false
	}
	width, err1 := strconv.Atoi(parts[0])
	height, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || width <= 0 || height <= 0 {
		return "", false
	}
	a, b := width, height
	for b != 0 {
		a, b = b, a%b
	}
	ratio := fmt.Sprintf("%d:%d", width/a, height/a)
	if !alibabaSupportedRatios[ratio] {
		return "", false
	}
	return ratio, true
}

func alibabaFileToImageString(file *provider.VideoModelV3File) string {
	if file.Type == "url" {
		return file.URL
	}
	return imageutil.EncodeToBase64(file.Data)
}

func alibabaFileToDataURI(file *provider.VideoModelV3File) string {
	if file.Type == "url" {
		return file.URL
	}
	return imageutil.ConvertToDataURI(file.Data, file.MediaType)
}

func alibabaGetFirstFrameImage(opts *provider.VideoModelV3CallOptions) *provider.VideoModelV3File {
	for i := range opts.FrameImages {
		if opts.FrameImages[i].FrameType == provider.VideoFrameTypeFirstFrame {
			return &opts.FrameImages[i].Image
		}
	}
	return nil
}

func alibabaGetLastFrameImage(opts *provider.VideoModelV3CallOptions) *provider.VideoModelV3File {
	for i := range opts.FrameImages {
		if opts.FrameImages[i].FrameType == provider.VideoFrameTypeLastFrame {
			return &opts.FrameImages[i].Image
		}
	}
	return nil
}

func alibabaResolveStartImage(opts *provider.VideoModelV3CallOptions) *provider.VideoModelV3File {
	if img := alibabaGetFirstFrameImage(opts); img != nil {
		return img
	}
	return opts.Image
}

func alibabaIsVideoURL(url string) bool {
	lower := strings.ToLower(url)
	// Strip query/fragment before checking the extension.
	if idx := strings.IndexAny(lower, "?#"); idx >= 0 {
		lower = lower[:idx]
	}
	return strings.HasSuffix(lower, ".mp4") || strings.HasSuffix(lower, ".mov")
}

// alibabaResolveMedia builds the input.media array (wan2.7 and wan3) from
// inputReferences plus the frame images the caller resolved for this
// protocol.
func alibabaResolveMedia(
	opts *provider.VideoModelV3CallOptions,
	alibabaOpts *AlibabaVideoModelOptions,
	warnings *[]types.Warning,
	first, last *provider.VideoModelV3File,
) []map[string]interface{} {
	if alibabaOpts != nil && len(alibabaOpts.Media) > 0 {
		media := make([]map[string]interface{}, 0, len(alibabaOpts.Media))
		for _, item := range alibabaOpts.Media {
			entry := map[string]interface{}{
				"type": item.Type,
				"url":  item.URL,
			}
			if item.ReferenceVoice != nil {
				entry["reference_voice"] = *item.ReferenceVoice
			}
			media = append(media, entry)
		}
		return media
	}

	media := []map[string]interface{}{}

	for _, reference := range opts.InputReferences {
		if reference.Type == "url" {
			mediaType := "reference_image"
			if alibabaIsVideoURL(reference.URL) {
				mediaType = "reference_video"
			}
			media = append(media, map[string]interface{}{
				"type": mediaType,
				"url":  reference.URL,
			})
		} else if strings.HasPrefix(reference.MediaType, "image/") {
			media = append(media, map[string]interface{}{
				"type": "reference_image",
				"url":  alibabaFileToDataURI(&reference),
			})
		} else {
			*warnings = append(*warnings, types.Warning{
				Type:    "unsupported",
				Feature: "inputReferences",
				Details: "Alibaba reference-to-video requires URL references for videos. Non-URL video reference was skipped.",
			})
		}
	}

	if first != nil {
		media = append(media, map[string]interface{}{
			"type": "first_frame",
			"url":  alibabaFileToDataURI(first),
		})
	}

	if last != nil {
		media = append(media, map[string]interface{}{
			"type": "last_frame",
			"url":  alibabaFileToDataURI(last),
		})
	}

	if len(media) == 0 {
		return nil
	}
	return media
}

func alibabaResolveReferenceURLs(
	opts *provider.VideoModelV3CallOptions,
	alibabaOpts *AlibabaVideoModelOptions,
	warnings *[]types.Warning,
) []string {
	if len(opts.FrameImages) > 0 {
		return nil
	}

	if len(opts.InputReferences) > 0 {
		urls := make([]string, 0, len(opts.InputReferences))
		for _, reference := range opts.InputReferences {
			if reference.Type == "url" {
				urls = append(urls, reference.URL)
			} else {
				*warnings = append(*warnings, types.Warning{
					Type:    "unsupported",
					Feature: "inputReferences",
					Details: "Alibaba reference-to-video requires URL references. Non-URL reference was skipped.",
				})
			}
		}
		if len(urls) > 0 {
			return urls
		}
		return nil
	}

	if alibabaOpts != nil {
		return alibabaOpts.ReferenceURLs
	}
	return nil
}

// alibabaRequest is the built request payload plus the warnings and parsed
// provider options produced while assembling it.
type alibabaRequest struct {
	Input       map[string]interface{}
	Parameters  map[string]interface{}
	Warnings    []types.Warning
	AlibabaOpts *AlibabaVideoModelOptions
}

// buildRequest builds the DashScope input/parameters objects for DoStart,
// selecting the legacy, wan2.7, or wan3 protocol from the model id
// (TS AlibabaVideoModel#buildRequest).
func (m *VideoModel) buildRequest(opts *provider.VideoModelV3CallOptions) *alibabaRequest {
	var warnings []types.Warning
	mode := m.detectMode()

	alibabaOpts := parseAlibabaVideoModelOptions(opts.ProviderOptions)

	input := map[string]interface{}{}

	if opts.PromptSet || opts.Prompt != "" {
		input["prompt"] = opts.Prompt
	}

	if alibabaOpts != nil && alibabaOpts.NegativePrompt != nil {
		input["negative_prompt"] = *alibabaOpts.NegativePrompt
	}

	if alibabaOpts != nil && alibabaOpts.AudioURL != nil {
		input["audio_url"] = *alibabaOpts.AudioURL
	}

	startImage := alibabaResolveStartImage(opts)
	protocol := detectAlibabaVideoProtocol(m.modelID)
	wan27 := protocol == alibabaProtocolWan27
	wan3 := protocol == alibabaProtocolWan3
	tieredProtocol := wan27 || wan3
	supportsRatio := wan3 || (wan27 && mode != "i2v")

	if !wan3 && mode == "i2v" && startImage != nil {
		input["img_url"] = alibabaFileToImageString(startImage)
	}

	if wan3 {
		media := alibabaResolveMedia(opts, alibabaOpts, &warnings, startImage, alibabaGetLastFrameImage(opts))
		if media != nil {
			input["media"] = media
		}
	} else if mode == "r2v" {
		if wan27 {
			media := alibabaResolveMedia(opts, alibabaOpts, &warnings, alibabaGetFirstFrameImage(opts), nil)
			if media != nil {
				input["media"] = media
			}
		} else {
			referenceURLs := alibabaResolveReferenceURLs(opts, alibabaOpts, &warnings)
			if len(referenceURLs) > 0 {
				input["reference_urls"] = referenceURLs
			}
		}
	}

	lastFrame := alibabaGetLastFrameImage(opts)
	if lastFrame != nil && !wan3 {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "frameImages",
			Details: "This model does not support last_frame. The last frame image was ignored.",
		})
	}

	if len(opts.InputReferences) > 0 && mode != "r2v" && !wan3 {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "inputReferences",
			Details: "Alibaba only supports inputReferences (reference-to-video) on reference-to-video models. The reference images were ignored.",
		})
	}

	parameters := map[string]interface{}{}

	if opts.Duration != nil {
		parameters["duration"] = *opts.Duration
	}

	if opts.Seed != nil {
		parameters["seed"] = *opts.Seed
	}

	if opts.Resolution != "" {
		if mode == "i2v" || tieredProtocol {
			resolutionTier, ok := alibabaResolutionTierMap[opts.Resolution]
			if !ok {
				resolutionTier = opts.Resolution
			}
			supportedTiers := []string{"720P", "1080P"}
			if wan3 {
				supportedTiers = []string{"480P", "720P", "1080P"}
			}
			if tieredProtocol && !containsString(supportedTiers, resolutionTier) {
				modelName := "wan2.7"
				if wan3 {
					modelName = "wan3"
				}
				warnings = append(warnings, types.Warning{
					Type:    "unsupported",
					Feature: "resolution",
					Details: fmt.Sprintf("%s models only support the %s resolution tiers. The resolution %q was ignored.", modelName, strings.Join(supportedTiers, ", "), opts.Resolution),
				})
			} else {
				parameters["resolution"] = resolutionTier
			}
		} else {
			parameters["size"] = strings.Replace(opts.Resolution, "x", "*", 1)
		}
	}

	if supportsRatio {
		var ratio string
		if alibabaOpts != nil && alibabaOpts.Ratio != nil {
			ratio = *alibabaOpts.Ratio
		} else if opts.AspectRatio != "" {
			ratio = opts.AspectRatio
		} else if opts.Resolution != "" {
			if derived, ok := deriveAlibabaRatioFromResolution(opts.Resolution); ok {
				ratio = derived
			}
		}
		if ratio != "" {
			parameters["ratio"] = ratio
		}
	}

	if alibabaOpts != nil && alibabaOpts.PromptExtend != nil {
		parameters["prompt_extend"] = *alibabaOpts.PromptExtend
	}
	if alibabaOpts != nil && alibabaOpts.ShotType != nil {
		if tieredProtocol {
			modelName := "wan2.7"
			if wan3 {
				modelName = "wan3"
			}
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "shotType",
				Details: fmt.Sprintf("%s models do not support the shotType option. Describe the shot structure in the prompt instead.", modelName),
			})
		} else {
			parameters["shot_type"] = *alibabaOpts.ShotType
		}
	}
	if alibabaOpts != nil && alibabaOpts.Watermark != nil {
		parameters["watermark"] = *alibabaOpts.Watermark
	}

	var audio *bool
	if opts.GenerateAudio != nil {
		audio = opts.GenerateAudio
	} else if alibabaOpts != nil {
		audio = alibabaOpts.Audio
	}
	if audio != nil {
		if wan27 {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "generateAudio",
				Details: "wan2.7 models always generate audio. The audio option was ignored.",
			})
		} else {
			parameters["audio"] = *audio
		}
	}

	if opts.AspectRatio != "" && !supportsRatio {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "aspectRatio",
			Details: "Alibaba video models use explicit size/resolution dimensions. Use the resolution option or providerOptions.alibaba for size control.",
		})
	}
	if opts.FPS != nil && *opts.FPS != 0 {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "fps",
			Details: "Alibaba video models do not support custom FPS.",
		})
	}
	if opts.N > 1 {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "n",
			Details: "Alibaba video models only support generating 1 video per call.",
		})
	}

	return &alibabaRequest{
		Input:       input,
		Parameters:  parameters,
		Warnings:    warnings,
		AlibabaOpts: alibabaOpts,
	}
}

func containsString(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// DoStart starts an asynchronous video generation via the DashScope
// video-synthesis endpoint and returns an opaque operation reference
// (TS AlibabaVideoModel#doStart).
func (m *VideoModel) DoStart(ctx context.Context, opts *provider.VideoModelV3StartOptions) (*provider.VideoModelV3OperationStartResult, error) {
	req := m.buildRequest(&opts.VideoModelV3CallOptions)

	headers := map[string]string{"X-DashScope-Async": "enable"}
	for k, v := range opts.Headers {
		headers[k] = v
	}

	var createResp alibabaVideoCreateResponse
	httpResp, err := m.prov.videoClient.DoJSONResponse(ctx, internalhttp.Request{
		Method: "POST",
		Path:   "/api/v1/services/aigc/video-generation/video-synthesis",
		Body: map[string]interface{}{
			"model":      m.modelID,
			"input":      req.Input,
			"parameters": req.Parameters,
		},
		Headers: headers,
	}, &createResp)
	if err != nil {
		return nil, m.handleError(err)
	}

	taskID := ""
	if createResp.Output != nil {
		taskID = createResp.Output.TaskID
	}
	if taskID == "" {
		return nil, providererrors.NewVideoGenerationError("alibaba", m.modelID,
			fmt.Sprintf("No task_id returned from Alibaba API. Response: %s", string(mustMarshal(createResp))), nil)
	}

	operation, _ := json.Marshal(alibabaOperation{TaskID: taskID})

	return &provider.VideoModelV3OperationStartResult{
		Operation: operation,
		Warnings:  req.Warnings,
		Response: provider.VideoModelV3ResponseInfo{
			Timestamp: time.Now(),
			ModelID:   m.modelID,
			Headers:   flattenHTTPHeaders(httpResp.Headers),
		},
	}, nil
}

// DoStatus checks the status of an asynchronous video generation started
// with DoStart via the DashScope tasks endpoint
// (TS AlibabaVideoModel#doStatus). The URL is built from the provider's own
// trusted base URL and an opaque task id (never a full URL taken from a
// response), matching TS's `getFromApi({ validateUrl: false })` for this
// call, so no additional SSRF validation is required.
func (m *VideoModel) DoStatus(ctx context.Context, opts *provider.VideoModelV3StatusOptions) (*provider.VideoModelV3OperationStatusResult, error) {
	var operation alibabaOperation
	if err := json.Unmarshal(opts.Operation, &operation); err != nil {
		return nil, fmt.Errorf("alibaba: invalid operation reference: %w", err)
	}

	var statusResp alibabaVideoStatusResponse
	httpResp, err := m.prov.videoClient.DoJSONResponse(ctx, internalhttp.Request{
		Method:  "GET",
		Path:    "/api/v1/tasks/" + operation.TaskID,
		Headers: opts.Headers,
	}, &statusResp)
	if err != nil {
		return nil, m.handleError(err)
	}

	responseInfo := provider.VideoModelV3ResponseInfo{
		Timestamp: time.Now(),
		ModelID:   m.modelID,
		Headers:   flattenHTTPHeaders(httpResp.Headers),
	}

	var taskStatus string
	if statusResp.Output != nil {
		taskStatus = statusResp.Output.TaskStatus
	}

	switch taskStatus {
	case "SUCCEEDED":
		return m.buildCompletedResult(&statusResp, responseInfo)

	case "FAILED", "CANCELED":
		message := ""
		if statusResp.Output != nil {
			message = statusResp.Output.Message
		}
		errMsg := strings.TrimSpace(fmt.Sprintf("Video generation %s. Task ID: %s. %s", strings.ToLower(taskStatus), operation.TaskID, message))
		return &provider.VideoModelV3OperationStatusResult{
			Status:   provider.VideoOperationStatusError,
			Error:    errMsg,
			Response: responseInfo,
		}, nil

	default:
		return &provider.VideoModelV3OperationStatusResult{
			Status:   provider.VideoOperationStatusPending,
			Response: responseInfo,
		}, nil
	}
}

func (m *VideoModel) buildCompletedResult(statusResp *alibabaVideoStatusResponse, responseInfo provider.VideoModelV3ResponseInfo) (*provider.VideoModelV3OperationStatusResult, error) {
	var taskID, videoURL, actualPrompt string
	if statusResp.Output != nil {
		taskID = statusResp.Output.TaskID
		videoURL = statusResp.Output.VideoURL
		actualPrompt = statusResp.Output.ActualPrompt
	}

	if videoURL == "" {
		return nil, providererrors.NewVideoGenerationError("alibaba", m.modelID,
			fmt.Sprintf("No video URL in response. Task ID: %s", taskID), nil)
	}

	alibabaMeta := map[string]interface{}{
		"taskId":   taskID,
		"videoUrl": videoURL,
	}
	if actualPrompt != "" {
		alibabaMeta["actualPrompt"] = actualPrompt
	}
	if statusResp.Usage != nil {
		usage := map[string]interface{}{}
		if statusResp.Usage.Duration != nil {
			usage["duration"] = *statusResp.Usage.Duration
		}
		if statusResp.Usage.OutputVideoDuration != nil {
			usage["outputVideoDuration"] = *statusResp.Usage.OutputVideoDuration
		}
		if statusResp.Usage.SR != nil {
			usage["resolution"] = *statusResp.Usage.SR
		}
		if statusResp.Usage.Size != "" {
			usage["size"] = statusResp.Usage.Size
		}
		if statusResp.Usage.InputVideoDuration != nil {
			usage["inputVideoDuration"] = *statusResp.Usage.InputVideoDuration
		}
		if statusResp.Usage.FPS != nil {
			usage["fps"] = *statusResp.Usage.FPS
		}
		if statusResp.Usage.Ratio != "" {
			usage["ratio"] = statusResp.Usage.Ratio
		}
		alibabaMeta["usage"] = usage
	}

	return &provider.VideoModelV3OperationStatusResult{
		Status: provider.VideoOperationStatusCompleted,
		Videos: []provider.VideoModelV3VideoData{
			{Type: "url", URL: videoURL, MediaType: "video/mp4"},
		},
		ProviderMetadata: map[string]interface{}{"alibaba": alibabaMeta},
		Response:         responseInfo,
	}, nil
}

// DoGenerate generates a video synchronously by starting the operation and
// polling DoStatus until it completes, honoring providerOptions.alibaba's
// pollIntervalMs/pollTimeoutMs. Go's VideoModelV3 always requires DoGenerate
// (unlike TS, where these models implement only doStart/doStatus and the
// core generate-video flow polls them directly), so this method is the Go
// equivalent of that default polling behavior, built entirely on top of
// DoStart/DoStatus rather than a separate request implementation.
func (m *VideoModel) DoGenerate(ctx context.Context, opts *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
	startResult, err := m.DoStart(ctx, &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: *opts})
	if err != nil {
		return nil, err
	}

	alibabaOpts := parseAlibabaVideoModelOptions(opts.ProviderOptions)
	pollOpts := polling.DefaultPollOptions()
	pollOpts.PollIntervalMs = 5000
	pollOpts.PollTimeoutMs = 600000
	if alibabaOpts != nil {
		if alibabaOpts.PollIntervalMs != nil {
			pollOpts.PollIntervalMs = *alibabaOpts.PollIntervalMs
		}
		if alibabaOpts.PollTimeoutMs != nil {
			pollOpts.PollTimeoutMs = *alibabaOpts.PollTimeoutMs
		}
	}

	var finalStatus *provider.VideoModelV3OperationStatusResult
	checker := func(ctx context.Context) (*polling.JobResult, error) {
		status, err := m.DoStatus(ctx, &provider.VideoModelV3StatusOptions{
			Operation: startResult.Operation,
			Headers:   opts.Headers,
		})
		if err != nil {
			return nil, err
		}
		switch status.Status {
		case provider.VideoOperationStatusCompleted:
			finalStatus = status
			return &polling.JobResult{Status: polling.JobStatusCompleted}, nil
		case provider.VideoOperationStatusError:
			return &polling.JobResult{Status: polling.JobStatusFailed, Error: status.Error}, nil
		default:
			return &polling.JobResult{Status: polling.JobStatusProcessing}, nil
		}
	}

	if _, err := polling.PollForCompletion(ctx, checker, pollOpts); err != nil {
		return nil, providererrors.NewVideoGenerationError("alibaba", m.modelID, err.Error(), err)
	}

	return &provider.VideoModelV3Response{
		Videos:           finalStatus.Videos,
		Warnings:         append(append([]types.Warning{}, startResult.Warnings...), finalStatus.Warnings...),
		ProviderMetadata: finalStatus.ProviderMetadata,
		Response:         finalStatus.Response,
	}, nil
}

// handleError converts an HTTPStatusError carrying the DashScope error
// envelope ({code, message, request_id}) into a provider error.
func (m *VideoModel) handleError(err error) error {
	var httpErr *internalhttp.HTTPStatusError
	if statusErr, ok := err.(*internalhttp.HTTPStatusError); ok {
		httpErr = statusErr
	}
	if httpErr == nil {
		return providererrors.NewVideoGenerationError("alibaba", m.modelID, err.Error(), err)
	}
	var apiErr alibabaVideoAPIError
	message := string(httpErr.Body)
	if jsonErr := json.Unmarshal(httpErr.Body, &apiErr); jsonErr == nil && apiErr.Message != "" {
		message = apiErr.Message
	}
	return providererrors.NewProviderError("alibaba", httpErr.StatusCode, apiErr.Code, message, err)
}

func flattenHTTPHeaders(headers map[string][]string) map[string]string {
	out := make(map[string]string, len(headers))
	for k, vs := range headers {
		if len(vs) > 0 {
			out[k] = vs[0]
		}
	}
	return out
}

func mustMarshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}

// alibabaOperation is the opaque operation reference returned by DoStart and
// passed back into DoStatus.
type alibabaOperation struct {
	TaskID string `json:"taskId"`
}

// alibabaVideoAPIError is the DashScope native API error format (different
// from the OpenAI-compatible chat endpoint).
type alibabaVideoAPIError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

type alibabaVideoCreateResponse struct {
	Output *struct {
		TaskStatus string `json:"task_status"`
		TaskID     string `json:"task_id"`
	} `json:"output"`
	RequestID string `json:"request_id"`
}

type alibabaVideoStatusResponse struct {
	Output *struct {
		TaskID         string `json:"task_id"`
		TaskStatus     string `json:"task_status"`
		VideoURL       string `json:"video_url"`
		SubmitTime     string `json:"submit_time"`
		ScheduledTime  string `json:"scheduled_time"`
		EndTime        string `json:"end_time"`
		OrigPrompt     string `json:"orig_prompt"`
		ActualPrompt   string `json:"actual_prompt"`
		Code           string `json:"code"`
		Message        string `json:"message"`
	} `json:"output"`
	Usage *struct {
		Duration            *float64 `json:"duration"`
		OutputVideoDuration *float64 `json:"output_video_duration"`
		InputVideoDuration  *float64 `json:"input_video_duration"`
		FPS                 *float64 `json:"fps"`
		SR                  *float64 `json:"SR"`
		Size                string   `json:"size"`
		Ratio               string   `json:"ratio"`
	} `json:"usage"`
	RequestID string `json:"request_id"`
}
