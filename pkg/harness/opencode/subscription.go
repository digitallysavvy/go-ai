package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil/subscription"
)

// Native OpenCode subscription auth. Mirrors TS opencode-subscription.ts.
// Unlike the OS-keychain-backed native subscription readers used by other
// bridge adapters (claude-code/codex/github-copilot/cursor,
// harnessutil/subscription), OpenCode's own subscription store is a plain
// JSON file (`~/.local/share/opencode/auth.json`, or `OPENCODE_AUTH_CONTENT`
// verbatim) plus HTTP OAuth-refresh and, for GitLab, one extra HTTP call
// (GitLab AI Gateway's "direct_access" exchange). It never touches an OS
// keychain, so every mechanism it uses is host-portable and is ported here
// in full — nothing is deferred.

const (
	openAIClientID     = "app_EMoamEEZ73f0CkXaXp7hrann"
	xaiClientID        = "b1a00492-073a-47ea-816f-4c329264a828"
	gitlabClientID     = "1d89f9fdb23ee96d4e603201f6861dab6e143c5c3c00469a018a2d94bdc03d4e"
	gitlabAIGatewayURL = "https://cloud.gitlab.com"

	// GitLabAnthropicProviderID etc. are the OpenCode native provider ids
	// this adapter injects into OpenCodeConfig when brokering GitLab AI
	// Gateway credentials. Mirrors TS's GITLAB_*_PROVIDER_ID constants.
	GitLabAnthropicProviderID       = "ai-sdk-gitlab-anthropic"
	GitLabOpenAIChatProviderID      = "ai-sdk-gitlab-openai-chat"
	GitLabOpenAIResponsesProviderID = "ai-sdk-gitlab-openai-responses"

	// numberMaxSafeInteger mirrors JS's Number.MAX_SAFE_INTEGER (2^53-1),
	// used as a "never expires" sentinel in the auth.json content this
	// adapter writes for the sandbox's own OpenCode CLI to read (the host
	// already holds a live, host-managed access token; there is nothing for
	// the sandboxed CLI to refresh).
	numberMaxSafeInteger = 9007199254740991
)

// gitlabModel is one entry of the native GitLab Duo model catalog.
type gitlabModel struct {
	ProviderID string // one of the GitLab*ProviderID consts, or "workflow"
	ModelID    string
}

