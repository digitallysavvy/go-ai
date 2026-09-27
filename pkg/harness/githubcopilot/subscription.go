package githubcopilot

import (
	"bytes"
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil/subscription"
)

const copilotKeyringService = "copilot-cli"

// GitHubCopilotAccount is a logged-in account entry from Copilot CLI's
// config.json.
type GitHubCopilotAccount struct {
	Host  string
	Login string
}

// Subscription is a resolved GitHub token for Copilot CLI use.
type Subscription struct {
	Token string
	Host  string
}

// ResolveSubscriptionEnvironmentOptions is the input of
// ResolveSubscriptionEnvironment.
type ResolveSubscriptionEnvironmentOptions struct {
	Auth AuthenticationMode
	Env  map[string]string
	// ReadSubscription defaults to ReadSubscription.
	ReadSubscription func(ctx context.Context, opts ReadSubscriptionOptions) (*Subscription, error)
}

// ResolveSubscriptionEnvironment resolves COPILOT_GITHUB_TOKEN /
// COPILOT_GH_HOST from Copilot CLI's own config, the OS credential store, or
// the `gh` CLI when settings.auth allows it. Mirrors TS
// `resolveGitHubCopilotSubscriptionEnvironment`.
func ResolveSubscriptionEnvironment(ctx context.Context, opts ResolveSubscriptionEnvironmentOptions) (map[string]string, error) {
	if harnessutil.IsAuthenticationEnvironment(opts.Auth) {
		return opts.Auth.Environment, nil
	}
	hasDirect := opts.Env["COPILOT_GITHUB_TOKEN"] != "" || opts.Env["GH_TOKEN"] != "" || opts.Env["GITHUB_TOKEN"] != ""
	if !harnessutil.ShouldResolveNativeSubscription(opts.Auth.Mode, opts.Env, hasDirect) {
		return opts.Env, nil
	}
	readSubscription := opts.ReadSubscription
	if readSubscription == nil {
		readSubscription = func(ctx context.Context, o ReadSubscriptionOptions) (*Subscription, error) {
			return ReadSubscription(ctx, o)
		}
	}
	subscr, err := readSubscription(ctx, ReadSubscriptionOptions{Env: opts.Env})
	if err != nil {
		return nil, err
	}
	if subscr == nil {
		return opts.Env, nil
	}
	out := make(map[string]string, len(opts.Env)+2)
	for k, v := range opts.Env {
		out[k] = v
	}
	out["COPILOT_GITHUB_TOKEN"] = subscr.Token
	out["COPILOT_GH_HOST"] = normalizeGitHubHost(subscr.Host)
	return out, nil
}

// ReadSubscriptionOptions is the input of ReadSubscription. Every field past
// Env is a seam used by tests to stand in for OS credential stores and the
// `gh` CLI, mirroring TS `readGitHubCopilotSubscription`'s injectable
// defaults.
type ReadSubscriptionOptions struct {
	Env                     map[string]string
	HomeDirectory           string
	GOOS                    string
	ReadSecureCredential    func(ctx context.Context, service, account string) (string, error)
	FindGitHubCliExecutable func(ctx context.Context, env map[string]string, goos string) (string, error)
	ReadGitHubCliToken      func(ctx context.Context, executable, hostname string, env map[string]string) (string, error)
}

// ReadSubscription reads a GitHub token for Copilot CLI: the last
// logged-in account's token from the OS secure credential store or
// plaintext config, falling back to `gh auth token`. Mirrors TS
// `readGitHubCopilotSubscription`.
func ReadSubscription(ctx context.Context, opts ReadSubscriptionOptions) (*Subscription, error) {
	env := opts.Env
	if env == nil {
		env = map[string]string{}
	}
	homeDirectory := opts.HomeDirectory
	if homeDirectory == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		homeDirectory = h
	}
	goos := opts.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	copilotHome := env["COPILOT_HOME"]
	if copilotHome == "" {
		copilotHome = filepath.Join(homeDirectory, ".copilot")
	}
	config := readCopilotConfig(filepath.Join(copilotHome, "config.json"))
	account := selectAccount(config)

	readSecureCredential := opts.ReadSecureCredential
	if readSecureCredential == nil {
		readSecureCredential = func(ctx context.Context, service, account string) (string, error) {
			return readSecureCredentialForPlatform(ctx, service, account, goos)
		}
	}

	if account != nil {
		accountKey := account.Host + ":" + account.Login
		if secureToken, err := readSecureCredential(ctx, copilotKeyringService, accountKey); err == nil && isNonEmpty(secureToken) {
			return &Subscription{Token: secureToken, Host: account.Host}, nil
		}
		if plaintext := plaintextToken(config, accountKey); plaintext != "" {
			return &Subscription{Token: plaintext, Host: account.Host}, nil
		}
	}

	host := ""
	if account != nil {
		host = account.Host
	} else if normalized := normalizeGitHubOrigin(firstNonEmpty(env["COPILOT_GH_HOST"], env["GH_HOST"], "github.com")); normalized != "" {
		host = normalized
	}
	if host == "" {
		return nil, nil
	}

	findExecutable := opts.FindGitHubCliExecutable
	if findExecutable == nil {
		findExecutable = FindGitHubCliExecutable
	}
	executable, err := findExecutable(ctx, env, goos)
	if err != nil || executable == "" {
		return nil, nil
	}

	readToken := opts.ReadGitHubCliToken
	if readToken == nil {
		readToken = readHostGitHubCliToken
	}
	token, err := readToken(ctx, executable, normalizeGitHubHost(host), env)
	if err != nil || !isNonEmpty(token) {
		return nil, nil
	}
	return &Subscription{Token: strings.TrimSpace(token), Host: host}, nil
}

