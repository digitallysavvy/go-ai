package vercel

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// PolicySandbox is the minimal surface VercelNetworkPolicyManager needs from
// the underlying sandbox: read the currently-effective network policy and
// replace it wholesale. *nativeSandbox implements this.
type PolicySandbox interface {
	// CurrentNetworkPolicy returns the sandbox's current session network
	// policy, defaulting to AllowAllNetworkPolicy() when unset (mirrors TS
	// `sandbox.currentSession().networkPolicy ?? 'allow-all'`).
	CurrentNetworkPolicy() NetworkPolicy
	// UpdateNetworkPolicy replaces the sandbox's network policy.
	UpdateNetworkPolicy(ctx context.Context, policy NetworkPolicy) error
}

// PolicyManager owns the complete Vercel Sandbox network-policy value for one
// live session. Vercel combines network access, request transformations, and
// forwarding in a single structure, so every mutation is composed here before
// replacing that structure through sandbox.UpdateNetworkPolicy.
//
// Ports packages/sandbox-vercel/src/vercel-network-policy-manager.ts
// (VercelNetworkPolicyManager) exactly, including its FIFO mutation queue and
// redacted-transformation rehydration rules.
type PolicyManager struct {
	sandbox PolicySandbox

	mu    sync.Mutex
	state *managedPolicyState
	// queue serializes mutations: each call chains onto the previous one so
	// the inspect/compose/update/commit sequence runs in invocation order,
	// while a failed operation does not poison later ones.
	queue chan struct{}
}

type managedPolicyState struct {
	accessPolicy           networkAccessPolicy
	requestTransformations []harness.RequestTransformation
	forwardRules           []forwardRule
	effectivePolicy        NetworkPolicy
}

type networkAccessPolicy struct {
	mode         string // "allow-all", "deny-all", "custom"
	allowedHosts []string
	allowedCIDRs []string
	deniedCIDRs  []string
}

type forwardRule struct {
	host       string
	match      *PolicyRuleMatch
	forwardURL string
}

// NewPolicyManager constructs a manager bound to sandbox.
func NewPolicyManager(sandbox PolicySandbox) *PolicyManager {
	m := &PolicyManager{sandbox: sandbox, queue: make(chan struct{}, 1)}
	m.queue <- struct{}{}
	return m
}

// enqueue runs op strictly after every previously-enqueued op has finished
// (success or failure), and returns op's own error.
func (m *PolicyManager) enqueue(op func() error) error {
	<-m.queue
	defer func() { m.queue <- struct{}{} }()
	return op()
}

type currentPolicyInspection struct {
	accessPolicy               networkAccessPolicy
	forwardRules               []forwardRule
	requestTransformationHosts []string
	currentPolicy              NetworkPolicy
}

func (m *PolicyManager) inspectPolicy() currentPolicyInspection {
	m.mu.Lock()
	state := m.state
	m.mu.Unlock()
	if state != nil {
		return currentPolicyInspection{
			accessPolicy:  state.accessPolicy,
			forwardRules:  state.forwardRules,
			currentPolicy: state.effectivePolicy,
		}
	}
	policy := m.sandbox.CurrentNetworkPolicy()
	if policy.IsZero() {
		policy = AllowAllNetworkPolicy()
	}
	return inspectCurrentPolicy(policy)
}

// SetNetworkPolicy replaces the access policy (mirrors setNetworkPolicy).
func (m *PolicyManager) SetNetworkPolicy(ctx context.Context, policy harness.NetworkPolicy) error {
	return m.enqueue(func() error {
		inspection := m.inspectPolicy()
		m.mu.Lock()
		hasState := m.state != nil
		m.mu.Unlock()
		if !hasState && len(inspection.requestTransformationHosts) > 0 {
			return newPolicyConflictError("Cannot set the network policy because the current Vercel Sandbox policy contains request transformations whose redacted values cannot be preserved safely. Replace them explicitly with setRequestTransformations() or rehydrate them with addRequestTransformations() first.")
		}
		accessPolicy, err := toNetworkAccessPolicy(policy)
		if err != nil {
			return err
		}
		var existingTransformations []harness.RequestTransformation
		m.mu.Lock()
		if m.state != nil {
			existingTransformations = cloneRequestTransformations(m.state.requestTransformations)
		}
		m.mu.Unlock()
		return m.applyState(ctx, accessPolicy, existingTransformations, cloneForwardRules(inspection.forwardRules), inspection.currentPolicy)
	})
}