// openCodeGitLabModels mirrors TS `OPEN_CODE_GITLAB_MODELS`.
var openCodeGitLabModels = map[string]gitlabModel{
	"duo-chat-fable-5":        {GitLabAnthropicProviderID, "claude-fable-5"},
	"duo-chat-opus-5":         {GitLabAnthropicProviderID, "claude-opus-5"},
	"duo-chat-opus-4-8":       {GitLabAnthropicProviderID, "claude-opus-4-8"},
	"duo-chat-opus-4-7":       {GitLabAnthropicProviderID, "claude-opus-4-7"},
	"duo-chat-opus-4-6":       {GitLabAnthropicProviderID, "claude-opus-4-6"},
	"duo-chat-sonnet-5":       {GitLabAnthropicProviderID, "claude-sonnet-5"},
	"duo-chat-sonnet-4-6":     {GitLabAnthropicProviderID, "claude-sonnet-4-6"},
	"duo-chat-opus-4-5":       {GitLabAnthropicProviderID, "claude-opus-4-5-20251101"},
	"duo-chat-sonnet-4-5":     {GitLabAnthropicProviderID, "claude-sonnet-4-5-20250929"},
	"duo-chat-haiku-4-5":      {GitLabAnthropicProviderID, "claude-haiku-4-5-20251001"},
	"duo-chat-gpt-5-1":        {GitLabOpenAIChatProviderID, "gpt-5.1-2025-11-13"},
	"duo-chat-gpt-5-2":        {GitLabOpenAIChatProviderID, "gpt-5.2-2025-12-11"},
	"duo-chat-gpt-5-4":        {GitLabOpenAIChatProviderID, "gpt-5.4-2026-03-05"},
	"duo-chat-gpt-5-5":        {GitLabOpenAIChatProviderID, "gpt-5.5-2026-04-23"},
	"duo-chat-gpt-5-mini":     {GitLabOpenAIChatProviderID, "gpt-5-mini-2025-08-07"},
	"duo-chat-gpt-5-4-mini":   {GitLabOpenAIChatProviderID, "gpt-5.4-mini"},
	"duo-chat-gpt-5-4-nano":   {GitLabOpenAIChatProviderID, "gpt-5.4-nano"},
	"duo-chat-gpt-5-6-sol":    {GitLabOpenAIResponsesProviderID, "gpt-5.6-sol"},
	"duo-chat-gpt-5-6-terra":  {GitLabOpenAIResponsesProviderID, "gpt-5.6-terra"},
	"duo-chat-gpt-5-6-luna":   {GitLabOpenAIResponsesProviderID, "gpt-5.6-luna"},
	"duo-chat-gpt-5-codex":    {GitLabOpenAIResponsesProviderID, "gpt-5-codex"},
	"duo-chat-gpt-5-2-codex":  {GitLabOpenAIResponsesProviderID, "gpt-5.2-codex"},
	"duo-chat-gpt-5-3-codex":  {GitLabOpenAIResponsesProviderID, "gpt-5.3-codex"},
	"duo-workflow":            {"workflow", "default"},
	"duo-workflow-default":    {"workflow", "default"},
	"duo-workflow-sonnet-4-5": {"workflow", "anthropic/claude-sonnet-4-5-20250929"},
	"duo-workflow-sonnet-5":   {"workflow", "claude_sonnet_5"},
	"duo-workflow-opus-5":     {"workflow", "claude_opus_5"},
	"duo-workflow-sonnet-4-6": {"workflow", "claude_sonnet_4_6"},
	"duo-workflow-opus-4-5":   {"workflow", "anthropic/claude-opus-4-5-20251101"},
	"duo-workflow-haiku-4-5":  {"workflow", "claude_haiku_4_5_20251001"},
	"duo-workflow-opus-4-6":   {"workflow", "claude_opus_4_6_20260205"},
}

// SubscriptionProvider is the OpenCode-native provider a subscription was
// read for. Mirrors TS `OpenCodeSubscriptionProvider`.
type SubscriptionProvider string

const (
	SubscriptionOpenAI        SubscriptionProvider = "openai"
	SubscriptionXAI           SubscriptionProvider = "xai"
	SubscriptionGitHubCopilot SubscriptionProvider = "github-copilot"
	SubscriptionPoe           SubscriptionProvider = "poe"
	SubscriptionOpenCodeGo    SubscriptionProvider = "opencode-go"
	SubscriptionGitLab        SubscriptionProvider = "gitlab"
)

func isSubscriptionProvider(v string) bool {
	switch SubscriptionProvider(v) {
	case SubscriptionOpenAI, SubscriptionXAI, SubscriptionGitHubCopilot, SubscriptionPoe, SubscriptionOpenCodeGo, SubscriptionGitLab:
		return true
	}
	return false
}

// Subscription is a credential read from OpenCode's own auth store. Mirrors
// TS `OpenCodeSubscription`.
type Subscription struct {
	ProviderID    SubscriptionProvider
	AccessToken   string
	AccountID     string
	EnterpriseURL string
}

// GitLabDirectAccess is the result of exchanging a GitLab OAuth token for AI
// Gateway "direct access" credentials. Mirrors TS `OpenCodeGitLabDirectAccess`.
type GitLabDirectAccess struct {
	AccessToken  string
	Headers      map[string]string
	AIGatewayURL string
}

// resolvedAuthentication is the result of resolveOpenCodeAuthentication.
// Mirrors TS's inline return type.
type resolvedAuthentication struct {
	Environment        map[string]string
	AuthenticationMode ResolvedAuthenticationMode
	Subscription       *Subscription
}

// readSubscriptionFunc reads a subscription for one provider id. Overridable
// in tests. Mirrors TS's `readSubscription` parameter.
type readSubscriptionFunc func(ctx context.Context, providerID string, env map[string]string) (*Subscription, error)

