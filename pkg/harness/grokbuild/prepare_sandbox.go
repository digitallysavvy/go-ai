package grokbuild

import (
	"context"
	"errors"
	"path"
	"time"

	"github.com/google/uuid"
	"github.com/pelletier/go-toml/v2"

	"github.com/digitallysavvy/go-ai/pkg/harness/acp"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// PrepareSandbox adds a native `Read` ask rule to the sandbox's Grok Build
// configuration (`$GROK_HOME/config.toml`, else `~/.grok/config.toml`).
// Grok Build executes reads without requesting ACP permission by default;
// the ask rule routes them through ACP so its permission controller can
// approve reads inside the session workspace and ask about outside reads.
// Mirrors TS `prepareGrokBuildSandbox` (deeb2bfb62).
//
// The configuration is rewritten through a private temporary file
// (`umask 077; set -C`) that is then moved over the original, never in
// place; the temporary file is always deleted afterwards. The rewritten
// TOML is formatted by go-toml (TS uses @iarna/toml); comments are lost in
// both.
func PrepareSandbox(ctx context.Context, opts acp.PrepareSandboxOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sandbox := opts.Sandbox
	home, err := sandbox.Run(ctx, providerutils.SandboxProcessOptions{
		Command:          `printf "%s" "${GROK_HOME:-}"`,
		WorkingDirectory: opts.WorkDir,
	})
	if err != nil {
		return err
	}
	if home.ExitCode != 0 {
		return errors.New("Unable to resolve the Grok Build configuration directory.") //nolint:staticcheck // TS error text
	}
	configDirectory := home.Stdout
	if configDirectory == "" {
		homeDir, err := harnessutil.ResolveSandboxHomeDir(ctx, sandbox)
		if err != nil {
			return err
		}
		configDirectory = path.Join(homeDir, ".grok")
	}
	configDir := posixResolve(opts.WorkDir, configDirectory)
	configPath := path.Join(configDir, "config.toml")

	content, err := sandbox.ReadTextFile(ctx, providerutils.SandboxReadTextFileOptions{Path: configPath})
	if err != nil {
		return err
	}
	text := ""
	if content != nil {
		text = *content
	}
	config := map[string]any{}
	if err := toml.Unmarshal([]byte(text), &config); err != nil {
		return errors.New("Grok Build configuration must contain valid TOML.") //nolint:staticcheck // TS error text
	}
	if config == nil {
		config = map[string]any{}
	}
	if _, ok := config["permission"]; !ok {
		config["permission"] = map[string]any{}
	}
	// A TOML array of tables and every TOML date/time value (JS Date) are
	// rejected like TS's `Array.isArray` / `instanceof Date` checks.
	permission, ok := config["permission"].(map[string]any)
	if !ok {
		return errors.New("Grok Build configuration permission must be a table.") //nolint:staticcheck // TS error text
	}
	var ask []any
	if raw, present := permission["ask"]; present {
		list, isList := raw.([]any)
		if !isList {
			return errors.New("Grok Build configuration permission.ask must be a string array.") //nolint:staticcheck // TS error text
		}
		for _, rule := range list {
			if _, isString := rule.(string); !isString {
				return errors.New("Grok Build configuration permission.ask must be a string array.") //nolint:staticcheck // TS error text
			}
		}
		ask = list
	}
	for _, rule := range ask {
		if rule == "Read" {
			return nil
		}
	}
	permission["ask"] = append(append([]any{}, ask...), "Read")
	updated, err := toml.Marshal(config)
	if err != nil {
		return err
	}

	temporaryPath := configPath + "." + uuid.NewString() + ".tmp"
	created, err := sandbox.Run(ctx, providerutils.SandboxProcessOptions{
		Command: "mkdir -p " + harnessutil.ShellQuote(configDir) + " && " +
			"(umask 077; set -C; : > " + harnessutil.ShellQuote(temporaryPath) + ")",
	})
	if err != nil {
		return err
	}
	if created.ExitCode != 0 {
		return errors.New("Unable to create the temporary Grok Build configuration.") //nolint:staticcheck // TS error text
	}
	defer deleteTemporaryFile(ctx, sandbox, temporaryPath)

	if err := sandbox.WriteTextFile(ctx, providerutils.SandboxWriteTextFileOptions{Path: temporaryPath, Content: string(updated)}); err != nil {
		return err
	}
	replaced, err := sandbox.Run(ctx, providerutils.SandboxProcessOptions{
		Command: "chmod 600 " + harnessutil.ShellQuote(temporaryPath) + " && " +
			"mv -f " + harnessutil.ShellQuote(temporaryPath) + " " + harnessutil.ShellQuote(configPath),
	})
	if err != nil {
		return err
	}
	if replaced.ExitCode != 0 {
		return errors.New("Unable to replace the Grok Build configuration.") //nolint:staticcheck // TS error text
	}
	return nil
}

// deleteTemporaryFile removes the temporary configuration and ignores the
// result, like TS's `deleteFile(...).catch(() => {})`. TS calls it without
// the abort signal, so it runs even after cancellation. A sandbox without
// the optional DeleteFile extension removes it with `rm -f`.
func deleteTemporaryFile(ctx context.Context, sandbox providerutils.SandboxSession, temporaryPath string) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if deleter, ok := sandbox.(providerutils.SandboxFileDeleter); ok {
		_, _ = deleter.DeleteFile(cleanupCtx, temporaryPath)
		return
	}
	_, _ = sandbox.Run(cleanupCtx, providerutils.SandboxProcessOptions{Command: "rm -f " + harnessutil.ShellQuote(temporaryPath)})
}

// posixResolve is Node's `path.posix.resolve(base, p)` for an absolute base.
func posixResolve(base, p string) string {
	if path.IsAbs(p) {
		return path.Clean(p)
	}
	return path.Join(base, p)
}
