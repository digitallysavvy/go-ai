package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type oauthFieldKind int

const (
	oauthFieldString  oauthFieldKind = iota
	oauthFieldURL                    // z.string().url()
	oauthFieldSafeURL                // SafeUrlSchema
	oauthFieldStringArray
	oauthFieldBool
	oauthFieldSafeURLArray
)

type oauthFieldRule struct {
	name string
	kind oauthFieldKind
}

// Optional-field rules mirroring the TS Zod schemas in oauth-types.ts. Fields
// the Go structs also decode are rechecked here so that a JSON null (which
// Zod's .optional() rejects but encoding/json accepts) and the fields the
// structs do not carry are validated like TS.
var (
	oauthPRMFieldRules = []oauthFieldRule{
		{"authorization_servers", oauthFieldSafeURLArray},
		{"jwks_uri", oauthFieldURL},
		{"scopes_supported", oauthFieldStringArray},
		{"bearer_methods_supported", oauthFieldStringArray},
		{"resource_signing_alg_values_supported", oauthFieldStringArray},
		{"resource_name", oauthFieldString},
		{"resource_documentation", oauthFieldString},
		{"resource_policy_uri", oauthFieldURL},
		{"resource_tos_uri", oauthFieldURL},
		{"tls_client_certificate_bound_access_tokens", oauthFieldBool},
		{"authorization_details_types_supported", oauthFieldStringArray},
		{"dpop_signing_alg_values_supported", oauthFieldStringArray},
		{"dpop_bound_access_tokens_required", oauthFieldBool},
	}
	oauthASCommonFieldRules = []oauthFieldRule{
		{"registration_endpoint", oauthFieldSafeURL},
		{"authorization_response_iss_parameter_supported", oauthFieldBool},
		{"client_id_metadata_document_supported", oauthFieldBool},
		{"scopes_supported", oauthFieldStringArray},
		{"grant_types_supported", oauthFieldStringArray},
		{"code_challenge_methods_supported", oauthFieldStringArray},
		{"token_endpoint_auth_methods_supported", oauthFieldStringArray},
	}
	oauthASOAuthFieldRules = []oauthFieldRule{
		{"token_endpoint_auth_signing_alg_values_supported", oauthFieldStringArray},
	}
	oauthASOIDCFieldRules = []oauthFieldRule{
		{"userinfo_endpoint", oauthFieldSafeURL},
		{"claims_supported", oauthFieldStringArray},
	}
)

// checkOAuthOptionalFields validates every present field named by rules the
// way the TS Zod schema would: JSON null is rejected, the type must match and
// URL fields must parse (SafeUrlSchema also rejects javascript:, data: and
// vbscript:). Absent fields are fine.
func checkOAuthOptionalFields(fields map[string]json.RawMessage, rules ...[]oauthFieldRule) error {
	for _, group := range rules {
		for _, rule := range group {
			raw, present := fields[rule.name]
			if !present {
				continue
			}
			raw = bytes.TrimSpace(raw)
			if !oauthFieldMatches(raw, rule.kind) {
				return fmt.Errorf("%s is invalid", rule.name)
			}
		}
	}
	return nil
}

func oauthFieldMatches(raw json.RawMessage, kind oauthFieldKind) bool {
	switch kind {
	case oauthFieldString:
		return isJSONString(raw)
	case oauthFieldURL, oauthFieldSafeURL:
		var v string
		if !isJSONString(raw) || json.Unmarshal(raw, &v) != nil {
			return false
		}
		if kind == oauthFieldURL {
			return isAbsoluteOAuthURL(v)
		}
		return isSafeOAuthURL(v)
	case oauthFieldStringArray, oauthFieldSafeURLArray:
		var v []json.RawMessage
		if !isJSONArray(raw) || json.Unmarshal(raw, &v) != nil {
			return false
		}
		for _, item := range v {
			if kind == oauthFieldSafeURLArray {
				if !oauthFieldMatches(bytes.TrimSpace(item), oauthFieldSafeURL) {
					return false
				}
			} else if !isJSONString(item) {
				return false
			}
		}
		return true
	case oauthFieldBool:
		return string(raw) == "true" || string(raw) == "false"
	}
	return false
}