// resolveOpenCodeAuthentication mirrors TS `resolveOpenCodeAuthentication`:
// it resolves the plain credential-forwarding environment/mode first, and
// only falls back to reading a native OpenCode subscription when no
// environment or isolated-auth credential and no AI Gateway selection is
// already available.
func resolveOpenCodeAuthentication(ctx context.Context, auth AuthenticationMode, model, provider string, processEnv map[string]string, readSub readSubscriptionFunc) (resolvedAuthentication, error) {
	environment := resolveEnv(auth, model, provider, processEnv)
	authenticationMode := resolveAuthenticationMode(auth, model, provider, processEnv)
	if auth.IsEnvironment() || auth.Mode == harness.AuthModeAIGateway || authenticationMode == AuthAIGateway || hasOpenCodeCredential(environment, authenticationMode) {
		return resolvedAuthentication{Environment: environment, AuthenticationMode: authenticationMode}, nil
	}
	if readSub == nil {
		readSub = readOpenCodeSubscription
	}
	sub, err := readSub(ctx, string(authenticationMode), processEnv)
	if err != nil {
		return resolvedAuthentication{}, err
	}
	if sub == nil {
		return resolvedAuthentication{Environment: environment, AuthenticationMode: authenticationMode}, nil
	}
	merged := map[string]string{}
	for k, v := range environment {
		merged[k] = v
	}
	merged[SubscriptionAccessTokenEnvironmentVariable] = sub.AccessToken
	return resolvedAuthentication{Environment: merged, AuthenticationMode: authenticationMode, Subscription: sub}, nil
}

// hasOpenCodeCredential mirrors TS `hasOpenCodeCredential`.
func hasOpenCodeCredential(environment map[string]string, authenticationMode ResolvedAuthenticationMode) bool {
	if authenticationMode == AuthAIGateway {
		return environment["AI_GATEWAY_API_KEY"] != ""
	}
	for name := range environment {
		if strings.HasSuffix(name, "_API_KEY") || strings.HasSuffix(name, "_TOKEN") || name == "ANTHROPIC_AUTH_TOKEN" {
			return true
		}
	}
	return false
}

// subscriptionReadOptions overrides readOpenCodeSubscription's host lookups
// for tests. Mirrors the optional `homeDirectory`/`platform`/`fetch`
// parameters TS's `readOpenCodeSubscription` takes.
type subscriptionReadOptions struct {
	HomeDirectory string
	GOOS          string
	HTTPClient    *http.Client
}

// readOpenCodeSubscription mirrors TS `readOpenCodeSubscription`: it reads
// OpenCode's own auth store (host filesystem, or OPENCODE_AUTH_CONTENT),
// extracts the entry for providerID, and — for OAuth entries other than
// github-copilot/poe — refreshes the access token when it is expiring soon,
// writing the refreshed credential back to the store. Matches
// readSubscriptionFunc; see readOpenCodeSubscriptionWithOptions for the
// test-overridable form.
func readOpenCodeSubscription(ctx context.Context, providerID string, env map[string]string) (*Subscription, error) {
	return readOpenCodeSubscriptionWithOptions(ctx, providerID, env, subscriptionReadOptions{})
}

