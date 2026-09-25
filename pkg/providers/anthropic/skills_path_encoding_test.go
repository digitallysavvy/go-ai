package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS anthropic-skills.ts encodePathSegment (7de3612): the provider-returned
// skill id and version are path-encoded in the credentialed version URL.
func TestSkillsAPI_EncodesProviderReturnedIDs(t *testing.T) {
	tests := []struct{ id, version, want string }{
		{"skill_123", "1", "/v1/skills/skill_123/versions/1"},
		{"abc/../../internal", "..", "/v1/skills/abc%2F..%2F..%2Finternal/versions/%252E%252E"},
		{".", "1?x=y", "/v1/skills/%252E/versions/1%3Fx%3Dy"},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			var versionPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/skills" {
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"id":             tt.id,
						"latest_version": tt.version,
						"source":         "custom",
					})
					return
				}
				versionPath = r.URL.EscapedPath()
				if r.URL.RawQuery != "" {
					versionPath += "?" + r.URL.RawQuery
				}
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"name": "n"})
			}))
			defer srv.Close()

			p := New(Config{APIKey: "k", BaseURL: srv.URL})
			_, err := p.Skills().UploadSkill(context.Background(), types.UploadSkillOptions{
				Files: []types.UploadSkillFile{{Path: "index.ts", Data: types.FileData{Type: types.FileDataTypeData, Data: []byte("x")}}},
			})
			if err != nil {
				t.Fatalf("UploadSkill() err = %v", err)
			}
			if versionPath != tt.want || strings.Contains(versionPath, "?") {
				t.Fatalf("version request path = %q, want %q", versionPath, tt.want)
			}
		})
	}
}
