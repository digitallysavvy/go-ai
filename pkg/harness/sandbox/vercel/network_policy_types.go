package vercel

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// NetworkPolicy is the Go equivalent of @vercel/sandbox's public `NetworkPolicy`
// type: either the bare string "allow-all"/"deny-all", or an object with an
// `allow` list (bare hostnames) or map (hostname -> rules) plus optional
// `subnets`. Mirrors the shape produced/consumed by
// packages/sandbox-vercel/src/vercel-network-policy-manager.ts.
type NetworkPolicy struct {
	// Mode is "allow-all" or "deny-all" when this value is the bare string
	// form. Empty otherwise.
	Mode string

	// AllowList is set when `allow` is an array of hostnames (no rules).
	AllowList []string

	// AllowMap is set when `allow` is a map of hostname -> rules.
	AllowMap map[string][]PolicyRule

	Subnets *PolicySubnets

	// apiUnmapped lists the non-empty members of an API-shaped
	// (`{mode: "custom", ...}`) policy that this type cannot represent, such
	// as injection or response rules. The policy manager refuses to
	// recompose such a policy (it would silently drop them).
	apiUnmapped []string
}

// AllowAllNetworkPolicy is the bare "allow-all" policy value.
func AllowAllNetworkPolicy() NetworkPolicy { return NetworkPolicy{Mode: "allow-all"} }

// DenyAllNetworkPolicy is the bare "deny-all" policy value.
func DenyAllNetworkPolicy() NetworkPolicy { return NetworkPolicy{Mode: "deny-all"} }

// IsZero reports whether p is the empty/unset value.
func (p NetworkPolicy) IsZero() bool {
	return p.Mode == "" && p.AllowList == nil && p.AllowMap == nil && p.Subnets == nil
}

// PolicySubnets is the CIDR allow/deny lists on a NetworkPolicy.
type PolicySubnets struct {
	Allow []string `json:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty"`
}

// PolicyRuleMatcher matches a string exactly, by prefix, or by regex.
type PolicyRuleMatcher struct {
	Exact      string `json:"exact,omitempty"`
	StartsWith string `json:"startsWith,omitempty"`
	Regex      string `json:"regex,omitempty"`
}

// MarshalJSON emits exactly one of exact/startsWith/regex, like the TS union
// type. An empty Exact is kept on the wire ({"exact":""}) when neither
// StartsWith nor Regex is set, instead of being dropped by omitempty.
func (m PolicyRuleMatcher) MarshalJSON() ([]byte, error) {
	switch {
	case m.Regex != "" && m.Exact == "" && m.StartsWith == "":
		return json.Marshal(struct {
			Regex string `json:"regex"`
		}{m.Regex})
	case m.StartsWith != "" && m.Exact == "" && m.Regex == "":
		return json.Marshal(struct {
			StartsWith string `json:"startsWith"`
		}{m.StartsWith})
	case m.Exact == "" && m.StartsWith == "" && m.Regex == "":
		return []byte(`{"exact":""}`), nil
	}
	type plain PolicyRuleMatcher
	return json.Marshal(plain(m))
}

// PolicyKeyValueMatcher matches a header or query parameter.
type PolicyKeyValueMatcher struct {
	Key   *PolicyRuleMatcher `json:"key,omitempty"`
	Value *PolicyRuleMatcher `json:"value,omitempty"`
}

// PolicyRuleMatch selects outbound requests a PolicyRule applies to.
type PolicyRuleMatch struct {
	Path        *PolicyRuleMatcher      `json:"path,omitempty"`
	Method      []string                `json:"method,omitempty"`
	QueryString []PolicyKeyValueMatcher `json:"queryString,omitempty"`
	Headers     []PolicyKeyValueMatcher `json:"headers,omitempty"`
}

// PolicyTransform sets headers on a matching request.
type PolicyTransform struct {
	Headers map[string]string `json:"headers,omitempty"`
}

// PolicyRule is one entry in a host's rule list: either a header transform or
// a forward rule (mutually exclusive, mirrors the Vercel API contract).
type PolicyRule struct {
	Match      *PolicyRuleMatch  `json:"match,omitempty"`
	Transform  []PolicyTransform `json:"transform,omitempty"`
	ForwardURL string            `json:"forwardURL,omitempty"`
	// Response is a native response (deny) rule, e.g. {"statusCode": 403}:
	// the proxy answers matching requests itself. It is kept as decoded so a
	// policy read back from Vercel round-trips; the policy manager refuses
	// to mutate a policy that has one (TS 44a4261e52), because recomposing
	// it would let previously denied requests through.
	Response map[string]any `json:"response,omitempty"`
}

// MarshalJSON encodes NetworkPolicy as either a bare mode string or an
// {allow, subnets} object, matching the @vercel/sandbox wire/public shape.
func (p NetworkPolicy) MarshalJSON() ([]byte, error) {
	if p.Mode != "" {
		return json.Marshal(p.Mode)
	}
	obj := map[string]interface{}{}
	switch {
	case p.AllowMap != nil:
		obj["allow"] = p.AllowMap
	case p.AllowList != nil:
		obj["allow"] = p.AllowList
	}
	if p.Subnets != nil {
		obj["subnets"] = p.Subnets
	}
	return jsonstr.Marshal(obj)
}

// apiNetworkPolicy is a NetworkPolicy as the Sandbox API request bodies
// carry it: @vercel/sandbox 3.9.0 `toAPINetworkPolicy` sends the bare
// "allow-all" / "deny-all" modes as {"mode": ...} and every other policy in
// its public {allow, subnets} shape (createSandbox, updateSandbox,
// updateNetworkPolicy).
type apiNetworkPolicy NetworkPolicy