// SetRequestTransformations replaces the complete request-transformation set.
func (m *PolicyManager) SetRequestTransformations(ctx context.Context, transformations []harness.RequestTransformation) error {
	return m.enqueue(func() error {
		inspection := m.inspectPolicy()
		return m.applyState(ctx, inspection.accessPolicy, cloneRequestTransformations(transformations), cloneForwardRules(inspection.forwardRules), inspection.currentPolicy)
	})
}

// AddRequestTransformations adds request-transformation rules without
// replacing existing ones.
func (m *PolicyManager) AddRequestTransformations(ctx context.Context, transformations []harness.RequestTransformation) error {
	return m.enqueue(func() error {
		inspection := m.inspectPolicy()
		incoming := cloneRequestTransformations(transformations)

		m.mu.Lock()
		hasState := m.state != nil
		m.mu.Unlock()

		if !hasState {
			incomingPolicy := composeNetworkPolicy(inspection.accessPolicy, incoming, inspection.forwardRules)
			incomingHosts := getRequestTransformationHosts(incomingPolicy)
			if !isHostSetSubset(inspection.requestTransformationHosts, incomingHosts) {
				return newPolicyConflictError("Cannot add request transformations because the current Vercel Sandbox policy contains request transformations that cannot be attributed to this call. Their header values are redacted, so preserving them safely is not possible.")
			}
		}

		var merged []harness.RequestTransformation
		m.mu.Lock()
		if m.state == nil {
			merged = incoming
		} else {
			merged = mergeRequestTransformations(m.state.requestTransformations, incoming)
		}
		m.mu.Unlock()

		return m.applyState(ctx, inspection.accessPolicy, merged, cloneForwardRules(inspection.forwardRules), inspection.currentPolicy)
	})
}

func (m *PolicyManager) applyState(ctx context.Context, accessPolicy networkAccessPolicy, requestTransformations []harness.RequestTransformation, forwardRules []forwardRule, currentPolicy NetworkPolicy) error {
	effectivePolicy := composeNetworkPolicy(accessPolicy, requestTransformations, forwardRules)
	if !currentPolicy.Equal(effectivePolicy) {
		if err := m.sandbox.UpdateNetworkPolicy(ctx, effectivePolicy); err != nil {
			return err
		}
	}
	m.mu.Lock()
	m.state = &managedPolicyState{
		accessPolicy:           accessPolicy,
		requestTransformations: cloneRequestTransformations(requestTransformations),
		forwardRules:           cloneForwardRules(forwardRules),
		effectivePolicy:        effectivePolicy,
	}
	m.mu.Unlock()
	return nil
}

func newPolicyConflictError(message string) error {
	return harness.NewCapabilityUnsupportedError(message, vercelProviderID, nil)
}

// --- inspection of the current (unmanaged) policy ---

