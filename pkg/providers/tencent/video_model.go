package tencent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	"net/http"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/polling"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// Core default polling (TS generateVideo's start/status flow): DoGenerate's
// own loop uses the same values.
const (
	defaultPollIntervalMs = 5000
	defaultPollTimeoutMs  = 600000
)

// parsedVideoModelID is TS ParsedTencentVideoModelId.
type parsedVideoModelID struct {
	backend      string // "tokenhub" or "vod"
	kind         string // tokenhub: "standard", "omni" or "motion-control"
	modelName    string // vod
	modelVersion string // vod
}

// parseVideoModelID is TS parseTencentVideoModelId; ok is false for an
// unsupported ID.
func parseVideoModelID(modelID string) (parsedVideoModelID, bool) {
	if rest, isVod := strings.CutPrefix(modelID, "vod/"); isVod {
		separator := strings.Index(rest, "/")
		if separator <= 0 || separator == len(rest)-1 {
			return parsedVideoModelID{}, false
		}
		return parsedVideoModelID{backend: "vod", modelName: rest[:separator], modelVersion: rest[separator+1:]}, true
	}
	if strings.HasPrefix(modelID, "kling-video-motion-control-") {
		return parsedVideoModelID{backend: "tokenhub", kind: "motion-control"}, true
	}
	if strings.HasPrefix(modelID, "kling-video-") {
		kind := "standard"
		if strings.HasSuffix(modelID, "-omni") {
			kind = "omni"
		}
		return parsedVideoModelID{backend: "tokenhub", kind: kind}, true
	}
	return parsedVideoModelID{}, false
}

// VideoModel implements provider.VideoModelV3 (with DoStart / DoStatus) for
// TokenHub Kling and VOD AIGC video models (TS TencentVideoModel).
type VideoModel struct {
	prov    *Provider
	modelID string
	parsed  parsedVideoModelID

	// currentDate overrides the clock in tests (TS _internal.currentDate).
	currentDate func() time.Time
}

// NewVideoModel creates a video model; an unsupported ID is a
// NoSuchModelError (TS throws from the constructor).
func NewVideoModel(prov *Provider, modelID string) (*VideoModel, error) {
	parsed, ok := parseVideoModelID(modelID)
	if !ok {
		return nil, providererrors.NewNoSuchModelError(modelID, "videoModel")
	}
	return &VideoModel{prov: prov, modelID: modelID, parsed: parsed}, nil
}

// SpecificationVersion returns "v4".
func (m *VideoModel) SpecificationVersion() string { return "v4" }

// Provider returns "tencent.video".
func (m *VideoModel) Provider() string { return "tencent.video" }

// ModelID returns the model ID.
func (m *VideoModel) ModelID() string { return m.modelID }

// MaxVideosPerCall returns 1.
func (m *VideoModel) MaxVideosPerCall() *int {
	one := 1
	return &one
}

func (m *VideoModel) now() time.Time {
	if m.currentDate != nil {
		return m.currentDate()
	}
	return time.Now()
}

// tencentVideoOperation is the operation reference (TS {taskId}).
type tencentVideoOperation struct {
	TaskID string `json:"taskId"`
}

// DoStart submits a video generation task (TS TencentVideoModel#doStart).
func (m *VideoModel) DoStart(ctx context.Context, opts *provider.VideoModelV3StartOptions) (*provider.VideoModelV3OperationStartResult, error) {
	currentDate := m.now()
	warnings := []types.Warning{}

	tencentOpts, err := parseVideoOptions(opts.ProviderOptions)
	if err != nil {
		return nil, err
	}
	warnUnsupportedCommonOptions(opts, m.parsed.backend == "vod", &warnings)
	callOpts := &opts.VideoModelV3CallOptions

	var taskID string
	var responseHeaders map[string]string

	if m.parsed.backend == "tokenhub" {
		warnIgnoredProviderOptions(tencentOpts, vodOnlyOptions, "This option only applies to VOD models; it was ignored.", &warnings)
		endpoint, body := buildTokenHubKlingRequest(m.modelID, m.parsed.kind, callOpts, tencentOpts, &warnings)

		resp, err := m.prov.tokenHub.Do(ctx, internalhttp.Request{
			Method:  http.MethodPost,
			Path:    "/wand/kling/" + endpoint,
			Body:    body,
			Headers: opts.Headers,
		})
		if err != nil {
			// TS postJsonToApi lets a transport failure propagate unchanged;
			// a rejected headers() resolution (the APIKeyFunc error) is
			// thrown as is, without the client's FetchRejection marker.
			return nil, internalhttp.UnwrapFetchRejection(err)
		}
		resp.Body = []byte(textdecode.UTF8(resp.Body)) // TS reads the body as text (TextDecoder)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, tokenHubFailedStatus(resp.StatusError(resp.Body))
		}
		value, err := tokenHubResponse(resp.StatusCode, resp.Headers, resp.Body, tokenHubKlingCreateResponseSchema, stripCreateResponse)
		if err != nil {
			return nil, err
		}
		data, _ := value["data"].(map[string]interface{})
		id, _ := data["id"].(string)
		if id == "" {
			// TS AISDKError TENCENT_VIDEO_GENERATION_ERROR.
			return nil, providererrors.NewVideoGenerationError("tencent", m.modelID, "No task ID returned from TokenHub.", nil)
		}
		taskID = id
		responseHeaders = providerutils.ExtractHeaders(resp.Headers)
	} else {
		warnIgnoredProviderOptions(tencentOpts, tokenHubOnlyOptions, "This option only applies to TokenHub Kling models; it was ignored.", &warnings)
		creds, err := m.prov.vodCredentials()
		if err != nil {
			return nil, err
		}
		body := buildVodCreateTaskBody(m.parsed.modelName, m.parsed.modelVersion, creds.subAppID, callOpts, tencentOpts, &warnings)
		result, err := m.prov.postTencentCloudAPI(ctx, vodService, "CreateAigcVideoTask", vodVersion, creds, body,
			vodCreateTaskResponseSchema, opts.Headers, currentDate.Unix())
		if err != nil {
			return nil, err
		}
		taskID, _ = result.response["TaskId"].(string)
		responseHeaders = result.headers
	}

	operation, _ := json.Marshal(tencentVideoOperation{TaskID: taskID})
	return &provider.VideoModelV3OperationStartResult{
		Operation: operation,
		Warnings:  warnings,
		Response: provider.VideoModelV3ResponseInfo{
			Timestamp: currentDate,
			ModelID:   m.modelID,
			Headers:   responseHeaders,
		},
	}, nil
}

