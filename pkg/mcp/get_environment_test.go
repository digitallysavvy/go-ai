package mcp

import (
	"context"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// TestGetEnvironmentForGOOS ports TS getEnvironment.test.ts
// (mcp-stdio/get-environment.test.ts).
func TestGetEnvironmentForGOOS(t *testing.T) {
	t.Run("should not mutate the original custom environment slice", func(t *testing.T) {
		customEnv := []string{"CUSTOM_VAR=custom_value"}
		orig := append([]string(nil), customEnv...)

		_ = getEnvironmentForGOOS("darwin", customEnv, nil)

		if !reflect.DeepEqual(customEnv, orig) {
			t.Fatalf("customEnv mutated: got %v, want %v", customEnv, orig)
		}
	})

	t.Run("should let a custom value override an inherited default", func(t *testing.T) {
		result := getEnvironmentForGOOS("darwin", []string{"PATH=custom-path"}, []string{"PATH=/usr/bin"})

		if got := lookupEnv(result, "PATH"); got != "custom-path" {
			t.Fatalf("PATH = %q, want custom-path", got)
		}
		// Only one PATH entry should be present (no duplicate from inheritance).
		count := 0
		for _, kv := range result {
			if strings.HasPrefix(kv, "PATH=") {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("expected exactly one PATH entry, got %d (%v)", count, result)
		}
	})

	t.Run("should match custom environment keys case-insensitively on Windows", func(t *testing.T) {
		result := getEnvironmentForGOOS("windows", []string{"Path=custom-path"}, []string{"PATH=/usr/bin"})

		if got := lookupEnv(result, "Path"); got != "custom-path" {
			t.Fatalf("Path = %q, want custom-path", got)
		}
		if _, ok := lookupEnvOK(result, "PATH"); ok {
			t.Fatalf("expected no separate PATH entry when Path was set explicitly, got %v", result)
		}
	})

	t.Run("should not match custom environment keys case-insensitively on non-Windows", func(t *testing.T) {
		result := getEnvironmentForGOOS("darwin", []string{"path=custom-path"}, []string{"PATH=/usr/bin"})

		// "path" (lowercase) does not match the "PATH" allowlist entry on
		// non-Windows, so the parent's PATH is still inherited.
		if got := lookupEnv(result, "PATH"); got != "/usr/bin" {
			t.Fatalf("PATH = %q, want /usr/bin (inherited)", got)
		}
		if got := lookupEnv(result, "path"); got != "custom-path" {
			t.Fatalf("path = %q, want custom-path", got)
		}
	})

	t.Run("skips inherited values that look like shell function definitions", func(t *testing.T) {
		result := getEnvironmentForGOOS("darwin", nil, []string{"SHELL=() { :; }; echo vulnerable"})

		if _, ok := lookupEnvOK(result, "SHELL"); ok {
			t.Fatalf("expected SHELL to be skipped, got %v", result)
		}
	})

	t.Run("inherits the default allowlist from the parent environment", func(t *testing.T) {
		parent := []string{"HOME=/home/user", "LOGNAME=user", "PATH=/usr/bin", "SHELL=/bin/bash", "TERM=xterm", "USER=user", "UNRELATED=nope"}
		result := getEnvironmentForGOOS("darwin", nil, parent)

		for _, key := range []string{"HOME", "LOGNAME", "PATH", "SHELL", "TERM", "USER"} {
			if _, ok := lookupEnvOK(result, key); !ok {
				t.Fatalf("expected %s to be inherited, got %v", key, result)
			}
		}
		if _, ok := lookupEnvOK(result, "UNRELATED"); ok {
			t.Fatalf("expected UNRELATED to not be inherited, got %v", result)
		}
	})

	t.Run("inherits the Windows allowlist on windows", func(t *testing.T) {
		parent := []string{"APPDATA=x", "HOMEDRIVE=x", "HOMEPATH=x", "LOCALAPPDATA=x", "PATH=x", "PROCESSOR_ARCHITECTURE=x", "SYSTEMDRIVE=x", "SYSTEMROOT=x", "TEMP=x", "USERNAME=x", "USERPROFILE=x", "HOME=nope"}
		result := getEnvironmentForGOOS("windows", nil, parent)

		for _, key := range []string{"APPDATA", "HOMEDRIVE", "HOMEPATH", "LOCALAPPDATA", "PATH", "PROCESSOR_ARCHITECTURE", "SYSTEMDRIVE", "SYSTEMROOT", "TEMP", "USERNAME", "USERPROFILE"} {
			if _, ok := lookupEnvOK(result, key); !ok {
				t.Fatalf("expected %s to be inherited on windows, got %v", key, result)
			}
		}
		if _, ok := lookupEnvOK(result, "HOME"); ok {
			t.Fatalf("expected HOME (non-windows var) to not be inherited on windows, got %v", result)
		}
	})
}

// TestGetEnvironment exercises the real GOOS/os.Environ() entrypoint.
func TestGetEnvironment(t *testing.T) {
	result := getEnvironment([]string{"FOO=bar"})
	if got := lookupEnv(result, "FOO"); got != "bar" {
		t.Fatalf("FOO = %q, want bar", got)
	}

	// PATH should be inherited from the real parent process on all
	// platforms this test runs on, since the allowlist always includes it.
	if runtime.GOOS != "windows" {
		if _, ok := os.LookupEnv("PATH"); ok {
			if _, ok := lookupEnvOK(result, "PATH"); !ok {
				t.Fatalf("expected PATH to be inherited, got %v", result)
			}
		}
	}
}

// TestStdioTransportAppliesEnv ports the env-related cases from TS
// create-child-process.test.ts ("should spawn a child process with custom
// env"): a caller-supplied Env must reach the child process, and the
// process must still see the safe-allowlisted inherited vars (e.g. PATH).
func TestStdioTransportAppliesEnv(t *testing.T) {
	transport := NewStdioTransport(StdioTransportConfig{
		Command: "sh",
		Args:    []string{"-c", `printf '%s\n' "$CUSTOM_VAR"; printf '%s\n' "${PATH:+has-path}"`},
		Env:     []string{"CUSTOM_VAR=hello-from-env"},
	})

	if err := transport.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = transport.Close() }()

	if transport.cmd.Env == nil {
		t.Fatal("expected cmd.Env to be set, got nil")
	}
	if got := lookupEnv(transport.cmd.Env, "CUSTOM_VAR"); got != "hello-from-env" {
		t.Fatalf("cmd.Env CUSTOM_VAR = %q, want hello-from-env", got)
	}
	if _, ok := lookupEnvOK(transport.cmd.Env, "PATH"); !ok {
		t.Fatalf("expected PATH to be inherited into cmd.Env, got %v", transport.cmd.Env)
	}
}

func lookupEnv(env []string, key string) string {
	v, _ := lookupEnvOK(env, key)
	return v
}

func lookupEnvOK(env []string, key string) (string, bool) {
	for _, kv := range env {
		k, v, ok := splitEnvKV(kv)
		if ok && k == key {
			return v, true
		}
	}
	return "", false
}
