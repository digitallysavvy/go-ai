package harnessutil

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/internal/posixpath"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

const (
	skillsManifestFilename = ".ai-sdk-harness-skills.json"
	skillsManifestVersion  = 1
)

var (
	defaultSkillNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	safeManifestSkillName   = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	manifestHashPattern     = regexp.MustCompile(`^[a-f0-9]{64}$`)
	pathSeparators          = regexp.MustCompile(`[\\/]`)
)

// SkillFilePathMode controls how skill file paths are normalized.
type SkillFilePathMode string

const (
	// SkillFilePathRelative requires relative, normalized, non-traversing paths.
	SkillFilePathRelative SkillFilePathMode = "relative"
	// SkillFilePathStripLeadingSlashes strips leading "/" instead of rejecting.
	SkillFilePathStripLeadingSlashes SkillFilePathMode = "strip-leading-slashes"
)

// WriteSkillsOptions mirrors TS `WriteSkillsOptions`.
type WriteSkillsOptions struct {
	Sandbox providerutils.SandboxSession
	// HomePath is the absolute sandbox HOME; skills are materialized under
	// HomePath/SkillsDir (a39c8bf).
	HomePath  string
	SkillsDir string
	Skills    []harness.Skill

	// SkillNamePattern defaults to ^[A-Za-z0-9._-]+$.
	SkillNamePattern        *regexp.Regexp
	InvalidSkillNameMessage func(name string) string
	// FilePathMode defaults to SkillFilePathRelative.
	FilePathMode                SkillFilePathMode
	InvalidSkillFilePathMessage func(skillName, filePath string) string
	TrailingNewline             bool
}

// WriteSkillsResult mirrors TS `WriteSkillsResult`.
type WriteSkillsResult struct {
	Changed   bool     `json:"changed"`
	Written   []string `json:"written"`
	Removed   []string `json:"removed"`
	Unchanged []string `json:"unchanged"`
}

type projectedFile struct {
	path    string
	content string
}

type projectedSkill struct {
	name  string
	hash  string
	files []projectedFile
}

type skillsManifestEntry struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

type skillsManifest struct {
	Version int                   `json:"version"`
	State   string                `json:"state"`
	Skills  []skillsManifestEntry `json:"skills"`
}

