package harness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/harness/internal/posixpath"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// OnBootstrapMarkerPath returns the absolute path of the completion marker
// for a caller's sandboxConfig.OnBootstrap hook, keyed by bootstrapHash (its
// SHA-256 hex digest as the filename, under
// `<harnessStateDir>/.on-bootstrap/`). Distinct from BootstrapMarkerPath,
// which guards a specific adapter's own recipe. Mirrors TS
// `onBootstrapMarkerPath` (31742b9a1b).
func OnBootstrapMarkerPath(ctx context.Context, session providerutils.SandboxSession, bootstrapHash string) (string, error) {
	home, err := ResolveSandboxHomeDir(ctx, session)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(bootstrapHash))
	return posixpath.Join(StateDirectoryPath(home), ".on-bootstrap", hex.EncodeToString(digest[:])+".ok"), nil
}

// HasOnBootstrapMarker reports whether OnBootstrap has already completed for
// bootstrapHash on this sandbox. Mirrors TS `hasOnBootstrapMarker`.
func HasOnBootstrapMarker(ctx context.Context, session providerutils.SandboxSession, bootstrapHash string) (bool, error) {
	path, err := OnBootstrapMarkerPath(ctx, session, bootstrapHash)
	if err != nil {
		return false, err
	}
	existing, err := session.ReadTextFile(ctx, providerutils.SandboxReadTextFileOptions{Path: path})
	if err != nil {
		return false, err
	}
	return existing != nil, nil
}

// WriteOnBootstrapMarker records that OnBootstrap has completed for
// bootstrapHash on this sandbox. Mirrors TS `writeOnBootstrapMarker`.
func WriteOnBootstrapMarker(ctx context.Context, session providerutils.SandboxSession, bootstrapHash string) error {
	path, err := OnBootstrapMarkerPath(ctx, session, bootstrapHash)
	if err != nil {
		return err
	}
	dir := posixpath.Dirname(path)
	result, err := session.Run(ctx, providerutils.SandboxProcessOptions{
		Command: `mkdir -p "$MARKER_DIR"`,
		Env:     map[string]string{"MARKER_DIR": dir},
	})
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("Failed to create onBootstrap marker directory: %s", orString(result.Stderr, result.Stdout)) //nolint:staticcheck // matches TS SDK's exact error text
	}
	return session.WriteTextFile(ctx, providerutils.SandboxWriteTextFileOptions{Path: path, Content: ""})
}
