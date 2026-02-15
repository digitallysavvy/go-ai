# KlingAI Provider for Go-AI SDK

The KlingAI provider implements video generation capabilities for the Go-AI SDK, supporting text-to-video, image-to-video, and motion control video generation.

## Installation

```bash
go get github.com/digitallysavvy/go-ai
```

## Quick Start

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/digitallysavvy/go-ai/pkg/providers/klingai"
)

func main() {
	// Create provider with credentials
	provider, err := klingai.New(klingai.Config{
		AccessKey: "your-access-key",
		SecretKey: "your-secret-key",
	})
	if err != nil {
		log.Fatal(err)
	}

	// Get a video model
	model, err := provider.VideoModel("kling-v2.6-t2v")
	if err != nil {
		log.Fatal(err)
	}

	// Generate a video
	ctx := context.Background()
	duration := 5.0

	response, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
		Prompt:      "A sunrise over mountains in 4K",
		AspectRatio: "16:9",
		Duration:    &duration,
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Generated video: %s\n", response.Videos[0].URL)
}
```

## Authentication

KlingAI uses access key and secret key authentication. You can provide credentials in two ways:

### Environment Variables

```bash
export KLINGAI_ACCESS_KEY=your-access-key
export KLINGAI_SECRET_KEY=your-secret-key
```

```go
provider, err := klingai.New(klingai.Config{})
```

### Explicit Configuration

```go
provider, err := klingai.New(klingai.Config{
	AccessKey: "your-access-key",
	SecretKey: "your-secret-key",
})
```

## Video Generation Modes

KlingAI supports three video generation modes, determined by the model ID suffix:

### Text-to-Video (`-t2v`)

Generate videos from text prompts.

**Available Models:**
- `kling-v1-t2v`
- `kling-v1.6-t2v`
- `kling-v2-master-t2v`
- `kling-v2.1-master-t2v`
- `kling-v2.5-turbo-t2v`
- `kling-v2.6-t2v`

**Example:**

```go
model, _ := provider.VideoModel("kling-v2.6-t2v")

duration := 10.0
response, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
	Prompt:      "A chicken flying into the sunset in the style of 90s anime",
	AspectRatio: "16:9",
	Duration:    &duration,
	ProviderOptions: map[string]interface{}{
		"klingai": map[string]interface{}{
			"mode": "std",
			"negativePrompt": "low quality, blurry",
		},
	},
})
```

### Image-to-Video (`-i2v`)

Animate static images with optional start and end frame control.

**Available Models:**
- `kling-v1-i2v`
- `kling-v1.5-i2v`
- `kling-v1.6-i2v`
- `kling-v2-master-i2v`
- `kling-v2.1-i2v`
- `kling-v2.1-master-i2v`
- `kling-v2.5-turbo-i2v`
- `kling-v2.6-i2v`

**Example:**

```go
model, _ := provider.VideoModel("kling-v2.6-i2v")

duration := 5.0
imageTail := "https://example.com/end-frame.png"

response, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
	Prompt: "The cat slowly turns its head and blinks",
	Image: &provider.VideoModelV3File{
		Type: "url",
		URL:  "https://example.com/start-frame.png",
	},
	Duration: &duration,
	ProviderOptions: map[string]interface{}{
		"klingai": map[string]interface{}{
			"mode": "pro",
			"imageTail": imageTail, // End frame control
		},
	},
})
```

### Motion Control (`-motion-control`)

Generate videos using reference motion videos.

**Available Models:**
- `kling-v2.6-motion-control`

**Example:**

```go
model, _ := provider.VideoModel("kling-v2.6-motion-control")

response, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
	Prompt: "The character performs a smooth dance move",
	Image: &provider.VideoModelV3File{
		Type: "url",
		URL:  "https://example.com/character.png",
	},
	ProviderOptions: map[string]interface{}{
		"klingai": map[string]interface{}{
			"videoUrl": "https://example.com/reference-motion.mp4",
			"characterOrientation": "image",
			"mode": "std",
			"keepOriginalSound": "yes",
		},
	},
})
```

## Provider Options

Configure generation with `ProviderOptions.klingai`:

| Option | Type | Modes | Description |
|--------|------|-------|-------------|
| `mode` | `string` | All | `"std"` or `"pro"` quality mode |
| `negativePrompt` | `string` | T2V, I2V | What to avoid (max 2500 chars) |
| `sound` | `string` | T2V, I2V | `"on"` or `"off"` (V2.6+ pro only) |
| `cfgScale` | `float64` | T2V, I2V | Prompt adherence 0.0-1.0 (V1.x only) |
| `cameraControl` | `CameraControl` | T2V, I2V | Camera movement configuration |
| `imageTail` | `string` | I2V | End frame image URL or base64 |
| `staticMask` | `string` | I2V | Static brush mask image |
| `dynamicMasks` | `[]DynamicMask` | I2V | Dynamic brush configurations |
| `videoUrl` | `string` | Motion | Reference video URL (required) |
| `characterOrientation` | `string` | Motion | `"image"` or `"video"` (required) |
| `keepOriginalSound` | `string` | Motion | `"yes"` or `"no"` |
| `watermarkEnabled` | `bool` | Motion | Enable watermark |
| `pollIntervalMs` | `int` | All | Polling interval (default: 5000ms) |
| `pollTimeoutMs` | `int` | All | Max wait time (default: 600000ms) |

## Polling Behavior

KlingAI video generation is asynchronous. The SDK automatically polls for completion:

- **Default interval:** 5 seconds
- **Default timeout:** 10 minutes
- **Configurable** via provider options

```go
response, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
	Prompt: "test",
	ProviderOptions: map[string]interface{}{
		"klingai": map[string]interface{}{
			"pollIntervalMs": 3000,  // Poll every 3 seconds
			"pollTimeoutMs": 300000, // 5 minute timeout
		},
	},
})
```

## Camera Control

Configure camera movement for T2V and I2V:

```go
ProviderOptions: map[string]interface{}{
	"klingai": map[string]interface{}{
		"cameraControl": map[string]interface{}{
			"type": "forward_up",
			"config": map[string]interface{}{
				"horizontal": 0.5,
				"vertical": 0.3,
				"zoom": 0.2,
			},
		},
	},
}
```

**Camera Types:**
- `"simple"`
- `"down_back"`
- `"forward_up"`
- `"right_turn_forward"`
- `"left_turn_forward"`

## Error Handling

The provider includes typed errors for common scenarios:

```go
response, err := model.DoGenerate(ctx, opts)
if err != nil {
	if klingErr, ok := err.(*klingai.Error); ok {
		switch klingErr.Code {
		case 401:
			log.Println("Authentication failed")
		case 504:
			log.Println("Generation timeout")
		default:
			log.Printf("KlingAI error %d: %s", klingErr.Code, klingErr.Message)
		}
	}
	return err
}
```

## Response Format

Generated videos are returned with metadata:

```go
type VideoModelV3Response struct {
	Videos []VideoModelV3VideoData  // Generated videos
	Warnings []types.Warning          // Generation warnings
	ProviderMetadata map[string]interface{}  // KlingAI-specific metadata
	Response VideoModelV3ResponseInfo // Response info
}

// Video data
response.Videos[0].URL         // Video URL
response.Videos[0].MediaType   // "video/mp4"

// Provider metadata
metadata := response.ProviderMetadata["klingai"].(map[string]interface{})
taskID := metadata["taskId"].(string)
videos := metadata["videos"].([]map[string]interface{})
watermarkURL := videos[0]["watermarkUrl"].(string)
```

## Limitations

- **Resolution:** Not configurable (determined by model/image)
- **FPS:** Not configurable
- **Seed:** Not supported for deterministic generation
- **Batch generation:** One video per API call
- **Max videos per call:** 1

Attempting to use these options will generate warnings but not fail.

## API Reference

### Types

```go
type Config struct {
	AccessKey string // KlingAI access key
	SecretKey string // KlingAI secret key
	BaseURL   string // Optional custom base URL
	Headers   map[string]string // Additional headers
}

type ProviderOptions struct {
	Mode               *string
	PollIntervalMs     *int
	PollTimeoutMs      *int
	NegativePrompt     *string
	Sound              *string
	CfgScale           *float64
	CameraControl      *CameraControl
	ImageTail          *string
	StaticMask         *string
	DynamicMasks       []DynamicMask
	VideoUrl           *string
	CharacterOrientation *string
	KeepOriginalSound  *string
	WatermarkEnabled   *bool
	Additional         map[string]interface{}
}

type CameraControl struct {
	Type   string         // Camera movement type
	Config *CameraConfig  // Movement parameters
}

type CameraConfig struct {
	Horizontal *float64
	Vertical   *float64
	Pan        *float64
	Tilt       *float64
	Roll       *float64
	Zoom       *float64
}
```

### Functions

```go
// Create a new KlingAI provider
func New(cfg Config) (*Provider, error)

// Get a video generation model
func (p *Provider) VideoModel(modelID string) (provider.VideoModelV3, error)

// Generate a video
func (m *VideoModel) DoGenerate(ctx context.Context, opts *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error)
```

## Examples

See the `examples/` directory for complete working examples:

- `text-to-video/` - Basic text-to-video generation
- `image-to-video/` - Image animation with start/end frames
- `motion-control/` - Reference motion video generation

## Links

- [KlingAI API Documentation](https://app.klingai.com/global/dev/document-api)
- [KlingAI Capability Map](https://app.klingai.com/global/dev/document-api/apiReference/model/skillsMap)
- [Go-AI SDK Documentation](https://github.com/digitallysavvy/go-ai)

## License

Apache 2.0
