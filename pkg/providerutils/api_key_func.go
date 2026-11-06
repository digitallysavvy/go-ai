package providerutils

import (
	"context"
	"strings"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// APIKeyFunc resolves a provider credential per request: the Go form of a TS
// `apiKey?: Resolvable<string>` given as a (possibly async) function
// (TS 7716e189ee). It is called before each request with that request's
// context, so it can look the key up, refresh it or rotate it. A returned
// error fails the request unchanged.
//
// Provider config fields of this type carry `json:"-"`: a func does not
// serialize, while a static APIKey stays serializable (TS keeps synchronous
// credentials serializable across workflow boundaries).
//
// The same type fits other per-request credentials, such as an Anthropic
// auth token (TS 63bf860638 `authToken?: Resolvable<string>`).
type APIKeyFunc func(ctx context.Context) (string, error)

// ResolveAPIKeyOptions describes one provider credential for ResolveAPIKey.
type ResolveAPIKeyOptions struct {
	// APIKey is the static credential (the TS string form).
	APIKey string
	// APIKeyFunc is the per-request credential (the TS function form).
	APIKeyFunc APIKeyFunc
	// EnvVar is the environment variable read when neither APIKey nor
	// APIKeyFunc is set (TS loadApiKey environmentVariableName). Empty means
	// no environment fallback.
	EnvVar string
	// Description names the provider in errors, e.g. "OpenAI" (TS loadApiKey
	// description).
	Description string
	// ParameterName names the credential in the both-set error; it defaults
	// to "apiKey", giving "apiKey/apiKeyFunc".
	ParameterName string
	// Optional returns ("", nil) instead of a *providererrors.LoadAPIKeyError
	// when no credential is found (TS loadOptionalSetting, as the gateway
	// uses before its OIDC fallback).
	Optional bool
	// RequireNonEmpty rejects an APIKeyFunc result that is empty after a
	// JavaScript trim with "<Description> API key must be a non-empty
	// string." (TS amazon-bedrock, anthropic-aws, gateway and Vertex express
	// mode). Without it an empty result is sent as is, as TS loadApiKey
	// returns any string unchanged.
	RequireNonEmpty bool
}

// ResolveAPIKey resolves a provider credential for one request, with the TS
// 7716e189ee precedence:
//
//  1. Setting both APIKey and APIKeyFunc is an *providererrors.InvalidArgumentError
//     ("Both apiKey and apiKeyFunc were provided. Please use only one."). TS
//     has a single field, so the two can never both be set there; Go rejects
//     the ambiguity the way TS rejects apiKey plus authToken.
//  2. APIKeyFunc, when set, is called with ctx; its error is returned
//     unchanged, and its value is returned as is (an empty string included,
//     as TS sends `Bearer ` for an empty resolved key) unless
//     RequireNonEmpty is set. The environment is not read.
//  3. Otherwise APIKey, else the EnvVar environment variable (read per
//     request), else a *providererrors.LoadAPIKeyError (or "" with
//     Optional).
func ResolveAPIKey(ctx context.Context, opts ResolveAPIKeyOptions) (string, error) {
	if opts.APIKey != "" && opts.APIKeyFunc != nil {
		name := opts.ParameterName
		if name == "" {
			name = "apiKey"
		}
		return "", &providererrors.InvalidArgumentError{
			Field:   name + "/" + name + "Func",
			Message: "Both " + name + " and " + name + "Func were provided. Please use only one.",
		}
	}

	if opts.APIKeyFunc != nil {
		if ctx == nil {
			ctx = context.Background()
		}
		key, err := opts.APIKeyFunc(ctx)
		if err != nil {
			return "", err
		}
		if opts.RequireNonEmpty && JSTrim(key) == "" {
			return "", &providererrors.LoadAPIKeyError{Message: opts.Description + " API key must be a non-empty string."}
		}
		return key, nil
	}

	key := opts.APIKey
	if key == "" && opts.EnvVar != "" {
		key = APIKeyOrEnv("", opts.EnvVar)
	}
	if key == "" && !opts.Optional {
		return "", providererrors.NewLoadAPIKeyError(opts.Description, opts.EnvVar)
	}
	return key, nil
}

// RequestAuthHeaderFunc is RequestAuthHeader for a credential that may come
// from an APIKeyFunc: it resolves the key with ResolveAPIKey for this request
// and returns {header: prefix + key}. A caller that supplies the credential
// header itself through userHeaders (case-insensitive, or any name in
// alsoHeaders) keeps its own credential and gets (nil, nil) without the func
// being called.
func RequestAuthHeaderFunc(ctx context.Context, userHeaders map[string]string, opts ResolveAPIKeyOptions, header, prefix string, alsoHeaders ...string) (map[string]string, error) {
	for k := range userHeaders {
		if strings.EqualFold(k, header) {
			return nil, nil
		}
		for _, h := range alsoHeaders {
			if strings.EqualFold(k, h) {
				return nil, nil
			}
		}
	}
	key, err := ResolveAPIKey(ctx, opts)
	if err != nil {
		return nil, err
	}
	return map[string]string{header: prefix + key}, nil
}
