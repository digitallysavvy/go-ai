package fx

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness/acp"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil/subscription"
)

// Environment variable names used for fx's brokered subscription records.
// Mirrors TS constants in fx-subscription.ts.
const (
	ChatGPTAccessTokenEnvVar = "AI_SDK_FX_CHATGPT_ACCESS_TOKEN"
	ChatGPTAccountIDEnvVar   = "AI_SDK_FX_CHATGPT_ACCOUNT_ID"
	GrokAccessTokenEnvVar    = "AI_SDK_FX_GROK_ACCESS_TOKEN"
	GrokAccountIDEnvVar      = "AI_SDK_FX_GROK_ACCOUNT_ID"

	openAIClientID      = "app_EMoamEEZ73f0CkXaXp7hrann"
	xaiClientID         = "b1a00492-073a-47ea-816f-4c329264a828"
	sandboxRefreshToken = "ai-sdk-harness-brokered"
)

// SubscriptionEnvironmentVariables mirrors TS
// `FX_SUBSCRIPTION_ENVIRONMENT_VARIABLES`.
var SubscriptionEnvironmentVariables = []string{
	ChatGPTAccessTokenEnvVar, ChatGPTAccountIDEnvVar,
	GrokAccessTokenEnvVar, GrokAccountIDEnvVar,
}

type subscriptionProvider string

const (
	providerChatGPT subscriptionProvider = "chatgpt"
	providerGrok    subscriptionProvider = "grok"
)

// Credential is a stored fx OAuth credential.
type Credential struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    int64
	AccountID    string
}

// RequestCredential is one provider's sandbox->host credential pair, as
// returned by GetSubscriptionRequestCredentials.
type RequestCredential struct {
	Provider           string
	AccessToken        string
	SandboxAccessToken string
}

// ResolveSubscriptionEnvironmentOptions is the input of
// ResolveSubscriptionEnvironment.
type ResolveSubscriptionEnvironmentOptions struct {
	Auth          AuthenticationMode
	Env           map[string]string
	HomeDirectory string
	HTTPClient    *http.Client
}

// ResolveSubscriptionEnvironment resolves fx's ChatGPT/Grok subscription
// tokens when settings.auth allows it. Mirrors TS
// `resolveFxSubscriptionEnvironment`.
func ResolveSubscriptionEnvironment(ctx context.Context, opts ResolveSubscriptionEnvironmentOptions) (map[string]string, error) {
	if harnessutil.IsAuthenticationEnvironment(opts.Auth) {
		return opts.Auth.Environment, nil
	}
	if !harnessutil.ShouldResolveNativeSubscription(opts.Auth.Mode, opts.Env, false) {
		return opts.Env, nil
	}
	subs, err := ReadSubscriptions(ctx, ReadSubscriptionsOptions{
		HomeDirectory: opts.HomeDirectory,
		HTTPClient:    opts.HTTPClient,
	})
	if err != nil {
		return nil, err
	}
	if subs == nil {
		return opts.Env, nil
	}
	merged := make(map[string]string, len(opts.Env)+len(subs))
	for k, v := range opts.Env {
		merged[k] = v
	}
	for k, v := range subs {
		merged[k] = v
	}
	return merged, nil
}

// ReadSubscriptionsOptions is the input of ReadSubscriptions.
type ReadSubscriptionsOptions struct {
	HomeDirectory string
	HTTPClient    *http.Client
}

// ReadSubscriptions reads fx's ChatGPT and Grok subscription records from
// `~/.fx/{chatgpt,grok}-auth.json`, refreshing (and persisting) either that
// is expiring soon. Mirrors TS `readFxSubscriptions`.
func ReadSubscriptions(ctx context.Context, opts ReadSubscriptionsOptions) (map[string]string, error) {
	homeDirectory := opts.HomeDirectory
	if homeDirectory == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		homeDirectory = h
	}
	fxDirectory := filepath.Join(homeDirectory, ".fx")
	environment := map[string]string{}
	for _, provider := range []subscriptionProvider{providerChatGPT, providerGrok} {
		cred, err := readSubscription(ctx, provider, fxDirectory, opts.HTTPClient)
		if err != nil {
			return nil, err
		}
		if cred == nil {
			continue
		}
		environment[accessTokenEnvVar(provider)] = cred.AccessToken
		environment[accountIDEnvVar(provider)] = cred.AccountID
	}
	if len(environment) == 0 {
		return nil, nil
	}
	return environment, nil
}

