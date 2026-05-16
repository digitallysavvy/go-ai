package ai

import (
	"context"
	"regexp"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// IncludeOptions controls which large request/response details are retained
// in step results. The default matches the TypeScript SDK: bodies and request
// messages are excluded unless explicitly enabled.
type IncludeOptions struct {
	RequestBody     bool
	RequestMessages bool
	ResponseBody    bool
	// RawChunks controls whether raw provider stream chunks are forwarded.
	// Nil preserves the deprecated IncludeRawChunks fallback.
	RawChunks *bool
}

func effectiveInclude(include, experimental *IncludeOptions, includeRawChunks bool) IncludeOptions {
	if include == nil {
		include = experimental
	}
	if include == nil {
		return IncludeOptions{RawChunks: includeBoolPtr(includeRawChunks)}
	}
	resolved := *include
	if resolved.RawChunks == nil {
		resolved.RawChunks = includeBoolPtr(includeRawChunks)
	}
	return resolved
}

func includeRawChunksValue(include IncludeOptions) bool {
	return include.RawChunks != nil && *include.RawChunks
}

func includeBoolPtr(v bool) *bool {
	return &v
}

func effectiveDownload(download DownloadFunction) DownloadFunction {
	if download != nil {
		return download
	}
	return DefaultDownload
}

type supportedURLsProvider interface {
	SupportedURLs() map[string][]string
}

func supportedURLChecker(model provider.LanguageModel) func(mediaType, rawURL string) bool {
	return SupportedURLCheckerForModel(model)
}

// SupportedURLCheckerForModel returns a prompt URL support checker for models
// that expose SupportedURLs. Models without that optional capability are treated
// as not supporting direct URLs, matching TypeScript's empty supportedUrls map.
func SupportedURLCheckerForModel(model provider.LanguageModel) func(mediaType, rawURL string) bool {
	supported, ok := model.(supportedURLsProvider)
	if !ok {
		return nil
	}
	patternsByMediaType := supported.SupportedURLs()
	if len(patternsByMediaType) == 0 {
		return nil
	}
	compiled := make(map[string][]*regexp.Regexp, len(patternsByMediaType))
	for mediaType, patterns := range patternsByMediaType {
		for _, pattern := range patterns {
			re, err := regexp.Compile(pattern)
			if err == nil {
				compiled[strings.ToLower(mediaType)] = append(compiled[strings.ToLower(mediaType)], re)
			}
		}
	}
	if len(compiled) == 0 {
		return nil
	}
	return func(mediaType, rawURL string) bool {
		mediaType = strings.ToLower(mediaType)
		rawURL = strings.ToLower(rawURL)
		topLevelOnly := !strings.Contains(mediaType, "/")
		for key, regexes := range compiled {
			prefix := strings.ReplaceAll(key, "*", "")
			if key == "*" || key == "*/*" {
				prefix = ""
			}
			if prefix != "" {
				if topLevelOnly {
					if mediaType+"/" != prefix {
						continue
					}
				} else if !strings.HasPrefix(mediaType, prefix) {
					continue
				}
			}
			for _, re := range regexes {
				if re.MatchString(rawURL) {
					return true
				}
			}
		}
		return false
	}
}

// Sandbox executes shell commands in an isolated environment.
type Sandbox interface {
	Execute(ctx context.Context, command string, opts SandboxExecuteOptions) (SandboxExecuteResult, error)
}

// SandboxExecuteOptions are passed to Sandbox.Execute.
type SandboxExecuteOptions struct {
	WorkingDirectory string
	Environment      map[string]string
}

// SandboxExecuteResult is returned by Sandbox.Execute.
type SandboxExecuteResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// ToolInputRefiner can adjust parsed tool input before approval, callbacks,
// telemetry, tool execution, and response-message construction.
type ToolInputRefiner func(ctx context.Context, opts ToolInputRefinementOptions) (map[string]interface{}, error)

// ToolInputRefinementOptions contains the data passed to a tool input refiner.
type ToolInputRefinementOptions struct {
	ToolCall       types.ToolCall
	Tool           *types.Tool
	RuntimeContext interface{}
	ToolsContext   map[string]interface{}
}