// toAPINetworkPolicy returns p's request form, or nil for a nil policy.
func toAPINetworkPolicy(p *NetworkPolicy) *apiNetworkPolicy {
	if p == nil {
		return nil
	}
	a := apiNetworkPolicy(*p)
	return &a
}

// MarshalJSON implements the 3.9.0 `toAPINetworkPolicy` mapping.
func (p apiNetworkPolicy) MarshalJSON() ([]byte, error) {
	if p.Mode == "allow-all" || p.Mode == "deny-all" {
		return json.Marshal(struct {
			Mode string `json:"mode"`
		}{p.Mode})
	}
	return NetworkPolicy(p).MarshalJSON()
}

// UnmarshalJSON decodes either request form (used by the test server).
func (p *apiNetworkPolicy) UnmarshalJSON(data []byte) error {
	return (*NetworkPolicy)(p).UnmarshalJSON(data)
}

// UnmarshalJSON decodes either a bare mode string or an {allow, subnets}
// object.
func (p *NetworkPolicy) UnmarshalJSON(data []byte) error {
	var mode string
	if err := json.Unmarshal(data, &mode); err == nil {
		*p = NetworkPolicy{Mode: mode}
		return nil
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(data, &members); err != nil {
		return err
	}
	if rawMode, ok := members["mode"]; ok {
		return p.unmarshalAPIShape(rawMode, members)
	}
	var obj struct {
		Allow   json.RawMessage `json:"allow"`
		Subnets *PolicySubnets  `json:"subnets"`
	}
	// The API's policy is parsed by its zod schema: exact keys (jsonexact).
	if err := jsonexact.UnmarshalPlain(data, &obj); err != nil { // plain JSON.parse (base-client.js parse)
		return err
	}
	out := NetworkPolicy{Subnets: obj.Subnets}
	if len(obj.Allow) > 0 {
		var list []string
		if err := json.Unmarshal(obj.Allow, &list); err == nil {
			out.AllowList = list
		} else {
			var m map[string][]PolicyRule
			if err := jsonexact.UnmarshalPlain(obj.Allow, &m); err != nil {
				return err
			}
			out.AllowMap = m
		}
	}
	*p = out
	return nil
}

// unmarshalAPIShape decodes the Vercel Sandbox API's own session policy
// shape, `{mode, allowedDomains, allowedCIDRs, deniedCIDRs, ...}` (the
// recorded @vercel/sandbox 3.9.0 create response in TS
// network-policy-response-rules-readback.json), into the public shape the
// native SDK reads back: allow-all / deny-all, or `{allow: domains}` plus
// subnets. Before this, a custom API-shaped policy decoded to the zero value
// and was read as allow-all, so a later mutation replaced it with an
// allow-all policy. Any other non-empty member is recorded in apiUnmapped.
func (p *NetworkPolicy) unmarshalAPIShape(rawMode json.RawMessage, members map[string]json.RawMessage) error {
	var mode string
	if err := json.Unmarshal(rawMode, &mode); err != nil {
		return err
	}
	out := NetworkPolicy{}
	switch mode {
	case "allow-all", "deny-all":
		out.Mode = mode
	default:
		out.AllowList = []string{}
		if raw, ok := members["allowedDomains"]; ok && !isJSONNullOrEmpty(raw) {
			if err := json.Unmarshal(raw, &out.AllowList); err != nil {
				return err
			}
		}
		var subnets PolicySubnets
		for key, dst := range map[string]*[]string{"allowedCIDRs": &subnets.Allow, "deniedCIDRs": &subnets.Deny} {
			if raw, ok := members[key]; ok && !isJSONNullOrEmpty(raw) {
				if err := json.Unmarshal(raw, dst); err != nil {
					return err
				}
			}
		}
		if subnets.Allow != nil || subnets.Deny != nil {
			out.Subnets = &subnets
		}
		if mode != "custom" {
			out.apiUnmapped = append(out.apiUnmapped, "mode")
		}
	}
	keys := make([]string, 0, len(members))
	for key := range members {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		switch key {
		case "mode", "allowedDomains", "allowedCIDRs", "deniedCIDRs":
			continue
		}
		if !isJSONNullOrEmpty(members[key]) {
			out.apiUnmapped = append(out.apiUnmapped, key)
		}
	}
	*p = out
	return nil
}

// isJSONNullOrEmpty reports whether raw is null, [] or {}.
func isJSONNullOrEmpty(raw json.RawMessage) bool {
	switch strings.TrimSpace(string(raw)) {
	case "", "null", "[]", "{}":
		return true
	}
	return false
}

// Equal reports whether p and other serialize identically. encoding/json
// sorts map keys, so byte-for-byte comparison of the marshaled form is a
// correct structural equality check (mirrors the TS stableSerialize/
// areNetworkPoliciesEqual pair).
func (p NetworkPolicy) Equal(other NetworkPolicy) bool {
	a, errA := jsonstr.Marshal(p)
	b, errB := jsonstr.Marshal(other)
	if errA != nil || errB != nil {
		return false
	}
	return string(a) == string(b)
}

func clonePolicyRuleMatch(m *PolicyRuleMatch) *PolicyRuleMatch {
	if m == nil {
		return nil
	}
	clone := &PolicyRuleMatch{Path: m.Path}
	if m.Method != nil {
		clone.Method = append([]string(nil), m.Method...)
	}
	if m.QueryString != nil {
		clone.QueryString = append([]PolicyKeyValueMatcher(nil), m.QueryString...)
	}
	if m.Headers != nil {
		clone.Headers = append([]PolicyKeyValueMatcher(nil), m.Headers...)
	}
	return clone
}

func cloneStringSlice(s []string) []string {
	if s == nil {
		return nil
	}
	return append([]string(nil), s...)
}
