package perplexity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func portprovgDecisionServer(t *testing.T, body interface{}, status int, headers map[string]string) (*httptest.Server, *map[string]interface{}) {
	t.Helper()
	var got map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(server.Close)
	return server, &got
}

func portprovgModel(t *testing.T, url string) provider.DecisionModel {
	t.Helper()
	m, err := New(Config{APIKey: "test-key", BaseURL: url}).DecisionModel("pplx-decider-v1.3")
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// TS perplexity-decision-model.test.ts "forwards grounding options and
// preserves evidence and exact tool counts" (d5440b3311).
func TestPortprovgDecisionGroundingOptionsAndEvidence(t *testing.T) {
	fixture := fixtureBody(t)
	searchResults := []interface{}{map[string]interface{}{
		"title": "Webb launch", "url": "https://example.com/webb", "snippet": "Launched in December 2021.",
		"date": "2021-12-25", "last_updated": "2026-11-01",
	}}
	fetchedResults := []interface{}{map[string]interface{}{"title": "Webb", "url": "https://example.com/page", "snippet": "Page text."}}
	usage, _ := fixture["usage"].(map[string]interface{})
	usage["tool_calls_details"] = map[string]interface{}{
		"search_web": map[string]interface{}{"invocation": 2},
		"fetch_url":  map[string]interface{}{"invocation": 1},
		"other":      map[string]interface{}{},
	}
	fixture["search_results"] = searchResults
	fixture["fetched_results"] = fetchedResults
	server, body := portprovgDecisionServer(t, fixture, 200, nil)

	opts := decisionOptions()
	opts.ProviderOptions = map[string]interface{}{"perplexity": map[string]interface{}{"mode": "web_grounding", "maxStaleness": 0}}
	result, err := portprovgModel(t, server.URL).DoDecide(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if (*body)["mode"] != "web_grounding" || (*body)["max_staleness"] != float64(0) {
		t.Fatalf("body = %v", *body)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
	meta := result.ProviderMetadata["perplexity"].(map[string]interface{})
	if !reflect.DeepEqual(meta["searchResults"], interface{}(searchResults)) || !reflect.DeepEqual(meta["fetchedResults"], interface{}(fetchedResults)) {
		t.Fatalf("evidence = %#v / %#v", meta["searchResults"], meta["fetchedResults"])
	}
	want := map[string]interface{}{
		"search_web": map[string]interface{}{"invocation": float64(2)},
		"fetch_url":  map[string]interface{}{"invocation": float64(1)},
		"other":      map[string]interface{}{"invocation": nil},
	}
	if !reflect.DeepEqual(meta["toolCalls"], want) {
		t.Fatalf("toolCalls = %#v", meta["toolCalls"])
	}
	if _, ok := meta["confidence"]; !ok {
		t.Fatal("confidence metadata was dropped")
	}
}

func TestPortprovgDecisionOmitsGroundingMetadataAndOptionsWhenAbsent(t *testing.T) {
	server, body := portprovgDecisionServer(t, fixtureBody(t), 200, nil)
	result, err := portprovgModel(t, server.URL).DoDecide(context.Background(), decisionOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(*body) != 3 {
		t.Fatalf("body keys = %v", *body)
	}
	meta := result.ProviderMetadata["perplexity"].(map[string]interface{})
	for _, key := range []string{"searchResults", "fetchedResults", "toolCalls"} {
		if _, has := meta[key]; has {
			t.Fatalf("metadata has %s: %v", key, meta)
		}
	}
}

// "rejects invalid grounding options %j before sending a request".
func TestPortprovgDecisionRejectsInvalidGroundingOptions(t *testing.T) {
	for name, opts := range map[string]map[string]interface{}{
		"invalid mode":       {"mode": "invalid"},
		"negative staleness": {"maxStaleness": -1},
		"fractional":         {"maxStaleness": 0.5},
		"null mode":          {"mode": nil},
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
			defer server.Close()
			call := decisionOptions()
			call.ProviderOptions = map[string]interface{}{"perplexity": opts}
			_, err := portprovgModel(t, server.URL).DoDecide(context.Background(), call)
			var invalid *providererrors.InvalidArgumentError
			if !errors.As(err, &invalid) {
				t.Fatalf("err = %v (%T)", err, err)
			}
			if calls != 0 {
				t.Fatalf("%d requests were sent", calls)
			}
		})
	}
}

// "preserves completed grounding counts of %s on HTTP errors".
func TestPortprovgDecisionKeepsGroundingCountsOnHTTPErrors(t *testing.T) {
	for _, count := range []string{"0", "1"} {
		headers := map[string]string{"x-perplexity-search-queries": count, "x-perplexity-fetched-urls": count}
		server, _ := portprovgDecisionServer(t, map[string]interface{}{"error": map[string]interface{}{
			"message": "Context limit exceeded", "type": "invalid_request", "code": "400",
		}}, 400, headers)
		call := decisionOptions()
		call.ProviderOptions = map[string]interface{}{"perplexity": map[string]interface{}{"mode": "web_grounding"}}
		_, err := portprovgModel(t, server.URL).DoDecide(context.Background(), call)
		var target *providererrors.ProviderError
		if !errors.As(err, &target) || target.StatusCode != 400 || target.IsRetryable() ||
			target.ResponseHeaders["x-perplexity-search-queries"] != count || target.ResponseHeaders["x-perplexity-fetched-urls"] != count {
			t.Fatalf("count %s: err = %#v", count, err)
		}
	}
}

// The response schema (search_results, fetched_results, tool_calls_details)
// rejects malformed grounding fields like TS createJsonResponseHandler.
func TestPortprovgDecisionRejectsMalformedGroundingResponse(t *testing.T) {
	for name, patch := range map[string]func(map[string]interface{}){
		"search result without a title": func(f map[string]interface{}) {
			f["search_results"] = []interface{}{map[string]interface{}{"url": "u", "snippet": "s"}}
		},
		"fetched result with a numeric url": func(f map[string]interface{}) {
			f["fetched_results"] = []interface{}{map[string]interface{}{"title": "t", "url": 1, "snippet": "s"}}
		},
		"non-numeric invocation": func(f map[string]interface{}) {
			f["usage"].(map[string]interface{})["tool_calls_details"] = map[string]interface{}{"a": map[string]interface{}{"invocation": "x"}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := fixtureBody(t)
			patch(fixture)
			server, _ := portprovgDecisionServer(t, fixture, 200, nil)
			if _, err := portprovgModel(t, server.URL).DoDecide(context.Background(), decisionOptions()); err == nil {
				t.Fatal("expected an invalid response error")
			}
		})
	}
}