func readOpenCodeSubscriptionWithOptions(ctx context.Context, providerID string, env map[string]string, opts subscriptionReadOptions) (*Subscription, error) {
	if !isSubscriptionProvider(providerID) {
		return nil, nil
	}
	store, err := readAuthStore(env, opts.HomeDirectory, opts.GOOS)
	if err != nil || store == nil {
		return nil, nil
	}
	candidate, ok := store.Value[providerID].(map[string]any)
	if !ok {
		return nil, nil
	}

	typ, _ := candidate["type"].(string)
	if typ == "api" {
		key, _ := candidate["key"].(string)
		if key == "" {
			return nil, nil
		}
		return &Subscription{ProviderID: SubscriptionProvider(providerID), AccessToken: key}, nil
	}
	if typ != "oauth" {
		return nil, nil
	}
	if providerID == string(SubscriptionOpenCodeGo) {
		return nil, nil
	}

	access, _ := candidate["access"].(string)
	refresh, _ := candidate["refresh"].(string)
	expiresF, expiresOK := candidate["expires"].(float64)
	if access == "" || refresh == "" || !expiresOK {
		return nil, nil
	}
	expires := int64(expiresF)
	enterpriseURL, _ := candidate["enterpriseUrl"].(string)

	if providerID == string(SubscriptionGitHubCopilot) {
		return &Subscription{ProviderID: SubscriptionGitHubCopilot, AccessToken: refresh, EnterpriseURL: enterpriseURL}, nil
	}
	if providerID == string(SubscriptionPoe) {
		if harnessutil.IsAccessTokenExpiringSoon(expires, time.Now().UnixMilli(), 0) {
			return nil, errors.New("OpenCode Poe subscription API key is expiring soon. Run OpenCode login again.") //nolint:staticcheck // matches TS SDK's exact error text
		}
		return &Subscription{ProviderID: SubscriptionPoe, AccessToken: access}, nil
	}

	accessToken := access
	accountID, hasAccountID := candidate["accountId"].(string)
	if !hasAccountID && providerID == string(SubscriptionOpenAI) {
		accountID = extractOpenAIAccountID(access)
	}
	if harnessutil.IsAccessTokenExpiringSoon(expires, time.Now().UnixMilli(), 0) {
		tokenURL, clientID := resolveRefreshSettings(providerID, candidate, env)
		refreshed, err := harnessutil.RefreshOAuthAccessToken(ctx, harnessutil.RefreshOAuthAccessTokenOptions{
			TokenURL: tokenURL, ClientID: clientID, RefreshToken: refresh, HTTPClient: opts.HTTPClient,
		})
		if err != nil {
			return nil, err
		}
		accessToken = refreshed.AccessToken
		if providerID == string(SubscriptionOpenAI) {
			if aid := extractOpenAIAccountID(refreshed.AccessToken); aid != "" {
				accountID = aid
			}
		}
		updated := map[string]any{}
		for k, v := range candidate {
			updated[k] = v
		}
		updated["access"] = refreshed.AccessToken
		newRefresh := refresh
		if refreshed.RefreshToken != "" {
			newRefresh = refreshed.RefreshToken
		}
		updated["refresh"] = newRefresh
		updated["expires"] = float64(refreshed.ExpiresAt)
		if accountID != "" {
			updated["accountId"] = accountID
		}
		store.Value[providerID] = updated
		if store.Write != nil {
			if err := store.Write(store.Value); err != nil {
				return nil, err
			}
		}
	}

	return &Subscription{ProviderID: SubscriptionProvider(providerID), AccessToken: accessToken, AccountID: accountID, EnterpriseURL: enterpriseURL}, nil
}

