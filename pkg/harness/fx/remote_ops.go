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

// globPattern is a compiled glob (`*`, `?`, `**`, `[...]`, `{a,b}`) over
// slash-separated relative paths: the subset of Node's path.matchesGlob
// (minimatch without the dot option) the fx glob tool relies on. As there,
// a wildcard never matches a path segment that starts with "." unless the
// pattern segment itself starts with ".", so `**/*` skips .git and other
// dot directories.
type globPattern struct {
	alternatives [][]globSegment
}

// globSegment is one slash-separated pattern segment.
type globSegment struct {
	globstar bool           // the whole segment is **
	dot      bool           // the segment starts with a literal "."
	re       *regexp.Regexp // anchored; nil for globstar
}

func globRegexp(pattern string) (*globPattern, error) {
	g := &globPattern{}
	for _, alt := range expandBraces(pattern) {
		var segs []globSegment
		for _, part := range strings.Split(alt, "/") {
			if part == "**" {
				segs = append(segs, globSegment{globstar: true})
				continue
			}
			re, err := regexp.Compile("^" + globSegmentRegexp(part) + "$")
			if err != nil {
				return nil, fmt.Errorf("invalid glob pattern %q: %w", pattern, err)
			}
			segs = append(segs, globSegment{dot: strings.HasPrefix(part, "."), re: re})
		}
		g.alternatives = append(g.alternatives, segs)
	}
	return g, nil
}

// MatchString reports whether path matches the pattern.
func (g *globPattern) MatchString(path string) bool {
	parts := strings.Split(path, "/")
	for _, segs := range g.alternatives {
		if matchGlobSegments(segs, parts) {
			return true
		}
	}
	return false
}

func matchGlobSegments(segs []globSegment, parts []string) bool {
	if len(segs) == 0 {
		return len(parts) == 0
	}
	s := segs[0]
	if s.globstar {
		// A trailing ** needs at least one segment (minimatch: "a/**" does
		// not match "a"); elsewhere it matches zero or more. It never
		// crosses a dot segment.
		min := 0
		if len(segs) == 1 {
			min = 1
		}
		for n := 0; n <= len(parts); n++ {
			if n > 0 && strings.HasPrefix(parts[n-1], ".") {
				return false
			}
			if n >= min && matchGlobSegments(segs[1:], parts[n:]) {
				return true
			}
		}
		return false
	}
	if len(parts) == 0 {
		return false
	}
	if strings.HasPrefix(parts[0], ".") && !s.dot {
		return false
	}
	return s.re.MatchString(parts[0]) && matchGlobSegments(segs[1:], parts[1:])
}

// globSegmentRegexp translates one segment's wildcards to a regexp.
func globSegmentRegexp(part string) string {
	var b strings.Builder
	for i := 0; i < len(part); i++ {
		c := part[i]
		switch c {
		case '*':
			for i+1 < len(part) && part[i+1] == '*' {
				i++
			}
			b.WriteString("[^/]*")
		case '?':
			b.WriteString("[^/]")
		case '[':
			end := strings.IndexByte(part[i+1:], ']')
			if end < 0 {
				b.WriteString(`\[`)
				continue
			}
			class := part[i+1 : i+1+end]
			if strings.HasPrefix(class, "!") {
				class = "^" + class[1:]
			}
			b.WriteString("[" + strings.ReplaceAll(class, `\`, `\\`) + "]")
			i += end + 1
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return b.String()
}

// expandBraces expands {a,b} alternatives (nested braces included); a
// brace without a matching close, or without a comma, is literal.
func expandBraces(pattern string) []string {
	open := strings.IndexByte(pattern, '{')
	for open >= 0 {
		depth, commas, closeAt := 0, []int{}, -1
		for i := open; i < len(pattern) && closeAt < 0; i++ {
			switch pattern[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					closeAt = i
				}
			case ',':
				if depth == 1 {
					commas = append(commas, i)
				}
			}
		}
		if closeAt < 0 {
			return []string{pattern}
		}
		if len(commas) == 0 {
			next := strings.IndexByte(pattern[open+1:], '{')
			if next < 0 {
				return []string{pattern}
			}
			open += 1 + next
			continue
		}
		prefix, suffix := pattern[:open], pattern[closeAt+1:]
		var out []string
		start := open + 1
		for _, c := range append(commas, closeAt) {
			out = append(out, expandBraces(prefix+pattern[start:c]+suffix)...)
			start = c + 1
		}
		return out
	}
	return []string{pattern}
}