func inspectCurrentPolicy(policy NetworkPolicy) currentPolicyInspection {
	if policy.Mode == "allow-all" || policy.Mode == "deny-all" {
		return currentPolicyInspection{
			accessPolicy:  networkAccessPolicy{mode: policy.Mode},
			currentPolicy: policy,
		}
	}

	var allowedHosts []string
	if policy.AllowList != nil {
		allowedHosts = cloneStringSlice(policy.AllowList)
	} else {
		for host := range policy.AllowMap {
			allowedHosts = append(allowedHosts, host)
		}
		sort.Strings(allowedHosts)
	}

	var forwardRules []forwardRule
	transformationHostSet := map[string]struct{}{}

	if policy.AllowMap != nil {
		hosts := make([]string, 0, len(policy.AllowMap))
		for host := range policy.AllowMap {
			hosts = append(hosts, host)
		}
		sort.Strings(hosts)
		for _, host := range hosts {
			for _, rule := range policy.AllowMap[host] {
				if hasRequestTransformation(rule) {
					transformationHostSet[strings.ToLower(host)] = struct{}{}
				}
				if rule.ForwardURL != "" {
					forwardRules = append(forwardRules, forwardRule{
						host:       host,
						match:      clonePolicyRuleMatch(rule.Match),
						forwardURL: rule.ForwardURL,
					})
				}
			}
		}
	}

	hosts := make([]string, 0, len(transformationHostSet))
	for h := range transformationHostSet {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)

	allowedCIDRs, deniedCIDRs := []string{}, []string{}
	if policy.Subnets != nil {
		allowedCIDRs = cloneStringSlice(policy.Subnets.Allow)
		deniedCIDRs = cloneStringSlice(policy.Subnets.Deny)
	}

	return currentPolicyInspection{
		accessPolicy: networkAccessPolicy{
			mode:         "custom",
			allowedHosts: minimizeHostPatterns(allowedHosts),
			allowedCIDRs: allowedCIDRs,
			deniedCIDRs:  deniedCIDRs,
		},
		forwardRules:               forwardRules,
		requestTransformationHosts: hosts,
		currentPolicy:              policy,
	}
}

func toNetworkAccessPolicy(policy harness.NetworkPolicy) (networkAccessPolicy, error) {
	switch policy.Mode {
	case harness.NetworkPolicyAllowAll:
		return networkAccessPolicy{mode: "allow-all"}, nil
	case harness.NetworkPolicyDenyAll:
		return networkAccessPolicy{mode: "deny-all"}, nil
	case harness.NetworkPolicyCustom:
		allowedHosts := cloneStringSlice(policy.AllowedHosts)
		allowedCIDRs := cloneStringSlice(policy.AllowedCIDRs)
		deniedCIDRs := cloneStringSlice(policy.DeniedCIDRs)
		if len(allowedHosts) == 0 && len(allowedCIDRs) == 0 && len(deniedCIDRs) == 0 {
			return networkAccessPolicy{}, newPolicyConflictError("Custom network policy requires at least one of allowedHosts, allowedCIDRs, or deniedCIDRs to be non-empty.")
		}
		return networkAccessPolicy{mode: "custom", allowedHosts: allowedHosts, allowedCIDRs: allowedCIDRs, deniedCIDRs: deniedCIDRs}, nil
	}
	return networkAccessPolicy{}, newPolicyConflictError("invalid network policy mode " + policy.Mode)
}

// --- composing the effective policy ---

func composeNetworkPolicy(accessPolicy networkAccessPolicy, requestTransformations []harness.RequestTransformation, forwardRules []forwardRule) NetworkPolicy {
	if accessPolicy.mode == "deny-all" {
		return DenyAllNetworkPolicy()
	}

	rulesByHost := map[string][]PolicyRule{}
	var hostOrder []string
	allowedHosts := accessPolicy.allowedHosts
	if accessPolicy.mode == "allow-all" {
		allowedHosts = []string{"*"}
	}
	for _, host := range allowedHosts {
		if _, ok := rulesByHost[host]; !ok {
			hostOrder = append(hostOrder, host)
			// Ensure every allowed host has a map entry, even with no
			// rules yet, so it's still present when rules are appended
			// below or when the policy is serialized.
			rulesByHost[host] = nil
		}
	}

	for _, transformation := range requestTransformations {
		for _, host := range getActiveHostPatterns(allowedHosts, transformation.Match.Host) {
			appendRule(rulesByHost, &hostOrder, host, toVercelRequestTransformationRule(transformation))
		}
	}

	for _, fr := range forwardRules {
		for _, host := range getActiveHostPatterns(allowedHosts, fr.host) {
			appendRule(rulesByHost, &hostOrder, host, PolicyRule{Match: clonePolicyRuleMatch(fr.match), ForwardURL: fr.forwardURL})
		}
	}

	hasActiveRules := false
	for _, rules := range rulesByHost {
		if len(rules) > 0 {
			hasActiveRules = true
			break
		}
	}
	if !hasActiveRules {
		return toVercelAccessPolicy(accessPolicy)
	}

	allowMap := map[string][]PolicyRule{}
	for host, rules := range rulesByHost {
		if rules == nil {
			rules = []PolicyRule{}
		}
		allowMap[host] = rules
	}
	out := NetworkPolicy{AllowMap: allowMap}
	if accessPolicy.mode == "custom" {
		out.Subnets = toVercelSubnets(accessPolicy)
	}
	return out
}