// DoStatus checks a task (TS TencentVideoModel#doStatus): a TokenHub task
// GET, or a signed VOD DescribeTaskDetail.
func (m *VideoModel) DoStatus(ctx context.Context, opts *provider.VideoModelV3StatusOptions) (*provider.VideoModelV3OperationStatusResult, error) {
	currentDate := m.now()
	var op tencentVideoOperation
	if err := json.Unmarshal(opts.Operation, &op); err != nil {
		return nil, fmt.Errorf("tencent: invalid operation reference: %w", err)
	}
	responseInfo := func(headers map[string]string) provider.VideoModelV3ResponseInfo {
		return provider.VideoModelV3ResponseInfo{Timestamp: currentDate, ModelID: m.modelID, Headers: headers}
	}

	if m.parsed.backend == "tokenhub" {
		// The task URL is built from the configured base URL, so it stays
		// on the trusted origin (TS validateUrl with trustedOrigin: baseURL).
		resp, err := m.prov.tokenHub.Do(ctx, internalhttp.Request{
			Method:  http.MethodGet,
			Path:    "/wand/kling/tasks/" + providerutils.EncodePathSegment(op.TaskID),
			Headers: opts.Headers,
		})
		if err != nil {
			return nil, internalhttp.UnwrapFetchRejection(err)
		}
		resp.Body = []byte(textdecode.UTF8(resp.Body)) // TS reads the body as text (TextDecoder)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, tokenHubFailedStatus(resp.StatusError(resp.Body))
		}
		value, err := tokenHubResponse(resp.StatusCode, resp.Headers, resp.Body, tokenHubKlingStatusResponseSchema, stripStatusResponse)
		if err != nil {
			return nil, err
		}
		return interpretTokenHubKlingStatus(value, op.TaskID, responseInfo(providerutils.ExtractHeaders(resp.Headers))), nil
	}

	creds, err := m.prov.vodCredentials()
	if err != nil {
		return nil, err
	}
	body := newOrderedBody("TaskId", op.TaskID, "SubAppId", creds.subAppID)
	result, err := m.prov.postTencentCloudAPI(ctx, vodService, "DescribeTaskDetail", vodVersion, creds, body,
		vodDescribeTaskDetailResponseSchema, opts.Headers, currentDate.Unix())
	if err != nil {
		return nil, err
	}
	return interpretVodTaskDetail(result.response, op.TaskID, responseInfo(result.headers)), nil
}

// VideoStartStatusPollDefaults marks the model as start/status only (TS
// TencentVideoModel has no doGenerate), so ai.GenerateVideo drives it
// through the core start/status flow with the core poll defaults.
func (m *VideoModel) VideoStartStatusPollDefaults(map[string]interface{}) (intervalMs, timeoutMs int) {
	return 0, 0
}

// DoGenerate starts the task and polls DoStatus until it completes. Go's
// VideoModelV3 requires DoGenerate; TS has only doStart / doStatus and the
// core generateVideo flow polls them, which this mirrors with the core
// defaults (5 s interval, 10 min timeout).
func (m *VideoModel) DoGenerate(ctx context.Context, opts *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
	start, err := m.DoStart(ctx, &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: *opts})
	if err != nil {
		return nil, err
	}
	var final *provider.VideoModelV3OperationStatusResult
	var jobErr error
	checker := func(ctx context.Context) (*polling.JobResult, error) {
		status, err := m.DoStatus(ctx, &provider.VideoModelV3StatusOptions{Operation: start.Operation, Headers: opts.Headers})
		if err != nil {
			return nil, err
		}
		switch status.Status {
		case provider.VideoOperationStatusCompleted:
			final = status
			return &polling.JobResult{Status: polling.JobStatusCompleted}, nil
		case provider.VideoOperationStatusError:
			jobErr = providererrors.NewVideoGenerationError("tencent", m.modelID, status.Error, nil)
			return &polling.JobResult{Status: polling.JobStatusFailed, Error: status.Error}, nil
		}
		return &polling.JobResult{Status: polling.JobStatusProcessing}, nil
	}
	if _, err := polling.PollForCompletion(ctx, checker, polling.PollOptions{PollIntervalMs: defaultPollIntervalMs, PollTimeoutMs: defaultPollTimeoutMs}); err != nil {
		if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
			return nil, ctx.Err()
		}
		if jobErr != nil {
			return nil, jobErr
		}
		return nil, err
	}
	return &provider.VideoModelV3Response{
		Videos:           final.Videos,
		Warnings:         append(append([]types.Warning{}, start.Warnings...), final.Warnings...),
		ProviderMetadata: final.ProviderMetadata,
		Response:         final.Response,
	}, nil
}
