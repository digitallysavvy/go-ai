package tencent

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// VideoModelOptions are the provider-specific options for Tencent video
// generation, passed as ProviderOptions["tencent"] (TS
// TencentVideoModelOptions). Each option applies to TokenHub Kling models,
// VOD AIGC models, or both; an option that does not apply to the selected
// model is ignored with a warning.
type VideoModelOptions struct {
	// Resolution is the output resolution tier, sent as is; it wins over
	// the top-level Resolution. TokenHub Kling: 720p, 1080p, 4k. VOD: e.g.
	// 720P, 1080P, 2K, 4K depending on the model. Both backends.
	Resolution *string `json:"resolution,omitempty"`
	// Audio is the TokenHub Kling audio track mode: native, original or off.
	// It wins over the top-level GenerateAudio.
	Audio *string `json:"audio,omitempty"`
	// MultiShot (TokenHub Kling) asks for multi-shot output.
	MultiShot *bool `json:"multiShot,omitempty"`
	// CharacterOrientation (TokenHub Kling motion control): image or video.
	CharacterOrientation *string `json:"characterOrientation,omitempty"`
	// Elements (TokenHub Kling) are custom elements created through the
	// TokenHub element API.
	Elements []VideoElement `json:"elements,omitempty"`
	// Voices (TokenHub Kling omni) are referenced in the prompt as @id.
	Voices []VideoVoice `json:"voices,omitempty"`
	// ReferenceVideoType is how reference videos are used: feature
	// (default) or base. TokenHub Kling omni and VOD Kling.
	ReferenceVideoType *string `json:"referenceVideoType,omitempty"`
	// Watermark adds a watermark (TokenHub watermark_info, VOD LogoAdd).
	Watermark *bool `json:"watermark,omitempty"`
	// ExternalTaskID (TokenHub Kling) is a custom task ID.
	ExternalTaskID *string `json:"externalTaskId,omitempty"`
	// NegativePrompt (VOD) describes what to avoid.
	NegativePrompt *string `json:"negativePrompt,omitempty"`
	// EnhancePrompt (VOD) optimizes the prompt automatically.
	EnhancePrompt *bool `json:"enhancePrompt,omitempty"`
	// InputRegion (VOD): Oversea, OverseaUSWest or Mainland.
	InputRegion *string `json:"inputRegion,omitempty"`
	// SceneType (VOD), e.g. motion_control, avatar_i2v or lip_sync.
	SceneType *string `json:"sceneType,omitempty"`
	// PersonGeneration (VOD): AllowAdult or Disallowed.
	PersonGeneration *string `json:"personGeneration,omitempty"`
	// InputComplianceCheck (VOD) checks the input.
	InputComplianceCheck *bool `json:"inputComplianceCheck,omitempty"`
	// OutputComplianceCheck (VOD) checks the output.
	OutputComplianceCheck *bool `json:"outputComplianceCheck,omitempty"`
	// EnhanceSwitch (VOD) upscales output with video enhancement.
	EnhanceSwitch *bool `json:"enhanceSwitch,omitempty"`
	// OffPeak (VOD) runs the task in off-peak mode.
	OffPeak *bool `json:"offPeak,omitempty"`
	// SessionID (VOD) is a deduplication identifier.
	SessionID *string `json:"sessionId,omitempty"`
	// SessionContext (VOD) is passed through to callbacks.
	SessionContext *string `json:"sessionContext,omitempty"`
	// TasksPriority (VOD) is an integer from -10 to 10.
	TasksPriority *int `json:"tasksPriority,omitempty"`
	// ExtInfo (VOD) is a reserved field sent as is.
	ExtInfo *string `json:"extInfo,omitempty"`
	// Assets (VOD) are CreateAigcMaterial materials referenced by their
	// asset:// URL, sent after the other file inputs.
	Assets []VideoAsset `json:"assets,omitempty"`
}

// VideoElement is a TokenHub Kling element (TS {elementId, id?}).
type VideoElement struct {
	ElementID string  `json:"elementId"`
	ID        *string `json:"id,omitempty"`
}

// VideoVoice is a TokenHub Kling omni voice (TS {voiceId, id}).
type VideoVoice struct {
	VoiceID string `json:"voiceId"`
	ID      string `json:"id"`
}