func toVercelAccessPolicy(accessPolicy networkAccessPolicy) NetworkPolicy {
	if accessPolicy.mode == "allow-all" || accessPolicy.mode == "deny-all" {
		return NetworkPolicy{Mode: accessPolicy.mode}
	}
	out := NetworkPolicy{}
	if len(accessPolicy.allowedHosts) > 0 {
		out.AllowList = accessPolicy.allowedHosts
	}
	out.Subnets = toVercelSubnets(accessPolicy)
	return out
}

func toVercelSubnets(accessPolicy networkAccessPolicy) *PolicySubnets {
	if len(accessPolicy.allowedCIDRs) == 0 && len(accessPolicy.deniedCIDRs) == 0 {
		return nil
	}
	subnets := &PolicySubnets{}
	if len(accessPolicy.allowedCIDRs) > 0 {
		subnets.Allow = accessPolicy.allowedCIDRs
	}
	if len(accessPolicy.deniedCIDRs) > 0 {
		subnets.Deny = accessPolicy.deniedCIDRs
	}
	return subnets
}

func getActiveHostPatterns(allowedHosts []string, ruleHost string) []string {
	var out []string
	for _, allowedHost := range allowedHosts {
		if intersection, ok := intersectHostPatterns(allowedHost, ruleHost); ok {
			out = append(out, intersection)
		}
	}
	return minimizeHostPatterns(out)
}

func intersectHostPatterns(first, second string) (string, bool) {
	if isHostPatternSubset(first, second) {
		return first, true
	}
	if isHostPatternSubset(second, first) {
		return second, true
	}
	return "", false
}

func isHostPatternSubset(candidate, container string) bool {
	normalizedCandidate := strings.ToLower(candidate)
	normalizedContainer := strings.ToLower(container)
	if normalizedCandidate == normalizedContainer || normalizedContainer == "*" {
		return true
	}
	if normalizedCandidate == "*" || !strings.HasPrefix(normalizedContainer, "*") {
		return false
	}
	containerSuffix := normalizedContainer[1:]
	if strings.HasPrefix(normalizedCandidate, "*") {
		return strings.HasSuffix(normalizedCandidate[1:], containerSuffix)
	}
	return strings.HasSuffix(normalizedCandidate, containerSuffix)
}

func minimizeHostPatterns(patterns []string) []string {
	unique := dedupeStrings(patterns)
	var out []string
	for _, pattern := range unique {
		subsumed := false
		for _, other := range unique {
			if other != pattern && isHostPatternSubset(pattern, other) {
				subsumed = true
				break
			}
		}
		if !subsumed {
			out = append(out, pattern)
		}
	}
	return out
}

