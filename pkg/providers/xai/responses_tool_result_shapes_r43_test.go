package xai

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
)

// TS mapWebSearchAction passes a missing open_page/find_in_page url (and
// pattern) through as undefined, so the keys are absent rather than "".
func TestMapWebSearchActionKeepsMissingURLAbsent(t *testing.T) {
	got := mapWebSearchAction(json.RawMessage(`{"type":"open_page"}`))
	want := map[string]interface{}{"action": map[string]interface{}{"type": "openPage"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("open_page = %#v, want %#v", got, want)
	}
	got = mapWebSearchAction(json.RawMessage(`{"type":"find_in_page","url":"https://a.test"}`))
	want = map[string]interface{}{"action": map[string]interface{}{"type": "findInPage", "url": "https://a.test"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("find_in_page = %#v, want %#v", got, want)
	}
}

// TS file_search_call results carry `queries: part.queries ?? []`.
func TestFileSearchCallQueriesDefaultToEmptyArray(t *testing.T) {
	m := &ResponsesLanguageModel{}
	resp := responses.ResponsesAPIResponse{Output: []json.RawMessage{json.RawMessage(`{"type":"file_search_call","id":"fs_1","status":"completed"}`)}}
	result, err := m.convertResponse(resp, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range result.Content {
		if tr, ok := part.(types.ToolResultContent); ok {
			raw, _ := json.Marshal(tr.Result)
			if string(raw) != `{"queries":[],"results":null}` {
				t.Errorf("result = %s", raw)
			}
			return
		}
	}
	t.Fatal("no tool result")
}
