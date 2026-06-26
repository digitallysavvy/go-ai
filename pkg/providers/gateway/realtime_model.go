package gateway

import (
	"context"
	"net/url"
	"strings"
)

type RealtimeClientSecretOptions struct {
	ExpiresAfterSeconds *int
	SessionConfig       interface{}
}

type RealtimeClientSecretResult struct {
	Token     string
	URL       string
	ExpiresAt *int64
}

type RealtimeWebSocketConfig struct {
	URL       string
	Protocols []string
}

type RealtimeModel struct {
	provider *Provider
	modelID  string
}

func (p *Provider) ExperimentalRealtime(modelID string) *RealtimeModel {
	return &RealtimeModel{provider: p, modelID: modelID}
}

func (p *Provider) GetRealtimeToken(ctx context.Context, modelID string, opts *RealtimeClientSecretOptions) (*RealtimeClientSecretResult, error) {
	return p.ExperimentalRealtime(modelID).DoCreateClientSecret(ctx, opts)
}

func (m *RealtimeModel) SpecificationVersion() string { return "v4" }
func (m *RealtimeModel) Provider() string             { return "gateway.realtime" }
func (m *RealtimeModel) ModelID() string              { return m.modelID }

func (m *RealtimeModel) DoCreateClientSecret(ctx context.Context, opts *RealtimeClientSecretOptions) (*RealtimeClientSecretResult, error) {
	params := MintRealtimeClientSecretParams{ModelID: m.modelID}
	if opts != nil && opts.ExpiresAfterSeconds != nil {
		params.ExpiresAfterSeconds = opts.ExpiresAfterSeconds
	}
	secret, err := m.provider.MintRealtimeClientSecret(ctx, params)
	if err != nil {
		return nil, err
	}
	return &RealtimeClientSecretResult{
		Token:     secret.Token,
		URL:       ToGatewayRealtimeURL(m.provider.baseURL, m.modelID),
		ExpiresAt: secret.ExpiresAt,
	}, nil
}

func (m *RealtimeModel) GetWebSocketConfig(token string, url string) RealtimeWebSocketConfig {
	return RealtimeWebSocketConfig{
		URL:       url,
		Protocols: GetGatewayRealtimeProtocols(token, m.provider.config.TeamIDOrSlug),
	}
}

func (m *RealtimeModel) ParseServerEvent(raw interface{}) interface{} {
	return raw
}

func (m *RealtimeModel) SerializeClientEvent(event interface{}) interface{} {
	return event
}

func (m *RealtimeModel) BuildSessionConfig(config interface{}) interface{} {
	return config
}

func ToGatewayRealtimeURL(baseURL string, modelID string) string {
	u, err := url.Parse(strings.Replace(baseURL, "http", "ws", 1) + "/realtime-model")
	if err != nil {
		return ""
	}
	query := u.Query()
	query.Set("ai-model-id", modelID)
	u.RawQuery = query.Encode()
	return u.String()
}
