package minimax

// MiniMaxVideoModelID is a MiniMax video generation model identifier.
// https://platform.minimax.io/docs
type MiniMaxVideoModelID = string

// Named MiniMax video model ID constants (TS MiniMaxVideoModelId). Any model
// ID string is accepted; these constants exist for convenience and IDE
// support.
const (
	ModelH3    MiniMaxVideoModelID = "MiniMax-H3"
	ModelH3Max MiniMaxVideoModelID = "MiniMax-H3-Max"
)

// minimaxVideoRatios are the aspect ratios supported by MiniMax video models
// (TS minimaxVideoRatios).
var minimaxVideoRatios = map[string]bool{
	"adaptive": true,
	"21:9":     true,
	"16:9":     true,
	"4:3":      true,
	"1:1":      true,
	"3:4":      true,
	"9:16":     true,
}

// minimaxVideoResolutions are the output resolutions supported by MiniMax
// video models; availability depends on the selected model (TS
// minimaxVideoResolutions).
var minimaxVideoResolutions = map[string]bool{
	"480P": true,
	"768P": true,
	"2K":   true,
}

// minimaxResolutionSetting describes a model's supported resolution tiers
// and default (TS modelResolutionSettings).
type minimaxResolutionSetting struct {
	supported []string
	def       string
}

var minimaxModelResolutionSettings = map[string]minimaxResolutionSetting{
	ModelH3:    {supported: []string{"768P", "2K"}, def: minimaxDefaultResolution},
	ModelH3Max: {supported: []string{"480P", "768P"}, def: "768P"},
}

const (
	minimaxDefaultResolution   = "2K"
	minimaxDefaultAspectRatio  = "16:9"
	minimaxDefaultPollInterval = 10000  // ms
	minimaxDefaultPollTimeout  = 600000 // ms
	minimaxDefaultDuration     = 5      // seconds
	minimaxMaxDuration         = 15     // seconds
	minimaxMaxReferenceImages  = 9
	minimaxMaxReferenceVideos  = 3
	minimaxMaxReferenceAudios  = 3
)

// minimaxResolutionMap maps WxH pixel-size strings to a MiniMax resolution
// tier (TS RESOLUTION_MAP). MiniMax does not publish tier dimensions, so
// this is a closed table rather than a rule: 480P and 768P rows are named
// for the shorter side, the 2K rows for the longer one.
var minimaxResolutionMap = map[string]string{
	// 480P — square, landscape, portrait
	"480x480":  "480P",
	"1120x480": "480P",
	"854x480":  "480P",
	"640x480":  "480P",
	"480x854":  "480P",
	"480x640":  "480P",
	// 768P — square, landscape, portrait
	"768x768":  "768P",
	"1792x768": "768P",
	"1366x768": "768P",
	"1024x768": "768P",
	"768x1366": "768P",
	"768x1024": "768P",
	// 2K — square, landscape, portrait
	"2048x2048": "2K",
	"2560x1080": "2K",
	"2560x1440": "2K",
	"2048x1536": "2K",
	"1440x2560": "2K",
	"1536x2048": "2K",
}
