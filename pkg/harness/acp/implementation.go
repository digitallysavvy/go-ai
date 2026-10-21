package acp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/digitallysavvy/go-ai/pkg/harness/internal/jsstr"
	"github.com/digitallysavvy/go-ai/pkg/harness/internal/posixpath"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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
			return fmt.Errorf("ACP source.packageJson must not be empty.") //nolint:staticcheck // matches TS SDK's exact error text
		}
		if impl.Source.PnpmLockYAML == "" {
			return fmt.Errorf("ACP source.pnpmLockYaml must not be empty.") //nolint:staticcheck // matches TS SDK's exact error text
		}
		if impl.Source.PnpmWorkspaceYAMLSet && impl.Source.PnpmWorkspaceYAML == "" {
			return fmt.Errorf("ACP source.pnpmWorkspaceYaml must not be empty.") //nolint:staticcheck // matches TS SDK's exact error text
		}
	case SourceNPMSimple:
		if !packageNameRegexp.MatchString(impl.Source.PackageName) {
			return fmt.Errorf("ACP npm package name is invalid: %s.", jsonString(impl.Source.PackageName)) //nolint:staticcheck // matches TS SDK's exact error text
		}
		if impl.Source.PackageVersion != "" && !exactSemverRegexp.MatchString(impl.Source.PackageVersion) {
			return fmt.Errorf("ACP npm package version must be an exact semantic version; received %s.", jsonString(impl.Source.PackageVersion)) //nolint:staticcheck // matches TS SDK's exact error text
		}
	default:
		if providerutils.JSTrim(impl.Source.Command) == "" {
			return fmt.Errorf("ACP source.command must not be empty.") //nolint:staticcheck // matches TS SDK's exact error text
		}
	}
	if !executableNameRegexp.MatchString(impl.Executable) {
		return fmt.Errorf("ACP executable must be a bare command name without a path; received %s.", jsonString(impl.Executable)) //nolint:staticcheck // matches TS SDK's exact error text
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
	// JS iterates the credential Set in configuration order; the conflict named
	// by the error must not depend on Go's map order.
	for _, k := range impl.CredentialEnv {
		if forwarded[k] {
			return fmt.Errorf("ACP runtime environment key %s cannot be configured in both forwardEnv and credentialEnv.", jsonString(k)) //nolint:staticcheck // matches TS SDK's exact error text
		}
	}
	envKeys := make([]string, 0, len(impl.Env))
	for k := range impl.Env {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	for _, k := range envKeys {
		if forwarded[k] {
			return fmt.Errorf("ACP runtime environment key %s cannot be configured in both forwardEnv and env.", jsonString(k)) //nolint:staticcheck // matches TS SDK's exact error text
		}
		if credential[k] {
			return fmt.Errorf("ACP runtime environment key %s cannot be configured in both credentialEnv and env.", jsonString(k)) //nolint:staticcheck // matches TS SDK's exact error text
		}
	}
	return nil
}

func validateEnvironment(env map[string]string) error {
	// Go maps have no insertion order; sort so the reported key is stable.
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := env[k]
		if !envVarNameRegexp.MatchString(k) {
			return fmt.Errorf("ACP environment variable name is invalid: %s.", jsonString(k)) //nolint:staticcheck // matches TS SDK's exact error text
		}
		if strings.Contains(v, "\x00") {
			return fmt.Errorf("ACP runtime environment value for %s contains NUL.", k) //nolint:staticcheck // matches TS SDK's exact error text
		}
	}
	return nil
}

