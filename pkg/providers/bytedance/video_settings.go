package bytedance

// ByteDanceVideoModelID is a ByteDance video generation model identifier
type ByteDanceVideoModelID string

const (
	// ModelSeedance15Pro is the Seedance 1.5 Pro model (latest)
	ModelSeedance15Pro ByteDanceVideoModelID = "seedance-1-5-pro-251215"

	// ModelSeedance10Pro is the Seedance 1.0 Pro model
	ModelSeedance10Pro ByteDanceVideoModelID = "seedance-1-0-pro-250528"

	// ModelSeedance10ProFast is the Seedance 1.0 Pro Fast model
	ModelSeedance10ProFast ByteDanceVideoModelID = "seedance-1-0-pro-fast-251015"

	// ModelSeedance10LiteT2V is the Seedance 1.0 Lite text-to-video model
	ModelSeedance10LiteT2V ByteDanceVideoModelID = "seedance-1-0-lite-t2v-250428"

	// ModelSeedance10LiteI2V is the Seedance 1.0 Lite image-to-video model
	ModelSeedance10LiteI2V ByteDanceVideoModelID = "seedance-1-0-lite-i2v-250428"
)

// ProviderOptions contains ByteDance-specific options for video generation
type ProviderOptions struct {
	// Watermark controls watermark on the generated video
	Watermark *bool `json:"watermark,omitempty"`

	// GenerateAudio controls audio generation
	GenerateAudio *bool `json:"generateAudio,omitempty"`

	// CameraFixed controls whether the camera is fixed
	CameraFixed *bool `json:"cameraFixed,omitempty"`

	// ReturnLastFrame controls whether to return the last frame
	ReturnLastFrame *bool `json:"returnLastFrame,omitempty"`

	// ServiceTier controls the service tier ("default" or "flex")
	ServiceTier *string `json:"serviceTier,omitempty"`

	// Draft controls draft mode generation (faster but lower quality)
	Draft *bool `json:"draft,omitempty"`

	// LastFrameImage is the URL for the last frame in start+end frame generation
	LastFrameImage *string `json:"lastFrameImage,omitempty"`

	// ReferenceImages are URLs for reference images
	ReferenceImages []string `json:"referenceImages,omitempty"`

	// PollIntervalMs is the polling interval in milliseconds (default: 3000)
	PollIntervalMs *int `json:"pollIntervalMs,omitempty"`

	// PollTimeoutMs is the maximum polling time in milliseconds (default: 300000 = 5 minutes)
	PollTimeoutMs *int `json:"pollTimeoutMs,omitempty"`

	// Additional passthrough options not explicitly handled
	Additional map[string]interface{} `json:"-"`
}

// resolutionMap maps WxH resolution strings to ByteDance API resolution values
var resolutionMap = map[string]string{
	"864x496":   "480p",
	"496x864":   "480p",
	"752x560":   "480p",
	"560x752":   "480p",
	"640x640":   "480p",
	"992x432":   "480p",
	"432x992":   "480p",
	"864x480":   "480p",
	"480x864":   "480p",
	"736x544":   "480p",
	"544x736":   "480p",
	"960x416":   "480p",
	"416x960":   "480p",
	"832x480":   "480p",
	"480x832":   "480p",
	"624x624":   "480p",
	"1280x720":  "720p",
	"720x1280":  "720p",
	"1112x834":  "720p",
	"834x1112":  "720p",
	"960x960":   "720p",
	"1470x630":  "720p",
	"630x1470":  "720p",
	"1248x704":  "720p",
	"704x1248":  "720p",
	"1120x832":  "720p",
	"832x1120":  "720p",
	"1504x640":  "720p",
	"640x1504":  "720p",
	"1920x1080": "1080p",
	"1080x1920": "1080p",
	"1664x1248": "1080p",
	"1248x1664": "1080p",
	"1440x1440": "1080p",
	"2206x946":  "1080p",
	"946x2206":  "1080p",
	"1920x1088": "1080p",
	"1088x1920": "1080p",
	"2176x928":  "1080p",
	"928x2176":  "1080p",
}

// mapResolution maps a WxH resolution string to the ByteDance API format.
// Returns the mapped value if known, or the original string if not in the map.
func mapResolution(resolution string) string {
	if mapped, ok := resolutionMap[resolution]; ok {
		return mapped
	}
	return resolution
}