// CreateSubscriptionAuthenticationFiles materializes sandbox-private
// authentication files carrying broker placeholders (never host tokens).
// Mirrors TS `createFxSubscriptionAuthenticationFiles`.
func CreateSubscriptionAuthenticationFiles(env, sandboxEnv map[string]string, credentialBrokeringAvailable bool) []acp.AuthenticationFile {
	var files []acp.AuthenticationFile
	for _, provider := range []subscriptionProvider{providerChatGPT, providerGrok} {
		envVar := accessTokenEnvVar(provider)
		_, hostOK := env[envVar]
		sandboxAccessToken, sandboxOK := sandboxEnv[envVar]
		accountID, accountOK := env[accountIDEnvVar(provider)]
		if !hostOK || !sandboxOK || !accountOK {
			continue
		}
		accessToken := sandboxAccessToken
		if provider == providerChatGPT && credentialBrokeringAvailable {
			accessToken = createChatGPTSandboxAccessToken(sandboxAccessToken, accountID)
		}
		path := ".fx/grok-auth.json"
		if provider == providerChatGPT {
			path = ".fx/chatgpt-auth.json"
		}
		content, _ := json.Marshal(map[string]any{
			"version":       1,
			"access_token":  accessToken,
			"refresh_token": sandboxRefreshToken,
			"expires_at_ms": int64(9007199254740991), // Number.MAX_SAFE_INTEGER
			"account_id":    accountID,
		})
		files = append(files, acp.AuthenticationFile{Path: path, Content: string(content) + "\n"})
	}
	return files
}

// GetSubscriptionRequestCredentials returns the provider credentials fx will
// present from the sandbox, mapped to the real host credential.  Mirrors TS
// `getFxSubscriptionRequestCredentials`.
func GetSubscriptionRequestCredentials(env, sandboxEnv map[string]string) []RequestCredential {
	var credentials []RequestCredential
	for _, provider := range []subscriptionProvider{providerChatGPT, providerGrok} {
		envVar := accessTokenEnvVar(provider)
		accessToken, accessOK := env[envVar]
		sandboxCredential, sandboxOK := sandboxEnv[envVar]
		accountID, accountOK := env[accountIDEnvVar(provider)]
		if !accessOK || !sandboxOK || !accountOK {
			continue
		}
		sandboxAccessToken := sandboxCredential
		if provider == providerChatGPT {
			sandboxAccessToken = createChatGPTSandboxAccessToken(sandboxCredential, accountID)
		}
		credentials = append(credentials, RequestCredential{
			Provider:           string(provider),
			AccessToken:        accessToken,
			SandboxAccessToken: sandboxAccessToken,
		})
	}
	return credentials
}

