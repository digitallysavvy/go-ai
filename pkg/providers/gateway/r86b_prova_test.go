package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS parseJsonLines (provider-utils response-handler.ts) skips a line only
// when line.trim() is empty and JSON.parses the line as read: a line holding
// only U+FEFF and a space is skipped, U+0085 alone and U+00A0 before the JSON
// value are JSONParseErrors (Node: JSON.parse("\u00a0{}") throws).
func TestR86bprovaGatewayResultsJSLineTrim(t *testing.T) {
	valid := `{"type":"text","id":"req-2","status":"failed","error":{"message":"boom","type":"invalid_request_error"}}`
	cases := []struct {
		name  string
		body  string
		items int
		fail  bool
	}{
		{"bom-only line skipped", valid + "\n\ufeff \n", 1, false},
		{"nbsp before value", "\u00a0" + valid + "\n", 0, true},
		{"nel-only line", valid + "\n\u0085\n", 1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stream := newGatewayBatchResultsStream(io.NopCloser(strings.NewReader(tc.body)), "https://x.example/r", 1<<20)
			items := 0
			for {
				item, err := stream.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					var jp *providererrors.JSONParseError
					if !tc.fail || !errors.As(err, &jp) {
						t.Fatalf("err = %T %v", err, err)
					}
					tc.fail = false
					break
				}
				if item == nil {
					t.Fatal("nil item")
				}
				items++
			}
			if items != tc.items || tc.fail {
				t.Fatalf("items = %d (want %d), missing JSONParseError = %v", items, tc.items, tc.fail)
			}
		})
	}
}

// TS findProtocol trims each subprotocol with String.prototype.trim. Node
// (packages/gateway/dist at 30548ee7f9): "tok", "tok\u0085", "team".
func TestR86bprovaRealtimeProtocolJSTrim(t *testing.T) {
	if got := GetGatewayRealtimeAuthToken("x, \ufeffai-gateway-auth.tok\u00a0"); got != "tok" {
		t.Fatalf("token = %q", got)
	}
	if got := GetGatewayRealtimeAuthToken(" ai-gateway-auth.tok\u0085 "); got != "tok\u0085" {
		t.Fatalf("token = %q", got)
	}
	if got := GetGatewayRealtimeTeamIDOrSlug("\u3000ai-gateway-team.dGVhbQ\u2028"); got != "team" {
		t.Fatalf("team = %q", got)
	}
}

func r86bprovaRawQueryServer(t *testing.T, body string, got *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*got = r.URL.Path + "?" + r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TS getTeamIdOrSlug trims the team header (value?.trim()) and the query
// uses URLSearchParams escaping; generation info uses encodeURIComponent.
// Expected strings from Node.
func TestR86bprovaOriginRequestQueries(t *testing.T) {
	var got string
	srv := r86bprovaRawQueryServer(t, `{"balance":"1.00","total_used":"0.00"}`, &got)
	for team, want := range map[string]string{
		" team_x\u00a0": "/v1/credits?teamId=team_x",
		"a~b*c d":       "/v1/credits?slug=a%7Eb*c+d",
	} {
		p, err := New(Config{APIKey: "k", BaseURL: srv.URL + "/v4/ai", TeamIDOrSlug: team})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.GetCredits(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("team %q: got %q, want %q", team, got, want)
		}
	}

	srv = r86bprovaRawQueryServer(t, `{"results":[]}`, &got)
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL + "/v4/ai"})
	if _, err := p.GetSpendReport(context.Background(), SpendReportParams{StartDate: "2026-01-01", EndDate: "2026-01-31", Model: "a~b*(c)", Tags: []string{"x y", "~"}}); err != nil {
		t.Fatal(err)
	}
	if want := "/v1/report?start_date=2026-01-01&end_date=2026-01-31&model=a%7Eb*%28c%29&tags=x+y%2C%7E"; got != want {
		t.Fatalf("spend report: got %q, want %q", got, want)
	}

	srv = r86bprovaRawQueryServer(t, `{}`, &got)
	p, _ = New(Config{APIKey: "k", BaseURL: srv.URL + "/v4/ai"})
	_, _ = p.GetGenerationInfo(context.Background(), GenerationInfoParams{ID: "gen a~*!()'"})
	if want := "/v1/generation?id=gen%20a~*!()'"; got != want {
		t.Fatalf("generation info: got %q, want %q", got, want)
	}
}

type r86bprovaRoundTripper func(*http.Request) (*http.Response, error)

func (f r86bprovaRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TS: withoutTrailingSlash removes one "/" from the base URL, and the batch
// results URL is `${baseURL}/batch/results` on the resolved base URL (the
// default when none is configured), also in the line-size DownloadError.
func TestR86bprovaBatchResultsURL(t *testing.T) {
	for base, want := range map[string]string{
		"":                       DefaultBaseURL + "/batch/results",
		"https://x.example/ai//": "https://x.example/ai//batch/results",
	} {
		var requested string
		client := &http.Client{Transport: r86bprovaRoundTripper(func(r *http.Request) (*http.Response, error) {
			requested = r.URL.String()
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/x-ndjson"}}, Body: io.NopCloser(strings.NewReader("0123456789\n")), Request: r}, nil
		})}
		p, err := New(Config{APIKey: "k", BaseURL: base, HTTPClient: client, BatchResultDownloads: providerutils.BatchResultDownloads{MaxLineBytes: 4}})
		if err != nil {
			t.Fatal(err)
		}
		stream, err := p.ExperimentalBatch().DoGetBatchResults(context.Background(), provider.BatchV4OperationOptions{BatchID: "b"})
		if err != nil {
			t.Fatal(err)
		}
		_, err = stream.Next()
		var de *providererrors.DownloadError
		if !errors.As(err, &de) || de.URL != want || requested != want {
			t.Fatalf("base %q: err %v, requested %q, want %q", base, err, requested, want)
		}
	}
}
