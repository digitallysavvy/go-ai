package harness

import (
	"context"
	"errors"
	"strconv"

	"github.com/digitallysavvy/go-ai/pkg/harness/internal/posixpath"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// PrepareSandboxForHarnessOptions is the input of PrepareSandboxForHarness.
type PrepareSandboxForHarnessOptions struct {
	Session   providerutils.SandboxSession
	Harnesses []Harness
	// SandboxConfig is optional; OnSession is ignored.
	SandboxConfig *SandboxConfig
}

// PrepareSandboxForHarnessResult mirrors TS `PrepareSandboxForHarnessResult`.
type PrepareSandboxForHarnessResult struct {
	// Identity is empty when no recipe was applied and no bootstrapHash set.
	Identity          string            `json:"identity,omitempty"`
	RecipeIdentities  map[string]string `json:"recipeIdentities"`
	SkippedHarnessIDs []string          `json:"skippedHarnessIds"`
}

// PrepareSandboxForHarness applies one or more harness bootstrap recipes to an
// existing (caller-owned) sandbox and returns a deterministic identity usable
// as a template/snapshot cache key. Repeated harness IDs are prepared once
// (the last adapter wins); harnesses are processed sorted by id. Mirrors TS
// `prepareSandboxForHarness` (51d10a0, 4cd4989).
func PrepareSandboxForHarness(ctx context.Context, opts PrepareSandboxForHarnessOptions) (*PrepareSandboxForHarnessResult, error) {
	cfg := SandboxConfig{}
	if opts.SandboxConfig != nil {
		cfg = *opts.SandboxConfig
	}
	if err := ValidateSandboxBootstrapSettings(cfg); err != nil {
		return nil, err
	}
	if len(opts.Harnesses) == 0 {
		return nil, errors.New("prepareSandboxForHarness: at least one harness must be provided.")
	}

	// Dedupe by id (last wins, keeping first-seen insertion order like a JS
	// Map), then sort by id with localeCompare.
	byID := map[string]Harness{}
	order := make([]string, 0, len(opts.Harnesses))
	for _, h := range opts.Harnesses {
		id := h.HarnessID()
		if _, seen := byID[id]; !seen {
			order = append(order, id)
		}
		byID[id] = h
	}
	posixpath.SortStrings(order)

	workDir := ""
	if cfg.WorkDir != "" {
		wd, err := NormalizeSandboxWorkDir(cfg.WorkDir)
		if err != nil {
			return nil, err
		}
		workDir = wd
	}

	result := &PrepareSandboxForHarnessResult{
		RecipeIdentities:  map[string]string{},
		SkippedHarnessIDs: []string{},
	}
	stateDirectory := ""
	for _, id := range order {
		harness := byID[id]
		recipe, err := GetBootstrap(ctx, harness)
		if err != nil {
			return nil, err
		}
		if recipe == nil {
			result.SkippedHarnessIDs = append(result.SkippedHarnessIDs, id)
			continue
		}
		recipeIdentity := HashHarnessBootstrap(*recipe)
		result.RecipeIdentities[id] = recipeIdentity
		if stateDirectory == "" {
			home, err := ResolveSandboxHomeDir(ctx, opts.Session)
			if err != nil {
				return nil, err
			}
			stateDirectory = StateDirectoryPath(home)
		}
		if err := ApplyBootstrapRecipe(ctx, opts.Session, *recipe, recipeIdentity, stateDirectory); err != nil {
			return nil, err
		}
	}

	if cfg.OnBootstrap != nil {
		defaultWD, err := ResolveSandboxDefaultWorkingDirectory(ctx, opts.Session)
		if err != nil {
			return nil, err
		}
		if err := RunSandboxBootstrap(ctx, RunSandboxBootstrapOptions{
			Session:                 opts.Session,
			WorkDir:                 workDir,
			OnBootstrap:             cfg.OnBootstrap,
			DefaultWorkingDirectory: defaultWD,
		}); err != nil {
			return nil, err
		}
	}

	result.Identity = resolvePreparedSandboxIdentity(result.RecipeIdentities, cfg.BootstrapHash, workDir)
	return result, nil
}

func resolvePreparedSandboxIdentity(recipeIdentities map[string]string, bootstrapHash, workDir string) string {
	if len(recipeIdentities) == 0 && bootstrapHash == "" {
		return ""
	}
	ids := make([]string, 0, len(recipeIdentities))
	for id := range recipeIdentities {
		ids = append(ids, id)
	}
	posixpath.SortStrings(ids)

	var h identityHasher
	h.push(strconv.Itoa(preparedSandboxIdentityVersion))
	h.push(workDir)
	h.push(bootstrapHash)
	for _, id := range ids {
		h.push(id)
		h.push(recipeIdentities[id])
	}
	return h.sum()
}

// PrepareHarnessSandboxTemplateOptions is the input of
// PrepareHarnessSandboxTemplate.
type PrepareHarnessSandboxTemplateOptions struct {
	Harness         Harness
	SandboxProvider SandboxProvider
	// SandboxConfig is optional; OnSession is ignored.
	SandboxConfig *SandboxConfig
}

// PrepareHarnessSandboxTemplate prepares a harness's sandbox template without
// running an agent. Idempotent; a no-op for adapters without a bootstrap
// recipe and no caller bootstrap. The temporary sandbox session is stopped
// before returning. Mirrors TS `prepareHarnessSandboxTemplate`.
func PrepareHarnessSandboxTemplate(ctx context.Context, opts PrepareHarnessSandboxTemplateOptions) error {
	cfg := SandboxConfig{}
	if opts.SandboxConfig != nil {
		cfg = *opts.SandboxConfig
	}
	if err := ValidateSandboxBootstrapSettings(cfg); err != nil {
		return err
	}
	recipe, err := GetBootstrap(ctx, opts.Harness)
	if err != nil {
		return err
	}
	plan, err := CreateSandboxBootstrapPlan(recipe, cfg)
	if err != nil {
		return err
	}
	if plan.Identity == "" || plan.OnFirstCreate == nil {
		return nil
	}

	session, err := opts.SandboxProvider.CreateSession(ctx, CreateSandboxSessionOptions{
		Identity:      plan.Identity,
		OnFirstCreate: plan.OnFirstCreate,
	})
	if err != nil {
		return err
	}
	defer func() {
		// Mirrors `Promise.resolve(sandboxSession.stop()).catch(() => {})`.
		_ = session.Stop(context.WithoutCancel(ctx))
	}()

	if plan.Recipe != nil && plan.RecipeIdentity != "" {
		restricted := session.Restricted()
		home, err := ResolveSandboxHomeDir(ctx, restricted)
		if err != nil {
			return err
		}
		return ApplyBootstrapRecipe(ctx, restricted, *plan.Recipe, plan.RecipeIdentity, StateDirectoryPath(home))
	}
	return nil
}

// PrewarmHarness is the deprecated alias of PrepareHarnessSandboxTemplate.
//
// Deprecated: use PrepareHarnessSandboxTemplate.
func PrewarmHarness(ctx context.Context, opts PrepareHarnessSandboxTemplateOptions) error {
	return PrepareHarnessSandboxTemplate(ctx, opts)
}
