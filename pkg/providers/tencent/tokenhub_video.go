package tencent

import (
	"math"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

var klingResolutionTiers = map[float64]string{720: "720p", 1080: "1080p", 2160: "4k"}

// klingModelsWithoutAudioOrMultiShot accept only resolution, aspect ratio and
// duration settings (TS KLING_MODELS_WITHOUT_AUDIO_OR_MULTI_SHOT).
var klingModelsWithoutAudioOrMultiShot = map[string]bool{"kling-video-v4": true, "kling-video-v3-turbo": true}

// klingStandardModelsWithOmni also serve the omni endpoint for reference
// inputs (TS KLING_STANDARD_MODELS_WITH_OMNI).
var klingStandardModelsWithOmni = map[string]bool{"kling-video-v4": true}

func content(pairs ...string) *jsonorder.Object {
	o := jsonorder.New()
	for i := 0; i+1 < len(pairs); i += 2 {
		o.Set(pairs[i], pairs[i+1])
	}
	return o
}

func promptContent(opts *provider.VideoModelV3CallOptions) []interface{} {
	if promptPresent(opts) {
		return []interface{}{content("type", "prompt", "text", opts.Prompt)}
	}
	return nil
}

func elementContents(t *parsedVideoOptions) []interface{} {
	if t == nil {
		return nil
	}
	var out []interface{}
	for _, e := range t.opts.Elements {
		c := content("type", "element", "element_id", e.ElementID)
		if e.ID != nil {
			c.Set("id", *e.ID)
		}
		out = append(out, c)
	}
	return out
}

// setting is one settings entry; set is false for TS undefined.
type setting struct {
	key   string
	value interface{}
	set   bool
}

// withSettingsAndOptions is TS withSettingsAndOptions: body, then settings
// (when any is set), then options (external_task_id, watermark_info).
func withSettingsAndOptions(body *jsonorder.Object, settings []setting, t *parsedVideoOptions) *jsonorder.Object {
	s := jsonorder.New()
	for _, e := range settings {
		if e.set {
			s.Set(e.key, e.value)
		}
	}
	if s.Len() > 0 {
		body.Set("settings", s)
	}
	o := jsonorder.New()
	if t != nil {
		if t.opts.ExternalTaskID != nil {
			o.Set("external_task_id", *t.opts.ExternalTaskID)
		}
		if t.opts.Watermark != nil {
			o.Set("watermark_info", content2("enabled", *t.opts.Watermark))
		}
	}
	if o.Len() > 0 {
		body.Set("options", o)
	}
	return body
}

func content2(key string, value interface{}) *jsonorder.Object {
	o := jsonorder.New()
	o.Set(key, value)
	return o
}

func durationSetting(opts *provider.VideoModelV3CallOptions) setting {
	if opts.Duration == nil {
		return setting{key: "duration"}
	}
	return setting{key: "duration", value: *opts.Duration, set: true}
}

func aspectRatioSetting(opts *provider.VideoModelV3CallOptions) setting {
	return setting{key: "aspect_ratio", value: opts.AspectRatio, set: opts.AspectRatio != ""}
}

// buildTokenHubKlingRequest is TS buildTokenHubKlingRequest: it picks the
// endpoint and builds the body.
func buildTokenHubKlingRequest(modelID, kind string, opts *provider.VideoModelV3CallOptions, t *parsedVideoOptions, warnings *[]types.Warning) (string, *jsonorder.Object) {
	firstFrame := resolveFirstFrameImage(opts, warnings)
	lastFrame := resolveLastFrameImage(opts, warnings)
	images, videos, audios := splitInputReferences(opts)
	resolution, hasResolution := resolveResolution(opts, t, klingResolutionTiers, warnings)
	resolutionSetting := setting{key: "resolution", value: resolution, set: hasResolution}

	if len(audios) > 0 {
		*warnings = append(*warnings, types.Warning{
			Type: "unsupported", Feature: "inputReferences",
			Details: "TokenHub Kling models do not accept audio references; use `providerOptions.tencent.voices` instead. The audio was ignored.",
		})
	}

	if kind == "motion-control" {
		var characterImage, motionVideo *provider.VideoModelV3File
		unusedImages := len(images)
		if firstFrame != nil {
			characterImage = firstFrame
		} else if len(images) > 0 {
			characterImage = images[0]
			unusedImages = len(images) - 1
		}
		otherVideos := 0
		if len(videos) > 0 {
			motionVideo = videos[0]
			otherVideos = len(videos) - 1
		}
		return "motion-control", buildMotionControlBody(modelID, opts, t, characterImage, motionVideo, lastFrame, unusedImages+otherVideos, resolutionSetting, warnings)
	}

	supported := !klingModelsWithoutAudioOrMultiShot[modelID]
	audio, hasAudio := resolveAudio(opts, t, supported, warnings)
	multiShot, hasMultiShot := resolveMultiShot(t, supported, warnings)
	audioSetting := setting{key: "audio", value: audio, set: hasAudio}
	multiShotSetting := setting{key: "multi_shot", value: multiShot, set: hasMultiShot}

	voices := 0
	elements := 0
	if t != nil {
		voices = len(t.opts.Voices)
		elements = len(t.opts.Elements)
	}
	hasReferences := len(images) > 0 || len(videos) > 0 || voices > 0
	hasElementsWithoutFirstFrame := firstFrame == nil && elements > 0

	if kind == "omni" || (klingStandardModelsWithOmni[modelID] && (hasReferences || hasElementsWithoutFirstFrame)) {
		videoType := "feature_video"
		if t != nil && t.opts.ReferenceVideoType != nil && *t.opts.ReferenceVideoType == "base" {
			videoType = "base_video"
		}
		contents := promptContent(opts)
		if firstFrame != nil {
			contents = append(contents, content("type", "first_frame", "url", toURLOrDataURI(firstFrame)))
		}
		if lastFrame != nil {
			contents = append(contents, content("type", "last_frame", "url", toURLOrDataURI(lastFrame)))
		}
		for _, image := range images {
			contents = append(contents, content("type", "refer_image", "url", toURLOrDataURI(image)))
		}
		for _, video := range videos {
			contents = append(contents, content("type", videoType, "url", toURLOrDataURI(video)))
		}
		contents = append(contents, elementContents(t)...)
		if t != nil {
			for _, v := range t.opts.Voices {
				contents = append(contents, content("type", "voice", "voice_id", v.VoiceID, "id", v.ID))
			}
		}
		body := jsonorder.New()
		body.Set("model", modelID)
		body.Set("contents", nonNil(contents))
		return "omni-video", withSettingsAndOptions(body, []setting{
			resolutionSetting, aspectRatioSetting(opts), durationSetting(opts), multiShotSetting, audioSetting,
		}, t)
	}

	if hasReferences {
		*warnings = append(*warnings, types.Warning{
			Type: "unsupported", Feature: "inputReferences",
			Details: modelID + " does not accept reference inputs; use an omni model such as kling-video-v3-omni or kling-video-v4. The references were ignored.",
		})
	}

	if firstFrame != nil {
		if opts.AspectRatio != "" {
			*warnings = append(*warnings, types.Warning{
				Type: "unsupported", Feature: "aspectRatio",
				Details: "TokenHub Kling image-to-video follows the aspect ratio of the input image.",
			})
		}
		contents := promptContent(opts)
		contents = append(contents, content("type", "first_frame", "url", toURLOrDataURI(firstFrame)))
		if lastFrame != nil {
			contents = append(contents, content("type", "last_frame", "url", toURLOrDataURI(lastFrame)))
		}
		contents = append(contents, elementContents(t)...)
		body := jsonorder.New()
		body.Set("model", modelID)
		body.Set("contents", contents)
		return "image-to-video", withSettingsAndOptions(body, []setting{
			resolutionSetting, durationSetting(opts), multiShotSetting, audioSetting,
		}, t)
	}

	if lastFrame != nil {
		*warnings = append(*warnings, types.Warning{
			Type: "unsupported", Feature: "frameImages",
			Details: "TokenHub Kling does not support a last frame without a first frame; the last frame was ignored.",
		})
	}
	if elements > 0 {
		*warnings = append(*warnings, types.Warning{
			Type: "unsupported", Feature: "providerOptions.tencent.elements",
			Details: "TokenHub Kling text-to-video does not accept elements; the elements were ignored.",
		})
	}
	body := jsonorder.New()
	body.Set("model", modelID)
	if promptPresent(opts) {
		body.Set("prompt", opts.Prompt)
	}
	return "text-to-video", withSettingsAndOptions(body, []setting{
		resolutionSetting, aspectRatioSetting(opts), durationSetting(opts), multiShotSetting, audioSetting,
	}, t)
}

func nonNil(list []interface{}) []interface{} {
	if list == nil {
		return []interface{}{}
	}
	return list
}

// resolveAudio is TS resolveAudio.
func resolveAudio(opts *provider.VideoModelV3CallOptions, t *parsedVideoOptions, supported bool, warnings *[]types.Warning) (string, bool) {
	audio, has := "", false
	switch {
	case t != nil && t.opts.Audio != nil:
		audio, has = *t.opts.Audio, true
	case opts.GenerateAudio != nil:
		audio, has = "off", true
		if *opts.GenerateAudio {
			audio = "native"
		}
	}
	if has && !supported {
		*warnings = append(*warnings, types.Warning{
			Type: "unsupported", Feature: "generateAudio",
			Details: "This TokenHub Kling model does not support the audio setting.",
		})
		return "", false
	}
	return audio, has
}

// resolveMultiShot is TS resolveMultiShot.
func resolveMultiShot(t *parsedVideoOptions, supported bool, warnings *[]types.Warning) (bool, bool) {
	if t == nil || t.opts.MultiShot == nil {
		return false, false
	}
	if !supported {
		*warnings = append(*warnings, types.Warning{
			Type: "unsupported", Feature: "providerOptions.tencent.multiShot",
			Details: "This TokenHub Kling model does not support multi-shot output.",
		})
		return false, false
	}
	return *t.opts.MultiShot, true
}

// buildMotionControlBody is TS buildMotionControlBody.
func buildMotionControlBody(modelID string, opts *provider.VideoModelV3CallOptions, t *parsedVideoOptions, characterImage, motionVideo, lastFrame *provider.VideoModelV3File, extraReferenceCount int, resolution setting, warnings *[]types.Warning) *jsonorder.Object {
	if opts.Duration != nil {
		*warnings = append(*warnings, types.Warning{Type: "unsupported", Feature: "duration", Details: "Motion control output follows the length of the motion reference video."})
	}
	if opts.AspectRatio != "" {
		*warnings = append(*warnings, types.Warning{Type: "unsupported", Feature: "aspectRatio", Details: "Motion control output follows the aspect ratio of the input media."})
	}
	if lastFrame != nil {
		*warnings = append(*warnings, types.Warning{Type: "unsupported", Feature: "frameImages", Details: "Motion control does not support a last frame; it was ignored."})
	}
	if extraReferenceCount > 0 {
		*warnings = append(*warnings, types.Warning{
			Type: "unsupported", Feature: "inputReferences",
			Details: "Motion control uses one character image and one motion reference video; additional references were ignored.",
		})
	}

	audio := setting{key: "audio"}
	if t != nil && t.opts.Audio != nil {
		audio = setting{key: "audio", value: *t.opts.Audio, set: true}
	} else if opts.GenerateAudio != nil && !*opts.GenerateAudio {
		audio = setting{key: "audio", value: "off", set: true}
	}
	orientation := setting{key: "character_orientation"}
	if t != nil && t.opts.CharacterOrientation != nil {
		orientation = setting{key: "character_orientation", value: *t.opts.CharacterOrientation, set: true}
	}

	contents := promptContent(opts)
	if characterImage != nil {
		contents = append(contents, content("type", "image", "url", toURLOrDataURI(characterImage)))
	}
	if motionVideo != nil {
		contents = append(contents, content("type", "video", "url", toURLOrDataURI(motionVideo)))
	}
	contents = append(contents, elementContents(t)...)
	body := jsonorder.New()
	body.Set("model", modelID)
	body.Set("contents", nonNil(contents))
	return withSettingsAndOptions(body, []setting{orientation, audio, resolution}, t)
}

// tokenHubKlingCreateResponseSchema is TS tokenHubKlingCreateResponseSchema.
var tokenHubKlingCreateResponseSchema = zodlite.Object(
	zodlite.F("code", zodlite.Nullish(numberOrString)),
	zodlite.F("message", zodlite.Nullish(zodlite.String)),
	zodlite.F("request_id", zodlite.Nullish(zodlite.String)),
	zodlite.F("data", zodlite.Nullish(zodlite.Object(
		zodlite.F("id", zodlite.Nullish(zodlite.String)),
		zodlite.F("status", zodlite.Nullish(zodlite.String)),
	))),
)

var tokenHubKlingTaskSchema = zodlite.Object(
	zodlite.F("id", zodlite.Nullish(zodlite.String)),
	zodlite.F("status", zodlite.Nullish(zodlite.String)),
	zodlite.F("message", zodlite.Nullish(zodlite.String)),
	zodlite.F("outputs", zodlite.Nullish(zodlite.Array(zodlite.Object(
		zodlite.F("type", zodlite.Nullish(zodlite.String)),
		zodlite.F("id", zodlite.Nullish(zodlite.String)),
		zodlite.F("url", zodlite.Nullish(zodlite.String)),
		zodlite.F("duration", zodlite.Nullish(zodlite.Union(zodlite.String, zodlite.Number))),
	)))),
)

// tokenHubKlingStatusResponseSchema is TS tokenHubKlingStatusResponseSchema:
// data is the documented task array or a single task object.
var tokenHubKlingStatusResponseSchema = zodlite.Object(
	zodlite.F("code", zodlite.Nullish(numberOrString)),
	zodlite.F("message", zodlite.Nullish(zodlite.String)),
	zodlite.F("request_id", zodlite.Nullish(zodlite.String)),
	zodlite.F("data", zodlite.Nullish(zodlite.Union(zodlite.Array(tokenHubKlingTaskSchema), tokenHubKlingTaskSchema))),
	zodlite.F("tokenhub_usage", zodlite.Nullish(zodlite.Object(
		zodlite.F("total_tokens", zodlite.Nullish(zodlite.Number)),
	))),
)

// pick copies the listed keys of m that are present (zod strips the others).
func pick(m map[string]interface{}, keys ...string) map[string]interface{} {
	if m == nil {
		return nil
	}
	out := make(map[string]interface{}, len(keys))
	for _, k := range keys {
		if v, ok := m[k]; ok {
			out[k] = v
		}
	}
	return out
}

func stripCreateResponse(raw map[string]interface{}) map[string]interface{} {
	out := pick(raw, "code", "message", "request_id", "data")
	if data, ok := out["data"].(map[string]interface{}); ok {
		out["data"] = pick(data, "id", "status")
	}
	return out
}

func stripTask(task map[string]interface{}) map[string]interface{} {
	out := pick(task, "id", "status", "message", "outputs")
	if outputs, ok := out["outputs"].([]interface{}); ok {
		stripped := make([]interface{}, len(outputs))
		for i, o := range outputs {
			om, _ := o.(map[string]interface{})
			stripped[i] = pick(om, "type", "id", "url", "duration")
		}
		out["outputs"] = stripped
	}
	return out
}

func stripStatusResponse(raw map[string]interface{}) map[string]interface{} {
	out := pick(raw, "code", "message", "request_id", "data", "tokenhub_usage")
	switch data := out["data"].(type) {
	case []interface{}:
		stripped := make([]interface{}, len(data))
		for i, task := range data {
			tm, _ := task.(map[string]interface{})
			stripped[i] = stripTask(tm)
		}
		out["data"] = stripped
	case map[string]interface{}:
		out["data"] = stripTask(data)
	}
	if usage, ok := out["tokenhub_usage"].(map[string]interface{}); ok {
		out["tokenhub_usage"] = pick(usage, "total_tokens")
	}
	return out
}

// interpretTokenHubKlingStatus is TS interpretTokenHubKlingStatus.
func interpretTokenHubKlingStatus(status map[string]interface{}, taskID string, response provider.VideoModelV3ResponseInfo) *provider.VideoModelV3OperationStatusResult {
	var task map[string]interface{}
	switch data := status["data"].(type) {
	case []interface{}:
		for _, entry := range data {
			if m, ok := entry.(map[string]interface{}); ok {
				if id, _ := m["id"].(string); id == taskID && m["id"] != nil {
					task = m
					break
				}
			}
		}
		if task == nil && len(data) > 0 {
			task, _ = data[0].(map[string]interface{})
		}
	case map[string]interface{}:
		task = data
	}
	taskStatus, _ := task["status"].(string)

	if taskStatus == "succeeded" {
		var outputs []map[string]interface{}
		list, _ := task["outputs"].([]interface{})
		for _, o := range list {
			om, _ := o.(map[string]interface{})
			_, hasURL := om["url"].(string)
			typ, _ := om["type"].(string)
			if om["type"] == nil {
				typ = "video"
			}
			if hasURL && typ == "video" {
				outputs = append(outputs, om)
			}
		}
		if len(outputs) == 0 {
			return &provider.VideoModelV3OperationStatusResult{
				Status:   provider.VideoOperationStatusError,
				Error:    "TokenHub Kling task " + taskID + " succeeded without a video URL.",
				Response: response,
			}
		}
		videos := make([]provider.VideoModelV3VideoData, len(outputs))
		videoMeta := make([]interface{}, len(outputs))
		for i, o := range outputs {
			url := o["url"].(string)
			videos[i] = provider.VideoModelV3VideoData{Type: provider.VideoDataTypeURL, URL: url, MediaType: "video/mp4"}
			entry := map[string]interface{}{"url": url}
			if id, ok := o["id"].(string); ok {
				entry["id"] = id
			}
			// TokenHub reports the duration as a string; VOD as a number.
			var duration float64
			hasDuration := false
			switch d := o["duration"].(type) {
			case string:
				if d != "" {
					duration, hasDuration = jsNumber(d), true
				}
			case float64:
				duration, hasDuration = d, true
			}
			if hasDuration && !math.IsNaN(duration) && !math.IsInf(duration, 0) {
				entry["duration"] = duration
			}
			videoMeta[i] = entry
		}
		meta := map[string]interface{}{"taskId": taskID, "videos": videoMeta}
		if requestID, ok := status["request_id"].(string); ok {
			meta["requestId"] = requestID
		}
		if usage, ok := status["tokenhub_usage"].(map[string]interface{}); ok {
			if total, ok := usage["total_tokens"].(float64); ok {
				meta["usage"] = map[string]interface{}{"totalTokens": total}
			}
		}
		return &provider.VideoModelV3OperationStatusResult{
			Status:           provider.VideoOperationStatusCompleted,
			Videos:           videos,
			Warnings:         []types.Warning{},
			Response:         response,
			ProviderMetadata: map[string]interface{}{"tencent": meta},
		}
	}

	if taskStatus == "failed" {
		message, _ := task["message"].(string)
		if message == "" {
			message = "unknown error"
		}
		return &provider.VideoModelV3OperationStatusResult{
			Status:   provider.VideoOperationStatusError,
			Error:    "Video generation failed: " + message + " (task " + taskID + ")",
			Response: response,
		}
	}
	return &provider.VideoModelV3OperationStatusResult{Status: provider.VideoOperationStatusPending, Response: response}
}
