package ai

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ToolSearchDefaultName is the tool name ToolSearch uses when config.Name is
// empty or omitted.
const ToolSearchDefaultName = "toolSearch"

// ToolSearchDefaultMaxResults is the number of matching tools ToolSearch
// returns per search when config.MaxResults is left unset.
const ToolSearchDefaultMaxResults = 5

// ToolSearchCandidate re-exports types.ToolSearchCandidate so callers can
// write ai.ToolSearchCandidate.
type ToolSearchCandidate = types.ToolSearchCandidate

// ToolSearchRankFunc re-exports types.ToolSearchRankFunc so callers can
// write ai.ToolSearchRankFunc.
type ToolSearchRankFunc = types.ToolSearchRankFunc

// ToolSearchConfig configures ai.ToolSearch. All fields are optional.
type ToolSearchConfig struct {
	// Name overrides the tool's registered name (default:
	// ToolSearchDefaultName). Set it to register more than one tool-search
	// instance in the same request (e.g. one per tool-caller boundary), or
	// to avoid a collision with an existing tool name. Whatever name is
	// used here must match the corresponding entry in
	// ExperimentalToolCallers and the tools list passed to
	// GenerateText/StreamText.
	Name string

	// MaxResults caps the number of matching tools returned per search.
	// Zero (the default) uses ToolSearchDefaultMaxResults (5). A negative
	// value panics with an InvalidArgumentError, mirroring the TypeScript
	// SDK's toolSearch({ maxResults }) synchronous validation throw.
	MaxResults int

	// Search optionally selects and ranks eligible deferred tools, instead
	// of the built-in keyword scoring over tool names and descriptions.
	// Mirrors the TypeScript SDK's toolSearch({ search }) callback.
	Search ToolSearchRankFunc
}

// ToolSearch returns the native tool-search tool, alongside tools marked
// DeferLoading: true; the surrounding generation binds the search registry
// so matches become available on the next model step. Use directly, or
// through a local tool caller whose PrepareModelMessage announces a
// discovery catalog in the conversation (code mode with conversation-based
// tool discovery). Mirrors the TypeScript SDK's toolSearch(), whose name is
// the tools-object key the caller chooses; Go tools are a flat slice keyed
// by Name, so ToolSearchConfig.Name plays the same role (mirroring
// MCPConfig.Name for the same reason).
//
// Panics with a *providererrors.InvalidArgumentError if config.MaxResults is
// negative.
func ToolSearch(config ...ToolSearchConfig) types.Tool {
	name := ToolSearchDefaultName
	maxResults := ToolSearchDefaultMaxResults
	var search ToolSearchRankFunc
	if len(config) > 0 {
		if config[0].Name != "" {
			name = config[0].Name
		}
		if config[0].MaxResults != 0 {
			maxResults = config[0].MaxResults
		}
		search = config[0].Search
	}
	if maxResults < 1 {
		panic(&providererrors.InvalidArgumentError{
			Field:   "maxResults",
			Message: "maxResults must be a positive safe integer.",
		})
	}

	description := "Search for tools by keywords in their names and descriptions."
	if search != nil {
		description = "Search for tools matching a query."
	}
	resultsPhrase := "five"
	if maxResults != ToolSearchDefaultMaxResults {
		resultsPhrase = fmt.Sprintf("%d", maxResults)
	}
	description += fmt.Sprintf(
		" Returns up to %s matching tools. Matches become available on the next model step, after this execution "+
			"finishes. Wait for their tool definitions before calling the discovered tools. If no tools match, try "+
			"different keywords.",
		resultsPhrase,
	)

	return types.Tool{
		Name:                 name,
		Description:          description,
		ToolSearchMaxResults: maxResults,
		ToolSearchRank:       search,
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{"type": "string", "minLength": 1},
			},
			"required":             []string{"query"},
			"additionalProperties": false,
		},
		OutputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tools": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"name":        map[string]interface{}{"type": "string"},
							"description": map[string]interface{}{"type": "string"},
						},
						"required":             []string{"name"},
						"additionalProperties": false,
					},
				},
			},
			"required":             []string{"tools"},
			"additionalProperties": false,
		},
		Type:         types.ToolTypeFunction,
		IsToolSearch: true,
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return nil, fmt.Errorf("toolSearch must be bound by an AI SDK generation")
		},
	}
}

