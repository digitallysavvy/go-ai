# Builds Directory

This directory is for storing compiled Go binaries during development and testing.

## Usage

When building examples or testing code, output binaries here instead of cluttering the source directories:

```bash
# Build an example
go build -o builds/video-example examples/video/text-to-video/main.go

# Build with a descriptive name
go build -o builds/anthropic-context-test examples/providers/anthropic/context-management/clear-tool-uses/main.go

# Build multiple examples
go build -o builds/fal-video pkg/providers/fal/video_model.go
go build -o builds/google-video pkg/providers/google/video_model.go
```

## Cleanup

Delete all binaries:
```bash
rm builds/*
```

## Git

This directory and all its contents (except this README) are ignored by git.