func readSubscription(ctx context.Context, provider subscriptionProvider, fxDirectory string, httpClient *http.Client) (*Credential, error) {
	filename := "grok-auth.json"
	if provider == providerChatGPT {
		filename = "chatgpt-auth.json"
	}
	path := filepath.Join(fxDirectory, filename)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return nil, nil
	}
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	var parsed map[string]any
	if err := json.Unmarshal(text, &parsed); err != nil {
		return nil, nil
	}
	cred := readCredential(parsed)
	if cred == nil {
		return nil, nil
	}
	if provider == providerChatGPT {
		accountID, err := extractChatGPTAccountID(cred.AccessToken)
		if err != nil || accountID != cred.AccountID {
			return nil, nil
		}
	}
	if !harnessutil.IsAccessTokenExpiringSoon(cred.ExpiresAt, time.Now().UnixMilli(), 0) {
		return cred, nil
	}

	client := httpClient
	if client == nil {
		client = http.DefaultClient
	}
	var refreshed *harnessutil.RefreshOAuthAccessTokenResult
	var refreshedAccountID string
	if provider == providerChatGPT {
		r, err := harnessutil.RefreshOAuthAccessToken(ctx, harnessutil.RefreshOAuthAccessTokenOptions{
			TokenURL:      "https://auth.openai.com/oauth/token",
			ClientID:      openAIClientID,
			RefreshToken:  cred.RefreshToken,
			RequestFormat: "json",
			HTTPClient:    client,
		})
		if err != nil {
			return nil, err
		}
		refreshed = r
		refreshedAccountID, err = extractChatGPTAccountID(refreshed.AccessToken)
		if err != nil {
			return nil, err
		}
	} else {
		r, err := harnessutil.RefreshOAuthAccessToken(ctx, harnessutil.RefreshOAuthAccessTokenOptions{
			TokenURL:      "https://auth.x.ai/oauth2/token",
			ClientID:      xaiClientID,
			RefreshToken:  cred.RefreshToken,
			RequestFormat: "form",
			HTTPClient:    client,
		})
		if err != nil {
			return nil, err
		}
		refreshed = r
		refreshedAccountID, err = fetchGrokAccountID(ctx, refreshed.AccessToken, client)
		if err != nil {
			return nil, err
		}
	}
	if refreshedAccountID != cred.AccountID {
		return nil, fmt.Errorf("fx %s OAuth refresh changed accounts.", provider) //nolint:staticcheck // matches TS SDK's exact error text
	}
	nextRefreshToken := cred.RefreshToken
	if refreshed.RefreshToken != "" {
		nextRefreshToken = refreshed.RefreshToken
	}
	next := &Credential{
		AccessToken:  refreshed.AccessToken,
		RefreshToken: nextRefreshToken,
		ExpiresAt:    refreshed.ExpiresAt,
		AccountID:    refreshedAccountID,
	}
	if err := persistCredential(path, next); err != nil {
		return nil, err
	}
	return next, nil
}

func persistCredential(path string, cred *Credential) error {
	data, err := json.Marshal(map[string]any{
		"version":       1,
		"access_token":  cred.AccessToken,
		"refresh_token": cred.RefreshToken,
		"expires_at_ms": cred.ExpiresAt,
		"account_id":    cred.AccountID,
	})
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readCredential(value map[string]any) *Credential {
	version, ok := value["version"].(float64)
	if !ok || version != 1 {
		return nil
	}
	accessToken, ok := value["access_token"].(string)
	if !ok || accessToken == "" {
		return nil
	}
	refreshToken, ok := value["refresh_token"].(string)
	if !ok || refreshToken == "" {
		return nil
	}
	expiresAt, ok := value["expires_at_ms"].(float64)
	if !ok {
		return nil
	}
	accountID, ok := value["account_id"].(string)
	if !ok || accountID == "" {
		return nil
	}
	return &Credential{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    int64(expiresAt),
		AccountID:    accountID,
	}
}

func fetchGrokAccountID(ctx context.Context, accessToken string, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://auth.x.ai/oauth2/userinfo", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("fx Grok user info request failed with status %d.", resp.StatusCode) //nolint:staticcheck // matches TS SDK's exact error text
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("fx Grok user info request returned an invalid account.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	sub, ok := parsed["sub"].(string)
	if !ok || sub == "" {
		return "", fmt.Errorf("fx Grok user info request returned an invalid account.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	return sub, nil
}

func extractChatGPTAccountID(accessToken string) (string, error) {
	payload := subscription.ParseJWTPayload(accessToken)
	if auth, ok := payload["https://api.openai.com/auth"].(map[string]any); ok {
		if accountID, ok := auth["chatgpt_account_id"].(string); ok && accountID != "" {
			return accountID, nil
		}
	}
	return "", fmt.Errorf("fx ChatGPT access token does not contain an account ID.") //nolint:staticcheck // matches TS SDK's exact error text
}

func createChatGPTSandboxAccessToken(credential, accountID string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payloadJSON, _ := json.Marshal(map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
	})
	payload := base64.RawURLEncoding.EncodeToString(payloadJSON)
	return header + "." + payload + "." + credential
}

func accessTokenEnvVar(provider subscriptionProvider) string {
	if provider == providerChatGPT {
		return ChatGPTAccessTokenEnvVar
	}
	return GrokAccessTokenEnvVar
}

func accountIDEnvVar(provider subscriptionProvider) string {
	if provider == providerChatGPT {
		return ChatGPTAccountIDEnvVar
	}
	return GrokAccountIDEnvVar
}
