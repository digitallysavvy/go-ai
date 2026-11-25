package tools

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// Ports the output validation cases of browserbase-fetch.test.ts and
// browserbase-search.test.ts (safeValidateTypes against outputSchema).
func TestR74prova_BrowserbaseOutputSchemaTSCases(t *testing.T) {
	fetch := schema.NewSimpleJSONSchema(NewBrowserbaseFetch(BrowserbaseFetchConfig{}).ToTool().OutputSchema.(map[string]interface{})).Validator()
	search := schema.NewSimpleJSONSchema(NewBrowserbaseSearch(BrowserbaseSearchConfig{}).ToTool().OutputSchema.(map[string]interface{})).Validator()
	cases := []struct {
		name  string
		v     schema.Validator
		value string
		valid bool
	}{
		{"validates a Browserbase Fetch API success response", fetch, `{"id":"fetch_9f8e7d6c5b4a","statusCode":200,"headers":{"content-type":"text/html; charset=utf-8","cache-control":"max-age=0"},"content":"<!DOCTYPE html><html><body>Example</body></html>","contentType":"text/html; charset=utf-8","encoding":"utf-8"}`, true},
		{"validates a JSON-extraction success response", fetch, `{"id":"fetch_1a2b3c4d5e6f","statusCode":200,"headers":{"content-type":"text/html"},"content":{"title":"Example Domain","price":42},"contentType":"text/html","encoding":"utf-8"}`, true},
		{"validates a Browserbase Fetch API error response", fetch, `{"error":"rate_limit","statusCode":429,"message":"Rate limit exceeded"}`, true},
		{"rejects a success response missing id", fetch, `{"statusCode":200,"headers":{"content-type":"text/html"},"content":"<!DOCTYPE html><html><body>Example</body></html>","contentType":"text/html; charset=utf-8","encoding":"utf-8"}`, false},
		{"validates a Browserbase Search API success response", search, `{"requestId":"req_9f8e7d6c5b4a","query":"half dome permit","results":[{"id":"result_1","url":"https://www.nps.gov/yose/planyourvisit/halfdome.htm","title":"Half Dome - Yosemite National Park","author":"National Park Service","favicon":"https://www.nps.gov/favicon.ico","image":"https://www.nps.gov/halfdome.jpg","publishedDate":"2026-01-15T00:00:00Z"},{"id":"result_2","url":"https://www.recreation.gov/permits/234652","title":"Half Dome Permits"}]}`, true},
		{"validates a Browserbase Search API error response", search, `{"error":"timeout","statusCode":504,"message":"Search request timed out"}`, true},
		{"rejects a success response missing requestId", search, `{"query":"half dome permit","results":[]}`, false},
	}
	for _, c := range cases {
		var v interface{}
		if err := json.Unmarshal([]byte(c.value), &v); err != nil {
			t.Fatal(err)
		}
		err := c.v.Validate(v)
		if c.valid && err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
		if !c.valid && err == nil {
			t.Errorf("%s: accepted", c.name)
		}
	}
}
