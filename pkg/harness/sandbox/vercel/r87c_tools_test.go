package vercel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// Round 87c, V061 48 and 49: @vercel/sandbox 3.9.0 get-credentials.js
// getCredentialsFromOIDCToken, @vercel/oidc 4.0.0 token lookup, and the
// api-client.js / base-client.js query (`{teamId: this.teamId,
// ...params.query}`, url.searchParams.append in object order, undefined and
// null dropped).

func r87ctoolsRawJWT(payload string) string {
	return "h." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".s"
}

func r87ctoolsZodIssue(expected, path, received string) string {
	pathJSON := "[]"
	if path != "" {
		pathJSON = "[\n      \"" + path + "\"\n    ]"
	}
	return "  {\n    \"expected\": \"" + expected + "\",\n    \"code\": \"invalid_type\",\n    \"path\": " + pathJSON +
		",\n    \"message\": \"Invalid input: expected " + expected + ", received " + received + "\"\n  }"
}

func r87ctoolsZodError(issues ...string) string {
	return "Invalid Vercel OIDC token: [\n" + strings.Join(issues, ",\n") + "\n]"
}

// getCredentialsFromOIDCToken, every case checked with a node run of the
// 3.9.0 function and zod 4.5.4 (the version ^4.1.1 resolves to in the TS
// checkout).
func TestR87ctoolsGetCredentialsFromOIDCToken(t *testing.T) {
	ok := base64.RawURLEncoding.EncodeToString([]byte(`{"owner_id":"t","project_id":"p"}`))
	cases := []struct {
		name, token, owner, project, err string
	}{
		{name: "no dot", token: "abc", err: "Invalid Vercel OIDC token: Cannot read properties of undefined (reading 'replace')"},
		{name: "two parts", token: "h." + ok, owner: "t", project: "p"},
		{name: "four parts", token: "h." + ok + ".s.x", owner: "t", project: "p"},
		{name: "empty claims", token: r87ctoolsRawJWT(`{"owner_id":"","project_id":""}`)},
		{name: "padded base64", token: "h." + base64.StdEncoding.EncodeToString([]byte(`{"owner_id":"t","project_id":"p"}`)) + ".s", owner: "t", project: "p"},
		{name: "junk characters skipped", token: "h." + ok[:4] + "!*" + ok[4:] + ".s", owner: "t", project: "p"},
		{name: "missing owner", token: r87ctoolsRawJWT(`{"project_id":"p"}`),
			err: r87ctoolsZodError(r87ctoolsZodIssue("string", "owner_id", "undefined"))},
		{name: "missing both", token: r87ctoolsRawJWT(`{}`),
			err: r87ctoolsZodError(r87ctoolsZodIssue("string", "owner_id", "undefined"), r87ctoolsZodIssue("string", "project_id", "undefined"))},
		{name: "owner number", token: r87ctoolsRawJWT(`{"owner_id":5,"project_id":"p"}`),
			err: r87ctoolsZodError(r87ctoolsZodIssue("string", "owner_id", "number"))},
		{name: "exp string", token: r87ctoolsRawJWT(`{"owner_id":"t","project_id":"p","exp":"1"}`),
			err: r87ctoolsZodError(r87ctoolsZodIssue("number", "exp", "string"))},
		{name: "iat null", token: r87ctoolsRawJWT(`{"owner_id":"t","project_id":"p","iat":null}`),
			err: r87ctoolsZodError(r87ctoolsZodIssue("number", "iat", "null"))},
		{name: "exp null and owner missing", token: r87ctoolsRawJWT(`{"project_id":"p","exp":null}`),
			err: r87ctoolsZodError(r87ctoolsZodIssue("number", "exp", "null"), r87ctoolsZodIssue("string", "owner_id", "undefined"))},
		{name: "null payload", token: r87ctoolsRawJWT(`null`), err: r87ctoolsZodError(r87ctoolsZodIssue("object", "", "null"))},
		{name: "array payload", token: r87ctoolsRawJWT(`[1]`), err: r87ctoolsZodError(r87ctoolsZodIssue("object", "", "array"))},
		{name: "string payload", token: r87ctoolsRawJWT(`"x"`), err: r87ctoolsZodError(r87ctoolsZodIssue("object", "", "string"))},
		{name: "number payload", token: r87ctoolsRawJWT(`5`), err: r87ctoolsZodError(r87ctoolsZodIssue("object", "", "number"))},
	}
	for _, tc := range cases {
		owner, project, err := decodeOIDCClaims(tc.token)
		if tc.err != "" {
			if err == nil || err.Error() != tc.err {
				t.Errorf("%s: err = %v\nwant %s", tc.name, err, tc.err)
			}
			continue
		}
		if err != nil || owner != tc.owner || project != tc.project {
			t.Errorf("%s: = %q, %q, %v", tc.name, owner, project, err)
		}
	}
	// JSON.parse failures keep the TS prefix; the reason is Go's parse
	// error, not V8's SyntaxError text (TS: "Unexpected end of JSON input").
	if _, _, err := decodeOIDCClaims("h..s"); err == nil || !strings.HasPrefix(err.Error(), "Invalid Vercel OIDC token: ") {
		t.Errorf("empty payload: err = %v", err)
	}
}

