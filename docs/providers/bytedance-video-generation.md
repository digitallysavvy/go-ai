# ByteDance Video Generation

This guide covers video generation using the ByteDance (Volcengine) provider in the Go-AI SDK.

## Overview

ByteDance's Volcengine Ark platform offers high-quality video generation models through the Seedance model family:

- Text-to-video generation
- Image-to-video animation
- Start + end frame interpolation
- Reference image guided generation

## Setup

```go
import (
    "github.com/digitallysavvy/go-ai/pkg/provider"
    "github.com/digitallysavvy/go-ai/pkg/providers/bytedance"
)

prov, err := bytedance.New(bytedance.Config{
    APIKey: os.Getenv("BYTEDANCE_API_KEY"),
})

model, err := prov.VideoModel(string(bytedance.ModelSeedance10Pro))
```

## Text-to-Video

```go
dur := 5.0
response, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
    Prompt:      "A cherry blossom tree sways gently in the spring breeze",
    AspectRatio: "16:9",
    Duration:    &dur,
})
if err != nil {
    log.Fatal(err)
}

fmt.Println(response.Videos[0].URL)
```

## Image-to-Video

Animate a static image with an optional text prompt:

```go
response, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
    Prompt: "The cat slowly blinks and turns its head",
    Image: &provider.VideoModelV3File{
        Type: "url",
        URL:  "https://example.com/cat.png",
    },
})
```

## Start + End Frame Generation

Generate video that transitions between two specific frames:

```go
response, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
    Prompt: "The flower blooms slowly",
    Image: &provider.VideoModelV3File{
        Type: "url",
        URL:  "https://example.com/bud.png",
    },
    ProviderOptions: map[string]interface{}{
        "bytedance": map[string]interface{}{
            "lastFrameImage": "https://example.com/bloom.png",
        },
    },
})
```

## Resolution

Pass a `WxH` resolution string to control output quality. The SDK maps these to ByteDance API values:

```go
response, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
    Prompt:     "Mountain sunrise",
    Resolution: "1920x1080", // maps to "1080p"
})
```

| Resolution | API Value |
|------------|-----------|
| 1920×1080, 1080×1920, etc. | `1080p` |
| 1280×720, 720×1280, etc. | `720p` |
| 864×480, 480×864, etc. | `480p` |

## Available Models

| Constant | Model ID |
|---|---|
| `bytedance.ModelSeedance15Pro` | `seedance-1-5-pro-251215` |
| `bytedance.ModelSeedance10Pro` | `seedance-1-0-pro-250528` |
| `bytedance.ModelSeedance10ProFast` | `seedance-1-0-pro-fast-251015` |
| `bytedance.ModelSeedance10LiteT2V` | `seedance-1-0-lite-t2v-250428` |
| `bytedance.ModelSeedance10LiteI2V` | `seedance-1-0-lite-i2v-250428` |

## Provider Options

Pass ByteDance-specific options under the `"bytedance"` key in `ProviderOptions`:

```go
response, err := model.DoGenerate(ctx, &provider.VideoModelV3CallOptions{
    Prompt: "A futuristic city at night",
    ProviderOptions: map[string]interface{}{
        "bytedance": map[string]interface{}{
            "generateAudio": true,
            "watermark":     false,
            "serviceTier":   "flex",
            "draft":         false,
        },
    },
})
```

| Option | Type | Description |
|--------|------|-------------|
| `watermark` | `bool` | Add a watermark |
| `generateAudio` | `bool` | Generate audio track |
| `cameraFixed` | `bool` | Fix camera position |
| `returnLastFrame` | `bool` | Include last frame in response |
| `serviceTier` | `string` | `"default"` or `"flex"` |
| `draft` | `bool` | Draft mode (faster but lower quality) |
| `lastFrameImage` | `string` | URL of the desired last frame |
| `referenceImages` | `[]string` | Reference image URLs |
| `pollIntervalMs` | `int` | Status polling interval (default: 3000ms) |
| `pollTimeoutMs` | `int` | Max polling timeout (default: 300000ms) |

## Async Polling

Video generation is asynchronous. The SDK handles the submit-then-poll loop automatically:

1. POST to `/contents/generations/tasks` → receive `task_id`
2. GET `/contents/generations/tasks/{task_id}` every `pollIntervalMs` ms
3. When `status == "succeeded"`, return the video URL

Context cancellation stops the polling loop immediately:

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
defer cancel()

response, err := model.DoGenerate(ctx, opts)
```

## Error Handling

```go
response, err := model.DoGenerate(ctx, opts)
if err != nil {
    if bdErr, ok := err.(*bytedance.Error); ok {
        fmt.Printf("ByteDance error %d: %s\n", bdErr.Code, bdErr.Message)
    }
}
```

Common error codes:

| Code | Meaning |
|------|---------|
| 400 | Invalid request (bad prompt, unsupported option) |
| 401 | Unauthorized (invalid API key) |
| 504 | Polling timed out |

## Complete Example

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/digitallysavvy/go-ai/pkg/provider"
    "github.com/digitallysavvy/go-ai/pkg/providers/bytedance"
)

func main() {
    prov, err := bytedance.New(bytedance.Config{
        APIKey: os.Getenv("BYTEDANCE_API_KEY"),
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
        Prompt:      "A majestic eagle soaring over snow-capped mountains",
        AspectRatio: "16:9",
        Duration:    &dur,
        ProviderOptions: map[string]interface{}{
            "bytedance": map[string]interface{}{
                "generateAudio": true,
            },
        },
    })
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Video URL: %s\n", response.Videos[0].URL)

    meta := response.ProviderMetadata["bytedance"].(map[string]interface{})
    fmt.Printf("Task ID: %v\n", meta["taskId"])
}
```

## References

- [ByteDance Ark Platform](https://ark.volces.com)
- [Provider package documentation](../../pkg/providers/bytedance/README.md)
- [Example: text-to-video](../../examples/providers/bytedance/01-text-to-video.go)