// requestOpenCodeGitLabDirectAccess mirrors TS
// `requestOpenCodeGitLabDirectAccess`.
func requestOpenCodeGitLabDirectAccess(ctx context.Context, httpClient *http.Client, accessToken, instanceURL, aiGatewayURL string) (*GitLabDirectAccess, error) {
	if instanceURL == "" {
		instanceURL = "https://gitlab.com"
	}
	if aiGatewayURL == "" {
		aiGatewayURL = gitlabAIGatewayURL
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	reqURL := strings.TrimRight(instanceURL, "/") + "/api/v4/ai/third_party_agents/direct_access"
	body := []byte(`{"feature_flags":{"DuoAgentPlatformNext":true}}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("OpenCode GitLab direct access request failed with status %d.", resp.StatusCode) //nolint:staticcheck // matches TS SDK's exact error text
	}
	var parsed map[string]any
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, errors.New("OpenCode GitLab direct access returned invalid JSON.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	token, _ := parsed["token"].(string)
	headersRaw, headersOK := parsed["headers"].(map[string]any)
	if token == "" || !headersOK {
		return nil, errors.New("OpenCode GitLab direct access returned invalid credentials.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	headers := map[string]string{}
	for k, v := range headersRaw {
		s, ok := v.(string)
		if !ok {
			return nil, errors.New("OpenCode GitLab direct access returned invalid credentials.") //nolint:staticcheck // matches TS SDK's exact error text
		}
		headers[k] = s
	}
	return &GitLabDirectAccess{AccessToken: token, Headers: headers, AIGatewayURL: strings.TrimRight(aiGatewayURL, "/")}, nil
}

// createOpenCodeGitLabSubscriptionRequestTransformations mirrors TS
// `createOpenCodeGitLabSubscriptionRequestTransformations`.
func createOpenCodeGitLabSubscriptionRequestTransformations(directAccess *GitLabDirectAccess, sandboxAccessToken string) ([]harness.RequestTransformation, error) {
	headers := map[string]string{}
	for k, v := range directAccess.Headers {
		lower := strings.ToLower(k)
		if lower == "authorization" || lower == "x-api-key" {
			continue
		}
		headers[k] = v
	}
	headers["Authorization"] = "Bearer " + directAccess.AccessToken

	var out []harness.RequestTransformation
	for _, matchURL := range []string{
		directAccess.AIGatewayURL + "/ai/v1/proxy/anthropic",
		directAccess.AIGatewayURL + "/ai/v1/proxy/openai/v1",
	} {
		t, err := harnessutil.CreateCredentialRequestTransformation(harnessutil.CreateCredentialRequestTransformationOptions{
			MatchURL: matchURL, MatchHeaders: map[string]string{"Authorization": "Bearer " + sandboxAccessToken},
			TransformHeaders: headers,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// createOpenCodeGitLabSubscriptionConfig mirrors TS
// `createOpenCodeGitLabSubscriptionConfig`.
func createOpenCodeGitLabSubscriptionConfig(openCodeConfig map[string]any, sandboxAccessToken, aiGatewayURL string, headers map[string]string) map[string]any {
	providerOptions := map[string]any{}
	if headers != nil {
		providerOptions["headers"] = headers
	}
	withOpts := func(extra map[string]any) map[string]any {
		m := map[string]any{}
		for k, v := range extra {
			m[k] = v
		}
		for k, v := range providerOptions {
			m[k] = v
		}
		return m
	}
	providers := map[string]any{
		GitLabAnthropicProviderID: map[string]any{
			"name": "GitLab Anthropic", "npm": "@ai-sdk/anthropic",
			"api": aiGatewayURL + "/ai/v1/proxy/anthropic/v1", "options": withOpts(map[string]any{"authToken": sandboxAccessToken}),
			"models": gitlabModelsFor(GitLabAnthropicProviderID),
		},
		GitLabOpenAIChatProviderID: map[string]any{
			"name": "GitLab OpenAI Chat", "npm": "@ai-sdk/openai-compatible",
			"api": aiGatewayURL + "/ai/v1/proxy/openai/v1", "options": withOpts(map[string]any{"apiKey": sandboxAccessToken}),
			"models": gitlabModelsFor(GitLabOpenAIChatProviderID),
		},
		GitLabOpenAIResponsesProviderID: map[string]any{
			"name": "GitLab OpenAI Responses", "npm": "@ai-sdk/openai",
			"api": aiGatewayURL + "/ai/v1/proxy/openai/v1", "options": withOpts(map[string]any{"apiKey": sandboxAccessToken}),
			"models": gitlabModelsFor(GitLabOpenAIResponsesProviderID),
		},
	}
	out := map[string]any{}
	for k, v := range openCodeConfig {
		out[k] = v
	}
	configuredProviders := map[string]any{}
	if p, ok := openCodeConfig["provider"].(map[string]any); ok {
		for k, v := range p {
			configuredProviders[k] = v
		}
	}
	for k, v := range providers {
		configuredProviders[k] = v
	}
	out["provider"] = configuredProviders
	return out
}

func gitlabModelsFor(providerID string) map[string]any {
	out := map[string]any{}
	for id, m := range openCodeGitLabModels {
		if m.ProviderID == providerID {
			out[id] = map[string]any{"id": m.ModelID, "name": id}
		}
	}
	return out
}

// resolveOpenCodeGitLabSubscriptionModel mirrors TS
// `resolveOpenCodeGitLabSubscriptionModel`.
func resolveOpenCodeGitLabSubscriptionModel(model, provider string) (string, error) {
	if model == "" {
		if provider == string(SubscriptionGitLab) {
			return "", unsupported("brokering native GitLab subscription credentials without an explicit model")
		}
		return "", nil
	}
	modelProvider, modelID := provider, model
	if idx := strings.Index(model, "/"); idx >= 0 {
		modelProvider, modelID = model[:idx], model[idx+1:]
	}
	if modelProvider != string(SubscriptionGitLab) {
		return model, nil
	}
	mapping, ok := openCodeGitLabModels[modelID]
	if !ok {
		return "", unsupported(fmt.Sprintf("brokering native GitLab subscription credentials for unknown model %q", modelID))
	}
	if mapping.ProviderID == "workflow" {
		return "", unsupported(fmt.Sprintf(
			"brokering native GitLab subscription credentials for workflow model %q because it authenticates over WebSocket. Use a sandbox without credential brokering to use the credential-forwarding fallback", modelID))
	}
	return mapping.ProviderID + "/" + modelID, nil
}

// createOpenCodeSubscriptionAuthContent mirrors TS
// `createOpenCodeSubscriptionAuthContent`: the sandboxed OpenCode CLI reads
// this content (via OPENCODE_AUTH_CONTENT) as if it were its own auth.json,
// with a host-managed access token that never expires from its point of
// view (the host is the one holding the real, refreshable OAuth credential).
func createOpenCodeSubscriptionAuthContent(authentication *Subscription, accessToken string) (string, error) {
	var value map[string]any
	if authentication.ProviderID == SubscriptionOpenCodeGo {
		value = map[string]any{"type": "api", "key": accessToken}
	} else {
		refresh := "host-managed"
		if authentication.ProviderID == SubscriptionGitHubCopilot {
			refresh = accessToken
		}
		value = map[string]any{
			"type": "oauth", "access": accessToken, "refresh": refresh, "expires": numberMaxSafeInteger,
		}
		if authentication.AccountID != "" {
			value["accountId"] = authentication.AccountID
		}
		if authentication.EnterpriseURL != "" {
			value["enterpriseUrl"] = authentication.EnterpriseURL
		}
	}
	out, err := json.Marshal(map[string]any{string(authentication.ProviderID): value})
	return string(out), err
}

// createOpenCodeSubscriptionRequestTransformations mirrors TS
// `createOpenCodeSubscriptionRequestTransformations`.
func createOpenCodeSubscriptionRequestTransformations(authentication *Subscription, sandboxAccessToken string) ([]harness.RequestTransformation, error) {
	matchURL := resolveOpenCodeSubscriptionRequestURL(authentication)
	transformHeaders := map[string]string{"Authorization": "Bearer " + authentication.AccessToken}
	if authentication.ProviderID == SubscriptionOpenAI && authentication.AccountID != "" {
		transformHeaders["ChatGPT-Account-Id"] = authentication.AccountID
	}
	t, err := harnessutil.CreateCredentialRequestTransformation(harnessutil.CreateCredentialRequestTransformationOptions{
		MatchURL: matchURL, MatchHeaders: map[string]string{"Authorization": "Bearer " + sandboxAccessToken},
		TransformHeaders: transformHeaders,
	})
	if err != nil {
		return nil, err
	}
	out := []harness.RequestTransformation{t}
	if authentication.ProviderID == SubscriptionPoe || authentication.ProviderID == SubscriptionOpenCodeGo {
		t2, err := harnessutil.CreateCredentialRequestTransformation(harnessutil.CreateCredentialRequestTransformationOptions{
			MatchURL: matchURL, MatchHeaders: map[string]string{"x-api-key": sandboxAccessToken},
			TransformHeaders: map[string]string{"x-api-key": authentication.AccessToken},
		})
		if err != nil {
			return nil, err
		}
		out = append(out, t2)
	}
	return out, nil
}

// resolveRefreshSettings mirrors TS `resolveRefreshSettings`.
func resolveRefreshSettings(providerID string, candidate map[string]any, env map[string]string) (tokenURL, clientID string) {
	switch providerID {
	case string(SubscriptionOpenAI):
		return "https://auth.openai.com/oauth/token", openAIClientID
	case string(SubscriptionXAI):
		return "https://auth.x.ai/oauth2/token", xaiClientID
	}
	instanceURL, _ := candidate["enterpriseUrl"].(string)
	if instanceURL == "" {
		instanceURL = env["GITLAB_INSTANCE_URL"]
	}
	if instanceURL == "" {
		instanceURL = "https://gitlab.com"
	}
	clientID = env["GITLAB_OAUTH_CLIENT_ID"]
	if clientID == "" {
		clientID = gitlabClientID
	}
	return strings.TrimRight(instanceURL, "/") + "/oauth/token", clientID
}

// resolveOpenCodeSubscriptionRequestURL mirrors TS `resolveRequestUrl`.
func resolveOpenCodeSubscriptionRequestURL(authentication *Subscription) string {
	switch authentication.ProviderID {
	case SubscriptionOpenAI:
		return "https://chatgpt.com/backend-api/codex"
	case SubscriptionXAI:
		return "https://api.x.ai/v1"
	case SubscriptionGitHubCopilot:
		if authentication.EnterpriseURL == "" {
			return "https://api.githubcopilot.com"
		}
		return "https://copilot-api." + normalizeDomain(authentication.EnterpriseURL)
	case SubscriptionPoe:
		return "https://api.poe.com"
	case SubscriptionOpenCodeGo:
		return "https://opencode.ai/zen/go/v1"
	case SubscriptionGitLab:
		if authentication.EnterpriseURL != "" {
			return authentication.EnterpriseURL
		}
		return "https://gitlab.com"
	}
	return ""
}

// normalizeDomain mirrors TS `normalizeDomain`.
func normalizeDomain(value string) string {
	target := value
	if !strings.Contains(value, "://") {
		target = "https://" + value
	}
	if u, err := url.Parse(target); err == nil && u.Host != "" {
		return u.Host
	}
	v := strings.TrimPrefix(value, "https://")
	v = strings.TrimPrefix(v, "http://")
	return strings.TrimRight(v, "/")
}

// extractOpenAIAccountID mirrors TS `extractOpenAIAccountId`.
func extractOpenAIAccountID(accessToken string) string {
	payload := subscription.ParseJWTPayload(accessToken)
	if payload == nil {
		return ""
	}
	auth, ok := payload["https://api.openai.com/auth"].(map[string]any)
	if !ok {
		return ""
	}
	accountID, _ := auth["chatgpt_account_id"].(string)
	return accountID
}

// authStore is OpenCode's on-disk (or env-supplied) credential store.
type authStore struct {
	Value map[string]any
	// Write persists an updated store (after an OAuth refresh). Nil for an
	// OPENCODE_AUTH_CONTENT-supplied store, which has nowhere to write back
	// to (mirrors TS `stored.write` being undefined in that case).
	Write func(map[string]any) error
}

// readAuthStore mirrors TS `readStore`. homeDirectoryOverride and
// goosOverride default to the real host (os.UserHomeDir, runtime.GOOS) when
// empty; tests supply them to avoid touching the real host's auth.json.
func readAuthStore(env map[string]string, homeDirectoryOverride, goosOverride string) (*authStore, error) {
	if content, ok := env["OPENCODE_AUTH_CONTENT"]; ok {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(content), &parsed); err != nil {
			return nil, nil
		}
		return &authStore{Value: parsed}, nil
	}
	homeDirectory := homeDirectoryOverride
	if homeDirectory == "" {
		if h, err := os.UserHomeDir(); err == nil {
			homeDirectory = h
		}
	}
	goos := goosOverride
	if goos == "" {
		goos = runtime.GOOS
	}
	var authPath string
	switch {
	case env["XDG_DATA_HOME"] != "":
		authPath = filepath.Join(env["XDG_DATA_HOME"], "opencode", "auth.json")
	case harnessutil.IsWindows(goos):
		authPath = filepath.Join(homeDirectory, ".opencode", "auth.json")
	default:
		authPath = filepath.Join(homeDirectory, ".local", "share", "opencode", "auth.json")
	}
	data, err := os.ReadFile(authPath)
	if err != nil {
		return nil, nil
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, nil
	}
	return &authStore{Value: parsed, Write: func(value map[string]any) error { return writeAuthStore(authPath, value) }}, nil
}

// writeAuthStore mirrors TS `writeStore`: write-then-rename so a concurrent
// reader never observes a partial file, with owner-only permissions
// throughout (the file holds live OAuth credentials).
func writeAuthStore(authPath string, value map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(authPath), 0o755); err != nil {
		return err
	}
	temporaryPath := fmt.Sprintf("%s.%d.tmp", authPath, os.Getpid())
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(temporaryPath, data, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(temporaryPath, 0o600); err != nil {
		return err
	}
	return os.Rename(temporaryPath, authPath)
}
