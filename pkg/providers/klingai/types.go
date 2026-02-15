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

	// I2V-specific options

	// ImageTail is the end frame image for start+end frame control (pro mode)
	ImageTail *string `json:"imageTail,omitempty"`

	// StaticMask is the static brush mask image for I2V motion brush
	StaticMask *string `json:"staticMask,omitempty"`

	// DynamicMasks are dynamic brush configurations for I2V motion brush
	DynamicMasks []DynamicMask `json:"dynamicMasks,omitempty"`

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
