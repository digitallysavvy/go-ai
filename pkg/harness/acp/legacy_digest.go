package acp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// v0.5.2 persisted lifecycle state carries an authentication profile digest and
// turn start configuration fingerprints that were computed with Go's default
// json.Marshal (byte-ordered map keys, HTML escaping, mapping types encoded by
// Go field name, a compatibility struct without json tags) rather than the TS
// shape used now. A state persisted by v0.5.2 must keep starting and
// continuing, so the digest and fingerprint are also computed in the v0.5.2
// format and either one is accepted. New digests always use the TS format.

type legacyCompat struct {
	Type             string
	Mode             string
	GatewayEnv       map[string]any
	CredentialSource gatewayCredentialSource
	BaseURL          string
}

type legacyPermissionTarget struct {
	Type     PermissionModeTargetType
	ModeID   string
	ConfigID string
	Value    any
}

type legacyPermissionMapping struct {
	AllowReads *legacyPermissionTarget
	AllowEdits *legacyPermissionTarget
	AllowAll   *legacyPermissionTarget
}

type legacyInstructionMapping struct {
	Type     InstructionMappingType
	Path     []string
	Variable string
	FilePath string
}

type legacyOutputSchemaMapping struct{ Path []string }

type legacyModelMapping struct {
	Type ModelMappingType
	Path string
}

func legacyDigestOf(payload any) string {
	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// legacyAuthenticationDigest is the v0.5.2 authentication profile digest.
func legacyAuthenticationDigest(authentication *Authentication, compat *providerAuthenticationCompatibility) string {
	payload := map[string]any{"authentication": nil, "providerAuthentication": nil}
	if authentication != nil {
		payload["authentication"] = map[string]any{
			"methodId": authentication.MethodID, "meta": authentication.Meta, "clientCapabilities": authentication.ClientCapabilities,
		}
	}
	if compat != nil {
		baseURL := compat.BaseURL
		if compat.Type == "ai-gateway" && baseURL == "" {
			baseURL = defaultAIGatewayBaseURL
		}
		payload["providerAuthentication"] = legacyCompat{
			Type: compat.Type, Mode: compat.Mode, GatewayEnv: compat.GatewayEnv,
			CredentialSource: compat.CredentialSource, BaseURL: baseURL,
		}
	}
	return legacyDigestOf(payload)
}

func legacyTarget(t *PermissionModeTarget) *legacyPermissionTarget {
	if t == nil {
		return nil
	}
	return &legacyPermissionTarget{Type: t.Type, ModeID: t.ModeID, ConfigID: t.ConfigID, Value: t.Value}
}

// legacyConfigurationFingerprint is the v0.5.2 turn start configuration
// fingerprint of the same input.
func legacyConfigurationFingerprint(in createTurnStartConfigInput) string {
	profile := in.AuthenticationProfile
	if profile.legacyDigest != "" {
		profile.Digest = profile.legacyDigest
	}
	var permission *legacyPermissionMapping
	if m := in.PermissionModeMapping; m != nil {
		permission = &legacyPermissionMapping{AllowReads: legacyTarget(m.AllowReads), AllowEdits: legacyTarget(m.AllowEdits), AllowAll: legacyTarget(m.AllowAll)}
	}
	payload := map[string]any{
		"authenticationProfile": profile,
		"sessionMeta":           in.SessionMeta,
		"modelMapping":          legacyModelMapping{Type: in.ModelMapping.Type, Path: in.ModelMapping.Path},
		"builtinTools":          in.BuiltinTools,
		"permissionModeMapping": permission,
		"mcpServers":            in.MCPServers,
	}
	if m := in.InstructionMapping; m != nil {
		payload["instructionMapping"] = legacyInstructionMapping{Type: m.Type, Path: m.Path, Variable: m.Variable, FilePath: m.FilePath}
	}
	if m := in.OutputSchemaMapping; m != nil {
		payload["outputSchemaMapping"] = legacyOutputSchemaMapping{Path: m.Path}
	}
	return legacyDigestOf(payload)
}
