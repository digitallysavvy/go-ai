package klingai

// VideoMode represents the KlingAI video generation mode
type VideoMode string

const (
	// VideoModeT2V is text-to-video generation
	VideoModeT2V VideoMode = "t2v"

	// VideoModeI2V is image-to-video generation
	VideoModeI2V VideoMode = "i2v"

	// VideoModeMotionControl is motion control video generation
	VideoModeMotionControl VideoMode = "motion-control"
)

// ProviderOptions contains KlingAI-specific options for video generation
type ProviderOptions struct {
	// Mode is the generation quality mode ("std" or "pro")
	Mode *string `json:"mode,omitempty"`

	// PollIntervalMs is the polling interval in milliseconds (default: 5000)
	PollIntervalMs *int `json:"pollIntervalMs,omitempty"`

	// PollTimeoutMs is the maximum polling time in milliseconds (default: 600000 = 10 minutes)
	PollTimeoutMs *int `json:"pollTimeoutMs,omitempty"`

	// T2V and I2V options

	// NegativePrompt specifies what to avoid (max 2500 characters)
	NegativePrompt *string `json:"negativePrompt,omitempty"`

	// Sound controls audio generation ("on" or "off", V2.6+ pro mode only)
	Sound *string `json:"sound,omitempty"`

	// CfgScale controls prompt adherence (0.0-1.0, V1.x models only)
	CfgScale *float64 `json:"cfgScale,omitempty"`

	// CameraControl configures camera movement
	CameraControl *CameraControl `json:"cameraControl,omitempty"`

	// v3.0 multi-shot options (T2V and I2V, Kling v3.0+)

	// MultiShot enables multi-shot video generation. When true, the video is split
	// into up to 6 storyboard shots. When true, the main prompt is ignored by the API.
	MultiShot *bool `json:"multiShot,omitempty"`

	// ShotType is the storyboard method for multi-shot generation ("customize" or "intelligence").
	// Required when MultiShot is true.
	ShotType *string `json:"shotType,omitempty"`

	// MultiPrompt contains per-shot details for multi-shot generation.
	// Required when MultiShot is true and ShotType is "customize".
	MultiPrompt []MultiShotPrompt `json:"multiPrompt,omitempty"`

	// v3.0 voice control (T2V and I2V, Kling v3.0+)

	// VoiceList contains voice references for voice control (up to 2).
	// Referenced via <<<voice_1>>> template syntax in the prompt.
	// When used, Sound should be set to "on".
	VoiceList []VoiceRef `json:"voiceList,omitempty"`

	// I2V-specific options

	// ImageTail is the end frame image for start+end frame control (pro mode)
	ImageTail *string `json:"imageTail,omitempty"`

	// StaticMask is the static brush mask image for I2V motion brush
	StaticMask *string `json:"staticMask,omitempty"`

	// DynamicMasks are dynamic brush configurations for I2V motion brush
	DynamicMasks []DynamicMask `json:"dynamicMasks,omitempty"`

	// v3.0 element control (I2V only, Kling v3.0+)

	// ElementList contains reference elements for element control (up to 3).
	// Cannot coexist with VoiceList on the I2V endpoint.
	ElementList []ElementRef `json:"elementList,omitempty"`

	// Motion-control-specific options

	// VideoUrl is the reference video URL for motion control
	VideoUrl *string `json:"videoUrl,omitempty"`

	// CharacterOrientation is the character orientation ("image" or "video")
	CharacterOrientation *string `json:"characterOrientation,omitempty"`

	// KeepOriginalSound controls original sound preservation ("yes" or "no")
	KeepOriginalSound *string `json:"keepOriginalSound,omitempty"`

	// WatermarkEnabled controls watermark generation
	WatermarkEnabled *bool `json:"watermarkEnabled,omitempty"`

	// Passthrough options (for future API additions)
	Additional map[string]interface{} `json:"-"`
}

// MultiShotPrompt contains per-shot details for multi-shot video generation (Kling v3.0+).
// Up to 6 shots. Shot durations must sum to the total duration.
type MultiShotPrompt struct {
	// Index is the shot index (1-based)
	Index int `json:"index"`
	// Prompt is the shot-specific prompt (max 512 characters)
	Prompt string `json:"prompt"`
	// Duration is the shot duration in seconds (as a string, e.g. "5")
	Duration string `json:"duration"`
}

// ElementRef references a video character or multi-image element for element control (Kling v3.0+ I2V).
type ElementRef struct {
	// ElementID is the element identifier
	ElementID int `json:"element_id"`
}

// VoiceRef references a voice for voice control (Kling v3.0+).
type VoiceRef struct {
	// VoiceID is the voice identifier
	VoiceID string `json:"voice_id"`
}

// CameraControl configures camera movement
type CameraControl struct {
	// Type is the camera movement type
	Type string `json:"type"`

	// Config contains movement parameters
	Config *CameraConfig `json:"config,omitempty"`
}

// CameraConfig contains camera movement parameters
type CameraConfig struct {
	Horizontal *float64 `json:"horizontal,omitempty"`
	Vertical   *float64 `json:"vertical,omitempty"`
	Pan        *float64 `json:"pan,omitempty"`
	Tilt       *float64 `json:"tilt,omitempty"`
	Roll       *float64 `json:"roll,omitempty"`
	Zoom       *float64 `json:"zoom,omitempty"`
}

// DynamicMask represents a dynamic brush configuration
type DynamicMask struct {
	Mask        string       `json:"mask"`
	Trajectories []Trajectory `json:"trajectories"`
}

// Trajectory represents a motion trajectory point
type Trajectory struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// API Response Types

// createTaskResponse is the response from task creation
type createTaskResponse struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
	Data      *struct {
		TaskID    string `json:"task_id"`
		TaskStatus string `json:"task_status,omitempty"`
		CreatedAt int64  `json:"created_at,omitempty"`
		UpdatedAt int64  `json:"updated_at,omitempty"`
	} `json:"data,omitempty"`
}

// taskStatusResponse is the response from task status polling
type taskStatusResponse struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
	Data      *struct {
		TaskID        string `json:"task_id"`
		TaskStatus    string `json:"task_status"`
		TaskStatusMsg string `json:"task_status_msg,omitempty"`
		CreatedAt     int64  `json:"created_at,omitempty"`
		UpdatedAt     int64  `json:"updated_at,omitempty"`
		TaskResult    *struct {
			Videos []struct {
				ID           string `json:"id,omitempty"`
				URL          string `json:"url,omitempty"`
				WatermarkURL string `json:"watermark_url,omitempty"`
				Duration     string `json:"duration,omitempty"`
			} `json:"videos,omitempty"`
		} `json:"task_result,omitempty"`
	} `json:"data,omitempty"`
}
