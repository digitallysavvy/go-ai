package harnessutil

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// SandboxCredentialPlaceholderPrefix prefixes generated placeholders.
const SandboxCredentialPlaceholderPrefix = "aisdkhc_"

var placeholderPattern = regexp.MustCompile(`^aisdkhc_[A-Za-z0-9_-]{43}$`)

// GenerateSandboxCredentialPlaceholder returns a random, recognizable
// placeholder ("aisdkhc_" + 32 random bytes base64url) used in place of real
// credentials inside the sandbox (0e590d2).
func GenerateSandboxCredentialPlaceholder() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("harnessutil: crypto/rand failed: %v", err))
	}
	return SandboxCredentialPlaceholderPrefix + base64.RawURLEncoding.EncodeToString(b)
}

// IsSandboxCredentialPlaceholder reports whether value is exactly a generated
// placeholder.
func IsSandboxCredentialPlaceholder(value string) bool {
	return placeholderPattern.MatchString(value)
}

func uniqueNames(names []string) []string {
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, n := range names {
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}

// CredentialForwardingOptions is the input of ApplyCredentialForwarding and
// CreateSandboxCredentialEnvironment.
type CredentialForwardingOptions struct {
	Environment                    map[string]string
	CredentialEnvironmentVariables []string
	CredentialForwarding           harness.CredentialForwarding
}

// ApplyCredentialForwarding returns a copy of Environment where each present
// credential variable is replaced by the CredentialForwarding result (8a15038).
// Without a callback the environment is returned unchanged. Callback errors
// propagate. Mirrors TS `applyCredentialForwarding`.
func ApplyCredentialForwarding(ctx context.Context, opts CredentialForwardingOptions) (map[string]string, error) {
	forwarded := make(map[string]string, len(opts.Environment))
	for k, v := range opts.Environment {
		forwarded[k] = v
	}
	if opts.CredentialForwarding == nil {
		return forwarded, nil
	}
	for _, name := range uniqueNames(opts.CredentialEnvironmentVariables) {
		credential, ok := forwarded[name]
		if !ok {
			continue
		}
		value, err := opts.CredentialForwarding(ctx, harness.CredentialForwardingOptions{
			Credential:              credential,
			EnvironmentVariableName: name,
		})
		if err != nil {
			return nil, err
		}
		forwarded[name] = value
	}
	return forwarded, nil
}

// CreateSandboxCredentialEnvironment returns, for each present credential
// variable, a fresh placeholder (passed through CredentialForwarding when set)
// for use inside the sandbox while the real value is injected by a request
// transformation. Mirrors TS `createSandboxCredentialEnvironment`.
func CreateSandboxCredentialEnvironment(ctx context.Context, opts CredentialForwardingOptions) (map[string]string, error) {
	out := map[string]string{}
	for _, name := range uniqueNames(opts.CredentialEnvironmentVariables) {
		if _, ok := opts.Environment[name]; !ok {
			continue
		}
		placeholder := GenerateSandboxCredentialPlaceholder()
		if opts.CredentialForwarding == nil {
			out[name] = placeholder
			continue
		}
		value, err := opts.CredentialForwarding(ctx, harness.CredentialForwardingOptions{
			Credential:              placeholder,
			EnvironmentVariableName: name,
		})
		if err != nil {
			return nil, err
		}
		out[name] = value
	}
	return out, nil
}

// CredentialBrokeringUnavailableWarning is the warning text.
const CredentialBrokeringUnavailableWarning = "The sandbox implementation does not support configuring request transformations, so credential brokering does not work. Falling back to less secure credential forwarding."

// Warn receives harness warnings (TS console.warn). Replaceable for tests or
// custom logging.
var Warn = func(message string) { log.Println(message) }

// WarnCredentialBrokeringUnavailableOptions is the input of
// WarnCredentialBrokeringUnavailable.
type WarnCredentialBrokeringUnavailableOptions struct {
	Environment                    map[string]string
	ForwardedEnvironment           map[string]string
	CredentialEnvironmentVariables []string
}

// WarnCredentialBrokeringUnavailable warns when brokering is unavailable, but
// only if a real credential still appears inside a forwarded value (0c37bf0).
// It reports whether the warning was emitted. Mirrors TS
// `warnCredentialBrokeringUnavailable`.
func WarnCredentialBrokeringUnavailable(opts WarnCredentialBrokeringUnavailableOptions) bool {
	names := uniqueNames(opts.CredentialEnvironmentVariables)
	for _, name := range names {
		credential := opts.Environment[name]
		if credential == "" {
			continue
		}
		for _, forwardedName := range names {
			forwarded, ok := opts.ForwardedEnvironment[forwardedName]
			if ok && strings.Contains(forwarded, credential) {
				Warn(CredentialBrokeringUnavailableWarning)
				return true
			}
		}
	}
	return false
}

// MaskSandboxCredentials replaces each present credential value with its own
// variable name. Mirrors TS `maskSandboxCredentials`.
func MaskSandboxCredentials(environment map[string]string, credentialEnvironmentVariables []string) map[string]string {
	masked := make(map[string]string, len(environment))
	for k, v := range environment {
		masked[k] = v
	}
	for _, name := range credentialEnvironmentVariables {
		if _, ok := masked[name]; ok {
			masked[name] = name
		}
	}
	return masked
}

// CreateCredentialRequestTransformationOptions is the input of
// CreateCredentialRequestTransformation.
type CreateCredentialRequestTransformationOptions struct {
	MatchURL         string
	MatchHeaders     map[string]string
	TransformHeaders map[string]string
}

// CreateCredentialRequestTransformation maps a base URL plus the sandbox-side
// header values to a request transformation injecting the real headers
// (69bb613). Host excludes the port; a root path is omitted. Header matchers
// are sorted by header name for deterministic output. Mirrors TS
// `createCredentialRequestTransformation`.
func CreateCredentialRequestTransformation(opts CreateCredentialRequestTransformationOptions) (harness.RequestTransformation, error) {
	u, err := url.Parse(opts.MatchURL)
	if err != nil || u.Host == "" {
		return harness.RequestTransformation{}, fmt.Errorf("Invalid URL: %s", opts.MatchURL) //nolint:staticcheck // matches TS SDK's exact error text
	}
	pathname := strings.TrimRight(u.EscapedPath(), "/")
	match := harness.RequestTransformationMatch{
		Host:    strings.ToLower(u.Hostname()),
		Headers: []harness.KeyValueMatcher{},
	}
	if pathname != "" {
		match.Path = &harness.StringMatcher{StartsWith: pathname}
	}
	keys := make([]string, 0, len(opts.MatchHeaders))
	for k := range opts.MatchHeaders {
		keys = append(keys, k)
	}
	sortStrings(keys)
	for _, k := range keys {
		match.Headers = append(match.Headers, harness.KeyValueMatcher{
			Key:   &harness.StringMatcher{Exact: k},
			Value: &harness.StringMatcher{Exact: opts.MatchHeaders[k]},
		})
	}
	transform := map[string]string{}
	for k, v := range opts.TransformHeaders {
		transform[k] = v
	}
	return harness.RequestTransformation{
		Match:     match,
		Transform: harness.RequestTransformationTransform{Headers: transform},
	}, nil
}
