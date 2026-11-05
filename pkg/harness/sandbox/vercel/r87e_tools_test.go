package vercel

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// Round 87e: known differences fixed against @vercel/sandbox 3.9.0
// (utils/paginator.js attachPaginator, sandbox.js listSessions,
// api-client/base-client.js `array(value).forEach(... append ...)`) and TS
// sandbox-vercel vercel-network-policy-manager.ts #inspectPolicy.

type r87etoolsRecorder struct {
	mu       sync.Mutex
	requests []string
}

func (r *r87etoolsRecorder) add(req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, req.Method+" "+req.URL.RequestURI())
}

func (r *r87etoolsRecorder) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.requests...)
}

// attachPaginator follows `pagination.next` until it is null, with no page
// bound (v0.6.0 before this fix stopped after 20 pages).
func TestR87etoolsSnapshotListingHasNoPageBound(t *testing.T) {
	const pages = 25
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := 0
		if c := r.URL.Query().Get("cursor"); c != "" {
			_, _ = fmt.Sscanf(c, "p%d", &page)
		}
		next := "null"
		if page+1 < pages {
			next = fmt.Sprintf(`"p%d"`, page+1)
		}
		_, _ = fmt.Fprintf(w, `{"snapshots":[{"id":"s%d","status":"created","creationMethod":"manual"}],"pagination":{"count":1,"next":%s}}`, page, next)
	}))
	defer srv.Close()
	c := NewAPIClient(srv.URL, Credentials{Token: "plain", TeamID: "team", ProjectID: "prj"})
	got, err := c.ListSnapshots(context.Background(), "tpl")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != pages || got[pages-1].ID != fmt.Sprintf("s%d", pages-1) {
		t.Fatalf("got %d snapshots, want %d", len(got), pages)
	}
}

