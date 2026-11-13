package tencent

import (
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

const (
	vodService = "vod"
	vodVersion = "2018-07-17"
)

var vodResolutionTiers = map[float64]string{480: "480P", 720: "720P", 1080: "1080P", 1440: "2K", 2160: "4K"}

func enabledDisabled(v *bool) (string, bool) {
	if v == nil {
		return "", false
	}
	if *v {
		return "Enabled", true
	}
	return "Disabled", true
}

// vodFileInfo is one FileInfos entry (TS toVodFileInfo plus Category,
// Usage and ReferenceType), in TS key order.
func vodFileInfo(file *provider.VideoModelV3File, category, usage string) *jsonorder.Object {
	o := jsonorder.New()
	if file.Type == "url" {
		o.Set("Type", "Url")
		o.Set("Url", file.URL)
	} else {
		o.Set("Type", "Base64")
		o.Set("Base64", getFileBase64(file))
	}
	o.Set("Category", category)
	o.Set("Usage", usage)
	return o
}

// buildVodCreateTaskBody is TS buildVodCreateTaskBody (the
// CreateAigcVideoTask body).
//
// See https://www.tencentcloud.com/document/api/266/76684.
func buildVodCreateTaskBody(modelName, modelVersion string, subAppID int64, opts *provider.VideoModelV3CallOptions, t *parsedVideoOptions, warnings *[]types.Warning) *jsonorder.Object {
	var fileInfos []*jsonorder.Object
	usages := []string{}

	if first := resolveFirstFrameImage(opts, warnings); first != nil {
		fileInfos = append(fileInfos, vodFileInfo(first, "Image", "FirstFrame"))
		usages = append(usages, "FirstFrame")
	}
	if last := resolveLastFrameImage(opts, warnings); last != nil {
		fileInfos = append(fileInfos, vodFileInfo(last, "Image", "LastFrame"))
		usages = append(usages, "LastFrame")
	}
	for i := range opts.InputReferences {
		ref := &opts.InputReferences[i]
		category := "Image"
		switch {
		case isVideoFile(ref):
			category = "Video"
		case isAudioFile(ref):
			category = "Audio"
		}
		info := vodFileInfo(ref, category, "Reference")
		if category == "Video" && t != nil && t.opts.ReferenceVideoType != nil {
			info.Set("ReferenceType", *t.opts.ReferenceVideoType)
		}
		fileInfos = append(fileInfos, info)
		usages = append(usages, "Reference")
	}

	// asset:// URLs cannot come through InputReferences, which only passes
	// http(s) URLs through as URLs.
	if t != nil {
		for _, asset := range t.opts.Assets {
			usage := "Reference"
			if asset.Usage != nil {
				usage = *asset.Usage
			}
			if usage != "Reference" && containsString(usages, usage) {
				which := "last"
				if usage == "FirstFrame" {
					which = "first"
				}
				*warnings = append(*warnings, types.Warning{
					Type: "unsupported", Feature: "providerOptions.tencent.assets",
					Details: "VOD accepts one " + which + " frame; the asset " + asset.URL + " was ignored.",
				})
				continue
			}
			category := "Image"
			if asset.Category != nil {
				category = *asset.Category
			}
			o := jsonorder.New()
			o.Set("Type", "Url")
			o.Set("Url", asset.URL)
			o.Set("Category", category)
			o.Set("Usage", usage)
			fileInfos = append(fileInfos, o)
			usages = append(usages, usage)
		}
	}

	var to VideoModelOptions
	if t != nil {
		to = t.opts
	}
	output := jsonorder.New()
	output.Set("StorageMode", "Temporary")
	if resolution, ok := resolveResolution(opts, t, vodResolutionTiers, warnings); ok {
		output.Set("Resolution", resolution)
	}
	if opts.AspectRatio != "" {
		output.Set("AspectRatio", opts.AspectRatio)
	}
	if opts.Duration != nil {
		output.Set("Duration", jsonNumberOrNull(*opts.Duration))
	}
	setFlag := func(o *jsonorder.Object, key string, v *bool) {
		if s, ok := enabledDisabled(v); ok {
			o.Set(key, s)
		}
	}
	setFlag(output, "AudioGeneration", opts.GenerateAudio)
	if to.PersonGeneration != nil {
		output.Set("PersonGeneration", *to.PersonGeneration)
	}
	setFlag(output, "InputComplianceCheck", to.InputComplianceCheck)
	setFlag(output, "OutputComplianceCheck", to.OutputComplianceCheck)
	setFlag(output, "EnhanceSwitch", to.EnhanceSwitch)
	setFlag(output, "OffPeak", to.OffPeak)
	setFlag(output, "LogoAdd", to.Watermark)

	body := jsonorder.New()
	body.Set("SubAppId", subAppID)
	body.Set("ModelName", modelName)
	body.Set("ModelVersion", modelVersion)
	if promptPresent(opts) {
		body.Set("Prompt", opts.Prompt)
	}
	setString := func(key string, v *string) {
		if v != nil {
			body.Set(key, *v)
		}
	}
	setString("NegativePrompt", to.NegativePrompt)
	setFlag(body, "EnhancePrompt", to.EnhancePrompt)
	if len(fileInfos) > 0 {
		list := make([]interface{}, len(fileInfos))
		for i, f := range fileInfos {
			list[i] = f
		}
		body.Set("FileInfos", list)
	}
	body.Set("OutputConfig", output)
	setString("InputRegion", to.InputRegion)
	setString("SceneType", to.SceneType)
	if opts.Seed != nil {
		body.Set("Seed", *opts.Seed)
	}
	setString("SessionId", to.SessionID)
	setString("SessionContext", to.SessionContext)
	if to.TasksPriority != nil {
		body.Set("TasksPriority", *to.TasksPriority)
	}
	setString("ExtInfo", to.ExtInfo)
	return body
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// vodCreateTaskResponseSchema is TS vodCreateTaskResponseSchema.
var vodCreateTaskResponseSchema = zodlite.Object(
	zodlite.F("TaskId", zodlite.String),
	zodlite.F("RequestId", zodlite.Nullish(zodlite.String)),
)

var nullishNumber = zodlite.Nullish(zodlite.Number)
var nullishString = zodlite.Nullish(zodlite.String)

// vodDescribeTaskDetailResponseSchema is TS
// vodDescribeTaskDetailResponseSchema.
var vodDescribeTaskDetailResponseSchema = zodlite.Object(
	zodlite.F("TaskType", nullishString),
	zodlite.F("Status", nullishString),
	zodlite.F("RequestId", nullishString),
	zodlite.F("AigcVideoTask", zodlite.Nullish(zodlite.Object(
		zodlite.F("TaskId", nullishString),
		zodlite.F("Status", nullishString),
		zodlite.F("ErrCode", nullishNumber),
		zodlite.F("ErrCodeExt", nullishString),
		zodlite.F("Message", nullishString),
		zodlite.F("Progress", nullishNumber),
		zodlite.F("Input", zodlite.Nullish(zodlite.Object(
			zodlite.F("OutputConfig", zodlite.Nullish(zodlite.Object(
				zodlite.F("Resolution", nullishString),
			))),
		))),
		zodlite.F("Output", zodlite.Nullish(zodlite.Object(
			zodlite.F("FileInfos", zodlite.Nullish(zodlite.Array(zodlite.Object(
				zodlite.F("FileUrl", nullishString),
				zodlite.F("FileType", nullishString),
				zodlite.F("FileId", nullishString),
				zodlite.F("UsageType", nullishString),
				zodlite.F("MetaData", zodlite.Nullish(zodlite.Object(
					zodlite.F("Duration", nullishNumber),
				))),
			)))),
			zodlite.F("Usage", zodlite.Nullish(zodlite.Object(
				zodlite.F("InputTokens", nullishNumber),
				zodlite.F("ThoughtTokens", nullishNumber),
				zodlite.F("InputImageCount", nullishNumber),
				zodlite.F("InputSeconds", nullishNumber),
				zodlite.F("OutputSeconds", nullishNumber),
				zodlite.F("TotalSeconds", nullishNumber),
				zodlite.F("SpecificationUsages", zodlite.Nullish(zodlite.Array(zodlite.Object(
					zodlite.F("Specification", nullishString),
					zodlite.F("Category", nullishString),
				)))),
			))),
		))),
	))),
)

var vodFileTypeMediaTypes = map[string]string{"mov": "video/quicktime", "mp4": "video/mp4", "webm": "video/webm"}

func obj(m map[string]interface{}, key string) map[string]interface{} {
	v, _ := m[key].(map[string]interface{})
	return v
}

// nonEmpty is TS `value || undefined` for a nullish string.
func nonEmpty(m map[string]interface{}, key string) (string, bool) {
	s, ok := m[key].(string)
	return s, ok && s != ""
}

// setNumber is compactJson for a nullish number member.
func setNumber(out map[string]interface{}, outKey string, m map[string]interface{}, key string) {
	if v, ok := m[key].(float64); ok {
		out[outKey] = v
	}
}

// interpretVodTaskDetail is TS interpretVodTaskDetail for a
// DescribeTaskDetail response of an AIGC video task.
//
// See https://www.tencentcloud.com/document/api/266/34129.
func interpretVodTaskDetail(detail map[string]interface{}, taskID string, response provider.VideoModelV3ResponseInfo) *provider.VideoModelV3OperationStatusResult {
	task := obj(detail, "AigcVideoTask")
	status, _ := detail["Status"].(string)

	if status == "ABORTED" {
		return &provider.VideoModelV3OperationStatusResult{Status: provider.VideoOperationStatusError, Error: "Video generation aborted (task " + taskID + ")", Response: response}
	}
	if status != "FINISH" {
		return &provider.VideoModelV3OperationStatusResult{Status: provider.VideoOperationStatusPending, Response: response}
	}
	if code, ok := task["ErrCode"].(float64); ok && code != 0 {
		codeText, hasExt := nonEmpty(task, "ErrCodeExt")
		if !hasExt {
			codeText = jsNumberString(code)
		}
		message, hasMessage := nonEmpty(task, "Message")
		if !hasMessage {
			message = "unknown error"
		}
		return &provider.VideoModelV3OperationStatusResult{
			Status:   provider.VideoOperationStatusError,
			Error:    "Video generation failed (" + codeText + "): " + message + " (task " + taskID + ")",
			Response: response,
		}
	}

	// VOD reports unset strings as "". return_last_frame (via extInfo) adds
	// the last frame as a PNG entry with UsageType last_frame_url; it is not
	// a video.
	var files []map[string]interface{}
	lastFrameURL, hasLastFrame := "", false
	output := obj(task, "Output")
	list, _ := output["FileInfos"].([]interface{})
	for _, entry := range list {
		f, _ := entry.(map[string]interface{})
		url, ok := nonEmpty(f, "FileUrl")
		if !ok {
			continue
		}
		if usage, _ := f["UsageType"].(string); usage == "last_frame_url" {
			if !hasLastFrame {
				lastFrameURL, hasLastFrame = url, true
			}
			continue
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		return &provider.VideoModelV3OperationStatusResult{Status: provider.VideoOperationStatusError, Error: "VOD task " + taskID + " finished without a video URL.", Response: response}
	}

	usage := obj(output, "Usage")
	var specificationUsages []interface{}
	specs, _ := usage["SpecificationUsages"].([]interface{})
	for _, s := range specs {
		sm, _ := s.(map[string]interface{})
		entry := map[string]interface{}{}
		if v, ok := nonEmpty(sm, "Specification"); ok {
			entry["specification"] = v
		}
		if v, ok := nonEmpty(sm, "Category"); ok {
			entry["category"] = v
		}
		if len(entry) > 0 {
			specificationUsages = append(specificationUsages, entry)
		}
	}

	videos := make([]provider.VideoModelV3VideoData, len(files))
	videoMeta := make([]interface{}, len(files))
	for i, f := range files {
		url := f["FileUrl"].(string)
		fileType, _ := f["FileType"].(string)
		mediaType, ok := vodFileTypeMediaTypes[strings.ToLower(fileType)]
		if !ok {
			mediaType = "video/mp4"
		}
		videos[i] = provider.VideoModelV3VideoData{Type: provider.VideoDataTypeURL, URL: url, MediaType: mediaType}
		entry := map[string]interface{}{"url": url}
		// Temporary storage reports an empty FileId.
		if id, ok := nonEmpty(f, "FileId"); ok {
			entry["fileId"] = id
		}
		if d, ok := obj(f, "MetaData")["Duration"].(float64); ok {
			entry["duration"] = d
		} else if d, ok := usage["OutputSeconds"].(float64); ok {
			entry["duration"] = d
		}
		videoMeta[i] = entry
	}

	meta := map[string]interface{}{"taskId": taskID}
	if v, ok := detail["RequestId"].(string); ok {
		meta["requestId"] = v
	}
	if v, ok := nonEmpty(obj(obj(task, "Input"), "OutputConfig"), "Resolution"); ok {
		meta["resolution"] = v
	}
	if hasLastFrame {
		meta["lastFrameUrl"] = lastFrameURL
	}
	meta["videos"] = videoMeta
	if usage != nil {
		u := map[string]interface{}{}
		setNumber(u, "inputImageCount", usage, "InputImageCount")
		setNumber(u, "inputSeconds", usage, "InputSeconds")
		setNumber(u, "inputTokens", usage, "InputTokens")
		setNumber(u, "outputSeconds", usage, "OutputSeconds")
		if len(specificationUsages) > 0 {
			u["specificationUsages"] = specificationUsages
		}
		setNumber(u, "thoughtTokens", usage, "ThoughtTokens")
		setNumber(u, "totalSeconds", usage, "TotalSeconds")
		meta["usage"] = u
	}

	return &provider.VideoModelV3OperationStatusResult{
		Status:           provider.VideoOperationStatusCompleted,
		Videos:           videos,
		Warnings:         []types.Warning{},
		Response:         response,
		ProviderMetadata: map[string]interface{}{"tencent": meta},
	}
}
