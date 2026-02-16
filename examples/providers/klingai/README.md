# KlingAI Video Generation Examples

This directory contains complete working examples demonstrating all KlingAI video generation capabilities.

## Prerequisites

1. Set your KlingAI credentials:
```bash
export KLINGAI_ACCESS_KEY=your-access-key
export KLINGAI_SECRET_KEY=your-secret-key
```

2. Install dependencies:
```bash
cd go-ai
go mod download
```

## Examples

### 1. Text-to-Video (`01-text-to-video.go`)

Generate videos from text descriptions.

**Features:**
- Basic text-to-video generation
- Duration control (5s or 10s)
- Aspect ratio selection
- Standard and pro modes
- Negative prompts

**Run:**
```bash
go run examples/providers/klingai/01-text-to-video.go
```

---

### 2. Image-to-Video (`02-image-to-video.go`)

Animate static images with camera movements and transformations.

**Features:**
- Image animation
- Camera control
- Duration and aspect ratio control
- Mode selection (std/pro)

**Run:**
```bash
go run examples/providers/klingai/02-image-to-video.go
```

---

### 3. Image-to-Video with Start/End Frames (`03-i2v-start-end-frames.go`)

Control both the start and end state of your video for precise transitions.

**Features:**
- Start frame control (via image input)
- End frame control (via imageTail)
- Smooth transitions between frames
- Pro mode quality
- Audio generation support

**Use Cases:**
- Character transformations
- Scene transitions
- Morph effects
- Storytelling sequences

**Run:**
```bash
go run examples/providers/klingai/03-i2v-start-end-frames.go
```

**Note:** Start/end frame control requires pro mode and is available on V2.6+ models.

---

### 4. Motion Control (`04-motion-control.go`)

Transfer motion from a reference video to your character image (standard mode).

**Features:**
- Reference motion video
- Character orientation control
- Original sound preservation
- Standard quality mode

**Use Cases:**
- Dance choreography transfer
- Action sequence replication
- Animation from live action
- Character animation

**Run:**
```bash
go run examples/providers/klingai/04-motion-control.go
```

---

### 5. Motion Control Pro (`05-motion-control-pro.go`)

High-quality motion transfer with enhanced capabilities (pro mode).

**Features:**
- Higher quality output
- Support for reference videos up to 30 seconds
- Better motion capture accuracy
- Original audio preservation
- Watermark support

**Use Cases:**
- Professional animation projects
- Complex motion sequences
- High-quality character animation
- Long-form motion capture

**Run:**
```bash
go run examples/providers/klingai/05-motion-control-pro.go
```

**Note:** Pro mode takes longer to generate but produces significantly better results.

---

## Configuration Options

All examples support the following configuration methods:

### Environment Variables
```bash
export KLINGAI_ACCESS_KEY=your-access-key
export KLINGAI_SECRET_KEY=your-secret-key
```

### Custom Base URL (Optional)
```go
prov, err := klingai.New(klingai.Config{
    AccessKey: "your-access-key",
    SecretKey: "your-secret-key",
    BaseURL:   "https://custom-endpoint.com",
})
```

## Common Provider Options

### Duration
- `5.0` - 5 second video
- `10.0` - 10 second video

### Aspect Ratio
- `"16:9"` - Landscape
- `"9:16"` - Portrait
- `"1:1"` - Square

### Mode
- `"std"` - Standard quality, faster generation
- `"pro"` - Professional quality, slower generation

### Polling Configuration
```go
ProviderOptions: map[string]interface{}{
    "klingai": map[string]interface{}{
        "pollIntervalMs": 5000,  // Poll every 5 seconds (default)
        "pollTimeoutMs": 600000, // 10 minute timeout (default)
    },
}
```

## Error Handling

All examples include error handling. Common errors:

- **Authentication Failed (401)**: Check your access key and secret key
- **Generation Timeout (504)**: Video generation took too long (increase `pollTimeoutMs`)
- **Invalid Parameters**: Check model compatibility and parameter constraints
- **Quota Exceeded**: Rate limit or account quota reached

## Output

Each example prints:
1. Generated video URL
2. Media type
3. Provider metadata (task ID, watermark URL, duration)
4. Warnings (if any)

## Model Support

| Model | Text-to-Video | Image-to-Video | Start/End | Motion Control |
|-------|---------------|----------------|-----------|----------------|
| kling-v1-t2v | ✓ | | | |
| kling-v1.6-t2v | ✓ | | | |
| kling-v2.6-t2v | ✓ | | | |
| kling-v1-i2v | | ✓ | | |
| kling-v2.6-i2v | | ✓ | ✓ | |
| kling-v2.6-motion-control | | | | ✓ |

## Additional Resources

- [KlingAI API Documentation](https://app.klingai.com/global/dev/document-api)
- [KlingAI Capability Map](https://app.klingai.com/global/dev/document-api/apiReference/model/skillsMap)
- [Go-AI SDK Documentation](https://github.com/digitallysavvy/go-ai)

## Tips

1. **Pro mode** provides better quality but takes 2-3x longer
2. **Motion control** output dimensions match the reference video
3. **Start/end frame control** requires pro mode
4. **Polling timeouts** should be higher for pro mode (10 minutes recommended)
5. **Reference videos** for motion control should be clear and well-lit

## Troubleshooting

**Video generation is slow:**
- Pro mode takes longer than standard mode
- Motion control requires more processing time
- Check your polling timeout settings

**Authentication errors:**
- Verify your credentials are set correctly
- Check if your account has sufficient quota
- Ensure your API keys haven't expired

**Video quality issues:**
- Use pro mode for higher quality
- Provide clear, detailed prompts
- Use high-quality input images
- Avoid negative prompts that are too restrictive

## Next Steps

After running these examples, explore:
- Combining camera controls with image-to-video
- Using different aspect ratios for your content
- Experimenting with negative prompts
- Testing various motion reference videos