func r87ctoolsEnv(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("VERCEL_OIDC_TOKEN", "")
	t.Setenv("VERCEL_OIDC_TOKEN_FILE", "")
	t.Setenv("VERCEL_URL", "")
	t.Chdir(t.TempDir())
}

const r87ctoolsLocalOIDCContextText = "Could not get credentials from OIDC context.\nPlease link your Vercel project using `npx vercel link`.\nThen, pull an initial OIDC token with `npx vercel env pull`\nand retry.\n╰▶ Make sure you are loading `.env.local` correctly, or passing $VERCEL_OIDC_TOKEN directly."

// 3.9.0 getCredentials -> getVercelToken -> @vercel/oidc 4.0.0
// getVercelOidcToken (without the opt-in refresh): VERCEL_OIDC_TOKEN_FILE is
// read before VERCEL_OIDC_TOKEN, empty owner_id / project_id claims are
// accepted (z.string()), and every token failure is a LocalOidcContextError,
// which the harness reports as an authentication failure.
func TestR87ctoolsResolveCredentialsFromOIDCToken(t *testing.T) {
	future := time.Now().Add(time.Hour).Unix()
	fileToken := r87ctoolsRawJWT(fmt.Sprintf(`{"owner_id":"team_file","project_id":"prj_file","exp":%d}`, future))
	envToken := r87ctoolsRawJWT(fmt.Sprintf(`{"owner_id":"team_env","project_id":"prj_env","exp":%d}`, future))

	t.Run("token file first", func(t *testing.T) {
		r87ctoolsEnv(t)
		file := filepath.Join(t.TempDir(), "oidc-token")
		if err := os.WriteFile(file, []byte(fileToken+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("VERCEL_OIDC_TOKEN_FILE", file)
		t.Setenv("VERCEL_OIDC_TOKEN", envToken)
		got, err := ResolveCredentials(Credentials{})
		if err != nil || got != (Credentials{Token: fileToken, TeamID: "team_file", ProjectID: "prj_file"}) {
			t.Fatalf("= %+v, %v", got, err)
		}
	})

	t.Run("empty claims", func(t *testing.T) {
		r87ctoolsEnv(t)
		token := r87ctoolsRawJWT(`{"owner_id":"","project_id":""}`)
		t.Setenv("VERCEL_OIDC_TOKEN", token)
		got, err := ResolveCredentials(Credentials{})
		if err != nil || got != (Credentials{Token: token}) {
			t.Fatalf("= %+v, %v", got, err)
		}
	})

	for _, tc := range []struct {
		name, token, cause string
	}{
		// getTokenPayload runs first (isExpired); its error is joined into
		// a VercelOidcTokenError.
		{"not a JWT", "not-a-jwt-token", "\nInvalid token."},
		{"two parts", "h." + base64.RawURLEncoding.EncodeToString([]byte(`{"owner_id":"t","project_id":"p"}`)), "\nInvalid token."},
		{"null payload", r87ctoolsRawJWT("null"), "\nCannot read properties of null (reading 'exp')"},
		{"missing project", r87ctoolsRawJWT(`{"owner_id":"t"}`), r87ctoolsZodError(r87ctoolsZodIssue("string", "project_id", "undefined"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r87ctoolsEnv(t)
			t.Setenv("VERCEL_OIDC_TOKEN", tc.token)
			_, err := ResolveCredentials(Credentials{})
			var ctxErr *vercelOIDCContextError
			if !errors.As(err, &ctxErr) || err.Error() != r87ctoolsLocalOIDCContextText {
				t.Fatalf("err = %v, want the LocalOidcContextError", err)
			}
			if cause := errors.Unwrap(err); cause == nil || cause.Error() != tc.cause {
				t.Fatalf("cause = %v, want %q", cause, tc.cause)
			}
			var authErr *harness.SandboxAuthenticationError
			if !errors.As(withAuthError(err), &authErr) {
				t.Fatal("not an authentication failure")
			}
		})
	}

	t.Run("token file problems", func(t *testing.T) {
		r87ctoolsEnv(t)
		t.Setenv("VERCEL_OIDC_TOKEN", envToken)
		t.Setenv("VERCEL_OIDC_TOKEN_FILE", filepath.Join(t.TempDir(), "missing"))
		_, err := ResolveCredentials(Credentials{})
		var ctxErr *vercelOIDCContextError
		if !errors.As(err, &ctxErr) || !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("missing file: err = %v", err)
		}
		file := filepath.Join(t.TempDir(), "oidc-token")
		if err := os.WriteFile(file, []byte(" \n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("VERCEL_OIDC_TOKEN_FILE", file)
		_, err = ResolveCredentials(Credentials{})
		if !errors.As(err, &ctxErr) || errors.Unwrap(err).Error() != "The Vercel OIDC token file is empty: "+file {
			t.Fatalf("empty file: err = %v", err)
		}
	})

	t.Run("with OIDCRefresh", func(t *testing.T) {
		r87ctoolsEnv(t)
		file := filepath.Join(t.TempDir(), "oidc-token")
		if err := os.WriteFile(file, []byte(fileToken), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("VERCEL_OIDC_TOKEN_FILE", file)
		t.Setenv("VERCEL_OIDC_TOKEN", envToken)
		got, err := resolveCredentialsWithRefresh(context.Background(), Credentials{}, &OIDCRefresh{})
		if err != nil || got.Token != fileToken || got.TeamID != "team_file" {
			t.Fatalf("= %+v, %v", got, err)
		}
	})
}

type r87ctoolsQuerySeen struct {
	mu   sync.Mutex
	uris []string
}

func (s *r87ctoolsQuerySeen) add(r *http.Request) {
	s.mu.Lock()
	s.uris = append(s.uris, r.Method+" "+r.URL.RequestURI())
	s.mu.Unlock()
}

func (s *r87ctoolsQuerySeen) all() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.uris...)
}

// api-client.js request: `teamId` first and always sent (an empty team is
// `teamId=`), then the call's query keys in TS order with "" kept.
func TestR87ctoolsQueryTeamFirstAndEmptyValues(t *testing.T) {
	var seen r87ctoolsQuerySeen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.add(r)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	ctx := context.Background()
	resume := true

	c := NewAPIClient(srv.URL, Credentials{Token: "plain", TeamID: "", ProjectID: ""})
	_, _ = c.GetSandbox(ctx, "sb", &resume)
	_, _ = c.UpdateSandbox(ctx, "sb", updateSandboxRequest{})
	_ = c.DeleteSandbox(ctx, "sb")
	_, _ = c.StopSession(ctx, "s1")
	c = NewAPIClient(srv.URL, Credentials{Token: "plain", TeamID: "team_1", ProjectID: "prj_1"})
	_, _ = c.GetSandbox(ctx, "sb", &resume)
	_, _ = c.GetSandbox(ctx, "sb", nil)
	_, _ = c.GetCommand(ctx, "s1", "c1", true)
	_, _ = c.GetCommand(ctx, "s1", "c1", false)
	// A token whose owner_id is "" is not a JWT for the client (the team
	// stays the credentials' team, here the claims' empty owner).
	empty := r87ctoolsRawJWT(`{"owner_id":"","project_id":""}`)
	c = NewAPIClient(srv.URL, Credentials{Token: empty})
	_, _ = c.StopSession(ctx, "s1")

	want := []string{
		"GET /v2/sandboxes/sb?teamId=&projectId=&resume=true",
		"PATCH /v2/sandboxes/sb?teamId=&projectId=",
		"DELETE /v2/sandboxes/sb?teamId=&projectId=",
		"POST /v2/sandboxes/sessions/s1/stop?teamId=",
		"GET /v2/sandboxes/sb?teamId=team_1&projectId=prj_1&resume=true",
		"GET /v2/sandboxes/sb?teamId=team_1&projectId=prj_1",
		"GET /v2/sandboxes/sessions/s1/cmd/c1?teamId=team_1&wait=true",
		"GET /v2/sandboxes/sessions/s1/cmd/c1?teamId=team_1",
		"POST /v2/sandboxes/sessions/s1/stop?teamId=",
	}
	if got := seen.all(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// Snapshot.list + attachPaginator: no cursor on the first page, listSnapshots
// query order (project, name, cursor, sortOrder), the next page fetched while
// `pagination.next !== null` (an empty page and an empty cursor included).
func TestR87ctoolsSnapshotPagination(t *testing.T) {
	var seen r87ctoolsQuerySeen
	pages := map[string]string{
		"":   `{"snapshots":[{"id":"a","status":"created","creationMethod":"automatic"}],"pagination":{"next":"c1"}}`,
		"c1": `{"snapshots":[],"pagination":{"next":""}}`,
		"e":  `{"snapshots":[{"id":"m","status":"created","creationMethod":"manual"}],"pagination":{"next":"c2"}}`,
		"c2": `{"snapshots":[{"id":"z","status":"created","creationMethod":"manual"}],"pagination":{"next":null}}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.add(r)
		key := r.URL.Query().Get("cursor")
		if r.URL.Query().Has("cursor") && key == "" {
			key = "e"
		}
		_, _ = w.Write([]byte(pages[key]))
	}))
	defer srv.Close()
	c := NewAPIClient(srv.URL, Credentials{Token: "plain", TeamID: "team_1", ProjectID: "prj_1"})

	got, err := c.ListSnapshots(context.Background(), "tpl")
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(got))
	for i, s := range got {
		ids[i] = s.ID
	}
	if strings.Join(ids, ",") != "a,m,z" {
		t.Fatalf("ids = %v", ids)
	}
	want := []string{
		"GET /v2/sandboxes/snapshots?teamId=team_1&project=prj_1&name=tpl&sortOrder=desc",
		"GET /v2/sandboxes/snapshots?teamId=team_1&project=prj_1&name=tpl&cursor=c1&sortOrder=desc",
		"GET /v2/sandboxes/snapshots?teamId=team_1&project=prj_1&name=tpl&cursor=&sortOrder=desc",
		"GET /v2/sandboxes/snapshots?teamId=team_1&project=prj_1&name=tpl&cursor=c2&sortOrder=desc",
	}
	if strings.Join(seen.all(), "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests:\n%s", strings.Join(seen.all(), "\n"))
	}
}

// ensureValidToken: the refreshed team replaces the first `teamId` pair.
func TestR87ctoolsOIDCRefreshTeamStaysFirst(t *testing.T) {
	r87ctoolsEnv(t)
	expiring := r87ctoolsRawJWT(fmt.Sprintf(`{"owner_id":"team_1","project_id":"prj_1","exp":%d}`, time.Now().Add(time.Minute).Unix()))
	fresh := r87ctoolsRawJWT(fmt.Sprintf(`{"owner_id":"team_2","project_id":"prj_1","exp":%d}`, time.Now().Add(time.Hour).Unix()))
	t.Setenv("VERCEL_OIDC_TOKEN", fresh)
	var seen r87ctoolsQuerySeen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.add(r)
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()
	c := newAPIClient(srv.URL, Credentials{Token: expiring, TeamID: "x", ProjectID: "prj_1"}, &OIDCRefresh{})
	resume := false
	_, _ = c.GetSandbox(context.Background(), "sb", &resume)
	want := "GET /v2/sandboxes/sb?teamId=team_2&projectId=prj_1&resume=false"
	if got := seen.all(); len(got) != 1 || got[0] != want {
		t.Fatalf("requests = %q, want %q", got, want)
	}
}

// createSandbox body: `projectId: params.projectId` is the credentials'
// string, so an empty project is sent as "".
func TestR87ctoolsCreateSandboxSendsEmptyProjectID(t *testing.T) {
	bodies := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		bodies <- string(raw)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := NewAPIClient(srv.URL, Credentials{Token: "plain"})
	_, _ = c.CreateSandbox(context.Background(), createSandboxRequest{})
	if body := <-bodies; !strings.HasPrefix(body, `{"projectId":"","ports":[]`) {
		t.Fatalf("body = %s", body)
	}
}
