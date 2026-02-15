# Download Security and Size Limits

The Go-AI SDK includes built-in protection against memory exhaustion attacks when downloading files from URLs. This guide explains how download security works and how to customize size limits for your use case.

## Overview

When generating content with images, videos, or audio from URLs, the SDK automatically enforces size limits to prevent:

- Memory exhaustion from downloading extremely large files
- Out-of-Memory (OOM) process crashes
- Resource depletion on your system
- Denial of Service (DoS) attacks

## Default Protection

**All downloads are automatically protected with a 2 GiB size limit.**

This limit applies to:
- Image downloads for vision models
- Video downloads for video generation
- Audio downloads for transcription (future)
- Any user-provided URL passed to the SDK

```go
// Automatically protected with 2 GiB limit
result, err := ai.GenerateImage(ctx, ai.GenerateImageOptions{
    Model: model,
    Prompt: "analyze this image",
    Files: []ai.ImagePart{
        ai.ImagePart("https://example.com/large-image.jpg"),
    },
})
```

## Why 2 GiB?

The 2 GiB default limit:
- Prevents most DoS attacks
- Allows legitimate large files (high-res images, long videos)
- Is easy to remember and reason about
- Matches the limit used by the TypeScript AI SDK

## Custom Size Limits

For specific use cases, you can configure custom download limits:

### Creating a Custom Download Function

```go
import (
    "github.com/digitallysavvy/go-ai/pkg/ai"
)

// Create download with 100 MB limit
customDownload := ai.CreateDownload(&ai.DownloadOptions{
    MaxBytes: 100 * 1024 * 1024, // 100 MB
})
```

### Using Custom Downloads with Video Generation

```go
result, err := ai.GenerateVideo(ctx, ai.GenerateVideoOptions{
    Model: model,
    Prompt: ai.VideoPrompt{
        Text: "generate a video from this image",
        Image: &ai.VideoPromptImage{
            URL: "https://example.com/image.jpg",
        },
    },
    Download: customDownload, // Use custom 100 MB limit
})
```

### Additional Options

You can also set custom HTTP headers:

```go
customDownload := ai.CreateDownload(&ai.DownloadOptions{
    MaxBytes: 500 * 1024 * 1024, // 500 MB
    Headers: map[string]string{
        "Authorization": "Bearer token",
        "User-Agent": "MyApp/1.0",
    },
})
```

## How It Works

The download protection uses multiple layers:

### 1. Content-Length Check

Before downloading, the SDK checks the `Content-Length` HTTP header:

```
Content-Length: 3000000000  (3 GB)
Limit: 2147483648 (2 GiB)
→ Rejects immediately without downloading
```

### 2. Streaming with Size Tracking

Even if `Content-Length` is missing or incorrect, the SDK tracks bytes while streaming:

```go
// Reads incrementally, not all at once
limitedReader := io.LimitReader(resp.Body, maxBytes+1)
data, err := io.ReadAll(limitedReader)

// Checks if limit was exceeded
if len(data) > maxBytes {
    return DownloadError{...}
}
```

### 3. Context Cancellation

Downloads respect context cancellation for timeouts:

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

data, err := fileutil.Download(ctx, url, opts)
// Automatically cancelled after 30 seconds
```

## Error Handling

When a download exceeds the size limit, you'll get a `DownloadError`:

```go
result, err := ai.GenerateImage(ctx, opts)
if err != nil {
    var downloadErr *providererrors.DownloadError
    if errors.As(err, &downloadErr) {
        // Download failed due to size limit or HTTP error
        fmt.Printf("Download failed for %s: %v\n", downloadErr.URL, err)

        if downloadErr.StatusCode > 0 {
            fmt.Printf("HTTP %d: %s\n", downloadErr.StatusCode, downloadErr.StatusText)
        }
    }
}
```

### Error Information

`DownloadError` provides:
- `URL`: The URL that failed
- `StatusCode`: HTTP status code (if applicable)
- `StatusText`: HTTP status text
- `Message`: Detailed error message
- `Cause`: Underlying error (if any)

## Best Practices

### 1. Use Appropriate Limits for Your Use Case

```go
// Small images for thumbnails
thumbnailDownload := ai.CreateDownload(&ai.DownloadOptions{
    MaxBytes: 10 * 1024 * 1024, // 10 MB
})

