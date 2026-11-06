package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

type oauthFieldKind int

const (
	oauthFieldString  oauthFieldKind = iota
	oauthFieldURL                    // z.string().url()
	oauthFieldSafeURL                // SafeUrlSchema
	oauthFieldStringArray
	oauthFieldBool
	oauthFieldSafeURLArray
	oauthFieldNumber
	oauthFieldApplicationType // "native" | "web"
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
	oauthTokenFieldRules = []oauthFieldRule{
		{"id_token", oauthFieldString},
		{"expires_in", oauthFieldNumber},
		{"scope", oauthFieldString},
		{"refresh_token", oauthFieldString},
		{"issuer", oauthFieldSafeURL},
		{"authorization_server", oauthFieldSafeURL},
		{"token_endpoint", oauthFieldSafeURL},
	}
	oauthRegistrationFieldRules = []oauthFieldRule{
		{"client_secret", oauthFieldString},
		{"client_id_issued_at", oauthFieldNumber},
		{"client_secret_expires_at", oauthFieldNumber},
		{"application_type", oauthFieldApplicationType},
		{"token_endpoint_auth_method", oauthFieldString},
		{"grant_types", oauthFieldStringArray},
		{"response_types", oauthFieldStringArray},
		{"client_name", oauthFieldString},
		{"client_uri", oauthFieldSafeURL},
		{"logo_uri", oauthFieldSafeURL},
		{"scope", oauthFieldString},
		{"contacts", oauthFieldStringArray},
		{"tos_uri", oauthFieldSafeURL},
		{"policy_uri", oauthFieldString},
		{"jwks_uri", oauthFieldSafeURL},
		{"software_id", oauthFieldString},
		{"software_version", oauthFieldString},
		{"software_statement", oauthFieldString},
		{"issuer", oauthFieldSafeURL},
		{"authorization_server", oauthFieldSafeURL},
		{"token_endpoint", oauthFieldSafeURL},
	}
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
		{"response_types_supported", oauthFieldStringArray},
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
		{"subject_types_supported", oauthFieldStringArray},
		{"id_token_signing_alg_values_supported", oauthFieldStringArray},
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
	case oauthFieldNumber:
		var v float64
		return json.Unmarshal(raw, &v) == nil && len(raw) > 0 && raw[0] != 'n'
	case oauthFieldApplicationType:
		var v string
		return isJSONString(raw) && json.Unmarshal(raw, &v) == nil && (v == "native" || v == "web")
	case oauthFieldBool:
		return string(raw) == "true" || string(raw) == "false"
	}
	return false
}

// integerizeOAuthNumbers rewrites the named fields, when they hold a JSON
// number with a fraction or exponent (3600.0, 3599.5, 3.6e3), as an integer so
// they decode into the integer fields of the Go structs. TS z.number() accepts
// any finite number, so such a response is valid there.
func integerizeOAuthNumbers(fields map[string]json.RawMessage, keys ...string) {
	for _, key := range keys {
		raw, ok := fields[key]
		if !ok {
			continue
		}
		raw = bytes.TrimSpace(raw)
		if !bytes.ContainsAny(raw, ".eE") {
			continue
		}
		var f float64
		if json.Unmarshal(raw, &f) != nil {
			continue
		}
		switch {
		case f >= math.MaxInt64:
			f = math.MaxInt64
		case f <= math.MinInt64:
			f = math.MinInt64
		}
		fields[key] = json.RawMessage(strconv.FormatInt(int64(f), 10))
	}
}

// decodeOAuthFields unmarshals the (possibly integerized) fields into v.
func decodeOAuthFields(fields map[string]json.RawMessage, v interface{}) error {
	data, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
