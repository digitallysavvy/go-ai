# Download API Reference

## Overview

The Download API provides secure file downloading with built-in size limits to prevent memory exhaustion attacks.

## Constants

### DefaultMaxDownloadSize

```go
const DefaultMaxDownloadSize = 2 * 1024 * 1024 * 1024 // 2 GiB
```

The default maximum download size: **2 GiB** (2,147,483,648 bytes).

This limit prevents memory exhaustion from unbounded downloads while allowing legitimate large files. All download operations use this limit by default.

## Types

### DownloadFunction

```go
type DownloadFunction func(ctx context.Context, url string) ([]byte, error)
```

A function that downloads a file from a URL with size limits.

**Parameters:**
- `ctx`: Context for cancellation and timeout
- `url`: URL to download from

**Returns:**
- `[]byte`: Downloaded data
- `error`: Error if download fails or exceeds size limit

### DownloadOptions

```go
type DownloadOptions struct {
    // MaxBytes is the maximum allowed download size in bytes.
    // Default: 2 GiB (DefaultMaxDownloadSize)
    MaxBytes int64

    // Headers are additional HTTP headers to include in download requests.
    Headers map[string]string
}
```

Configuration options for creating custom download functions.

**Fields:**
- `MaxBytes`: Maximum file size in bytes (default: 2 GiB)
- `Headers`: Custom HTTP headers to send with requests

### DownloadError

```go
type DownloadError struct {
    // URL that was being downloaded
    URL string

    // HTTP status code (if applicable)
    StatusCode int

    // HTTP status text
    StatusText string

    // Error message
    Message string

    // Underlying cause
    Cause error
}
```

Error type returned when downloads fail due to size limits or HTTP errors.

**Methods:**

#### Error

```go
func (e *DownloadError) Error() string
```

Returns a formatted error message.

#### Unwrap

```go
func (e *DownloadError) Unwrap() error
```

Returns the underlying cause of the error.

## Functions

### CreateDownload

```go
func CreateDownload(options *DownloadOptions) DownloadFunction
```

Creates a download function with configurable options.

The returned function enforces size limits to prevent memory exhaustion. Pass `nil` for default options (2 GiB limit).

**Parameters:**
- `options`: Download configuration (nil for defaults)

**Returns:**
- `DownloadFunction`: Configured download function

**Example:**

```go
// Create with default 2 GiB limit
defaultDownload := ai.CreateDownload(nil)

// Create with custom 100 MB limit
customDownload := ai.CreateDownload(&ai.DownloadOptions{
    MaxBytes: 100 * 1024 * 1024,
})

// Create with custom headers
authDownload := ai.CreateDownload(&ai.DownloadOptions{
    MaxBytes: 500 * 1024 * 1024,
    Headers: map[string]string{
        "Authorization": "Bearer token",
    },
})
```

### DefaultDownload

```go
var DefaultDownload = CreateDownload(nil)
```

The default download function with 2 GiB limit.

Use this when you don't need custom configuration:

```go
data, err := ai.DefaultDownload(ctx, url)
```

## Low-Level API

### fileutil.Download

```go
func Download(ctx context.Context, url string, opts DownloadOptions) ([]byte, error)
```

Low-level download function with size limits to prevent memory exhaustion.

Checks the `Content-Length` header for early rejection, then reads the body incrementally and aborts with a DownloadError when the limit is exceeded.

**Parameters:**
- `ctx`: Context for cancellation
- `url`: URL to download from
- `opts`: Download options

**Returns:**
- `[]byte`: Downloaded data
- `error`: DownloadError if download fails or exceeds limit

**Example:**

```go
import "github.com/digitallysavvy/go-ai/pkg/internal/fileutil"

opts := fileutil.DefaultDownloadOptions()
opts.MaxSize = 50 * 1024 * 1024  // 50 MB

data, err := fileutil.Download(ctx, url, opts)
```

### fileutil.DownloadOptions