// FindGitHubCliExecutable searches env's PATH for a `gh` executable. Mirrors
// TS `findHostGitHubCliExecutable`.
func FindGitHubCliExecutable(_ context.Context, env map[string]string, goos string) (string, error) {
	path := firstNonEmpty(env["PATH"], env["Path"], env["path"])
	if path == "" {
		return "", nil
	}
	separator := ":"
	names := []string{"gh"}
	if harnessutil.IsWindows(goos) {
		separator = ";"
		names = windowsGitHubCliExecutableNames(env["PATHEXT"])
	}
	for _, directory := range strings.Split(path, separator) {
		directory = strings.Trim(directory, `"`)
		if directory == "" {
			continue
		}
		for _, name := range names {
			candidate := filepath.Join(directory, name)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				if harnessutil.IsWindows(goos) || info.Mode().Perm()&0o111 != 0 {
					return candidate, nil
				}
			}
		}
	}
	return "", nil
}

func windowsGitHubCliExecutableNames(pathExt string) []string {
	if pathExt == "" {
		pathExt = ".COM;.EXE;.BAT;.CMD"
	}
	names := []string{"gh"}
	for _, ext := range strings.Split(pathExt, ";") {
		if ext != "" {
			names = append(names, "gh"+ext)
		}
	}
	return names
}

func readHostGitHubCliToken(ctx context.Context, executable, hostname string, env map[string]string) (string, error) {
	// Mirrors TS `readHostGitHubCliToken`'s `timeout: 10_000`.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "auth", "token", "--hostname", hostname)
	cmd.Env = mapToEnvList(env)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return stdout.String(), nil
}

func mapToEnvList(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

func readSecureCredentialForPlatform(ctx context.Context, service, account, goos string) (string, error) {
	switch {
	case harnessutil.IsMacOS(goos):
		if v := subscription.ReadMacOSKeychainPassword(ctx, service, account); v != "" {
			return v, nil
		}
	case harnessutil.IsLinux(goos):
		if v := subscription.ReadLinuxSecretServicePassword(ctx, []subscription.SecretServiceAttribute{
			{Name: "service", Value: service}, {Name: "username", Value: account},
		}); v != "" {
			return v, nil
		}
	case harnessutil.IsWindows(goos):
		if v := subscription.ReadWindowsCredentialManagerPassword(ctx, account+"."+service); v != "" {
			return v, nil
		}
	}
	return "", nil
}

func readCopilotConfig(path string) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	config, err := ParseJSONC(data)
	if err != nil {
		return nil
	}
	return config
}

func selectAccount(config map[string]any) *GitHubCopilotAccount {
	if config == nil {
		return nil
	}
	for _, key := range []string{"last_logged_in_user", "lastLoggedInUser"} {
		if account := toAccount(config[key]); account != nil {
			return account
		}
	}
	for _, key := range []string{"logged_in_users", "loggedInUsers"} {
		values, ok := config[key].([]any)
		if !ok {
			continue
		}
		for _, v := range values {
			if account := toAccount(v); account != nil {
				return account
			}
		}
	}
	return nil
}

func toAccount(value any) *GitHubCopilotAccount {
	record, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	host, hok := record["host"].(string)
	login, lok := record["login"].(string)
	if !hok || !lok || !isNonEmpty(host) || !isNonEmpty(login) {
		return nil
	}
	normalized := normalizeGitHubOrigin(host)
	if normalized == "" {
		return nil
	}
	return &GitHubCopilotAccount{Host: normalized, Login: login}
}

func plaintextToken(config map[string]any, accountKey string) string {
	if config == nil {
		return ""
	}
	tokens, ok := config["copilotTokens"].(map[string]any)
	if !ok {
		return ""
	}
	token, _ := tokens[accountKey].(string)
	if isNonEmpty(token) {
		return token
	}
	return ""
}

func normalizeGitHubOrigin(host string) string {
	full := host
	if !strings.Contains(host, "://") {
		full = "https://" + host
	}
	u, err := url.Parse(full)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// normalizeGitHubHost returns host's hostname (no scheme, no port unless
// present in the input). Mirrors TS `normalizeGitHubHost`.
func normalizeGitHubHost(host string) string {
	full := host
	if !strings.Contains(host, "://") {
		full = "https://" + host
	}
	u, err := url.Parse(full)
	if err != nil {
		return host
	}
	return u.Hostname()
}

func isNonEmpty(s string) bool {
	return strings.TrimSpace(s) != ""
}