// ToolSearchState holds the deferred-tool discovery bookkeeping for one
// generation. Create it once per generation with NewToolSearchState (never
// share an instance across generations), then call Apply once per step.
// Mirrors the TypeScript SDK's createToolSearchState.
type ToolSearchState struct {
	mu              sync.Mutex
	fullTools       map[string]types.Tool
	toolCallers     ResolvedToolCallers
	discovered      map[string]bool
	searchToolNames []string
}

// NewToolSearchState validates and prepares deferred-tool discovery state
// for one generation. It returns a state whose Apply is a no-op (returns
// its input unchanged) when tools contains no DeferLoading or toolSearch
// tools.
func NewToolSearchState(tools []types.Tool, toolCallers ResolvedToolCallers) (*ToolSearchState, error) {
	fullByName := make(map[string]types.Tool, len(tools))
	var searchNames []string
	for _, t := range tools {
		fullByName[t.Name] = t
		if t.DeferLoading || t.IsToolSearch {
			searchNames = append(searchNames, t.Name)
		}
	}

	state := &ToolSearchState{
		fullTools:       fullByName,
		toolCallers:     toolCallers,
		discovered:      make(map[string]bool),
		searchToolNames: searchNames,
	}
	if len(searchNames) == 0 {
		return state, nil
	}

	getCallers := func(name string) []string {
		if c, ok := toolCallers[name]; ok {
			return c
		}
		return []string{DirectToolCall}
	}

	for _, name := range searchNames {
		tool := fullByName[name]
		invalid := false
		for _, callerName := range getCallers(name) {
			if callerName == DirectToolCall {
				continue
			}
			callerTool, ok := fullByName[callerName]
			if !ok || callerTool.ExperimentalToolCaller == nil ||
				callerTool.ExperimentalToolCaller.Type != types.ToolCallerTypeLocal ||
				callerTool.ExperimentalToolCaller.PrepareModelMessage == nil {
				invalid = true
				break
			}
		}
		if invalid || (tool.IsToolSearch && tool.DeferLoading) {
			return nil, &providererrors.InvalidArgumentError{
				Field: "tools",
				Message: fmt.Sprintf(
					"tool %q must be callable directly or through code mode with toolDiscovery: 'conversation'. The search tool itself must not defer loading.",
					name,
				),
			}
		}
	}

	return state, nil
}

// Apply filters activeTools for one step: deferred tools that have not yet
// been discovered are hidden, and any toolSearch tool present is rebound
// with an Execute function scoped to the deferred candidates reachable
// through its own callers. Discoveries made by the returned tool's Execute
// become visible on the next call to Apply. Mirrors the TypeScript SDK's
// prepareToolSearch return value.
func (s *ToolSearchState) Apply(activeTools []types.Tool, toolsContext map[string]interface{}, sandbox interface{}) []types.Tool {
	if s == nil || len(s.searchToolNames) == 0 {
		return activeTools
	}
	if activeTools == nil {
		return nil
	}

	activeByName := make(map[string]bool, len(activeTools))
	for _, t := range activeTools {
		activeByName[t.Name] = true
	}
	getCallers := func(name string) []string {
		if c, ok := s.toolCallers[name]; ok {
			return c
		}
		return []string{DirectToolCall}
	}

	out := make([]types.Tool, 0, len(activeTools))
	for _, t := range activeTools {
		if t.DeferLoading && !s.hasDiscovered(t.Name) {
			continue
		}
		if !t.IsToolSearch {
			out = append(out, t)
			continue
		}

		callers := make([]string, 0)
		for _, c := range getCallers(t.Name) {
			if c == DirectToolCall || activeByName[c] {
				callers = append(callers, c)
			}
		}

		candidates := make([]types.Tool, 0)
		for _, cand := range activeTools {
			if !cand.DeferLoading || cand.IsToolSearch {
				continue
			}
			candCallers := getCallers(cand.Name)
			if callerSetsOverlap(callers, candCallers) {
				candidates = append(candidates, cand)
			}
		}

		maxResults := t.ToolSearchMaxResults
		if maxResults < 1 {
			maxResults = ToolSearchDefaultMaxResults
		}
		searchTool := t
		searchTool.Execute = s.newSearchExecute(candidates, toolsContext, sandbox, maxResults, t.ToolSearchRank)
		out = append(out, searchTool)
	}
	return out
}

