package fx

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness/fx/internal/libfx"
)

// Ports of the subscription cases of TS fx-transport.test.ts.

// responsesFixtures splits a TS chunks capture into one SSE body per
// response, as TS responsesFixtures does. skipReasoningSnapshots drops the
// intermediate codex reasoning snapshots, as fx-session-native.test.ts does.
func responsesFixtures(t *testing.T, provider string, skipReasoningSnapshots bool) []string {
	t.Helper()
	name := "responses-xai.chunks.txt"
	if provider == "codex" {
		name = "responses-openai.chunks.txt"
	}
	var responses []string
	body := ""
	for _, line := range strings.Split(strings.TrimSpace(readFixture(t, name)), "\n") {
		var part struct {
			Type string `json:"type"`
			Item *struct {
				Type string `json:"type"`
			} `json:"item"`
		}
		_ = json.Unmarshal([]byte(line), &part)
		if skipReasoningSnapshots && provider == "codex" && part.Type == "response.output_item.done" && part.Item != nil && part.Item.Type == "reasoning" {
			continue
		}
		body += "data: " + line + "\n\n"
		if part.Type == "response.completed" {
			responses = append(responses, body)
			body = ""
		}
	}
	return responses
}

func responsesURL(provider string) string {
	if provider == "codex" {
		return "https://chatgpt.com/backend-api/codex/responses"
	}
	return "https://cli-chat-proxy.grok.com/v1/responses"
}

func subscriptionOptions(h *transportHarness, provider string, network libfx.FetchFunc) transportOptions {
	o := h.options(network)
	o.protocol = responsesProtocol{provider: provider}
	o.baseURL = ""
	return o
}

func postBody(t *testing.T, rawURL, body string, header http.Header) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, rawURL, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if header != nil {
		req.Header = header
	}
	return req
}

func TestTransportResponsesToolBatchesBeforeReleaseAndOnReplay(t *testing.T) {
	for _, provider := range []string{"codex", "grok"} {
		t.Run(provider, func(t *testing.T) {
			fixtures := responsesFixtures(t, provider, false)
			h := &transportHarness{turn: newTurnState()}
			fetched := 0
			network := func(req *http.Request) (*http.Response, error) {
				body := fixtures[fetched]
				fetched++
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			}
			var live []string
			for _, replay := range []bool{false, true} {
				o := subscriptionOptions(h, provider, network)
				o.beforeRelease = func(_ context.Context, index int, _ bool) error {
					if !h.turn.Models[index].Complete {
						t.Errorf("request %d released before its record was complete", index)
					}
					return nil
				}
				tr := newTransport(o)
				for i := range fixtures {
					got, err := readAll(t, tr, postBody(t, responsesURL(provider), `{"model":"supplied-model","input":[`+string(rune('0'+i))+`]}`, nil))
					if err != nil {
						t.Fatal(err)
					}
					if got != fixtures[i] {
						t.Fatalf("response %d differs from the fixture", i)
					}
				}
				var calls []string
				for i := range fixtures {
					for _, c := range tr.toolCallsAt(i) {
						var input any
						if err := json.Unmarshal([]byte(c.Input.(string)), &input); err != nil {
							t.Fatal(err)
						}
						calls = append(calls, c.ToolName+" "+mustJSON(input))
					}
				}
				if replay {
					if strings.Join(calls, "\n") != strings.Join(live, "\n") {
						t.Fatalf("replayed calls %v, live %v", calls, live)
					}
				} else {
					live = calls
					want := 0
					if provider == "codex" {
						want = 3
					}
					if len(calls) != want {
						t.Fatalf("calls = %v", calls)
					}
					if provider == "codex" && calls[0] != `calculator {"a":12,"b":7,"op":"add"}` {
						t.Fatalf("first call = %s", calls[0])
					}
				}
				tr.stop()
			}
			if fetched != len(fixtures) {
				t.Fatalf("fetched %d, want %d (the replay must not fetch)", fetched, len(fixtures))
			}
		})
	}
}

