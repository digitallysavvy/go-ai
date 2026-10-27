package toolwebfetch

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS fetchWebResource calls new URL(location, currentUrl) outside any try, so
// a Location it cannot parse throws and fails the tool call (no result).
func TestInvalidRedirectLocationFailsTheToolCall(t *testing.T) {
	for _, location := range []string{"http://example.com:99999/", "http://[::1/"} {
		ft := serve(func(req *http.Request) *http.Response {
			return response(302, "", "Location", location)
		})
		tool := newTool(t, Options{Transport: ft})
		out, err := tool.Execute(context.Background(), map[string]interface{}{"url": "https://example.com/"}, types.ToolExecutionOptions{ToolCallID: "c"})
		if err == nil || err.Error() != "Invalid URL" {
			t.Fatalf("location %q: out = %#v, err = %v; want the Invalid URL error", location, out, err)
		}
		if ft.count() != 1 {
			t.Fatalf("location %q: %d requests, want 1", location, ft.count())
		}
	}
}

// TS measures html.length in UTF-16 code units: astral characters count twice.
func TestHTMLTruncationCountsUTF16CodeUnits(t *testing.T) {
	// 1_000_001 emoji are 2_000_002 UTF-16 units but only 1_000_001 runes.
	src := strings.Repeat("😀", maxHTMLLength/2+1)
	got, truncated := truncateUTF16(src, maxHTMLLength)
	if !truncated || got != strings.Repeat("😀", maxHTMLLength/2) {
		t.Fatalf("truncated = %v, len(got) = %d", truncated, len(got))
	}
	if _, truncated := truncateUTF16(strings.Repeat("é", maxHTMLLength), maxHTMLLength); truncated {
		t.Fatal("2M BMP characters are within the limit")
	}
	// A pair split by the limit is dropped whole.
	if got, truncated := truncateUTF16("ab😀", 3); !truncated || got != "ab" {
		t.Fatalf("got %q, %v", got, truncated)
	}
}

// Deeply nested documents use DOM document.title, not the heading heuristic.
func TestDeepDocumentTitleIsDocumentTitle(t *testing.T) {
	src := "<html><head><title>  Page  |\tSite </title></head><body><h1>Page</h1>" +
		strings.Repeat("<div>", maxDOMDepth+5) + "text" + strings.Repeat("</div>", maxDOMDepth+5) + "</body></html>"
	title, content, err := htmlToMarkdown(src, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if title != "Page  | Site" {
		t.Fatalf("title = %q", title)
	}
	if !strings.Contains(content, "text") {
		t.Fatalf("content = %q", content)
	}
}
