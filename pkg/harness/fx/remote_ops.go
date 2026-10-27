package fx

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

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

// Node's path.matchesGlob runs minimatch with `nocase: isMacOS || isWindows`
// and `nocaseMagicOnly: true`, and with the win32 platform on Windows. So on
// a macOS or Windows host a segment with a wildcard or class ignores case
// (like a JavaScript `i` regexp), while a plain segment such as `src` still
// compares exactly; on Windows, `\` also separates the path's segments.
// Variables so tests can exercise every host.
var (
	globNoCase     = runtime.GOOS == "darwin" || runtime.GOOS == "windows"
	globWin32Paths = runtime.GOOS == "windows"
)

// globPattern is a compiled glob (`*`, `?`, `**`, `[...]`, `{a,b}`) over
// slash-separated relative paths: the subset of Node's path.matchesGlob
// (minimatch without the dot option) the fx glob tool relies on. As there,
// a wildcard never matches a path segment that starts with "." unless the
// pattern segment itself starts with ".", so `**/*` skips .git and other
// dot directories. A `\` in the pattern is a separator, never an escape
// (minimatch windowsPathsNoEscape).
type globPattern struct {
	alternatives [][]globSegment
	noCase       bool
	win32        bool
}

// globSegment is one slash-separated pattern segment.
type globSegment struct {
	globstar bool // the whole segment is **
	dot      bool // the segment starts with a literal "."
	magic    bool // the segment has a wildcard or a class (minimatch hasMagic)
	// unicode: minimatch adds the u flag (a posix class other than
	// [:ascii:] and [:xdigit:]). Without it the JavaScript regexp sees
	// UTF-16 code units, so `?` matches half of a character outside the BMP.
	unicode bool
	literal string // the text a segment without magic must equal exactly
	tokens  []globToken
}

type globTokenKind int

const (
	globTokenLiteral globTokenKind = iota
	globTokenAny
	globTokenStar
	globTokenClass
)

// globToken is one element of a magic segment.
type globToken struct {
	kind globTokenKind
	r    rune // globTokenLiteral
	// globTokenClass, built like minimatch's parseClass: ranges holds the
	// listed characters, ranges and posix classes; negs the negated posix
	// classes ([:graph:], [:print:]). Either may be nil.
	ranges, negs *regexp.Regexp
	negate       bool
}

func globRegexp(pattern string) (*globPattern, error) {
	return compileGlob(pattern, globNoCase, globWin32Paths)
}