func TestTransportResponsesDoesNotRecordRejectedCredentials(t *testing.T) {
	fixture := responsesFixtures(t, "codex", false)[0]
	h := &transportHarness{turn: newTurnState()}
	calls := 0
	tr := newTransport(subscriptionOptions(h, "codex", func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return &http.Response{StatusCode: 401, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("rejected secret")), Request: req}, nil
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(fixture)), Request: req}, nil
	}))
	endpoint := responsesURL("codex")
	resp, err := tr.fetch(postBody(t, endpoint, `{"model":"unchanged"}`, nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 401 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
	if len(h.turn.Models) != 0 {
		t.Fatalf("a rejected response was recorded: %d models", len(h.turn.Models))
	}
	if rej, ok := tr.rejectedAt(0); !ok || rej.status != 401 || rej.modelID == nil || *rej.modelID != "unchanged" {
		t.Fatalf("rejected = %+v %v", rej, ok)
	}
	if _, err := readAll(t, tr, postBody(t, endpoint, `{"model":"unchanged"}`, nil)); err != nil {
		t.Fatal(err)
	}
	if tr.index() != 0 || len(h.turn.Models) != 1 {
		t.Fatalf("requestIndex %d, models %d", tr.index(), len(h.turn.Models))
	}
	state, _ := json.Marshal(h.turn)
	if strings.Contains(string(state), "rejected secret") {
		t.Fatal("the turn state holds the rejected body")
	}
	tr.stop()
}

func TestTransportResponsesTruncatedResponseKeepsToolsUnpublished(t *testing.T) {
	fixture := responsesFixtures(t, "codex", false)[0]
	truncated := fixture[:strings.Index(fixture, `data: {"type":"response.completed"`)]
	h := &transportHarness{turn: newTurnState()}
	o := subscriptionOptions(h, "codex", serve(truncated))
	released := 0
	o.beforeRelease = func(context.Context, int, bool) error { released++; return nil }
	tr := newTransport(o)
	_, err := readAll(t, tr, postBody(t, responsesURL("codex"), `{}`, nil))
	if err == nil || !strings.Contains(err.Error(), "before a terminal event") {
		t.Fatalf("err = %v", err)
	}
	tr.stop()
	if released != 0 || len(tr.toolCallsAt(0)) != 0 {
		t.Fatalf("released %d, calls %d", released, len(tr.toolCallsAt(0)))
	}
	if !strings.Contains(err.Error(), "codex stream ended") {
		t.Fatalf("err = %v", err)
	}
}

func TestTransportScopesGatewayHeadersAwayFromSubscriptionRequests(t *testing.T) {
	fixture := responsesFixtures(t, "grok", false)[0]
	h := &transportHarness{turn: newTurnState()}
	type seen struct {
		url    string
		header http.Header
		body   string
	}
	var got []seen
	o := subscriptionOptions(h, "grok", func(req *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(req.Body)
		got = append(got, seen{req.URL.String(), req.Header.Clone(), string(body)})
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(fixture)), Request: req}, nil
	})
	o.headers = map[string]string{"authorization": "gateway-key", "x-client-app": "gateway"}
	o.responseFormat = json.RawMessage(`{"type":"json","schema":{"type":"object"}}`)
	tr := newTransport(o)
	for _, u := range []string{"https://auth.x.ai/oauth2/token", "https://api.x.ai/v1/models", "https://cli-chat-proxy.grok.com/v1/responses"} {
		if _, err := readAll(t, tr, postBody(t, u, `{"model":"unchanged"}`, http.Header{"Authorization": {"subscription"}})); err != nil {
			t.Fatal(err)
		}
	}
	tr.stop()
	want := []string{
		`https://auth.x.ai/oauth2/token {"model":"unchanged"}`,
		`https://api.x.ai/v1/models {"model":"unchanged"}`,
		`https://cli-chat-proxy.grok.com/v1/responses {"model":"unchanged","text":{"format":{"type":"json_schema","name":"response","schema":{"type":"object"},"strict":true}}}`,
	}
	for i, s := range got {
		if line := s.url + " " + s.body; line != want[i] {
			t.Errorf("request %d = %s\nwant %s", i, line, want[i])
		}
		if len(s.header) != 1 || s.header.Get("authorization") != "subscription" {
			t.Errorf("request %d headers = %v", i, s.header)
		}
	}
}

func TestResponsesProtocolPrepareBody(t *testing.T) {
	p := responsesProtocol{provider: "codex"}
	for _, tc := range []struct{ format, body, want string }{
		{`{"type":"text"}`, `{"model":"m"}`, `{"model":"m"}`},
		{`{"type":"json"}`, `{"text":{"verbosity":"low"},"model":"m"}`, `{"text":{"verbosity":"low","format":{"type":"json_object"}},"model":"m"}`},
		{`{"type":"json","schema":{"type":"object"},"name":"answer","description":"d"}`, `{"model":"m"}`, `{"model":"m","text":{"format":{"type":"json_schema","name":"answer","description":"d","schema":{"type":"object"},"strict":true}}}`},
	} {
		got, err := p.prepareBody([]byte(tc.body), json.RawMessage(tc.format))
		if err != nil || string(got) != tc.want {
			t.Errorf("prepareBody(%s, %s) = %s, %v; want %s", tc.body, tc.format, got, err, tc.want)
		}
	}
	u, _ := http.NewRequest("POST", "https://ChatGPT.com:443/backend-api/codex/responses?stream=1", nil)
	if !p.isModelRequest(u.URL, false) {
		t.Error("a normalized Codex responses URL is a model request")
	}
	other, _ := http.NewRequest("POST", "https://chatgpt.com/backend-api/codex/models", nil)
	if p.isModelRequest(other.URL, false) {
		t.Error("the Codex models endpoint is not a model request")
	}
}

// TestTransportResponsesNeverRewritesOrigins: the subscription protocol
// sends every request to the URL the core named. A path that is itself a
// URL stays a path on the same origin, and is not a model request.
func TestTransportResponsesNeverRewritesOrigins(t *testing.T) {
	for _, provider := range []string{"codex", "grok"} {
		h := &transportHarness{turn: newTurnState()}
		var hosts []string
		tr := newTransport(subscriptionOptions(h, provider, func(req *http.Request) (*http.Response, error) {
			hosts = append(hosts, req.URL.Host+" "+req.URL.EscapedPath())
			return &http.Response{StatusCode: 404, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
		}))
		base := strings.TrimSuffix(responsesURL(provider), "/responses")
		origin := base[:strings.Index(base[len("https://"):], "/")+len("https://")]
		for _, path := range []string{"//evil.example/v1/responses", "/https://evil.example/v1/responses", "/https:/evil.example/x"} {
			resp, err := tr.fetch(postBody(t, origin+path, `{}`, nil))
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
		}
		tr.stop()
		for _, h := range hosts {
			if !strings.HasPrefix(h, origin[len("https://"):]+" ") {
				t.Errorf("%s: request left the origin: %s", provider, h)
			}
		}
		if len(h.turn.Models) != 0 || tr.index() != -1 {
			t.Errorf("%s: a path-as-URL request was treated as a model request", provider)
		}
	}
}
