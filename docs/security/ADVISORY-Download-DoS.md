# Security Advisory: Unbounded Download DoS Prevention

**Status:** Fixed
**Severity:** High
**CVE:** TBD
**Affected Versions:** All versions before v0.X.X
**Fixed in:** v0.X.X

## Summary

Prior versions of the Go-AI SDK allowed unbounded memory growth when downloading from user-provided URLs (images, videos, audio), enabling Denial of Service (DoS) attacks through memory exhaustion.

## Vulnerability Details

### Attack Vector

The SDK's download functions did not limit file sizes when fetching resources from user-provided URLs. An attacker could provide URLs pointing to extremely large files (e.g., 100GB+), causing:

- Memory exhaustion
- Process crashes from Out-of-Memory (OOM) errors
- Resource depletion on the host system
- Service unavailability

### Example Attack

```go
// Attacker provides URL to a huge file
model := xai.NewImageModel(provider, "grok-2-vision-1212")
result, err := ai.GenerateImage(ctx, ai.GenerateImageOptions{
    Model: model,
    Prompt: "analyze this image",
    Files: []ai.ImagePart{
        ai.ImagePart("https://evil.com/100gb-file.jpg"), // Causes OOM crash
    },
})
// Process crashes from memory exhaustion
```

### Affected Components

- Image download functions in providers (XAI, BFL, FAL, Replicate)
- Video download functions in providers (Google, FAL, Replicate)
- Any code path that downloads from user-provided URLs

## Fix

### Changes

The fix implements a **2 GiB default size limit** for all downloads with the following protections:

1. **Early rejection**: Checks `Content-Length` header before downloading
2. **Streaming with limits**: Reads response body incrementally with `io.LimitReader`
3. **Proper error handling**: Throws `DownloadError` when limit exceeded
4. **Context cancellation**: Respects context cancellation for aborting downloads

### Default Protection

All download operations now automatically enforce a 2 GiB limit:

```go
// Automatically protected with 2 GiB limit
data, err := fileutil.Download(ctx, url, fileutil.DefaultDownloadOptions())
```

### Custom Limits

Applications can configure custom size limits:

```go
// Create download function with custom 100 MB limit
customDownload := ai.CreateDownload(&ai.DownloadOptions{
    MaxBytes: 100 * 1024 * 1024, // 100 MB
})

// Use in generation functions
result, err := ai.GenerateVideo(ctx, ai.GenerateVideoOptions{
    Model: model,
    Prompt: videoPrompt,
    Download: customDownload,
})
```

## Mitigation

### Immediate Action

**Upgrade to v0.X.X or later immediately.** No workaround is available for earlier versions.

### Version Check

```bash
go list -m github.com/digitallysavvy/go-ai
```

If the version is below v0.X.X, upgrade with:

```bash
go get github.com/digitallysavvy/go-ai@latest
```

## Impact Assessment

### Who is Affected?

Any application that:
- Accepts user-provided image/video/audio URLs
- Uses the Go-AI SDK for generation with file inputs
- Runs in production environments with untrusted input

### Risk Level

- **High** for public-facing services with user-generated content
- **Medium** for internal services with trusted users
- **Low** for applications that only use SDK-generated content

## Timeline

- **Discovered:** 2026-02-12
- **Fixed:** 2026-02-15
- **Released:** 2026-02-XX
- **Public Disclosure:** 2026-02-XX

## Credits

Security fix implemented based on TypeScript AI SDK commit 4024a3af6.

## References

- [CWE-770: Allocation of Resources Without Limits](https://cwe.mitre.org/data/definitions/770.html)
- [TypeScript AI SDK Fix](https://github.com/vercel/ai/commit/4024a3af6)

## Contact

For security concerns, please report to: security@digitallysavvy.com