func validateForwardEnvironment(names []string) error {
	for _, name := range names {
		if !envVarNameRegexp.MatchString(name) {
			return fmt.Errorf("ACP environment variable name is invalid: %s.", jsonString(name)) //nolint:staticcheck // matches TS SDK's exact error text
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
		payload := struct {
			Name         string            `json:"name"`
			Version      string            `json:"version"`
			Private      bool              `json:"private"`
			Type         string            `json:"type"`
			Dependencies map[string]string `json:"dependencies"`
		}{"harness-acp-implementation", "0.0.0", true, "module", map[string]string{impl.Source.PackageName: version}}
		s := marshalIndentNoEscape(payload) + "\n"
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
	payload := struct {
		ExecutablePath string   `json:"executablePath"`
		PrivateHome    bool     `json:"privateHome"`
		Args           []string `json:"args"`
		EnvKeys        []string `json:"envKeys"`
	}{executablePath, impl.Source.Type == SourceInstallCommand, args, implementationEnvironmentKeys(impl)}
	data := marshalIndentNoEscape(payload)
	return data + "\n"
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
	NonTextContentTypes    []NonTextContentType
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
			"pnpmLockYaml": impl.Source.PnpmLockYAML,
		}
		// JSON.stringify drops an undefined workspace file.
		if impl.Source.PnpmWorkspaceYAML != "" {
			sourceIdentity["pnpmWorkspaceYaml"] = impl.Source.PnpmWorkspaceYAML
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
	if len(in.NonTextContentTypes) > 0 {
		// Only present when declared, so identities of implementations that
		// declare none are unchanged. Deduplicated and sorted like TS.
		names := make([]string, len(in.NonTextContentTypes))
		for i, t := range in.NonTextContentTypes {
			names[i] = string(t)
		}
		payload["nonTextContentTypes"] = sortedUnique(names)
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

// stableStringify mirrors TS `stableStringify`/`sortValue`: JSON.stringify of
// the value with every object's keys sorted by `String.prototype.localeCompare`
// (ICU root collation, not byte order: "A_B" sorts before "AB" and "a" before
// "B"). The digests built from it are compared across hosts, so the output must
// match TS byte for byte.
func stableStringify(value any) string {
	return stableStringifySorted(value, posixpath.SortStrings)
}

// stableStringifyCodeUnit is stableStringify with keys ordered by UTF-16 code
// units (`left < right`), which TS uses for the turn start configuration
// fingerprint and the authentication profile digest (only the implementation
// identity uses localeCompare).
func stableStringifyCodeUnit(value any) string {
	return stableStringifySorted(value, func(keys []string) {
		sort.SliceStable(keys, func(i, j int) bool {
			return slices.Compare(utf16.Encode([]rune(keys[i])), utf16.Encode([]rune(keys[j]))) < 0
		})
	})
}

func stableStringifySorted(value any, sortKeys func([]string)) string {
	raw, err := encodeNoEscape(value)
	if err != nil {
		panic(fmt.Sprintf("acp: value is not JSON-serializable: %v", err))
	}
	// Round-trip through a generic value so struct fields become sortable
	// object keys at every level.
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		panic(fmt.Sprintf("acp: value is not JSON-serializable: %v", err))
	}
	var out strings.Builder
	writeSortedJSON(&out, generic, sortKeys)
	return out.String()
}

func writeSortedJSON(out *strings.Builder, value any, sortKeys func([]string)) {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sortKeys(keys)
		// Object.fromEntries hoists integer-like keys, ascending, before the rest.
		keys = jsonorder.JSKeyOrder(keys)
		out.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			out.WriteString(jsonString(k))
			out.WriteByte(':')
			writeSortedJSON(out, v[k], sortKeys)
		}
		out.WriteByte('}')
	case []any:
		out.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				out.WriteByte(',')
			}
			writeSortedJSON(out, item, sortKeys)
		}
		out.WriteByte(']')
	case string:
		out.WriteString(jsonString(v))
	default:
		encoded, err := encodeNoEscape(v)
		if err != nil {
			panic(fmt.Sprintf("acp: value is not JSON-serializable: %v", err))
		}
		out.WriteString(encoded)
	}
}

// jsonString is JSON.stringify for a string: no HTML escaping and U+2028 /
// U+2029 left literal (jsstr handles both).
func jsonString(s string) string {
	encoded, _ := encodeNoEscape(s)
	return encoded
}

// encodeNoEscape is json.Marshal without HTML escaping (`<`, `>`, `&` stay
// literal, like JSON.stringify), so identity digests can match TS byte for byte.
func encodeNoEscape(value any) (string, error) {
	b, err := jsstr.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func marshalIndentNoEscape(value any) string {
	b, _ := jsstr.MarshalIndent(value)
	return string(b)
}
