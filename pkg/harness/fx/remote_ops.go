package fx

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
	"github.com/digitallysavvy/go-ai/pkg/harness/internal/posixpath"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// Port of TS fx-remote-ops.ts: the libfx runtime's built-in tools, run only
// through the session's sandbox and confined to the session workspace.

type remoteOps struct {
	sandbox providerutils.SandboxSession
	workDir string

	rootMu   sync.Mutex
	rootDone bool
	root     string
	rootErr  error
}

func newRemoteOps(sandbox providerutils.SandboxSession, workDir string) *remoteOps {
	return &remoteOps{sandbox: sandbox, workDir: posixpath.Resolve("/", workDir)}
}

// runResult is a sandbox command result as the model sees it.
type runResult struct {
	ExitCode int    `json:"exitCode"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

func assertContained(root, candidate string) (string, error) {
	rel := posixRelative(root, candidate)
	if rel == ".." || strings.HasPrefix(rel, "../") || posixpath.IsAbs(rel) {
		return "", fmt.Errorf("fx path escapes the workspace: %s", candidate)
	}
	return candidate, nil
}

func (r *remoteOps) run(ctx context.Context, command string) (providerutils.SandboxRunResult, error) {
	if err := ctx.Err(); err != nil {
		return providerutils.SandboxRunResult{}, err
	}
	return r.sandbox.Run(ctx, providerutils.SandboxProcessOptions{Command: command, WorkingDirectory: r.workDir})
}

func (r *remoteOps) realpath(ctx context.Context, target string) (string, error) {
	res, err := r.run(ctx, "bash -o pipefail -c "+harnessutil.ShellQuote("realpath -mz -- "+harnessutil.ShellQuote(target)+" | base64"))
	if err != nil {
		return "", err
	}
	trimmed := strings.TrimSpace(res.Stdout)
	if res.ExitCode != 0 || trimmed == "" {
		return "", fmt.Errorf("Unable to resolve sandbox path: %s", target) //nolint:staticcheck // matches TS SDK's exact error text
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(trimmed), ""))
	if err != nil {
		return "", fmt.Errorf("Unable to resolve sandbox path: %s", target) //nolint:staticcheck // matches TS SDK's exact error text
	}
	return strings.TrimSuffix(string(decoded), "\x00"), nil
}

// canonicalRoot resolves the workspace once; like TS, a failure is kept.
func (r *remoteOps) canonicalRoot(ctx context.Context) (string, error) {
	r.rootMu.Lock()
	defer r.rootMu.Unlock()
	if !r.rootDone {
		r.root, r.rootErr = r.realpath(context.WithoutCancel(ctx), r.workDir)
		r.rootDone = true
	}
	return r.root, r.rootErr
}

func (r *remoteOps) resolvePath(ctx context.Context, filePath string) (string, error) {
	candidate, err := assertContained(r.workDir, posixpath.Resolve(r.workDir, filePath))
	if err != nil {
		return "", err
	}
	root, err := r.canonicalRoot(ctx)
	if err != nil {
		return "", err
	}
	real, err := r.realpath(ctx, candidate)
	if err != nil {
		return "", err
	}
	return assertContained(root, real)
}

func decodeToolInput(input json.RawMessage, dst any) error {
	if err := json.Unmarshal(input, dst); err != nil {
		return fmt.Errorf("invalid tool input: %w", err)
	}
	return nil
}

func (r *remoteOps) read(ctx context.Context, filePath string) (string, error) {
	resolved, err := r.resolvePath(ctx, filePath)
	if err != nil {
		return "", err
	}
	content, err := r.sandbox.ReadTextFile(ctx, providerutils.SandboxReadTextFileOptions{Path: resolved})
	if err != nil {
		return "", err
	}
	if content == nil {
		return "", fmt.Errorf("File not found: %s", filePath) //nolint:staticcheck // matches TS SDK's exact error text
	}
	return *content, nil
}

func (r *remoteOps) write(ctx context.Context, filePath, content string) (string, error) {
	resolved, err := r.resolvePath(ctx, filePath)
	if err != nil {
		return "", err
	}
	if err := r.sandbox.WriteTextFile(ctx, providerutils.SandboxWriteTextFileOptions{Path: resolved, Content: content}); err != nil {
		return "", err
	}
	return "Wrote " + filePath, nil
}

// execute runs the built-in tool name.
func (r *remoteOps) execute(ctx context.Context, name string, input json.RawMessage) (any, error) {
	switch name {
	case "read":
		var in struct {
			FilePath string `json:"file_path"`
		}
		if err := decodeToolInput(input, &in); err != nil {
			return nil, err
		}
		return r.read(ctx, in.FilePath)
	case "write":
		var in struct {
			FilePath string `json:"file_path"`
			Content  string `json:"content"`
		}
		if err := decodeToolInput(input, &in); err != nil {
			return nil, err
		}
		return r.write(ctx, in.FilePath, in.Content)
	case "edit":
		var in struct {
			FilePath  string `json:"file_path"`
			OldString string `json:"old_string"`
			NewString string `json:"new_string"`
		}
		if err := decodeToolInput(input, &in); err != nil {
			return nil, err
		}
		content, err := r.read(ctx, in.FilePath)
		if err != nil {
			return nil, err
		}
		index := strings.Index(content, in.OldString)
		if index == -1 || in.OldString == "" {
			return nil, errors.New("Text to replace was not found or is empty.") //nolint:staticcheck // matches TS SDK's exact error text
		}
		if strings.Contains(content[index+len(in.OldString):], in.OldString) {
			return nil, errors.New("Text to replace occurs more than once. Include more context.") //nolint:staticcheck // matches TS SDK's exact error text
		}
		if _, err := r.write(ctx, in.FilePath, content[:index]+in.NewString+content[index+len(in.OldString):]); err != nil {
			return nil, err
		}
		return "Edited " + in.FilePath, nil
	case "bash":
		var in struct {
			Command string   `json:"command"`
			Timeout *float64 `json:"timeout"`
		}
		if err := decodeToolInput(input, &in); err != nil {
			return nil, err
		}
		if in.Timeout != nil && *in.Timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, time.Duration(*in.Timeout*float64(time.Second)))
			defer cancel()
		}
		res, err := r.run(ctx, in.Command)
		if err != nil {
			return nil, err
		}
		return runResult{ExitCode: res.ExitCode, Stdout: res.Stdout, Stderr: res.Stderr}, nil
	case "grep":
		var in struct {
			Pattern    string   `json:"pattern"`
			Path       *string  `json:"path"`
			Glob       string   `json:"glob"`
			IgnoreCase bool     `json:"ignoreCase"`
			Literal    bool     `json:"literal"`
			Context    *float64 `json:"context"`
			Limit      *float64 `json:"limit"`
		}
		if err := decodeToolInput(input, &in); err != nil {
			return nil, err
		}
		target, err := r.resolvePath(ctx, stringOr(in.Path, "."))
		if err != nil {
			return nil, err
		}
		flags := []string{"-r", "-n", "--binary-files=without-match"}
		if in.IgnoreCase {
			flags = append(flags, "-i")
		}
		if in.Literal {
			flags = append(flags, "-F")
		}
		if in.Glob != "" {
			flags = append(flags, "--include="+in.Glob)
		}
		if in.Context != nil && *in.Context != 0 {
			flags = append(flags, "-C", jsNumber(*in.Context))
		}
		quoted := make([]string, len(flags))
		for i, f := range flags {
			quoted[i] = harnessutil.ShellQuote(f)
		}
		limit := 100.0
		if in.Limit != nil {
			limit = *in.Limit
		}
		res, err := r.run(ctx, "grep "+strings.Join(quoted, " ")+" -- "+harnessutil.ShellQuote(in.Pattern)+" "+harnessutil.ShellQuote(target)+
			" | head -n "+jsNumber(math.Max(1, math.Floor(limit))))
		if err != nil {
			return nil, err
		}
		switch {
		case res.Stdout != "":
			return res.Stdout, nil
		case res.Stderr != "":
			return res.Stderr, nil
		}
		return "No matches found", nil
	case "glob":
		var in struct {
			Pattern string   `json:"pattern"`
			Path    *string  `json:"path"`
			Limit   *float64 `json:"limit"`
		}
		if err := decodeToolInput(input, &in); err != nil {
			return nil, err
		}
		target, err := r.resolvePath(ctx, stringOr(in.Path, "."))
		if err != nil {
			return nil, err
		}
		res, err := r.run(ctx, "find "+harnessutil.ShellQuote(target)+" -type f -print0")
		if err != nil {
			return nil, err
		}
		if res.ExitCode != 0 {
			return nil, errors.New(res.Stderr)
		}
		re, err := globRegexp(in.Pattern)
		if err != nil {
			return nil, err
		}
		files := []string{}
		for _, file := range strings.Split(res.Stdout, "\x00") {
			if file == "" {
				continue
			}
			if rel := posixRelative(target, file); re.MatchString(rel) {
				files = append(files, rel)
			}
		}
		sort.Strings(files)
		return limitStrings(files, in.Limit, 1000), nil
	case "ls":
		var in struct {
			Path  *string  `json:"path"`
			Limit *float64 `json:"limit"`
		}
		if err := decodeToolInput(input, &in); err != nil {
			return nil, err
		}
		target, err := r.resolvePath(ctx, stringOr(in.Path, "."))
		if err != nil {
			return nil, err
		}
		res, err := r.run(ctx, "find "+harnessutil.ShellQuote(target)+` -mindepth 1 -maxdepth 1 -printf '%f\0'`)
		if err != nil {
			return nil, err
		}
		if res.ExitCode != 0 {
			return nil, errors.New(res.Stderr)
		}
		names := []string{}
		for _, n := range strings.Split(res.Stdout, "\x00") {
			if n != "" {
				names = append(names, n)
			}
		}
		sort.Strings(names)
		return limitStrings(names, in.Limit, 500), nil
	}
	return nil, fmt.Errorf("Unsupported sandbox tool %s", name) //nolint:staticcheck // matches TS SDK's exact error text
}

func stringOr(p *string, def string) string {
	if p == nil {
		return def
	}
	return *p
}

// jsNumber formats n like JavaScript's String(n) for ordinary values.
func jsNumber(n float64) string { return strconv.FormatFloat(n, 'f', -1, 64) }

// limitStrings mirrors Array.slice(0, limit ?? def).
func limitStrings(values []string, limit *float64, def int) []string {
	n := float64(def)
	if limit != nil {
		n = *limit
	}
	end := len(values)
	switch {
	case math.IsNaN(n):
		end = 0
	case n < 0:
		end = len(values) + int(math.Ceil(n))
		if end < 0 {
			end = 0
		}
	case n < float64(len(values)):
		end = int(n)
	}
	return values[:end]
}

// globRegexp compiles a glob (`*`, `?`, `**`, `[...]`, `{a,b}`) to a
// regular expression over slash-separated relative paths, the subset of
// Node's path.matchesGlob the fx glob tool relies on.
func globRegexp(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	depth := 0
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				atStart := i == 1 || pattern[i-2] == '/'
				if atStart && i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
				continue
			}
			b.WriteString("[^/]*")
		case '?':
			b.WriteString("[^/]")
		case '[':
			end := strings.IndexByte(pattern[i+1:], ']')
			if end < 0 {
				b.WriteString(`\[`)
				continue
			}
			class := pattern[i+1 : i+1+end]
			if strings.HasPrefix(class, "!") {
				class = "^" + class[1:]
			}
			b.WriteString("[" + strings.ReplaceAll(class, `\`, `\\`) + "]")
			i += end + 1
		case '{':
			depth++
			b.WriteString("(?:")
		case '}':
			if depth > 0 {
				depth--
				b.WriteString(")")
			} else {
				b.WriteString(`\}`)
			}
		case ',':
			if depth > 0 {
				b.WriteString("|")
			} else {
				b.WriteString(",")
			}
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil, fmt.Errorf("invalid glob pattern %q: %w", pattern, err)
	}
	return re, nil
}