// WriteSkills materializes skills as `<root>/<name>/SKILL.md` (+ files) with an
// ownership manifest, replacing only skills the harness owns and recovering
// from interrupted (pending) updates. Mirrors TS `writeSkills` (1ea15a3,
// a39c8bf, c857346).
func WriteSkills(ctx context.Context, opts WriteSkillsOptions) (*WriteSkillsResult, error) {
	pattern := opts.SkillNamePattern
	if pattern == nil {
		pattern = defaultSkillNamePattern
	}
	nameMessage := opts.InvalidSkillNameMessage
	if nameMessage == nil {
		nameMessage = func(name string) string { return "Invalid skill name: " + name }
	}
	mode := opts.FilePathMode
	if mode == "" {
		mode = SkillFilePathRelative
	}
	pathMessage := opts.InvalidSkillFilePathMessage
	if pathMessage == nil {
		pathMessage = func(skillName, filePath string) string {
			return fmt.Sprintf("Invalid skill file path for %s: %s", skillName, filePath)
		}
	}

	rootDir, err := resolveSkillsRootDir(opts.HomePath, opts.SkillsDir)
	if err != nil {
		return nil, err
	}
	projected := make([]projectedSkill, 0, len(opts.Skills))
	for _, skill := range opts.Skills {
		p, err := projectSkill(skill, pattern, nameMessage, mode, pathMessage, opts.TrailingNewline)
		if err != nil {
			return nil, err
		}
		projected = append(projected, p)
	}
	posixpath.SortFunc(projected, func(p projectedSkill) string { return p.name })
	for i := 1; i < len(projected); i++ {
		if projected[i-1].name == projected[i].name {
			return nil, fmt.Errorf("Duplicate skill name: %s", projected[i].name) //nolint:staticcheck // matches TS SDK's exact error text
		}
	}

	sandbox := opts.Sandbox
	manifestPath := posixpath.Join(rootDir, skillsManifestFilename)
	existing, err := readSkillsManifest(ctx, sandbox, manifestPath)
	if err != nil {
		return nil, err
	}
	nextEntries := make([]skillsManifestEntry, len(projected))
	nextByName := make(map[string]skillsManifestEntry, len(projected))
	for i, p := range projected {
		nextEntries[i] = skillsManifestEntry{Name: p.name, Hash: p.hash}
		nextByName[p.name] = nextEntries[i]
	}
	allNames := func() []string {
		out := make([]string, len(projected))
		for i, p := range projected {
			out[i] = p.name
		}
		return out
	}

	// Pending manifest: every listed directory may be partial; clear them
	// before rewriting the requested skills.
	if existing != nil && existing.State == "pending" {
		if err := ensureSkillsDirectory(ctx, sandbox, rootDir); err != nil {
			return nil, err
		}
		recovery := make([]string, len(existing.Skills))
		for i, s := range existing.Skills {
			recovery[i] = s.Name
		}
		if err := removeSkillDirectories(ctx, sandbox, rootDir, recovery); err != nil {
			return nil, err
		}
		removed := []string{}
		for _, name := range recovery {
			if _, ok := nextByName[name]; !ok {
				removed = append(removed, name)
			}
		}
		posixpath.SortStrings(removed)
		if err := writeProjectedSkills(ctx, sandbox, rootDir, projected); err != nil {
			return nil, err
		}
		if err := writeSkillsManifest(ctx, sandbox, manifestPath, skillsManifest{Version: skillsManifestVersion, State: "complete", Skills: nextEntries}); err != nil {
			return nil, err
		}
		return &WriteSkillsResult{
			Changed:   len(recovery) > 0 || len(projected) > 0,
			Written:   allNames(),
			Removed:   removed,
			Unchanged: []string{},
		}, nil
	}

	var previous []skillsManifestEntry
	if existing != nil {
		previous = existing.Skills
	}
	previousByName := make(map[string]skillsManifestEntry, len(previous))
	for _, e := range previous {
		previousByName[e.Name] = e
	}
	removed, written, unchanged := []string{}, []string{}, []string{}
	for _, e := range previous {
		if _, ok := nextByName[e.Name]; !ok {
			removed = append(removed, e.Name)
		}
	}
	for _, p := range projected {
		if prev, ok := previousByName[p.name]; ok && prev.Hash == p.hash {
			unchanged = append(unchanged, p.name)
		} else {
			written = append(written, p.name)
		}
	}
	posixpath.SortStrings(removed)
	posixpath.SortStrings(written)
	posixpath.SortStrings(unchanged)
	changed := len(removed) > 0 || len(written) > 0

	if !changed && existing != nil {
		return &WriteSkillsResult{Changed: false, Written: written, Removed: removed, Unchanged: unchanged}, nil
	}

	for _, name := range written {
		if _, owned := previousByName[name]; owned {
			continue
		}
		skillDir := posixpath.Join(rootDir, name)
		result, err := sandbox.Run(ctx, providerutils.SandboxProcessOptions{Command: "test ! -e " + ShellQuote(skillDir)})
		if err != nil {
			return nil, err
		}
		if result.ExitCode != 0 {
			return nil, fmt.Errorf("Cannot write harness skill '%s': %s already exists and is not owned by the AI SDK harness.", name, skillDir) //nolint:staticcheck // matches TS SDK's exact error text
		}
	}

	if err := ensureSkillsDirectory(ctx, sandbox, rootDir); err != nil {
		return nil, err
	}

	pendingSet := map[string]struct{}{}
	pendingNames := []string{}
	for _, e := range previous {
		if _, ok := pendingSet[e.Name]; !ok {
			pendingSet[e.Name] = struct{}{}
			pendingNames = append(pendingNames, e.Name)
		}
	}
	for _, e := range nextEntries {
		if _, ok := pendingSet[e.Name]; !ok {
			pendingSet[e.Name] = struct{}{}
			pendingNames = append(pendingNames, e.Name)
		}
	}
	posixpath.SortStrings(pendingNames)
	pendingEntries := make([]skillsManifestEntry, len(pendingNames))
	for i, name := range pendingNames {
		hash := previousByName[name].Hash
		if next, ok := nextByName[name]; ok {
			hash = next.Hash
		}
		pendingEntries[i] = skillsManifestEntry{Name: name, Hash: hash}
	}
	if err := writeSkillsManifest(ctx, sandbox, manifestPath, skillsManifest{Version: skillsManifestVersion, State: "pending", Skills: pendingEntries}); err != nil {
		return nil, err
	}

	toRemove := append([]string{}, removed...)
	for _, name := range written {
		if _, owned := previousByName[name]; owned {
			toRemove = append(toRemove, name)
		}
	}
	if err := removeSkillDirectories(ctx, sandbox, rootDir, toRemove); err != nil {
		return nil, err
	}
	writtenSet := map[string]struct{}{}
	for _, name := range written {
		writtenSet[name] = struct{}{}
	}
	toWrite := []projectedSkill{}
	for _, p := range projected {
		if _, ok := writtenSet[p.name]; ok {
			toWrite = append(toWrite, p)
		}
	}
	if err := writeProjectedSkills(ctx, sandbox, rootDir, toWrite); err != nil {
		return nil, err
	}
	if err := writeSkillsManifest(ctx, sandbox, manifestPath, skillsManifest{Version: skillsManifestVersion, State: "complete", Skills: nextEntries}); err != nil {
		return nil, err
	}
	return &WriteSkillsResult{Changed: changed, Written: written, Removed: removed, Unchanged: unchanged}, nil
}

