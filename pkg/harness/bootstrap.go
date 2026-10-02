package harness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/digitallysavvy/go-ai/pkg/harness/internal/posixpath"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// BootstrapFile is one file an adapter's bootstrap recipe writes into the
// sandbox. Relative paths resolve against the harness state directory
// (`$HOME/.ai-sdk-harness`). Mirrors TS `HarnessV1BootstrapFile`.
type BootstrapFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// BootstrapCommand is one command run from the bootstrap directory after all
// files are written. Mirrors TS `HarnessV1BootstrapCommand` (no
// workingDirectory since 2c0a8aa).
type BootstrapCommand struct {
	Command string `json:"command"`
}

// Bootstrap is an adapter-owned bootstrap recipe. Mirrors TS
// `HarnessV1Bootstrap`.
type Bootstrap struct {
	// HarnessID is the id of the adapter that owns this recipe.
	HarnessID string `json:"harnessId"`
	// BootstrapDir is where the recipe writes its state. Relative paths
	// resolve against the harness state directory; the marker lives directly
	// under it.
	BootstrapDir string             `json:"bootstrapDir"`
	Files        []BootstrapFile    `json:"files"`
	Commands     []BootstrapCommand `json:"commands"`
}

// BootstrapSchemaVersion is the version of the recipe shape itself. Bumping it
// invalidates every snapshot and marker.
const BootstrapSchemaVersion = 1

const (
	sandboxBootstrapIdentityVersion   = 1
	preparedSandboxIdentityVersion    = 1
	bootstrapDirectoryCreateCommand   = `mkdir -p "$BOOTSTRAP_DIR"`
	sandboxWorkDirectoryCreateCommand = `mkdir -p "$WORK_DIR"`
)

// identityHasher reproduces the TS pattern: every value is UTF-8 encoded and
// followed by a NUL byte, then SHA-256; the identity is the first 8 bytes as
// lowercase hex (16 chars).
type identityHasher struct {
	buf []byte
}

func (h *identityHasher) push(value string) {
	h.buf = append(h.buf, value...)
	h.buf = append(h.buf, 0)
}

func (h *identityHasher) sum() string {
	digest := sha256.Sum256(h.buf)
	return hex.EncodeToString(digest[:8])
}

// HashHarnessBootstrap returns the deterministic 16-char hex identity of a
// recipe (state directory with symbolic $HOME, harnessId, bootstrapDir, files
// sorted by path with localeCompare, JSON of commands, schema version).
// Identical to TS `hashHarnessBootstrap` for identical inputs.
func HashHarnessBootstrap(recipe Bootstrap) string {
	var h identityHasher
	h.push(StateDirectoryPath("$HOME"))
	h.push(recipe.HarnessID)
	h.push(recipe.BootstrapDir)

	files := append([]BootstrapFile(nil), recipe.Files...)
	posixpath.SortFunc(files, func(f BootstrapFile) string { return f.Path })
	for _, file := range files {
		h.push(file.Path)
		h.push(file.Content)
	}

	h.push(stringifyCommands(recipe.Commands))
	h.push(strconv.Itoa(BootstrapSchemaVersion))
	return h.sum()
}

