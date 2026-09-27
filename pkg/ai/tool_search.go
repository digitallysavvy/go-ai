package ai

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ToolSearch returns the native tool-search tool. Register it under name
// alongside tools marked DeferLoading: true; the surrounding generation
// binds the search registry so matches become available on the next model
// step. Use directly, or through a local tool caller whose
// PrepareModelMessage announces a discovery catalog in the conversation
// (code mode with conversation-based tool discovery). Mirrors the
// TypeScript SDK's toolSearch().
func ToolSearch(name string) types.Tool {
	return types.Tool{
		Name: name,
		Description: "Search for tools by keywords in their names and descriptions. Returns up to five matching tools. " +
			"Matches become available on the next model step, after this execution finishes. Wait for their tool " +
			"definitions before calling the discovered tools. If no tools match, try different keywords.",
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
			return nil, fmt.Errorf(
				"tool %q must be callable directly or through code mode with toolDiscovery: 'conversation'. The search tool itself must not defer loading.",
				name,
			)
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

		searchTool := t
		searchTool.Execute = s.newSearchExecute(candidates, toolsContext, sandbox)
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

type toolSearchMatch struct {
	name        string
	description string
	hasDesc     bool
	score       int
}

func (s *ToolSearchState) newSearchExecute(candidates []types.Tool, toolsContext map[string]interface{}, sandbox interface{}) types.ToolExecutor {
	return func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
		query, _ := input["query"].(string)
		terms := uniqueTokens(tokenizeSearchText(query))

		matches := make([]toolSearchMatch, 0, len(candidates))
		for _, cand := range candidates {
			description := resolveCandidateDescription(ctx, cand, toolsContext, sandbox)
			nameTerms := tokenizeSearchText(cand.Name)
			descTerms := tokenizeSearchText(description)
			score := 0
			for _, term := range terms {
				if containsToken(nameTerms, term) {
					score += 2
				}
				if containsToken(descTerms, term) {
					score += 1
				}
			}
			if score > 0 {
				matches = append(matches, toolSearchMatch{
					name:        cand.Name,
					description: description,
					hasDesc:     description != "",
					score:       score,
				})
			}
		}

		sort.SliceStable(matches, func(i, j int) bool {
			return matches[i].score > matches[j].score
		})
		if len(matches) > 5 {
			matches = matches[:5]
		}

		results := make([]map[string]interface{}, 0, len(matches))
		for _, m := range matches {
			s.markDiscovered(m.name)
			entry := map[string]interface{}{"name": m.name}
			if m.hasDesc {
				entry["description"] = m.description
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