```go
type DownloadOptions struct {
    // Timeout for the download operation
    Timeout time.Duration

    // Headers to include in the request
    Headers map[string]string

    // MaxSize limits the size of the download (in bytes)
    // Default: 2 GiB (DefaultMaxDownloadSize)
    MaxSize int64
}
```

Low-level download configuration.

### fileutil.DefaultDownloadOptions

```go
func DefaultDownloadOptions() DownloadOptions
```

Returns default download options:
- Timeout: 60 seconds
- MaxSize: 2 GiB
- Headers: empty map

## Error Handling

### Checking for DownloadError

```go
import (
    "errors"
    providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

data, err := ai.DefaultDownload(ctx, url)
if err != nil {
    var downloadErr *providererrors.DownloadError
    if errors.As(err, &downloadErr) {
        // Handle download-specific error
        fmt.Printf("Failed to download %s\n", downloadErr.URL)

        if downloadErr.StatusCode > 0 {
            fmt.Printf("HTTP %d: %s\n",
                downloadErr.StatusCode,
                downloadErr.StatusText)
        }

        if downloadErr.Message != "" {
            fmt.Printf("Details: %s\n", downloadErr.Message)
        }
    }
}
```

### IsDownloadError

```go
func IsDownloadError(err error) bool
```

Helper function to check if an error is a DownloadError:

```go
if providererrors.IsDownloadError(err) {
    // Handle download error
}
```

### Common Error Messages

| Error Message | Cause | Solution |
|--------------|-------|----------|
| `download of {url} exceeded maximum size of {limit} bytes (Content-Length: {size})` | File size in Content-Length header exceeds limit | Increase limit or validate file size before downloading |
| `download of {url} exceeded maximum size of {limit} bytes` | File exceeded limit during streaming download | Increase limit or use chunked processing |
| `failed to download {url}: {status} {text}` | HTTP error response | Check URL validity and accessibility |
| `failed to download {url}: {cause}` | Network or other error | Check network connectivity and URL |

## Usage in Generation Functions

### Video Generation

```go
customDownload := ai.CreateDownload(&ai.DownloadOptions{
    MaxBytes: 200 * 1024 * 1024, // 200 MB
})

result, err := ai.GenerateVideo(ctx, ai.GenerateVideoOptions{
    Model: model,
    Prompt: ai.VideoPrompt{
        Text: "create video from this image",
        Image: &ai.VideoPromptImage{
            URL: "https://example.com/input.jpg",
        },
    },
    Download: customDownload,
})
```

### Image Generation

Image downloads happen automatically within providers and use the default 2 GiB limit. Provider implementations use `fileutil.Download` internally.

## Size Limit Guidelines

| Use Case | Recommended Limit | Rationale |
|----------|------------------|-----------|
| Thumbnails | 10 MB | Small images |
| Standard images | 100 MB | Most images |
| High-res images | 500 MB | Professional photography |
| Short videos | 500 MB | < 1 minute |
| Long videos | 2 GiB (default) | Several minutes |

## Security Best Practices

1. **Always use size limits** - Never disable or set to unlimited
2. **Validate URLs** - Check domain, scheme, and format
3. **Set timeouts** - Use context with timeout for all downloads
4. **Rate limit** - Prevent abuse in public-facing applications
5. **Log failures** - Monitor for attack attempts

## Performance Considerations

### Memory Usage

Downloads are read into memory entirely. For large files:
- Peak memory = file size + overhead
- Consider streaming approaches for very large files
- Monitor memory usage in production

### Timeout Guidelines

```go
// Quick images (< 10 MB)
ctx, cancel := context.WithTimeout(ctx, 10*time.Second)

// Standard images/videos (< 500 MB)
ctx, cancel := context.WithTimeout(ctx, 60*time.Second)

// Large files (> 500 MB)
ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
```

## See Also

- [Download Security Guide](../guides/download-security.md)
- [Security Advisory](../security/ADVISORY-Download-DoS.md)
- [Error Handling](../guides/error-handling.md)
