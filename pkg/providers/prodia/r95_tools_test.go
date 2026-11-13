package prodia

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r95toolsInputPart runs fn against a server that records the "input" part
// of the multipart job request, answering with resp (a multipart body).
func r95toolsInputPart(t *testing.T, resp []byte, respCT string, fn func(baseURL string)) (contentType, filename string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mr, err := r.MultipartReader(); err == nil {
			for {
				p, err := mr.NextPart()
				if err != nil {
					break
				}
				if p.FormName() == "input" {
					contentType, filename = p.Header.Get("Content-Type"), p.FileName()
				}
			}
		}
		w.Header().Set("Content-Type", respCT)
		_, _ = w.Write(resp)
	}))
	defer srv.Close()
	fn(srv.URL)
	return contentType, filename
}

// TS prodia-video-model.ts getExtension maps only image/png, image/jpeg,
// image/webp, video/mp4 and video/webm: an "image/jpg" input is named
// "input", not "input.jpg".
func TestR95toolsProdiaImg2VidImageJpgHasNoExtension(t *testing.T) {
	body, ct := buildTestMultipartBody(`{"id":"j"}`, []byte("vid"), "video/mp4")
	gotCT, gotName := r95toolsInputPart(t, body, ct, func(baseURL string) {
		vm, _ := New(Config{APIKey: "k", BaseURL: baseURL}).VideoModel(r94toolsImg2Vid)
		if _, err := vm.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
			Prompt: "p", Image: &provider.VideoModelV3File{Type: "file", Data: []byte("img"), MediaType: "image/jpg"},
		}); err != nil {
			t.Fatal(err)
		}
	})
	if gotCT != "image/jpg" || gotName != "input" {
		t.Fatalf("input part = %q %q, want image/jpg named input", gotCT, gotName)
	}
}

// TS prodia-language-model.ts: the input filename extension comes from the
// png / jpeg / webp ternary only, and a media type that is not a full one
// (isFullMediaType: "image/" has an empty subtype) is detected from the
// bytes.
func TestR95toolsProdiaLanguageModelInputMediaType(t *testing.T) {
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 0}
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0x10, 'J', 'F', 'I', 'F', 0}
	for _, tc := range []struct {
		mediaType string
		data      []byte
		wantCT    string
		wantName  string
	}{
		{"image/", jpeg, "image/jpeg", "input.jpg"},
		{"image/jpg", png, "image/jpg", "input"},
		{"image/png", png, "image/png", "input.png"},
	} {
		body, ct := buildTestMultipartBody(`{"id":"j"}`, png, "image/png")
		gotCT, gotName := r95toolsInputPart(t, body, ct, func(baseURL string) {
			model := NewLanguageModel(New(Config{APIKey: "k", BaseURL: baseURL}), LanguageModelNanoBananaImgToImgV2)
			if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{{
				Role: types.RoleUser,
				Content: []types.ContentPart{
					types.TextContent{Text: "edit"},
					types.FileContent{MediaType: tc.mediaType, FileData: types.FileData{Type: types.FileDataTypeData, Data: tc.data}},
				},
			}}}}); err != nil {
				t.Fatalf("%s: %v", tc.mediaType, err)
			}
		})
		if gotCT != tc.wantCT || gotName != tc.wantName {
			t.Errorf("%s: input part = %q %q, want %q %q", tc.mediaType, gotCT, gotName, tc.wantCT, tc.wantName)
		}
	}
}
