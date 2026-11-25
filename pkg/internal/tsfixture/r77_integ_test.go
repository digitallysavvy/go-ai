package tsfixture

import "testing"

// Round 77 integrator survivor test (provb fix 1): when a request path
// matches several routes, the longest TS path wins, whatever the order.
func TestR77Integ_MatchRouteLongestWins(t *testing.T) {
	routes := []Route{
		{Path: "/interactions", Response: &Response{}},
		{Path: "/v1beta/interactions", Response: &Response{}},
	}
	if got := matchRoute(routes, "/v1beta/interactions"); got != 1 {
		t.Errorf("matchRoute = %d, want 1 (the longer route)", got)
	}
}
