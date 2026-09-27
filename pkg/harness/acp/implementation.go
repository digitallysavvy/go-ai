package acp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	exactSemverRegexp    = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
	packageNameRegexp    = regexp.MustCompile(`^(?:@[a-z0-9][a-z0-9._-]*/)?[a-z0-9][a-z0-9._-]*$`)
	executableNameRegexp = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	envVarNameRegexp     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// implementation is the launch descriptor assembled from Settings, mirroring
// TS `ACPImplementation`.
type implementation struct {
	Source        Source
	Executable    string
	Args          []string
	ForwardEnv    []string
	CredentialEnv []string
	Env           map[string]string
}

func newImplementation(s Settings) implementation {
	return implementation{
		Source: s.Source, Executable: s.Executable, Args: s.Args,
		ForwardEnv: s.ForwardEnv, CredentialEnv: s.CredentialEnv, Env: s.Env,
	}
}

// validateImplementation mirrors TS `validateACPV1Implementation`.
func validateImplementation(impl implementation) error {
	switch impl.Source.Type {
	case SourceNPMLocked:
		if impl.Source.PackageJSON == "" {
			return fmt.Errorf("ACP source.packageJson must not be empty.")
		}
		if impl.Source.PnpmLockYAML == "" {
			return fmt.Errorf("ACP source.pnpmLockYaml must not be empty.")
		}
	case SourceNPMSimple:
		if !packageNameRegexp.MatchString(impl.Source.PackageName) {
			return fmt.Errorf("ACP npm package name is invalid: %q.", impl.Source.PackageName)
		}
		if impl.Source.PackageVersion != "" && !exactSemverRegexp.MatchString(impl.Source.PackageVersion) {
			return fmt.Errorf("ACP npm package version must be an exact semantic version; received %q.", impl.Source.PackageVersion)
		}
	default:
		if strings.TrimSpace(impl.Source.Command) == "" {
			return fmt.Errorf("ACP source.command must not be empty.")
		}
	}
	if !executableNameRegexp.MatchString(impl.Executable) {
		return fmt.Errorf("ACP executable must be a bare command name without a path; received %q.", impl.Executable)
	}
	if err := validateForwardEnvironment(impl.ForwardEnv); err != nil {
		return err
	}
	if err := validateForwardEnvironment(impl.CredentialEnv); err != nil {
		return err
	}
	if err := validateEnvironment(impl.Env); err != nil {
		return err
	}
	forwarded := toSet(impl.ForwardEnv)
	credential := toSet(impl.CredentialEnv)
	for k := range credential {
		if forwarded[k] {
			return fmt.Errorf("ACP runtime environment key %q cannot be configured in both forwardEnv and credentialEnv.", k)
		}
	}
	for k := range impl.Env {
		if forwarded[k] {
			return fmt.Errorf("ACP runtime environment key %q cannot be configured in both forwardEnv and env.", k)
		}
		if credential[k] {
			return fmt.Errorf("ACP runtime environment key %q cannot be configured in both credentialEnv and env.", k)
		}
	}
	return nil
}

func validateEnvironment(env map[string]string) error {
	for k, v := range env {
		if !envVarNameRegexp.MatchString(k) {
			return fmt.Errorf("ACP environment variable name is invalid: %q.", k)
		}
		if strings.Contains(v, "\x00") {
			return fmt.Errorf("ACP runtime environment value for %s contains NUL.", k)
		}
	}
	return nil
}

func validateForwardEnvironment(names []string) error {
	for _, name := range names {
		if !envVarNameRegexp.MatchString(name) {
			return fmt.Errorf("ACP environment variable name is invalid: %q.", name)
		}
	}
	return nil
}

func toSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, v := range values {
		out[v] = true
	}
	return out
}

// createImplementationManifest mirrors TS `createImplementationManifest`.
func createImplementationManifest(impl implementation) *string {
	switch impl.Source.Type {
	case SourceInstallCommand:
		return nil
	case SourceNPMLocked:
		return &impl.Source.PackageJSON
	default:
		version := impl.Source.PackageVersion
		if version == "" {
			version = "latest"
		}
		payload := map[string]any{
			"name": "harness-acp-implementation", "version": "0.0.0", "private": true, "type": "module",
			"dependencies": map[string]string{impl.Source.PackageName: version},
		}
		data, _ := json.MarshalIndent(payload, "", "  ")
		s := string(data) + "\n"
		return &s
	}
}

// getImplementationLockfile mirrors TS `getImplementationLockfile`.
func getImplementationLockfile(impl implementation) *string {
	if impl.Source.Type == SourceNPMLocked {
		return &impl.Source.PnpmLockYAML
	}
	return nil
}

// getImplementationWorkspaceFile mirrors TS `getImplementationWorkspaceFile`.
func getImplementationWorkspaceFile(impl implementation) *string {
	if impl.Source.Type == SourceNPMLocked && impl.Source.PnpmWorkspaceYAML != "" {
		return &impl.Source.PnpmWorkspaceYAML
	}
	return nil
}

// createImplementationDescriptor mirrors TS `createImplementationDescriptor`.
func createImplementationDescriptor(impl implementation) string {
	executablePath := "node_modules/.bin/" + impl.Executable
	if impl.Source.Type == SourceInstallCommand {
		executablePath = "home/.local/bin/" + impl.Executable
	}
	args := impl.Args
	if args == nil {
		args = []string{}
	}
	payload := map[string]any{
		"executablePath": executablePath,
		"privateHome":    impl.Source.Type == SourceInstallCommand,
		"args":           args,
		"envKeys":        implementationEnvironmentKeys(impl),
	}
	data, _ := json.MarshalIndent(payload, "", "  ")
	return string(data) + "\n"
}

