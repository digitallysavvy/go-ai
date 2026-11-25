package voyage

import (
	"encoding/json"
	"errors"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS APICallError keeps an empty message from {"detail":""}.
func TestHandleErrorKeepsEmptyDetail(t *testing.T) {
	err := handleError(&internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(`{"detail":""}`)})
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.Message != "" || perr.StatusCode != 400 || perr.ResponseBody != `{"detail":""}` {
		t.Fatalf("got %#v", perr)
	}
}

func TestToVoyageDocumentsObjectOrderAndHTML(t *testing.T) {
	docs, warns := toVoyageDocuments([]json.RawMessage{json.RawMessage(`{ "title": "a<b", "id": 1.0, "body": "&" }`)})
	if len(warns) != 1 || len(docs) != 1 || docs[0] != `{"title":"a<b","id":1,"body":"&"}` {
		t.Fatalf("docs = %q, warnings = %v", docs, warns)
	}
	docs, _ = toVoyageDocuments([]map[string]interface{}{{"t": "<x>"}})
	if docs[0] != `{"t":"<x>"}` {
		t.Fatalf("docs = %q", docs)
	}
}
