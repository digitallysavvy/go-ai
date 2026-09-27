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
)

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
