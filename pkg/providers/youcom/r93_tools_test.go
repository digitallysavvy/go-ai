package youcom

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// fetch response.json() decodes the body with the WHATWG UTF-8 decoder,
// which drops a leading BOM, so a BOM-prefixed success body (and a
// Contents error body's detail) still parses.
func TestR93toolsYouComJSONBodyDropsBOM(t *testing.T) {
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		if status != http.StatusOK {
			_, _ = w.Write([]byte("\xEF\xBB\xBF{\"detail\":\"bad url\"}"))
			return
		}
		_, _ = w.Write([]byte("\xEF\xBB\xBF{\"results\":{\"web\":[]}}"))
	}))
	defer server.Close()
	cfg := YouToolsConfig{APIKey: "k", BaseURL: server.URL, HTTPClient: server.Client()}
	result, err := YouSearch(cfg).Execute(t.Context(), map[string]interface{}{"query": "go"}, types.ToolExecutionOptions{})
	if raw, ok := result.(map[string]interface{}); err != nil || !ok || raw["results"] == nil {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
	status = http.StatusBadRequest
	_, err = YouContents(cfg).Execute(t.Context(), map[string]interface{}{"urls": []interface{}{"https://a.test"}}, types.ToolExecutionOptions{})
	if err == nil || err.Error() != "bad url" {
		t.Fatalf("contents err = %v, want the body detail", err)
	}
}
