package gateway

import (
	"encoding/base64"
	"strings"
)

const (
	GatewayRealtimeSubprotocol = "ai-gateway-realtime.v1"

	// GatewayTranscriptionSubprotocol is offered on every streaming
	// transcription handshake (same negotiation purpose as
	// GatewayRealtimeSubprotocol), TS GATEWAY_TRANSCRIPTION_SUBPROTOCOL.
	GatewayTranscriptionSubprotocol = "ai-gateway-transcription.v1"

	GatewayAuthSubprotocolPrefix = "ai-gateway-auth."
	GatewayTeamSubprotocolPrefix = "ai-gateway-team."

	// vercelAIGatewayTeamHeader is the header key that carries the Vercel
	// team ID or slug, TS VERCEL_AI_GATEWAY_TEAM_HEADER
	// (gateway-headers.ts). This is the single source of truth for the team
	// scope a WebSocket handshake's subprotocol encodes: both
	// transcription_stream.go and realtime_model.go read it from the
	// resolved header set (config + per-call) via gatewayTeamFromHeaders,
	// rather than a static config field, so a per-call header override takes
	// effect exactly like it does for every other Gateway request.
	vercelAIGatewayTeamHeader = "x-vercel-ai-gateway-team"
)

// gatewayTeamFromHeaders extracts the Vercel team ID or slug from an already
// merged header map (provider-level headers combined with any per-call
// override), looking the key up case-insensitively since MergeHeaders does
// not normalize casing and callers may pass arbitrary casing.
func gatewayTeamFromHeaders(headers map[string]string) string {
	for k, v := range headers {
		if strings.EqualFold(k, vercelAIGatewayTeamHeader) {
			return v
		}
	}
	return ""
}

func GetGatewayRealtimeProtocols(token string, teamIDOrSlug string) []string {
	return buildGatewayProtocols(GatewayRealtimeSubprotocol, token, teamIDOrSlug)
}

// GetGatewayTranscriptionProtocols is GetGatewayRealtimeProtocols with the
// streaming transcription marker subprotocol (TS
// getGatewayTranscriptionProtocols).
func GetGatewayTranscriptionProtocols(token string, teamIDOrSlug string) []string {
	return buildGatewayProtocols(GatewayTranscriptionSubprotocol, token, teamIDOrSlug)
}

func buildGatewayProtocols(marker, token, teamIDOrSlug string) []string {
	protocols := []string{
		marker,
		GatewayAuthSubprotocolPrefix + token,
	}
	if teamIDOrSlug != "" {
		protocols = append(protocols, GatewayTeamSubprotocolPrefix+encodeSubprotocolValue(teamIDOrSlug))
	}
	return protocols
}

func GetGatewayRealtimeAuthToken(secWebSocketProtocol string) string {
	protocol := findProtocol(secWebSocketProtocol, GatewayAuthSubprotocolPrefix)
	if protocol == "" {
		return ""
	}
	return strings.TrimPrefix(protocol, GatewayAuthSubprotocolPrefix)
}

func GetGatewayRealtimeTeamIDOrSlug(secWebSocketProtocol string) string {
	protocol := findProtocol(secWebSocketProtocol, GatewayTeamSubprotocolPrefix)
	if protocol == "" {
		return ""
	}
	decoded, err := decodeSubprotocolValue(strings.TrimPrefix(protocol, GatewayTeamSubprotocolPrefix))
	if err != nil {
		return ""
	}
	return decoded
}

func findProtocol(header string, prefix string) string {
	for _, part := range strings.Split(header, ",") {
		protocol := strings.TrimSpace(part)
		if strings.HasPrefix(protocol, prefix) {
			return protocol
		}
	}
	return ""
}

func encodeSubprotocolValue(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func decodeSubprotocolValue(value string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}
