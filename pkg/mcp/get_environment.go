package mcp

import (
	"os"
	"runtime"
	"strings"
)

// defaultInheritedEnvVars mirrors TS DEFAULT_INHERITED_ENV_VARS
// (mcp-stdio/get-environment.ts): the safe allowlist of parent-process
// environment variables a stdio MCP child process inherits by default.
func defaultInheritedEnvVars(goos string) []string {
	if goos == "windows" {
		return []string{
			"APPDATA",
			"HOMEDRIVE",
			"HOMEPATH",
			"LOCALAPPDATA",
			"PATH",
			"PROCESSOR_ARCHITECTURE",
			"SYSTEMDRIVE",
			"SYSTEMROOT",
			"TEMP",
			"USERNAME",
			"USERPROFILE",
		}
	}
	return []string{"HOME", "LOGNAME", "PATH", "SHELL", "TERM", "USER"}
}

// splitEnvKV splits a "KEY=VALUE" environment entry (the os.Environ() /
// exec.Cmd.Env convention) into its key and value.
func splitEnvKV(kv string) (key, value string, ok bool) {
	idx := strings.IndexByte(kv, '=')
	if idx < 0 {
		return "", "", false
	}
	return kv[:idx], kv[idx+1:], true
}

// getEnvironment builds the environment for a stdio MCP child process,
// mirroring TS getEnvironment (mcp-stdio/get-environment.ts): it starts from
// the caller-supplied custom env and fills in a safe allowlist of inherited
// parent-process env vars, skipping any key the caller already supplied
// explicitly (matched case-insensitively on Windows) and skipping any
// inherited value that looks like a Bash function definition (the
// CVE-2014-6271 "Shellshock" pattern, values starting with "()").
func getEnvironment(customEnv []string) []string {
	return getEnvironmentForGOOS(runtime.GOOS, customEnv, os.Environ())
}

// getEnvironmentForGOOS is getEnvironment parameterized by GOOS and the
// parent environment so tests can exercise the Windows branch and control
// inputs deterministically on any platform.
func getEnvironmentForGOOS(goos string, customEnv []string, parentEnv []string) []string {
	isWindows := goos == "windows"

	env := make([]string, 0, len(customEnv)+len(parentEnv))
	env = append(env, customEnv...)

	customKeys := make(map[string]bool, len(customEnv))
	for _, kv := range customEnv {
		key, _, ok := splitEnvKV(kv)
		if !ok {
			continue
		}
		if isWindows {
			key = strings.ToUpper(key)
		}
		customKeys[key] = true
	}

	parent := make(map[string]string, len(parentEnv))
	for _, kv := range parentEnv {
		key, value, ok := splitEnvKV(kv)
		if !ok {
			continue
		}
		if isWindows {
			key = strings.ToUpper(key)
		}
		parent[key] = value
	}

	for _, key := range defaultInheritedEnvVars(goos) {
		lookupKey := key
		if isWindows {
			lookupKey = strings.ToUpper(lookupKey)
		}
		if customKeys[lookupKey] {
			continue
		}

		value, ok := parent[lookupKey]
		if !ok {
			continue
		}
		if strings.HasPrefix(value, "()") {
			continue
		}

		env = append(env, key+"="+value)
	}

	return env
}