// High-resolution images for analysis
hiresDownload := ai.CreateDownload(&ai.DownloadOptions{
    MaxBytes: 500 * 1024 * 1024, // 500 MB
})

// Videos (use larger limit)
videoDownload := ai.CreateDownload(&ai.DownloadOptions{
    MaxBytes: 2 * 1024 * 1024 * 1024, // 2 GiB (default)
})
```

### 2. Validate URLs Before Downloading

```go
func isAllowedDomain(url string) bool {
    parsed, err := url.Parse(url)
    if err != nil {
        return false
    }

    allowedDomains := []string{"cdn.example.com", "storage.example.com"}
    for _, domain := range allowedDomains {
        if parsed.Host == domain {
            return true
        }
    }
    return false
}

if !isAllowedDomain(userProvidedURL) {
    return errors.New("URL not from allowed domain")
}
```

### 3. Set Appropriate Timeouts

```go
ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
defer cancel()

result, err := ai.GenerateVideo(ctx, ai.GenerateVideoOptions{
    Model: model,
    Prompt: prompt,
})
```

### 4. Handle Errors Gracefully

```go
result, err := ai.GenerateImage(ctx, opts)
if err != nil {
    var downloadErr *providererrors.DownloadError
    if errors.As(err, &downloadErr) {
        // Log error details
        log.Printf("Download failed: %v", err)

        // Return user-friendly error
        return fmt.Errorf("failed to process image: file too large or unavailable")
    }
    return err
}
```

## Security Considerations

### Public-Facing Applications

If your application accepts URLs from untrusted users:

1. ✅ **Always use size limits** (enabled by default)
2. ✅ **Validate URL domains** (allowlist approach)
3. ✅ **Set timeout contexts** (prevent slowloris attacks)
4. ✅ **Rate limit requests** (prevent abuse)
5. ✅ **Monitor resource usage** (detect anomalies)

### Internal Applications

Even for internal services:

1. ✅ **Keep default limits** (defense in depth)
2. ✅ **Validate URL schemes** (prevent SSRF)
3. ✅ **Log download failures** (detect issues early)

## Low-Level API

For advanced use cases, you can use the download utilities directly:

```go
import (
    "github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
)

// Configure download options
opts := fileutil.DefaultDownloadOptions()
opts.MaxSize = 50 * 1024 * 1024  // 50 MB
opts.Timeout = 30 * time.Second
opts.Headers = map[string]string{
    "User-Agent": "MyApp/1.0",
}

// Download with options
data, err := fileutil.Download(ctx, url, opts)
if err != nil {
    var downloadErr *providererrors.DownloadError
    if errors.As(err, &downloadErr) {
        fmt.Printf("Download failed: %v\n", err)
    }
    return err
}
```

## FAQ

### Q: Can I disable size limits?

**A:** No. Size limits cannot be disabled for security reasons. The maximum you can set is the platform's `int64` limit, but this is strongly discouraged.

### Q: What if I need to download files larger than 2 GiB?

**A:** Consider:
1. Downloading the file separately with your own size checking
2. Processing the file in chunks/streaming
3. Using a different approach that doesn't require loading the entire file into memory

### Q: Does this affect generated content from providers?

**A:** No. Limits only apply to downloads from user-provided URLs. Content generated by AI providers is handled separately.

### Q: Will this break my existing code?

**A:** No. The 2 GiB default is very generous and unlikely to affect normal use cases. If you were downloading files larger than 2 GiB before, you should explicitly configure a larger limit.

## See Also

- [Security Advisory: Unbounded Download DoS](../security/ADVISORY-Download-DoS.md)
- [Error Handling Guide](error-handling.md)
- [API Reference: CreateDownload](../reference/api.md#createdownload)
