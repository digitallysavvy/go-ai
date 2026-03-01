# ByteDance Provider for Go-AI SDK

The ByteDance (Volcengine) provider implements video generation capabilities for the Go-AI SDK, supporting text-to-video and image-to-video generation via the Ark API.

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

    "github.com/digitallysavvy/go-ai/pkg/provider"
    "github.com/digitallysavvy/go-ai/pkg/providers/bytedance"
)

func main() {
    prov, err := bytedance.New(bytedance.Config{
        APIKey: "your-ark-api-key",
    })
    if err != nil {
        log.Fatal(err)
    }

    model, err := prov.VideoModel(string(bytedance.ModelSeedance10Pro))
    if err != nil {
        log.Fatal(err)
    }

    ctx := context.Background()
    dur := 5.0

    response, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
        Prompt:      "A sunrise over mountains in 4K",
        AspectRatio: "16:9",
        Duration:    &dur,
    })
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Generated video: %s\n", response.Videos[0].URL)
}
```

## Authentication

Obtain an API key from the [ByteDance Ark platform](https://ark.volces.com). The provider reads credentials in two ways:

### Environment Variable

```bash
export BYTEDANCE_API_KEY=your-ark-api-key
```

```go
prov, err := bytedance.New(bytedance.Config{})
```

### Explicit Configuration

```go
prov, err := bytedance.New(bytedance.Config{
    APIKey: "your-ark-api-key",
})
```

## Available Models

| Constant | Model ID | Description |
|---|---|---|
| `ModelSeedance15Pro` | `seedance-1-5-pro-251215` | Seedance 1.5 Pro (latest) |
| `ModelSeedance10Pro` | `seedance-1-0-pro-250528` | Seedance 1.0 Pro |
| `ModelSeedance10ProFast` | `seedance-1-0-pro-fast-251015` | Seedance 1.0 Pro Fast |
| `ModelSeedance10LiteT2V` | `seedance-1-0-lite-t2v-250428` | Seedance 1.0 Lite (text-to-video) |
| `ModelSeedance10LiteI2V` | `seedance-1-0-lite-i2v-250428` | Seedance 1.0 Lite (image-to-video) |

You may also pass any model ID string directly:

```go
model, err := prov.VideoModel("seedance-1-5-pro-251215")
```

## Text-to-Video

```go
dur := 5.0
response, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
    Prompt:      "A cherry blossom tree sways in the spring breeze",
    AspectRatio: "16:9",
    Duration:    &dur,
})
```

## Image-to-Video

Animate a static image by providing an `Image` field:

```go
response, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
    Prompt: "The cat slowly turns its head",
    Image: &provider.VideoModelV3File{
        Type: "url",
        URL:  "https://example.com/cat.png",
    },
})
```

To use a local image file:

```go
data, _ := os.ReadFile("cat.png")
response, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
    Prompt: "The cat slowly turns its head",
    Image: &provider.VideoModelV3File{
        Type:      "file",
        Data:      data,
        MediaType: "image/png",
    },
})
```

## Provider-Specific Options

Configure generation with `ProviderOptions["bytedance"]`:

| Option | Type | Description |
|--------|------|-------------|
| `watermark` | `bool` | Add watermark to the video |
| `generateAudio` | `bool` | Generate audio for the video |
| `cameraFixed` | `bool` | Fix the camera position |
| `returnLastFrame` | `bool` | Return the last frame of the video |
| `serviceTier` | `string` | Service tier: `"default"` or `"flex"` |
| `draft` | `bool` | Draft mode (faster, lower quality) |
| `lastFrameImage` | `string` | URL for end-frame in start+end generation |
| `referenceImages` | `[]string` | Reference image URLs |
| `pollIntervalMs` | `int` | Polling interval (default: 3000ms) |
| `pollTimeoutMs` | `int` | Max wait time (default: 300000ms = 5 minutes) |

**Example:**

```go
response, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
    Prompt: "A futuristic city",
    ProviderOptions: map[string]interface{}{
        "bytedance": map[string]interface{}{
            "generateAudio": true,
            "serviceTier":   "flex",
            "draft":         false,
        },
    },
})
```

## Start + End Frame Generation

Generate a video that transitions between a first and last frame:

```go
response, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
    Prompt: "The flower blooms",
    Image: &provider.VideoModelV3File{
        Type: "url",
        URL:  "https://example.com/bud.png", // first frame
    },
    ProviderOptions: map[string]interface{}{
        "bytedance": map[string]interface{}{
            "lastFrameImage": "https://example.com/bloom.png", // last frame
        },
    },
})
```

## Resolution

Pass a `WxH` resolution string; it is automatically mapped to the ByteDance API format:

```go
response, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
    Prompt:     "Mountain sunrise",
    Resolution: "1920x1080", // maps to "1080p"
})
```

Supported resolutions are mapped as follows:
- 1920×1080 and similar → `"1080p"`
- 1280×720 and similar → `"720p"`
- 864×480 and similar → `"480p"`

Unmapped values are passed through unchanged.

## Polling Behavior

ByteDance video generation is asynchronous. The SDK polls the status endpoint until the video is ready:

- **Default interval:** 3 seconds
- **Default timeout:** 5 minutes
- **Configurable** via provider options

```go
response, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
    Prompt: "test",
    ProviderOptions: map[string]interface{}{
        "bytedance": map[string]interface{}{
            "pollIntervalMs": 5000,   // poll every 5 seconds
            "pollTimeoutMs":  600000, // 10-minute timeout
        },
    },
})
```

Context cancellation is also respected — cancelling the context will stop polling immediately.

## Error Handling

```go
response, err := model.DoGenerate(ctx, opts)
if err != nil {
    if bdErr, ok := err.(*bytedance.Error); ok {
        switch bdErr.Code {
        case 400:
            log.Println("Invalid request:", bdErr.Message)
        case 504:
            log.Println("Generation timed out")
        default:
            log.Printf("ByteDance error %d: %s", bdErr.Code, bdErr.Message)
        }
    }
}
```

## Response Format

```go
type VideoModelV3Response struct {
    Videos           []VideoModelV3VideoData         // Generated videos
    Warnings         []types.Warning                 // Generation warnings
    ProviderMetadata map[string]interface{}           // ByteDance-specific metadata
    Response         VideoModelV3ResponseInfo         // Response info
}

// Video data
response.Videos[0].URL       // Video URL
response.Videos[0].MediaType // "video/mp4"

// Provider metadata
meta := response.ProviderMetadata["bytedance"].(map[string]interface{})
taskID := meta["taskId"].(string)
```

## Limitations

- **FPS:** Not configurable (fixed at 24 fps)
- **Batch generation:** One video per API call
- **Region:** Ark API is primarily accessible from the Asia-Pacific region

Attempting to use `FPS` or `N > 1` generates a warning but does not fail.

## Examples

See `examples/providers/bytedance/` for complete working examples:

- `01-text-to-video.go` — Basic text-to-video generation

Run any example with:
```bash
go run examples/providers/bytedance/01-text-to-video.go
```

## Links

- [ByteDance Ark Platform](https://ark.volces.com)
- [Ark API Documentation](https://www.volcengine.com/docs/82379)
- [Go-AI SDK Documentation](https://github.com/digitallysavvy/go-ai)

## License

Apache 2.0