func compileGlob(pattern string, noCase, win32 bool) (*globPattern, error) {
	g := &globPattern{noCase: noCase, win32: win32}
	for _, alt := range expandBraces(strings.ReplaceAll(pattern, `\`, "/")) {
		var segs []globSegment
		for _, part := range strings.Split(alt, "/") {
			if part == "**" {
				segs = append(segs, globSegment{globstar: true})
				continue
			}
			seg, err := parseGlobSegment(part)
			if err != nil {
				return nil, fmt.Errorf("invalid glob pattern %q: %w", pattern, err)
			}
			segs = append(segs, seg)
		}
		g.alternatives = append(g.alternatives, segs)
	}
	return g, nil
}

// MatchString reports whether path matches the pattern.
func (g *globPattern) MatchString(path string) bool {
	if g.win32 {
		path = strings.ReplaceAll(path, `\`, "/")
	}
	parts := strings.Split(path, "/")
	for _, segs := range g.alternatives {
		if g.matchSegments(segs, parts) {
			return true
		}
	}
	return false
}

func (g *globPattern) matchSegments(segs []globSegment, parts []string) bool {
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
			if n >= min && g.matchSegments(segs[1:], parts[n:]) {
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
	return g.matchSegment(s, parts[0]) && g.matchSegments(segs[1:], parts[1:])
}

// matchSegment matches one path segment. Without magic the segment is a
// string compared exactly even when case is ignored (nocaseMagicOnly).
func (g *globPattern) matchSegment(s globSegment, name string) bool {
	if !s.magic {
		return name == s.literal
	}
	toks, rs := s.tokens, []rune(name)
	if !s.unicode {
		rs = utf16Units(rs)
	}
	ti, ni, starTi, starNi := 0, 0, -1, 0
	for ni < len(rs) {
		switch {
		case ti < len(toks) && toks[ti].kind == globTokenStar:
			starTi, starNi = ti, ni
			ti++
		case ti < len(toks) && g.matchToken(toks[ti], rs[ni], s.unicode):
			ti++
			ni++
		case starTi >= 0:
			ti = starTi + 1
			starNi++
			ni = starNi
		default:
			return false
		}
	}
	for ti < len(toks) && toks[ti].kind == globTokenStar {
		ti++
	}
	return ti == len(toks)
}

func (g *globPattern) matchToken(t globToken, c rune, unicodeMode bool) bool {
	switch t.kind {
	case globTokenAny:
		return true
	case globTokenLiteral:
		switch {
		case !g.noCase:
			return t.r == c
		case unicodeMode:
			// An iu regexp compares simple case foldings.
			return foldOrbitHas(t.r, func(x rune) bool { return x == c })
		}
		return jsCanonicalize(t.r) == jsCanonicalize(c)
	}
	// minimatch builds `[ranges]`, `[^negs]` or `([ranges]|[^negs])`, with
	// the inner negations flipped for `[!...]`.
	matched := false
	if t.ranges != nil {
		matched = g.classHas(t.ranges, c, unicodeMode) != t.negate
	}
	if !matched && t.negs != nil {
		matched = g.classHas(t.negs, c, unicodeMode) == t.negate
	}
	return matched
}

// foldOrbitHas reports whether f holds for r or any character in r's
// simple case-folding orbit.
func foldOrbitHas(r rune, f func(rune) bool) bool {
	for x := r; ; {
		if f(x) {
			return true
		}
		if x = unicode.SimpleFold(x); x == r {
			return false
		}
	}
}

// utf16Units splits the characters outside the BMP into surrogate pairs.
func utf16Units(rs []rune) []rune {
	var out []rune
	for _, r := range rs {
		if r > 0xFFFF {
			hi, lo := utf16.EncodeRune(r)
			out = append(out, hi, lo)
			continue
		}
		out = append(out, r)
	}
	return out
}

// classHas reports whether the class holds c; ignoring case, whether it
// holds any character that canonicalizes like c (the ES CharacterSetMatcher
// rule for an `i` regexp).
func (g *globPattern) classHas(class *regexp.Regexp, c rune, unicodeMode bool) bool {
	if !g.noCase {
		return class.MatchString(string(c))
	}
	if unicodeMode {
		return foldOrbitHas(c, func(x rune) bool { return class.MatchString(string(x)) })
	}
	want := jsCanonicalize(c)
	if class.MatchString(string(c)) {
		return true
	}
	for x := unicode.SimpleFold(want); ; x = unicode.SimpleFold(x) {
		if jsCanonicalize(x) == want && class.MatchString(string(x)) {
			return true
		}
		if x == want {
			return false
		}
	}
}

// jsCanonicalize is ES Canonicalize(ch) for a regexp with the i flag and
// no u flag: the character's toUpperCase when that is one UTF-16 code unit
// and does not map a non-ASCII character into ASCII. Characters outside the
// BMP are two code units there, so they never change case.
func jsCanonicalize(r rune) rune {
	if r < utf8.RuneSelf {
		if 'a' <= r && r <= 'z' {
			return r - ('a' - 'A')
		}
		return r
	}
	if r > 0xFFFF || jsUpperIsMultiUnit(r) {
		return r
	}
	u := unicode.ToUpper(r)
	if u < utf8.RuneSelf || u > 0xFFFF {
		return r
	}
	return u
}

// jsUpperIsMultiUnit reports the BMP characters whose JavaScript
// toUpperCase is more than one code unit (SpecialCasing.txt), such as ß.
func jsUpperIsMultiUnit(r rune) bool {
	for _, rg := range jsMultiUnitUpper {
		if r < rg[0] {
			return false
		}
		if r <= rg[1] {
			return true
		}
	}
	return false
}

var jsMultiUnitUpper = [][2]rune{
	{0xdf, 0xdf}, {0x149, 0x149}, {0x1f0, 0x1f0}, {0x390, 0x390}, {0x3b0, 0x3b0}, {0x587, 0x587},
	{0x1e96, 0x1e9a}, {0x1f50, 0x1f50}, {0x1f52, 0x1f52}, {0x1f54, 0x1f54}, {0x1f56, 0x1f56},
	{0x1f80, 0x1faf}, {0x1fb2, 0x1fb4}, {0x1fb6, 0x1fb7}, {0x1fbc, 0x1fbc}, {0x1fc2, 0x1fc4},
	{0x1fc6, 0x1fc7}, {0x1fcc, 0x1fcc}, {0x1fd2, 0x1fd3}, {0x1fd6, 0x1fd7}, {0x1fe2, 0x1fe4},
	{0x1fe6, 0x1fe7}, {0x1ff2, 0x1ff4}, {0x1ff6, 0x1ff7}, {0x1ffc, 0x1ffc}, {0xfb00, 0xfb06},
	{0xfb13, 0xfb17},
}

// globPosixClasses are minimatch's posix classes: the regexp class body and
// whether the class is negated.
var globPosixClasses = map[string]struct {
	body            string
	unicode, negate bool
}{
	"[:alnum:]": {`\p{L}\p{Nl}\p{Nd}`, true, false}, "[:alpha:]": {`\p{L}\p{Nl}`, true, false},
	"[:ascii:]": {`\x00-\x7f`, false, false}, "[:blank:]": {`\p{Zs}\t`, true, false},
	"[:cntrl:]": {`\p{Cc}`, true, false}, "[:digit:]": {`\p{Nd}`, true, false},
	"[:graph:]": {`\p{Z}\p{C}`, true, true}, "[:lower:]": {`\p{Ll}`, true, false},
	"[:print:]": {`\p{C}`, true, true}, "[:punct:]": {`\p{P}`, true, false},
	"[:space:]": {`\p{Z}\t\r\n\v\f`, true, false}, "[:upper:]": {`\p{Lu}`, true, false},
	"[:word:]": {`\p{L}\p{Nl}\p{Nd}\p{Pc}`, true, false}, "[:xdigit:]": {`A-Fa-f0-9`, false, false},
}

// parseGlobSegment splits one segment into tokens.
func parseGlobSegment(part string) (globSegment, error) {
	seg := globSegment{dot: strings.HasPrefix(part, ".")}
	var literal strings.Builder
	rs := []rune(part)
	for i := 0; i < len(rs); i++ {
		switch c := rs[i]; c {
		case '*':
			for i+1 < len(rs) && rs[i+1] == '*' {
				i++
			}
			seg.magic = true
			seg.tokens = append(seg.tokens, globToken{kind: globTokenStar})
		case '?':
			seg.magic = true
			seg.tokens = append(seg.tokens, globToken{kind: globTokenAny})
		case '[':
			tok, single, end, ok, uflag, err := parseGlobClass(rs, i)
			if err != nil {
				return globSegment{}, err
			}
			if !ok {
				literal.WriteRune(c)
				seg.tokens = append(seg.tokens, globToken{kind: globTokenLiteral, r: c})
				continue
			}
			i = end
			seg.unicode = seg.unicode || uflag
			if single >= 0 {
				// minimatch: a class of one character is that character.
				literal.WriteRune(single)
				seg.tokens = append(seg.tokens, globToken{kind: globTokenLiteral, r: single})
				continue
			}
			seg.magic = true
			seg.tokens = append(seg.tokens, tok)
		default:
			literal.WriteRune(c)
			seg.tokens = append(seg.tokens, globToken{kind: globTokenLiteral, r: c})
		}
	}
	seg.literal = literal.String()
	if !seg.unicode {
		// Without the u flag a character outside the BMP is two code units.
		var toks []globToken
		for _, t := range seg.tokens {
			if t.kind == globTokenLiteral && t.r > 0xFFFF {
				hi, lo := utf16.EncodeRune(t.r)
				toks = append(toks, globToken{kind: globTokenLiteral, r: hi}, globToken{kind: globTokenLiteral, r: lo})
				continue
			}
			toks = append(toks, t)
		}
		seg.tokens = toks
	}
	return seg, nil
}

// classRune escapes r for a regexp class body.
func classRune(r rune) string {
	if r == '-' {
		return `\-`
	}
	return regexp.QuoteMeta(string(r))
}

// parseGlobClass parses the class opening at rs[start]. It returns the
// closing index, or ok=false when the class never closes (the "[" is then
// literal). single is the class's only character when it is a plain
// one-character class, else -1.
func parseGlobClass(rs []rune, start int) (tok globToken, single rune, end int, ok, uflag bool, err error) {
	i := start + 1
	if i < len(rs) && (rs[i] == '!' || rs[i] == '^') {
		tok.negate = true
		i++
	}
	var ranges, negs strings.Builder
	var items []string
	first := i
	for ; i < len(rs); i++ {
		c := rs[i]
		if c == ']' && i > first {
			break
		}
		if c == '[' && i+1 < len(rs) && rs[i+1] == ':' {
			if closeAt := strings.Index(string(rs[i:]), ":]"); closeAt > 0 {
				name := string(rs[i:])[:closeAt+2]
				if pc, known := globPosixClasses[name]; known {
					uflag = uflag || pc.unicode
					if pc.negate {
						negs.WriteString(pc.body)
					} else {
						ranges.WriteString(pc.body)
					}
					items = append(items, name)
					i += utf8.RuneCountInString(name) - 1
					continue
				}
			}
		}
		if i+2 < len(rs) && rs[i+1] == '-' && rs[i+2] != ']' {
			ranges.WriteString(classRune(c) + "-" + classRune(rs[i+2]))
			items = append(items, string(rs[i:i+3]))
			i += 2
			continue
		}
		ranges.WriteString(classRune(c))
		items = append(items, string(c))
	}
	if i >= len(rs) {
		return globToken{}, -1, 0, false, false, nil
	}
	if !tok.negate && len(items) == 1 && negs.Len() == 0 {
		if r := []rune(items[0]); len(r) == 1 && r[0] <= 0xFFFF {
			return globToken{}, r[0], i, true, false, nil
		}
	}
	tok.kind = globTokenClass
	if ranges.Len() > 0 {
		if tok.ranges, err = regexp.Compile("^[" + ranges.String() + "]$"); err != nil {
			return globToken{}, -1, 0, false, false, err
		}
	}
	if negs.Len() > 0 {
		if tok.negs, err = regexp.Compile("^[" + negs.String() + "]$"); err != nil {
			return globToken{}, -1, 0, false, false, err
		}
	}
	return tok, -1, i, true, uflag, nil
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
