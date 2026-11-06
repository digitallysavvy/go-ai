package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestSkillsAPI_UploadSkill(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/skills" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":              "skill_1",
			"name":            "skill-name",
			"description":     "desc",
			"latest_version":  "1",
			"default_version": "1",
			"created_at":      100,
		})
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	res, err := p.Skills().UploadSkill(context.Background(), types.UploadSkillOptions{
		DisplayTitle: "My Skill",
		Files: []types.UploadSkillFile{{
			Path: "index.ts",
			Data: types.FileData{Type: types.FileDataTypeData, Data: []byte("x")},
		}},
	})
	if err != nil {
		t.Fatalf("UploadSkill() err = %v", err)
	}
	if res.ProviderReference["openai"] != "skill_1" {
		t.Fatalf("ProviderReference = %+v", res.ProviderReference)
	}
	if len(res.Warnings) == 0 || res.Warnings[0].Feature != "displayTitle" {
		t.Fatalf("warnings = %+v", res.Warnings)
	}
}

// Ported from packages/openai/src/skills/openai-skills.test.ts.
func TestSkillsAPI_UploadSkillWire(t *testing.T) {
	var auth, filename, partName, partType string
	var content []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		mr, err := r.MultipartReader()
		if err != nil {
			t.Errorf("multipart: %v", err)
			return
		}
		part, err := mr.NextPart()
		if err != nil {
			t.Errorf("part: %v", err)
			return
		}
		partName, partType = part.FormName(), part.Header.Get("Content-Type")
		filename = part.FileName()
		content, _ = io.ReadAll(part)
		_, _ = w.Write([]byte(`{"id":"skill_699","object":"skill","name":"test-capture-skill","description":"A test skill","default_version":"1","latest_version":"1","created_at":1772078479}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "test-api-key", BaseURL: srv.URL})
	res, err := p.Skills().UploadSkill(context.Background(), types.UploadSkillOptions{
		Files: []types.UploadSkillFile{{Path: "index.ts", Data: types.FileData{Type: types.FileDataTypeData, DataString: "aGVsbG8="}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer test-api-key" || partName != "files[]" || partType != "application/octet-stream" || string(content) != "hello" {
		t.Fatalf("wire: auth=%q name=%q type=%q content=%q", auth, partName, partType, content)
	}
	if filename != "index.ts" {
		t.Fatalf("filename = %q", filename)
	}
	if res.Name != "test-capture-skill" || res.LatestVersion != "1" {
		t.Fatalf("result = %+v", res)
	}
	meta, _ := res.ProviderMetadata["openai"].(map[string]interface{})
	if meta["defaultVersion"] != "1" || meta["createdAt"] != int64(1772078479) || len(meta) != 2 {
		t.Fatalf("metadata = %+v", meta)
	}
	if res.Warnings == nil || len(res.Warnings) != 0 {
		t.Fatalf("warnings must be an empty list, got %#v", res.Warnings)
	}
}