// VideoAsset is a VOD material (TS {url, usage?, category?}). Usage is
// FirstFrame, LastFrame or Reference (default); Category is Image (default),
// Video or Audio.
type VideoAsset struct {
	URL      string  `json:"url"`
	Usage    *string `json:"usage,omitempty"`
	Category *string `json:"category,omitempty"`
}

var tasksPriorityMin, tasksPriorityMax = -10, 10

// videoOptionsSchema is TS tencentVideoModelOptionsSchema (a z.object with
// .optional() members: unknown keys pass, null fails).
var videoOptionsSchema = providerutils.OptionSchema{
	"resolution":           providerutils.OptString,
	"audio":                providerutils.OptEnum("native", "original", "off"),
	"multiShot":            providerutils.OptBool,
	"characterOrientation": providerutils.OptEnum("image", "video"),
	"elements": providerutils.OptArray(providerutils.OptObjectWith(providerutils.OptionSchema{
		"elementId": providerutils.OptString,
		"id":        providerutils.OptString,
	}, "elementId"), 0, -1),
	"voices": providerutils.OptArray(providerutils.OptObjectWith(providerutils.OptionSchema{
		"voiceId": providerutils.OptString,
		"id":      providerutils.OptString,
	}, "voiceId", "id"), 0, -1),
	"referenceVideoType":    providerutils.OptEnum("feature", "base"),
	"watermark":             providerutils.OptBool,
	"externalTaskId":        providerutils.OptString,
	"negativePrompt":        providerutils.OptString,
	"enhancePrompt":         providerutils.OptBool,
	"inputRegion":           providerutils.OptEnum("Oversea", "OverseaUSWest", "Mainland"),
	"sceneType":             providerutils.OptString,
	"personGeneration":      providerutils.OptEnum("AllowAdult", "Disallowed"),
	"inputComplianceCheck":  providerutils.OptBool,
	"outputComplianceCheck": providerutils.OptBool,
	"enhanceSwitch":         providerutils.OptBool,
	"offPeak":               providerutils.OptBool,
	"sessionId":             providerutils.OptString,
	"sessionContext":        providerutils.OptString,
	"tasksPriority":         providerutils.OptInt(&tasksPriorityMin, &tasksPriorityMax),
	"extInfo":               providerutils.OptString,
	"assets": providerutils.OptArray(providerutils.OptObjectWith(providerutils.OptionSchema{
		"url":      providerutils.OptString,
		"usage":    providerutils.OptEnum("FirstFrame", "LastFrame", "Reference"),
		"category": providerutils.OptEnum("Image", "Video", "Audio"),
	}, "url"), 0, -1),
}

// tokenHubOnlyOptions are the options only TokenHub Kling models read (TS
// TOKENHUB_ONLY_OPTIONS), in TS order.
var tokenHubOnlyOptions = []string{"audio", "multiShot", "characterOrientation", "elements", "voices", "externalTaskId"}

// vodOnlyOptions are the options only VOD models read (TS VOD_ONLY_OPTIONS),
// in TS order.
var vodOnlyOptions = []string{
	"negativePrompt", "enhancePrompt", "inputRegion", "sceneType", "personGeneration",
	"inputComplianceCheck", "outputComplianceCheck", "enhanceSwitch", "offPeak",
	"sessionId", "sessionContext", "tasksPriority", "extInfo", "assets",
}

// parsedVideoOptions is the validated providerOptions.tencent: the typed
// options and the set of keys present with a non-null value.
type parsedVideoOptions struct {
	opts    VideoModelOptions
	present map[string]bool
}

// parseVideoOptions is TS parseProviderOptions({provider: 'tencent', schema})
// for the video model. The options are normalized through their JSON form
// first, so Go-typed literals (typed structs, []map[string]string, int64)
// read like the decoded JSON. nil means no options.
func parseVideoOptions(providerOptions map[string]interface{}) (*parsedVideoOptions, error) {
	normalized := providerutils.NormalizeOptionsJSON(providerOptions)
	obj, err := providerutils.ParseProviderOptions("tencent", normalized, videoOptionsSchema)
	if err != nil || obj == nil {
		return nil, err
	}
	var opts VideoModelOptions
	encoded, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(encoded, &opts); err != nil {
		return nil, err
	}
	present := map[string]bool{}
	for k, v := range obj {
		if _, known := videoOptionsSchema[k]; known && v != nil {
			present[k] = true
		}
	}
	return &parsedVideoOptions{opts: opts, present: present}, nil
}
