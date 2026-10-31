package tools

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS gateway tools (createProviderExecutedToolFactory with only id, input and
// output schema) carry no title, so their tool calls and UI parts have none.
func TestR73prova_GatewayToolsHaveNoTitle(t *testing.T) {
	for _, tl := range []types.Tool{
		types.Tool(NewBrowserbaseSearch(BrowserbaseSearchConfig{})), types.Tool(NewBrowserbaseFetch(BrowserbaseFetchConfig{})),
		types.Tool(NewExaSearch(ExaSearchConfig{})), types.Tool(NewParallelSearch(ParallelSearchConfig{})),
		types.Tool(NewPerplexitySearch(PerplexitySearchConfig{})), types.Tool(NewTakoSearch(TakoSearchConfig{})),
	} {
		if tl.Title != "" {
			t.Errorf("%s: Title = %q, TS has none", tl.ProviderID, tl.Title)
		}
	}
}

// TS sends the config object as the args, so an explicit empty list stays an
// empty list on the wire.
func TestR73prova_GatewayToolArgsKeepEmptyLists(t *testing.T) {
	got := map[string]string{}
	add := func(tl types.Tool) {
		b, _ := json.Marshal(tl.ProviderArgs)
		got[tl.ProviderID] = string(b)
	}
	add(types.Tool(NewExaSearch(ExaSearchConfig{IncludeDomains: []string{}, ExcludeDomains: []string{}})))
	add(types.Tool(NewPerplexitySearch(PerplexitySearchConfig{SearchDomainFilter: []string{}, SearchLanguageFilter: []string{}})))
	add(types.Tool(NewParallelSearch(ParallelSearchConfig{SourcePolicy: &ParallelSearchSourcePolicy{IncludeDomains: []string{}, ExcludeDomains: []string{}}})))
	add(types.Tool(NewTakoSearch(TakoSearchConfig{Sources: &TakoSearchSources{
		Data: &TakoDataSourceConfig{NodeIDs: []string{}},
		Web:  &TakoWebSourceConfig{IncludeDomains: []string{}, ExcludeDomains: []string{}},
	}})))
	want := map[string]string{
		"gateway.exa_search":        `{"excludeDomains":[],"includeDomains":[]}`,
		"gateway.perplexity_search": `{"searchDomainFilter":[],"searchLanguageFilter":[]}`,
		"gateway.parallel_search":   `{"sourcePolicy":{"excludeDomains":[],"includeDomains":[]}}`,
		"gateway.tako_search":       `{"sources":{"data":{"nodeIds":[]},"web":{"excludeDomains":[],"includeDomains":[]}}}`,
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s args = %s, want %s", id, got[id], w)
		}
	}
}
