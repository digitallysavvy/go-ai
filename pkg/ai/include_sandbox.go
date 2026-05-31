package ai

import (
	"context"
	"io"
	"regexp"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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
	Description() string
	RunCommand(ctx context.Context, opts SandboxRunCommandOptions) (SandboxRunCommandResult, error)
	Spawn(ctx context.Context, opts SandboxSpawnOptions) (SandboxProcess, error)
	ReadFile(ctx context.Context, path string) (io.ReadCloser, error)
	ReadBinaryFile(ctx context.Context, path string) ([]byte, error)
	ReadTextFile(ctx context.Context, opts SandboxReadTextFileOptions) (*string, error)
	WriteFile(ctx context.Context, path string, content io.Reader) error
	WriteBinaryFile(ctx context.Context, path string, content []byte) error
	WriteTextFile(ctx context.Context, opts SandboxWriteTextFileOptions) error
}

// SandboxRunCommandOptions are passed to Sandbox.RunCommand.
type SandboxRunCommandOptions struct {
	Command          string
	WorkingDirectory string
	Environment      map[string]string
}

// SandboxSpawnOptions are passed to Sandbox.Spawn.
type SandboxSpawnOptions struct {
	Command          string
	WorkingDirectory string
}

// SandboxProcess is a handle to a process started by Sandbox.Spawn.
type SandboxProcess = providerutils.SandboxProcess

// SandboxProcessResult is returned by SandboxProcess.Wait.
type SandboxProcessResult = providerutils.SandboxProcessResult

// SandboxRunCommandResult is returned by Sandbox.RunCommand.
type SandboxRunCommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// SandboxReadTextFileOptions controls text file reads.
type SandboxReadTextFileOptions struct {
	Path      string
	Encoding  string
	StartLine *int
	EndLine   *int
}

// SandboxWriteTextFileOptions controls text file writes.
type SandboxWriteTextFileOptions struct {
	Path     string
	Content  string
	Encoding string
}

// SandboxExecuteOptions is kept as a source-compatibility alias for older Go
// callers. New code should use SandboxRunCommandOptions.
type SandboxExecuteOptions = SandboxRunCommandOptions

// SandboxExecuteResult is kept as a source-compatibility alias for older Go
// callers. New code should use SandboxRunCommandResult.
type SandboxExecuteResult = SandboxRunCommandResult

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
