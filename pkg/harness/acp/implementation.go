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

	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
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

// validateImplementation mirrors TS `validateACPV1Settings`'s source,
// executable and environment checks.
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
		// TS removed `npm-simple`. It keeps working here as an install-command
		// shim (see effectiveSource); the package spec is still validated.
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
	forwarded := toSet(impl.ForwardEnv)
	credential := toSet(impl.CredentialEnv)
	// JS iterates the Sets in configuration order; the error named must not
	// depend on Go's map order.
	for _, name := range uniqueInOrder(impl.ForwardEnv) {
		if err := validateEnvironmentVariableName(name); err != nil {
			return err
		}
	}
	for _, name := range uniqueInOrder(impl.CredentialEnv) {
		if err := validateEnvironmentVariableName(name); err != nil {
			return err
		}
	}
	for _, k := range uniqueInOrder(impl.CredentialEnv) {
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
		if err := validateEnvironmentVariableName(k); err != nil {
			return err
		}
		if strings.Contains(impl.Env[k], "\x00") {
			return fmt.Errorf("ACP runtime environment value for %s contains NUL.", k) //nolint:staticcheck // matches TS SDK's exact error text
		}
		if forwarded[k] {
			return fmt.Errorf("ACP runtime environment key %s cannot be configured in both forwardEnv and env.", jsonString(k)) //nolint:staticcheck // matches TS SDK's exact error text
		}
		if credential[k] {
			return fmt.Errorf("ACP runtime environment key %s cannot be configured in both credentialEnv and env.", jsonString(k)) //nolint:staticcheck // matches TS SDK's exact error text
		}
	}
	return nil
}

func validateEnvironmentVariableName(name string) error {
	if !envVarNameRegexp.MatchString(name) {
		return fmt.Errorf("ACP environment variable name is invalid: %s.", jsonString(name)) //nolint:staticcheck // matches TS SDK's exact error text
	}
	return nil
}

func uniqueInOrder(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func toSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, v := range values {
		out[v] = true
	}
	return out
}

// effectiveSource returns the source the bootstrap and identity are built
// from. The deprecated npm-simple source (removed from TS) is translated into
// an equivalent install command that installs the package into the agent's
// private home; every other source is returned unchanged.
func effectiveSource(src Source) Source {
	if src.Type != SourceNPMSimple {
		return src
	}
	version := src.PackageVersion
	if version == "" {
		version = "latest"
	}
	return Source{
		Type:    SourceInstallCommand,
		Command: "npm install --global --prefix \"$HOME/.local\" " + harnessutil.ShellQuote(src.PackageName+"@"+version),
	}
}

// agentLaunch is the launch command of the ACP agent inside the sandbox. Home
// is set for agents acquired with an install command, which run with a private
// HOME below the harness bootstrap directory. Mirrors TS `ACPAgentLaunch`.
type agentLaunch struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Home    string   `json:"home,omitempty"`
}

// resolveAgentLaunch mirrors TS `resolveACPAgentLaunch`.
func resolveAgentLaunch(bootstrapDirectory string, impl implementation) agentLaunch {
	args := append(make([]string, 0, len(impl.Args)), impl.Args...)
	if effectiveSource(impl.Source).Type == SourceInstallCommand {
		home := bootstrapDirectory + "/home"
		return agentLaunch{Command: home + "/.local/bin/" + impl.Executable, Args: args, Home: home}
	}
	return agentLaunch{Command: bootstrapDirectory + "/node_modules/.bin/" + impl.Executable, Args: args}
}

// createInstallScript mirrors TS `createInstallScript`: it runs an install
// command with HOME pointed at a private home below the bootstrap directory.
func createInstallScript(command string) string {
	return "#!/usr/bin/env bash\n" +
		"set -euo pipefail\n\n" +
		`ACP_BOOTSTRAP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"` + "\n" +
		`export HOME="$ACP_BOOTSTRAP_DIR/home"` + "\n" +
		`export PATH="$HOME/.local/bin:$PATH"` + "\n\n" +
		`mkdir -p "$HOME/.local/bin"` + "\n" +
		`cd "$HOME"` + "\n\n" +
		command + "\n"
}

// agentIdentityInput is the input of createAgentIdentity.
type agentIdentityInput struct {
	HarnessID              string
	Implementation         implementation
	ClientApp              ClientApp
	ClientCapabilities     map[string]any
	ModelMapping           ModelMapping
	ProviderAuthentication *providerAuthenticationCompatibility
	PermissionModeMapping  *PermissionModeMapping
	NonTextContentTypes    []NonTextContentType
}

// createAgentIdentity mirrors TS `createACPAgentIdentity`: a stable sha256
// digest over every non-secret input that determines how the ACP agent is
// installed, launched and configured. Lifecycle state produced under one
// identity cannot be resumed under another.
func createAgentIdentity(in agentIdentityInput) string {
	impl := in.Implementation
	src := effectiveSource(impl.Source)
	var sourceIdentity map[string]any
	switch src.Type {
	case SourceNPMLocked:
		sourceIdentity = map[string]any{
			"type": src.Type, "packageJson": src.PackageJSON, "pnpmLockYaml": src.PnpmLockYAML,
		}
		// An undefined workspace file is dropped by the canonical fingerprint.
		if src.PnpmWorkspaceYAML != "" {
			sourceIdentity["pnpmWorkspaceYaml"] = src.PnpmWorkspaceYAML
		}
	default:
		sourceIdentity = map[string]any{"type": src.Type, "command": src.Command}
	}
	args := impl.Args
	if args == nil {
		args = []string{}
	}
	literal := map[string]any{}
	for k, v := range impl.Env {
		literal[k] = v
	}
	names := make([]string, len(in.NonTextContentTypes))
	for i, t := range in.NonTextContentTypes {
		names[i] = string(t)
	}
	payload := map[string]any{
		"harnessId": in.HarnessID, "acpVersion": "v1", "source": sourceIdentity,
		"executable": impl.Executable, "args": args,
		"clientApp":          map[string]string{"name": in.ClientApp.Name, "version": in.ClientApp.Version},
		"clientCapabilities": in.ClientCapabilities,
		"modelMapping":       map[string]any{"type": in.ModelMapping.Type, "path": in.ModelMapping.Path},
		"environment": map[string]any{
			"forwarded": sortedUnique(impl.ForwardEnv), "credential": sortedUnique(impl.CredentialEnv), "literal": literal,
		},
		"providerAuthentication": in.ProviderAuthentication,
		"permissionModeMapping":  in.PermissionModeMapping,
		"nonTextContentTypes":    sortedUnique(names),
	}
	sum := sha256.Sum256([]byte(stableStringifyCodeUnit(payload)))
	return hex.EncodeToString(sum[:])
}

// resolveImplementationEnvironment mirrors TS
// `resolveACPAgentEnvironment`. Credential variables are read only from
// credentialEnv, never from env.
func resolveImplementationEnvironment(impl implementation, env, credentialEnv map[string]string) map[string]string {
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
