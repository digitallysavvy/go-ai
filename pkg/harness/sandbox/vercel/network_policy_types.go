package vercel

import "encoding/json"

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
	return json.Marshal(obj)
}

// UnmarshalJSON decodes either a bare mode string or an {allow, subnets}
// object.
func (p *NetworkPolicy) UnmarshalJSON(data []byte) error {
	var mode string
	if err := json.Unmarshal(data, &mode); err == nil {
		*p = NetworkPolicy{Mode: mode}
		return nil
	}
	var obj struct {
		Allow   json.RawMessage `json:"allow"`
		Subnets *PolicySubnets  `json:"subnets"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	out := NetworkPolicy{Subnets: obj.Subnets}
	if len(obj.Allow) > 0 {
		var list []string
		if err := json.Unmarshal(obj.Allow, &list); err == nil {
			out.AllowList = list
		} else {
			var m map[string][]PolicyRule
			if err := json.Unmarshal(obj.Allow, &m); err != nil {
				return err
			}
			out.AllowMap = m
		}
	}
	*p = out
	return nil
}

// Equal reports whether p and other serialize identically. encoding/json
// sorts map keys, so byte-for-byte comparison of the marshaled form is a
// correct structural equality check (mirrors the TS stableSerialize/
// areNetworkPoliciesEqual pair).
func (p NetworkPolicy) Equal(other NetworkPolicy) bool {
	a, errA := json.Marshal(p)
	b, errB := json.Marshal(other)
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