func projectSkill(skill harness.Skill, pattern *regexp.Regexp, nameMessage func(string) string, mode SkillFilePathMode, pathMessage func(string, string) string, trailingNewline bool) (projectedSkill, error) {
	if !pattern.MatchString(skill.Name) || skill.Name == "." || skill.Name == ".." {
		return projectedSkill{}, fmt.Errorf("%s", nameMessage(skill.Name))
	}
	content := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n%s", skill.Name, skill.Description, skill.Content)
	if trailingNewline {
		content += "\n"
	}
	// Map semantics: later entries with the same path overwrite earlier ones
	// but keep their first position (irrelevant after sorting).
	files := map[string]string{"SKILL.md": content}
	for _, file := range skill.Files {
		p, err := normalizeSkillFilePath(skill.Name, file.Path, mode, pathMessage)
		if err != nil {
			return projectedSkill{}, err
		}
		files[p] = file.Content
	}
	out := make([]projectedFile, 0, len(files))
	for p, c := range files {
		out = append(out, projectedFile{path: p, content: c})
	}
	posixpath.SortFunc(out, func(f projectedFile) string { return f.path })

	h := sha256.New()
	for _, f := range out {
		h.Write([]byte(strconv.Itoa(len(f.path)) + ":" + f.path))
		h.Write([]byte(strconv.Itoa(len(f.content)) + ":" + f.content))
	}
	return projectedSkill{name: skill.Name, hash: hex.EncodeToString(h.Sum(nil)), files: out}, nil
}

func normalizeSkillFilePath(skillName, filePath string, mode SkillFilePathMode, message func(string, string) string) (string, error) {
	var normalized string
	if mode == SkillFilePathStripLeadingSlashes {
		normalized = strings.TrimLeft(filePath, "/")
	} else {
		normalized = posixpath.Normalize(filePath)
	}
	invalid := normalized == "" ||
		(mode == SkillFilePathRelative && normalized == ".") ||
		strings.HasPrefix(normalized, "../") ||
		strings.Contains(normalized, "/../") ||
		strings.HasSuffix(normalized, "/..") ||
		(mode == SkillFilePathRelative && posixpath.IsAbs(normalized))
	if invalid {
		return "", fmt.Errorf("%s", message(skillName, filePath))
	}
	return normalized, nil
}

func readSkillsManifest(ctx context.Context, sandbox providerutils.SandboxSession, manifestPath string) (*skillsManifest, error) {
	content, err := sandbox.ReadTextFile(ctx, providerutils.SandboxReadTextFileOptions{Path: manifestPath})
	if err != nil {
		return nil, err
	}
	if content == nil {
		return nil, nil
	}
	invalid := fmt.Errorf("Invalid AI SDK harness skills manifest: %s", manifestPath) //nolint:staticcheck // matches TS SDK's exact error text
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(*content), &raw); err != nil || raw == nil {
		return nil, invalid
	}
	var m skillsManifest
	if err := json.Unmarshal([]byte(*content), &m); err != nil {
		return nil, invalid
	}
	if m.Version != skillsManifestVersion || (m.State != "complete" && m.State != "pending") || !isJSONArray(raw["skills"]) {
		return nil, invalid
	}
	names := map[string]struct{}{}
	for _, s := range m.Skills {
		if !safeManifestSkillName.MatchString(s.Name) || s.Name == "." || s.Name == ".." || strings.Contains(s.Name, "/") ||
			!manifestHashPattern.MatchString(s.Hash) {
			return nil, invalid
		}
		if _, dup := names[s.Name]; dup {
			return nil, invalid
		}
		names[s.Name] = struct{}{}
	}
	return &m, nil
}

func isJSONArray(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '['
}