// implementationIdentityInput is the input of createImplementationIdentity.
type implementationIdentityInput struct {
	HarnessID              string
	Implementation         implementation
	ClientApp              ClientApp
	ClientCapabilities     map[string]any
	ModelMapping           ModelMapping
	ProviderAuthentication *providerAuthenticationCompatibility
	PermissionModeMapping  *PermissionModeMapping
}

// createImplementationIdentity mirrors TS `createImplementationIdentity`: a
// stable sha256 digest over the non-secret launch configuration, used to
// detect an incompatible resume.
func createImplementationIdentity(in implementationIdentityInput) string {
	impl := in.Implementation
	var sourceIdentity map[string]any
	switch impl.Source.Type {
	case SourceNPMLocked:
		sourceIdentity = map[string]any{
			"type": impl.Source.Type, "packageJson": impl.Source.PackageJSON,
			"pnpmLockYaml": impl.Source.PnpmLockYAML, "pnpmWorkspaceYaml": impl.Source.PnpmWorkspaceYAML,
		}
	case SourceNPMSimple:
		sourceIdentity = map[string]any{"type": impl.Source.Type, "packageName": impl.Source.PackageName}
		if impl.Source.PackageVersion != "" {
			sourceIdentity["packageVersion"] = impl.Source.PackageVersion
		}
	default:
		sourceIdentity = map[string]any{"type": impl.Source.Type, "command": impl.Source.Command}
	}
	forwarded := sortedUnique(impl.ForwardEnv)
	credential := sortedUnique(impl.CredentialEnv)
	literal := map[string]any{}
	for k, v := range impl.Env {
		literal[k] = map[string]string{"value": v}
	}
	args := impl.Args
	if args == nil {
		args = []string{}
	}
	payload := map[string]any{
		"harnessId": in.HarnessID, "acpVersion": "v1", "source": sourceIdentity,
		"executable": impl.Executable, "args": args,
		"clientApp":          map[string]string{"name": in.ClientApp.Name, "version": in.ClientApp.Version},
		"clientCapabilities": in.ClientCapabilities,
		"modelMapping":       map[string]any{"type": in.ModelMapping.Type, "path": in.ModelMapping.Path},
		"environment": map[string]any{
			"forwarded": forwarded, "credential": credential, "literal": literal,
		},
		"providerAuthentication": in.ProviderAuthentication,
		"permissionModeMapping":  in.PermissionModeMapping,
	}
	sum := sha256.Sum256([]byte(stableStringify(payload)))
	return hex.EncodeToString(sum[:])
}

// createImplementationInstallCommand mirrors TS
// `createImplementationInstallCommand`.
func createImplementationInstallCommand(implementationDir, storeDir string, impl implementation) string {
	if impl.Source.Type == SourceInstallCommand {
		return "bash implementation/install.sh"
	}
	cmd := "pnpm --dir " + implementationDir + " install"
	if impl.Source.Type == SourceNPMLocked {
		cmd += " --frozen-lockfile"
	}
	cmd += " --prod --store-dir " + storeDir
	return cmd
}

// getImplementationInstallScript mirrors TS `getImplementationInstallScript`.
func getImplementationInstallScript(impl implementation) *string {
	if impl.Source.Type != SourceInstallCommand {
		return nil
	}
	script := "#!/usr/bin/env bash\n" +
		"set -euo pipefail\n\n" +
		`ACP_IMPLEMENTATION_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"` + "\n" +
		`ACP_INSTALL_HOME="$ACP_IMPLEMENTATION_DIR/home"` + "\n" +
		`export HOME="$ACP_INSTALL_HOME"` + "\n" +
		`export PATH="$ACP_INSTALL_HOME/.local/bin:$PATH"` + "\n\n" +
		`mkdir -p "$ACP_INSTALL_HOME/.local/bin"` + "\n" +
		`cd "$ACP_IMPLEMENTATION_DIR"` + "\n\n" +
		impl.Source.Command + "\n"
	return &script
}

// resolveImplementationEnvironment mirrors TS
// `resolveImplementationEnvironment`.
func resolveImplementationEnvironment(impl implementation, env, credentialEnv map[string]string) map[string]string {
	if credentialEnv == nil {
		credentialEnv = env
	}
	out := map[string]string{}
	for _, name := range impl.ForwardEnv {
		if v, ok := env[name]; ok && v != "" {
			out[name] = v
		}
	}
	for _, name := range impl.CredentialEnv {
		if v, ok := credentialEnv[name]; ok && v != "" {
			out[name] = v
		}
	}
	for k, v := range impl.Env {
		out[k] = v
	}
	return out
}

func implementationEnvironmentKeys(impl implementation) []string {
	set := map[string]bool{}
	var keys []string
	add := func(name string) {
		if !set[name] {
			set[name] = true
			keys = append(keys, name)
		}
	}
	for _, n := range impl.ForwardEnv {
		add(n)
	}
	for _, n := range impl.CredentialEnv {
		add(n)
	}
	envKeys := make([]string, 0, len(impl.Env))
	for k := range impl.Env {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	for _, n := range envKeys {
		add(n)
	}
	sort.Strings(keys)
	return keys
}

func sortedUnique(values []string) []string {
	set := toSet(values)
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// stableStringify mirrors TS `stableStringify`/`sortValue`: a deterministic
// JSON encoding used only as sha256 input for identity/fingerprint hashes
// computed and compared entirely on the Go host (never sent to or compared
// against a TS host), so byte-for-byte parity with TS's stringification is
// not required — only that the same Go value always encodes the same way.
// encoding/json already sorts map[string]T keys, which is sufuficient.
func stableStringify(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("acp: value is not JSON-serializable: %v", err))
	}
	return string(data)
}