func (s *ToolSearchState) hasDiscovered(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.discovered[name]
}

func (s *ToolSearchState) markDiscovered(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.discovered[name] = true
}

func callerSetsOverlap(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}

type toolSearchNameScore struct {
	name  string
	score int
}

// newSearchExecute builds the Execute function bound to one toolSearch
// tool's step. It mirrors the TypeScript SDK's prepare-tool-search.ts
// execute: resolve each candidate's current description, rank candidates
// (via rank when set, else built-in keyword scoring), then deduplicate and
// cap the ranked names before looking them up and marking them discovered.
func (s *ToolSearchState) newSearchExecute(candidates []types.Tool, toolsContext map[string]interface{}, sandbox interface{}, maxResults int, rank types.ToolSearchRankFunc) types.ToolExecutor {
	return func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
		query, _ := input["query"].(string)

		available := make([]types.ToolSearchCandidate, 0, len(candidates))
		for _, cand := range candidates {
			available = append(available, types.ToolSearchCandidate{
				Name:        cand.Name,
				Description: resolveCandidateDescription(ctx, cand, toolsContext, sandbox),
			})
		}
		byName := make(map[string]types.ToolSearchCandidate, len(available))
		for _, a := range available {
			byName[a.Name] = a
		}

		var rankedNames []string
		if rank != nil {
			// Pass a copy of the candidates so a callback that mutates its
			// slice or entries cannot affect byName, mirroring the
			// TypeScript SDK's shallow-copied `tools` argument.
			cp := make([]types.ToolSearchCandidate, len(available))
			copy(cp, available)
			names, err := rank(ctx, query, cp)
			if err != nil {
				return nil, err
			}
			rankedNames = names
		} else {
			terms := uniqueTokens(tokenizeSearchText(query))
			scoredMatches := make([]toolSearchNameScore, 0, len(available))
			for _, a := range available {
				nameTerms := tokenizeSearchText(a.Name)
				descTerms := tokenizeSearchText(a.Description)
				score := 0
				for _, term := range terms {
					if containsToken(nameTerms, term) {
						score += 2
					}
					if containsToken(descTerms, term) {
						score++
					}
				}
				if score > 0 {
					scoredMatches = append(scoredMatches, toolSearchNameScore{name: a.Name, score: score})
				}
			}
			sort.SliceStable(scoredMatches, func(i, j int) bool {
				return scoredMatches[i].score > scoredMatches[j].score
			})
			rankedNames = make([]string, len(scoredMatches))
			for i, m := range scoredMatches {
				rankedNames[i] = m.name
			}
		}

		seen := make(map[string]bool, len(rankedNames))
		results := make([]map[string]interface{}, 0, len(rankedNames))
		for _, name := range rankedNames {
			if seen[name] {
				continue
			}
			seen[name] = true
			cand, ok := byName[name]
			if !ok {
				continue
			}
			if len(results) >= maxResults {
				break
			}
			s.markDiscovered(cand.Name)
			entry := map[string]interface{}{"name": cand.Name}
			if cand.Description != "" {
				entry["description"] = cand.Description
			}
			results = append(results, entry)
		}
		return map[string]interface{}{"tools": results}, nil
	}
}

func resolveCandidateDescription(ctx context.Context, tool types.Tool, toolsContext map[string]interface{}, sandbox interface{}) string {
	if tool.DescriptionFunc != nil {
		return tool.DescriptionFunc(ctx, types.ToolDescriptionOptions{
			Context:             toolsContext[tool.Name],
			ExperimentalSandbox: sandbox,
		})
	}
	return tool.Description
}

var (
	camelCaseBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	searchWordToken   = regexp.MustCompile(`[\p{L}\p{N}]+`)
)

// tokenizeSearchText splits camelCase/description text into lowercase
// unicode word tokens, matching the TypeScript SDK's tokenize helper in
// prepare-tool-search.ts.
func tokenizeSearchText(text string) []string {
	spaced := camelCaseBoundary.ReplaceAllString(text, "$1 $2")
	lower := strings.ToLower(spaced)
	return searchWordToken.FindAllString(lower, -1)
}

func uniqueTokens(tokens []string) []string {
	seen := make(map[string]bool, len(tokens))
	out := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

func containsToken(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