func dedupeStrings(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range in {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}

func appendRule(rulesByHost map[string][]PolicyRule, hostOrder *[]string, host string, rule PolicyRule) {
	if _, ok := rulesByHost[host]; !ok {
		*hostOrder = append(*hostOrder, host)
	}
	rulesByHost[host] = append(rulesByHost[host], rule)
}

func toVercelRequestTransformationRule(t harness.RequestTransformation) PolicyRule {
	match := &PolicyRuleMatch{
		Path:        toWireMatcher(t.Match.Path),
		Method:      cloneStringSlice(t.Match.Method),
		QueryString: toWireKeyValueMatchers(t.Match.QueryString),
		Headers:     toWireKeyValueMatchers(t.Match.Headers),
	}
	if match.Path == nil && match.Method == nil && match.QueryString == nil && match.Headers == nil {
		match = nil
	}
	headers := map[string]string{}
	for k, v := range t.Transform.Headers {
		headers[k] = v
	}
	return PolicyRule{Match: match, Transform: []PolicyTransform{{Headers: headers}}}
}

func hasRequestTransformation(rule PolicyRule) bool {
	for _, t := range rule.Transform {
		if len(t.Headers) > 0 {
			return true
		}
	}
	return false
}

func getRequestTransformationHosts(policy NetworkPolicy) []string {
	if policy.Mode != "" || policy.AllowMap == nil {
		return nil
	}
	set := map[string]struct{}{}
	for host, rules := range policy.AllowMap {
		for _, rule := range rules {
			if hasRequestTransformation(rule) {
				set[strings.ToLower(host)] = struct{}{}
			}
		}
	}
	hosts := make([]string, 0, len(set))
	for h := range set {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	return hosts
}

func isHostSetSubset(subset, superset []string) bool {
	set := map[string]struct{}{}
	for _, h := range superset {
		set[strings.ToLower(h)] = struct{}{}
	}
	for _, h := range subset {
		if _, ok := set[strings.ToLower(h)]; !ok {
			return false
		}
	}
	return true
}

// --- request transformation merge/clone helpers ---

func mergeRequestTransformations(existing, incoming []harness.RequestTransformation) []harness.RequestTransformation {
	merged := cloneRequestTransformations(existing)
	identities := map[string]int{}
	for i, t := range merged {
		identities[requestTransformationIdentity(t)] = i
	}
	for _, t := range incoming {
		clone := cloneRequestTransformation(t)
		identity := requestTransformationIdentity(clone)
		if idx, ok := identities[identity]; ok {
			merged[idx] = clone
		} else {
			identities[identity] = len(merged)
			merged = append(merged, clone)
		}
	}
	return merged
}

func requestTransformationIdentity(t harness.RequestTransformation) string {
	names := make([]string, 0, len(t.Transform.Headers))
	for name := range t.Transform.Headers {
		names = append(names, strings.ToLower(name))
	}
	sort.Strings(names)
	payload := struct {
		Match                  harness.RequestTransformationMatch `json:"match"`
		TransformedHeaderNames []string                           `json:"transformedHeaderNames"`
	}{Match: t.Match, TransformedHeaderNames: names}
	b, _ := json.Marshal(payload)
	return string(b)
}

func cloneRequestTransformations(in []harness.RequestTransformation) []harness.RequestTransformation {
	if in == nil {
		return nil
	}
	out := make([]harness.RequestTransformation, len(in))
	for i, t := range in {
		out[i] = cloneRequestTransformation(t)
	}
	return out
}

func cloneRequestTransformation(t harness.RequestTransformation) harness.RequestTransformation {
	headers := map[string]string{}
	for k, v := range t.Transform.Headers {
		headers[k] = v
	}
	match := harness.RequestTransformationMatch{
		Host:   t.Match.Host,
		Path:   t.Match.Path,
		Method: cloneStringSlice(t.Match.Method),
	}
	if t.Match.QueryString != nil {
		match.QueryString = append([]harness.KeyValueMatcher(nil), t.Match.QueryString...)
	}
	if t.Match.Headers != nil {
		match.Headers = append([]harness.KeyValueMatcher(nil), t.Match.Headers...)
	}
	return harness.RequestTransformation{Match: match, Transform: harness.RequestTransformationTransform{Headers: headers}}
}

func cloneForwardRules(in []forwardRule) []forwardRule {
	if in == nil {
		return nil
	}
	out := make([]forwardRule, len(in))
	for i, r := range in {
		out[i] = forwardRule{host: r.host, match: clonePolicyRuleMatch(r.match), forwardURL: r.forwardURL}
	}
	return out
}

func toWireMatcher(m *harness.StringMatcher) *PolicyRuleMatcher {
	if m == nil {
		return nil
	}
	return &PolicyRuleMatcher{Exact: m.Exact, StartsWith: m.StartsWith, Regex: m.Regex}
}

func toWireKeyValueMatchers(in []harness.KeyValueMatcher) []PolicyKeyValueMatcher {
	if in == nil {
		return nil
	}
	out := make([]PolicyKeyValueMatcher, len(in))
	for i, m := range in {
		out[i] = PolicyKeyValueMatcher{Key: toWireMatcher(m.Key), Value: toWireMatcher(m.Value)}
	}
	return out
}
