package providerutils

import (
	"context"
	"errors"
	"reflect"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS 7716e189ee: credentials may be functions resolved per request.

type r85prepCtxKey struct{}

func TestResolveAPIKeyFuncIsCalledPerRequestWithCtx(t *testing.T) {
	t.Setenv("R85PREP_KEY", "env-key")
	calls := 0
	opts := ResolveAPIKeyOptions{
		APIKeyFunc: func(ctx context.Context) (string, error) {
			calls++
			return ctx.Value(r85prepCtxKey{}).(string), nil
		},
		EnvVar: "R85PREP_KEY", Description: "Test",
	}
	for _, want := range []string{"first", "second"} {
		got, err := ResolveAPIKey(context.WithValue(context.Background(), r85prepCtxKey{}, want), opts)
		if err != nil || got != want {
			t.Fatalf("got (%q, %v), want %q", got, err, want)
		}
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestResolveAPIKeyFuncEmptyIsSentAsIs(t *testing.T) {
	t.Setenv("R85PREP_KEY", "env-key")
	got, err := ResolveAPIKey(context.Background(), ResolveAPIKeyOptions{
		APIKeyFunc: func(context.Context) (string, error) { return "", nil },
		EnvVar:     "R85PREP_KEY", Description: "Test",
	})
	if err != nil || got != "" {
		t.Fatalf("got (%q, %v), want empty key and no env fallback", got, err)
	}
}

func TestResolveAPIKeyFuncErrorUnchanged(t *testing.T) {
	sentinel := errors.New("vault down")
	_, err := ResolveAPIKey(context.Background(), ResolveAPIKeyOptions{
		APIKeyFunc: func(context.Context) (string, error) { return "", sentinel },
	})
	if err != sentinel {
		t.Fatalf("err = %v, want the func's error unchanged", err)
	}
}

func TestResolveAPIKeyRequireNonEmpty(t *testing.T) {
	for _, v := range []string{"", " \t", "\uFEFF"} {
		_, err := ResolveAPIKey(context.Background(), ResolveAPIKeyOptions{
			APIKeyFunc:      func(context.Context) (string, error) { return v, nil },
			Description:     "Amazon Bedrock",
			RequireNonEmpty: true,
		})
		var lerr *providererrors.LoadAPIKeyError
		if !errors.As(err, &lerr) || lerr.Message != "Amazon Bedrock API key must be a non-empty string." {
			t.Fatalf("%q: err = %v", v, err)
		}
	}
	got, err := ResolveAPIKey(context.Background(), ResolveAPIKeyOptions{
		APIKeyFunc:      func(context.Context) (string, error) { return " k ", nil },
		RequireNonEmpty: true,
	})
	if err != nil || got != " k " {
		t.Fatalf("got (%q, %v); the value is returned untrimmed", got, err)
	}
}

func TestResolveAPIKeyBothSet(t *testing.T) {
	for _, tc := range []struct{ param, field, msg string }{
		{"", "apiKey/apiKeyFunc", "Both apiKey and apiKeyFunc were provided. Please use only one."},
		{"authToken", "authToken/authTokenFunc", "Both authToken and authTokenFunc were provided. Please use only one."},
	} {
		called := false
		_, err := ResolveAPIKey(context.Background(), ResolveAPIKeyOptions{
			APIKey:        "static",
			APIKeyFunc:    func(context.Context) (string, error) { called = true; return "f", nil },
			ParameterName: tc.param,
		})
		var iae *providererrors.InvalidArgumentError
		if !errors.As(err, &iae) || iae.Field != tc.field || iae.Message != tc.msg || called {
			t.Fatalf("err = %#v, called = %v", err, called)
		}
	}
}

func TestResolveAPIKeyStaticAndEnv(t *testing.T) {
	t.Setenv("R85PREP_KEY", "env-key")
	if got, err := ResolveAPIKey(context.Background(), ResolveAPIKeyOptions{APIKey: "static", EnvVar: "R85PREP_KEY"}); err != nil || got != "static" {
		t.Fatalf("static: (%q, %v)", got, err)
	}
	if got, err := ResolveAPIKey(context.Background(), ResolveAPIKeyOptions{EnvVar: "R85PREP_KEY"}); err != nil || got != "env-key" {
		t.Fatalf("env: (%q, %v)", got, err)
	}
	t.Setenv("R85PREP_KEY", "")
	_, err := ResolveAPIKey(context.Background(), ResolveAPIKeyOptions{EnvVar: "R85PREP_KEY", Description: "Test"})
	var lerr *providererrors.LoadAPIKeyError
	if !errors.As(err, &lerr) || lerr.Message != "Test API key is missing. Pass it using the 'apiKey' parameter or the R85PREP_KEY environment variable." {
		t.Fatalf("missing: %v", err)
	}
	if got, err := ResolveAPIKey(context.Background(), ResolveAPIKeyOptions{EnvVar: "R85PREP_KEY", Optional: true}); err != nil || got != "" {
		t.Fatalf("optional: (%q, %v)", got, err)
	}
}

func TestRequestAuthHeaderFunc(t *testing.T) {
	called := false
	fn := func(context.Context) (string, error) { called = true; return "tok", nil }
	got, err := RequestAuthHeaderFunc(context.Background(), nil, ResolveAPIKeyOptions{APIKeyFunc: fn}, "Authorization", "Bearer ")
	if err != nil || !reflect.DeepEqual(got, map[string]string{"Authorization": "Bearer tok"}) {
		t.Fatalf("got (%v, %v)", got, err)
	}
	called = false
	got, err = RequestAuthHeaderFunc(context.Background(), map[string]string{"authorization": "Bearer mine"}, ResolveAPIKeyOptions{APIKeyFunc: fn}, "Authorization", "Bearer ")
	if err != nil || got != nil || called {
		t.Fatalf("user header: got (%v, %v), called = %v", got, err, called)
	}
}
