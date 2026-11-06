package anthropic

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
)

func skillFixture(t *testing.T, name string) []byte {
	t.Helper()
	// The skills fixtures live next to the TS skills tests.
	b, err := os.ReadFile(filepath.Join("testdata", "ts", "skills", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type skillCall struct {
	method, uri, beta, apiKey string
	form                      map[string][]string
	fileNames                 []string
}

func skillsServer(t *testing.T, create, version []byte, calls *[]skillCall) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := skillCall{method: r.Method, uri: r.RequestURI, beta: r.Header.Get("anthropic-beta"), apiKey: r.Header.Get("x-api-key")}
		if r.Method == http.MethodPost {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("multipart: %v", err)
			} else {
				c.form = r.MultipartForm.Value
				for _, fh := range r.MultipartForm.File["files[]"] {
					c.fileNames = append(c.fileNames, fh.Filename)
				}
			}
		} else {
			_, _ = io.Copy(io.Discard, r.Body)
		}
		*calls = append(*calls, c)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			_, _ = w.Write(create)
		} else {
			_, _ = w.Write(version)
		}
	}))
}

func skillFile() types.UploadSkillFile {
	return types.UploadSkillFile{Path: "index.ts", Data: types.FileData{Type: types.FileDataTypeData, DataString: base64.StdEncoding.EncodeToString([]byte(`console.log("hello")`))}}
}

// anthropic/src/skills/anthropic-skills.test.ts with its recorded fixtures.
func TestFixtureSkillsUpload(t *testing.T) {
	var calls []skillCall
	srv := skillsServer(t, skillFixture(t, "anthropic-skill-create"), skillFixture(t, "anthropic-skill-version-create"), &calls)
	defer srv.Close()
	skills := New(Config{APIKey: "test-api-key", BaseURL: srv.URL}).Skills()

	res, err := skills.UploadSkill(context.Background(), types.UploadSkillOptions{Files: []types.UploadSkillFile{skillFile()}})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0].method != "POST" || len(calls[0].fileNames) != 1 {
		t.Fatalf("calls = %+v", calls)
	}
	if calls[0].beta != "skills-2025-10-02" || calls[0].apiKey != "test-api-key" {
		t.Fatalf("headers = %+v", calls[0])
	}
	if _, has := calls[0].form["display_title"]; has {
		t.Fatal("display_title sent without a displayTitle")
	}
	if res.ProviderReference["anthropic"] != "skill_01Xud7kLMsjLfc7Aa6RvigZf" || res.DisplayTitle != "Test Capture Skill" ||
		res.Name != "test-capture-skill" || res.Description != "An updated test skill for fixture capture" || res.LatestVersion != "1772078378207930" {
		t.Fatalf("result = %+v", res)
	}
	meta := res.ProviderMetadata["anthropic"].(map[string]interface{})
	if meta["source"] != "custom" || meta["createdAt"] != "2026-02-26T03:59:39.314772Z" || meta["updatedAt"] != "2026-02-26T03:59:39.314772Z" {
		t.Fatalf("metadata = %+v", meta)
	}
	if res.Warnings == nil || len(res.Warnings) != 0 {
		t.Fatalf("warnings = %#v", res.Warnings)
	}

	calls = nil
	if _, err := skills.UploadSkill(context.Background(), types.UploadSkillOptions{Files: []types.UploadSkillFile{skillFile()}, DisplayTitle: "My Custom Title"}); err != nil {
		t.Fatal(err)
	}
	if got := calls[0].form["display_title"]; len(got) != 1 || got[0] != "My Custom Title" {
		t.Fatalf("display_title = %v", got)
	}
}

// "should preserve skill $skillId and version $version as URL path segments".
func TestFixtureSkillsPathSegments(t *testing.T) {
	cases := []struct{ skillID, version, wantSkill, wantVersion string }{
		{"skill/../../admin", "v1/../../admin", "skill%2F..%2F..%2Fadmin", "v1%2F..%2F..%2Fadmin"},
		{".", "version-1", "%252E", "version-1"},
		{"..", "version-1", "%252E%252E", "version-1"},
		{"skill-1", ".", "skill-1", "%252E"},
		{"skill-1", "..", "skill-1", "%252E%252E"},
	}
	for _, tc := range cases {
		var calls []skillCall
		create := fmt.Sprintf(`{"id":%q,"latest_version":%q,"source":"custom","created_at":"2026-08-24T00:00:00Z","updated_at":"2026-08-24T00:00:00Z"}`, tc.skillID, tc.version)
		version := fmt.Sprintf(`{"type":"skill_version","skill_id":%q,"name":"test-skill","description":"test skill"}`, tc.skillID)
		srv := skillsServer(t, []byte(create), []byte(version), &calls)
		skills := New(Config{APIKey: "k", BaseURL: srv.URL}).Skills()
		if _, err := skills.UploadSkill(context.Background(), types.UploadSkillOptions{Files: []types.UploadSkillFile{skillFile()}}); err != nil {
			srv.Close()
			t.Fatal(err)
		}
		srv.Close()
		want := "/skills/" + tc.wantSkill + "/versions/" + tc.wantVersion
		if len(calls) != 2 || !strings.HasSuffix(calls[1].uri, want) {
			t.Fatalf("%q/%q: second request = %+v, want suffix %s", tc.skillID, tc.version, calls, want)
		}
	}
}

// TS snapshots "should expose container information as provider metadata"
// (agent skills with the pptx skill, and plain code execution).
func TestFixtureContainerProviderMetadata(t *testing.T) {
	ce := []types.Tool{tools.CodeExecution20250825()}
	res := generateFixture(t, "anthropic-code-execution-20250825.pptx-skill", &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Tools: ce})
	meta := anthropicMeta(t, res.ProviderMetadata)
	if got, want := jsonString(t, meta["container"]), `{"expiresAt":"2025-10-20T11:48:47.798310Z","id":"container_011CUJKA7aVjWm5XJL9ozHGS","skills":[{"skillId":"pptx","type":"anthropic","version":"20251013"}]}`; got != want {
		t.Fatalf("container\n got %s\nwant %s", got, want)
	}
	if meta["contextManagement"] != nil || meta["iterations"] != nil || meta["stopSequence"] != nil {
		t.Fatalf("metadata = %v", meta)
	}
	usage := meta["usage"].(map[string]interface{})
	if usage["input_tokens"] != float64(950648) || usage["output_tokens"] != float64(13856) || usage["service_tier"] != "standard" {
		t.Fatalf("usage = %v", usage)
	}

	res = generateFixture(t, "anthropic-code-execution-20250825.1", &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Tools: ce})
	meta = anthropicMeta(t, res.ProviderMetadata)
	if got, want := jsonString(t, meta["container"]), `{"expiresAt":"2025-10-14T10:28:40.590791Z","id":"container_011CU6rVteVhydYXRyNs6G1M","skills":null}`; got != want {
		t.Fatalf("container\n got %s\nwant %s", got, want)
	}
}