// stringifyJSON mirrors `${JSON.stringify(value, null, 2)}\n`.
func stringifyJSON(value any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func writeSkillsManifest(ctx context.Context, sandbox providerutils.SandboxSession, manifestPath string, manifest skillsManifest) error {
	if manifest.Skills == nil {
		manifest.Skills = []skillsManifestEntry{}
	}
	content, err := stringifyJSON(manifest)
	if err != nil {
		return err
	}
	temporary := manifestPath + ".tmp"
	if err := sandbox.WriteTextFile(ctx, providerutils.SandboxWriteTextFileOptions{Path: temporary, Content: content}); err != nil {
		return err
	}
	return runSandboxCommand(ctx, sandbox, "mv -f "+ShellQuote(temporary)+" "+ShellQuote(manifestPath), "Failed to update skills manifest: "+manifestPath)
}

func ensureSkillsDirectory(ctx context.Context, sandbox providerutils.SandboxSession, rootDir string) error {
	return runSandboxCommand(ctx, sandbox, "mkdir -p "+ShellQuote(rootDir), "Failed to create skills directory: "+rootDir)
}

func removeSkillDirectories(ctx context.Context, sandbox providerutils.SandboxSession, rootDir string, names []string) error {
	if len(names) == 0 {
		return nil
	}
	unique := uniqueNames(names)
	posixpath.SortStrings(unique)
	quoted := make([]string, len(unique))
	for i, name := range unique {
		quoted[i] = ShellQuote(posixpath.Join(rootDir, name))
	}
	return runSandboxCommand(ctx, sandbox, "rm -rf -- "+strings.Join(quoted, " "), "Failed to replace harness skills in: "+rootDir)
}

func writeProjectedSkills(ctx context.Context, sandbox providerutils.SandboxSession, rootDir string, skills []projectedSkill) error {
	for _, skill := range skills {
		skillDir := posixpath.Join(rootDir, skill.name)
		for _, f := range skill.files {
			if err := sandbox.WriteTextFile(ctx, providerutils.SandboxWriteTextFileOptions{Path: posixpath.Join(skillDir, f.path), Content: f.content}); err != nil {
				return err
			}
		}
	}
	return nil
}

func runSandboxCommand(ctx context.Context, sandbox providerutils.SandboxSession, command, errorMessage string) error {
	result, err := sandbox.Run(ctx, providerutils.SandboxProcessOptions{Command: command})
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		suffix := ""
		if result.Stderr != "" {
			suffix = ": " + result.Stderr
		}
		return fmt.Errorf("%s (exit %d)%s", errorMessage, result.ExitCode, suffix)
	}
	return nil
}

// validateHomeRelativePath implements the shared homePath/relative-path checks
// of resolveSkillsRootDir and resolveInstructionsFilePath. It returns the
// normalized path without trailing slashes.
func validateHomeRelativePath(homePath, rel, label string, extraInvalid bool) (string, error) {
	if strings.TrimSpace(homePath) == "" {
		return "", fmt.Errorf("Invalid homePath: expected a non-empty string.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	if !posixpath.IsAbs(homePath) {
		return "", fmt.Errorf("Invalid homePath %s: expected an absolute POSIX path.", jsonString(homePath)) //nolint:staticcheck // matches TS SDK's exact error text
	}
	invalid := fmt.Errorf("Invalid %s %s: expected a relative POSIX path without traversal.", label, jsonString(rel)) //nolint:staticcheck // matches TS SDK's exact error text
	if strings.TrimSpace(rel) == "" {
		return "", invalid
	}
	containsTraversal := false
	for _, seg := range pathSeparators.Split(rel, -1) {
		if seg == ".." {
			containsTraversal = true
		}
	}
	normalized := posixpath.Normalize(strings.TrimSpace(rel))
	noTrailing := strings.TrimRight(normalized, "/")
	if strings.Contains(rel, `\`) || posixpath.IsAbs(rel) || posixpath.IsWin32Abs(rel) || containsTraversal || extraInvalid ||
		noTrailing == "" || noTrailing == "." ||
		strings.HasPrefix(normalized, "../") || strings.Contains(normalized, "/../") || strings.HasSuffix(normalized, "/..") {
		return "", invalid
	}
	return noTrailing, nil
}

func resolveSkillsRootDir(homePath, skillsDir string) (string, error) {
	normalized, err := validateHomeRelativePath(homePath, skillsDir, "skillsDir", false)
	if err != nil {
		return "", err
	}
	return posixpath.Join(homePath, normalized), nil
}