// stringifyCommands reproduces `JSON.stringify(recipe.commands)` byte for
// byte (Go's encoder escapes <, >, &, U+2028 and U+2029, JavaScript does not).
func stringifyCommands(commands []BootstrapCommand) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, cmd := range commands {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"command":`)
		writeJSString(&b, cmd.Command)
		b.WriteByte('}')
	}
	b.WriteByte(']')
	return b.String()
}

func writeJSString(b *strings.Builder, s string) {
	const hexDigits = "0123456789abcdef"
	b.WriteByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\b':
			b.WriteString(`\b`)
		case r == '\f':
			b.WriteString(`\f`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20:
			b.WriteString(`\u00`)
			b.WriteByte(hexDigits[r>>4])
			b.WriteByte(hexDigits[r&0xf])
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	b.WriteByte('"')
}

// BootstrapMarkerPath returns the absolute marker path written after a recipe
// with identity has been applied. Mirrors TS `bootstrapMarkerPath`.
func BootstrapMarkerPath(recipe Bootstrap, identity, stateDirectory string) string {
	return posixpath.Join(resolveBootstrapPath(recipe.BootstrapDir, stateDirectory), ".bootstrap-"+identity+".ok")
}

func resolveBootstrapPath(p, stateDirectory string) string {
	if posixpath.IsAbs(p) {
		return p
	}
	return posixpath.Resolve(stateDirectory, p)
}

// ApplyBootstrapRecipe applies a recipe idempotently: if the marker exists it
// returns immediately; otherwise it creates the bootstrap directory, writes the
// files, runs the commands from the bootstrap directory and writes the marker.
// Mirrors TS `applyBootstrapRecipe`.
func ApplyBootstrapRecipe(ctx context.Context, session providerutils.SandboxSession, recipe Bootstrap, identity, stateDirectory string) error {
	markerPath := BootstrapMarkerPath(recipe, identity, stateDirectory)
	existing, err := session.ReadTextFile(ctx, providerutils.SandboxReadTextFileOptions{Path: markerPath})
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}

	bootstrapDir := resolveBootstrapPath(recipe.BootstrapDir, stateDirectory)
	mkdir, err := session.Run(ctx, providerutils.SandboxProcessOptions{
		Command: bootstrapDirectoryCreateCommand,
		Env:     map[string]string{"BOOTSTRAP_DIR": bootstrapDir},
	})
	if err != nil {
		return err
	}
	if mkdir.ExitCode != 0 {
		return fmt.Errorf("Failed to create bootstrap directory for harness '%s' (exit %d): %s\n%s", //nolint:staticcheck // matches TS SDK's exact error text
			recipe.HarnessID, mkdir.ExitCode, bootstrapDir, orString(mkdir.Stderr, mkdir.Stdout))
	}

	for _, file := range recipe.Files {
		if err := session.WriteTextFile(ctx, providerutils.SandboxWriteTextFileOptions{
			Path:    resolveBootstrapPath(file.Path, stateDirectory),
			Content: file.Content,
		}); err != nil {
			return err
		}
	}

	for _, cmd := range recipe.Commands {
		result, err := session.Run(ctx, providerutils.SandboxProcessOptions{
			Command:          cmd.Command,
			WorkingDirectory: bootstrapDir,
		})
		if err != nil {
			return err
		}
		if result.ExitCode != 0 {
			return fmt.Errorf("Bootstrap command failed for harness '%s' (exit %d): %s\n%s",
				recipe.HarnessID, result.ExitCode, cmd.Command, orString(result.Stderr, result.Stdout))
		}
	}

	return session.WriteTextFile(ctx, providerutils.SandboxWriteTextFileOptions{Path: markerPath, Content: ""})
}

// SandboxBootstrapContext is passed to SandboxConfig.OnBootstrap.
type SandboxBootstrapContext struct {
	Session providerutils.SandboxSession
	WorkDir string
}

// SandboxSessionContext is passed to SandboxConfig.OnSession.
type SandboxSessionContext struct {
	Session        providerutils.SandboxSession
	SessionWorkDir string
}

// SandboxConfig is the harness sandbox configuration. Mirrors TS
// `HarnessAgentSandboxConfig`.
type SandboxConfig struct {
	// WorkDir is an optional fixed working directory for all sessions,
	// relative to the sandbox default working directory.
	WorkDir string
	// BootstrapHash is the caller-controlled identity for OnBootstrap.
	BootstrapHash string
	// OnBootstrap runs during sandbox template creation after the adapter's
	// own bootstrap, before snapshot-capable providers publish a snapshot
	// (a83a367). Must be provided together with BootstrapHash.
	OnBootstrap func(ctx context.Context, bc SandboxBootstrapContext) error
	// OnSession runs after each sandbox session is acquired and the session
	// work directory exists, before the adapter starts.
	OnSession func(ctx context.Context, sc SandboxSessionContext) error
}

// ValidateSandboxBootstrapSettings mirrors TS `validateSandboxBootstrapSettings`.
func ValidateSandboxBootstrapSettings(cfg SandboxConfig) error {
	if (cfg.OnBootstrap == nil) != (cfg.BootstrapHash == "") {
		return errors.New("HarnessAgent: `sandboxConfig.onBootstrap` and `sandboxConfig.bootstrapHash` must be provided together.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	if cfg.WorkDir != "" {
		if _, err := NormalizeSandboxWorkDir(cfg.WorkDir); err != nil {
			return err
		}
	}
	return nil
}

// NormalizeSandboxWorkDir validates and normalizes a relative work directory.
// Mirrors TS `normalizeSandboxWorkDir`.
func NormalizeSandboxWorkDir(workDir string) (string, error) {
	if workDir == "" {
		return "", errors.New("HarnessAgent: `sandboxConfig.workDir` must not be empty.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	if strings.Contains(workDir, "\x00") {
		return "", errors.New("HarnessAgent: `sandboxConfig.workDir` must not contain NUL.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	if strings.Contains(workDir, `\`) {
		return "", errors.New("HarnessAgent: `sandboxConfig.workDir` must use POSIX path separators.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	if posixpath.IsAbs(workDir) {
		return "", errors.New("HarnessAgent: `sandboxConfig.workDir` must be relative.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	normalized := posixpath.Normalize(workDir)
	if ((normalized == "." || normalized == "./") && workDir != ".") ||
		normalized == ".." || strings.HasPrefix(normalized, "../") {
		return "", errors.New("HarnessAgent: `sandboxConfig.workDir` must stay inside the sandbox default working directory.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	return normalized, nil
}

// ResolveSessionWorkDir composes the per-session working directory
// (`<default>/<harnessId>-<sessionId>` or `<default>/<workDir>`). Mirrors TS
// `resolveSessionWorkDir`.
func ResolveSessionWorkDir(defaultWorkingDirectory, harnessID, sessionID, workDir string) string {
	p := workDir
	if p == "" {
		p = EncodePathSegment(harnessID) + "-" + EncodePathSegment(sessionID)
	}
	return posixpath.Join(defaultWorkingDirectory, p)
}

// SandboxBootstrapPlan is the result of CreateSandboxBootstrapPlan. Mirrors TS
// `SandboxBootstrapPlan`.
type SandboxBootstrapPlan struct {
	Recipe         *Bootstrap
	RecipeIdentity string
	// Identity is the sandbox identity passed to SandboxProvider.CreateSession.
	Identity      string
	WorkDir       string
	OnFirstCreate OnFirstCreateFunc
}

// CreateSandboxBootstrapPlan computes the recipe identity and the combined
// sandbox identity. Mirrors TS `createSandboxBootstrapPlan`.
func CreateSandboxBootstrapPlan(recipe *Bootstrap, cfg SandboxConfig) (SandboxBootstrapPlan, error) {
	var plan SandboxBootstrapPlan
	if cfg.WorkDir != "" {
		wd, err := NormalizeSandboxWorkDir(cfg.WorkDir)
		if err != nil {
			return plan, err
		}
		plan.WorkDir = wd
	}
	if recipe != nil {
		plan.Recipe = recipe
		plan.RecipeIdentity = HashHarnessBootstrap(*recipe)
	}
	hasCallerBootstrap := cfg.OnBootstrap != nil
	needsCombinedIdentity := hasCallerBootstrap || (plan.RecipeIdentity != "" && plan.WorkDir != "")
	if needsCombinedIdentity && (plan.RecipeIdentity != "" || hasCallerBootstrap) {
		plan.Identity = hashSandboxBootstrapIdentity(plan.RecipeIdentity, cfg.BootstrapHash, plan.WorkDir)
	} else {
		plan.Identity = plan.RecipeIdentity
	}
	if recipe != nil || cfg.OnBootstrap != nil {
		recipeIdentity, workDir, onBootstrap, bootstrapHash := plan.RecipeIdentity, plan.WorkDir, cfg.OnBootstrap, cfg.BootstrapHash
		plan.OnFirstCreate = func(ctx context.Context, session providerutils.SandboxSession) error {
			return RunSandboxBootstrap(ctx, RunSandboxBootstrapOptions{
				Session:        session,
				Recipe:         recipe,
				RecipeIdentity: recipeIdentity,
				WorkDir:        workDir,
				OnBootstrap:    onBootstrap,
				BootstrapHash:  bootstrapHash,
			})
		}
	}
	return plan, nil
}

// RunSandboxBootstrapOptions is the input of RunSandboxBootstrap.
type RunSandboxBootstrapOptions struct {
	Session        providerutils.SandboxSession
	Recipe         *Bootstrap
	RecipeIdentity string
	WorkDir        string
	OnBootstrap    func(ctx context.Context, bc SandboxBootstrapContext) error
	// BootstrapHash is the caller-controlled identity OnBootstrap is keyed
	// by. Required for SkipOnBootstrapIfMarked and for the completion
	// marker this writes after a successful OnBootstrap run.
	BootstrapHash string
	// SkipOnBootstrapIfMarked skips OnBootstrap entirely when a marker for
	// BootstrapHash already exists on this sandbox (from a prior call, e.g.
	// a snapshot-provider's OnFirstCreate having already run it). Mirrors
	// TS `runSandboxBootstrap`'s `skipOnBootstrapIfMarked` (31742b9a1b).
	SkipOnBootstrapIfMarked bool
	// DefaultWorkingDirectory skips the `pwd` lookup when known.
	DefaultWorkingDirectory string
}

// RunSandboxBootstrap applies the adapter recipe under the sandbox HOME, then
// runs the caller's OnBootstrap in the (optionally fixed) work directory.
// When BootstrapHash is set, a completion marker is written after OnBootstrap
// succeeds (and, with SkipOnBootstrapIfMarked, checked first) so repeated
// calls against the same physical sandbox are cheap no-ops. Mirrors TS
// `runSandboxBootstrap` (31742b9a1b).
func RunSandboxBootstrap(ctx context.Context, opts RunSandboxBootstrapOptions) error {
	if opts.Recipe == nil && opts.OnBootstrap == nil {
		return nil
	}
	if opts.Recipe != nil && opts.RecipeIdentity != "" {
		// Harness infrastructure always lives under the sandbox's own HOME,
		// never the working directory.
		home, err := ResolveSandboxHomeDir(ctx, opts.Session)
		if err != nil {
			return err
		}
		if err := ApplyBootstrapRecipe(ctx, opts.Session, *opts.Recipe, opts.RecipeIdentity, StateDirectoryPath(home)); err != nil {
			return err
		}
	}
	if opts.OnBootstrap == nil {
		return nil
	}
	if opts.SkipOnBootstrapIfMarked && opts.BootstrapHash != "" {
		marked, err := HasOnBootstrapMarker(ctx, opts.Session, opts.BootstrapHash)
		if err != nil {
			return err
		}
		if marked {
			return nil
		}
	}
	defaultWD := opts.DefaultWorkingDirectory
	if defaultWD == "" {
		var err error
		defaultWD, err = ResolveSandboxDefaultWorkingDirectory(ctx, opts.Session)
		if err != nil {
			return err
		}
	}
	bootstrapWorkDir := defaultWD
	if opts.WorkDir != "" {
		bootstrapWorkDir = posixpath.Join(defaultWD, opts.WorkDir)
	}
	if err := EnsureSandboxDirectory(ctx, opts.Session, bootstrapWorkDir); err != nil {
		return err
	}
	if err := opts.OnBootstrap(ctx, SandboxBootstrapContext{Session: opts.Session, WorkDir: bootstrapWorkDir}); err != nil {
		return err
	}
	if opts.BootstrapHash != "" {
		return WriteOnBootstrapMarker(ctx, opts.Session, opts.BootstrapHash)
	}
	return nil
}

// EnsureSandboxDirectory runs `mkdir -p "$WORK_DIR"`. Mirrors TS
// `ensureSandboxDirectory`.
func EnsureSandboxDirectory(ctx context.Context, session providerutils.SandboxSession, workDir string) error {
	result, err := session.Run(ctx, providerutils.SandboxProcessOptions{
		Command: sandboxWorkDirectoryCreateCommand,
		Env:     map[string]string{"WORK_DIR": workDir},
	})
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("Failed to create sandbox work directory %s (exit %d): %s", workDir, result.ExitCode, orString(result.Stderr, result.Stdout)) //nolint:staticcheck // matches TS SDK's exact error text
	}
	return nil
}

func hashSandboxBootstrapIdentity(recipeIdentity, bootstrapHash, workDir string) string {
	var h identityHasher
	h.push(strconv.Itoa(sandboxBootstrapIdentityVersion))
	h.push(recipeIdentity)
	h.push(bootstrapHash)
	h.push(workDir)
	return h.sum()
}
