package typesafeai

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports the "long context" and "image input" blocks of
// typesafe-ai-decision-model.test.ts (TS #23353, #23404).

type capturedCall struct {
	header http.Header
	json   map[string]interface{} // JSON body, or the multipart "request" part
	files  map[string]capturedFile
	order  []string // multipart part names in order
	multi  bool
}

type capturedFile struct {
	filename, contentType string
	data                  []byte
}

type captureServer struct {
	*httptest.Server
	mu    sync.Mutex
	calls []capturedCall
}

func newCaptureServer(t *testing.T) *captureServer {
	t.Helper()
	cs := &captureServer{}
	cs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := capturedCall{header: r.Header.Clone(), files: map[string]capturedFile{}}
		mediaType, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if strings.HasPrefix(mediaType, "multipart/") {
			call.multi = true
			mr := multipart.NewReader(r.Body, params["boundary"])
			for {
				part, err := mr.NextPart()
				if err != nil {
					break
				}
				data, _ := io.ReadAll(part)
				call.order = append(call.order, part.FormName())
				if part.FormName() == "request" {
					_ = json.Unmarshal(data, &call.json)
				}
				call.files[part.FormName()] = capturedFile{filename: part.FileName(), contentType: part.Header.Get("Content-Type"), data: data}
			}
		} else {
			_ = json.NewDecoder(r.Body).Decode(&call.json)
		}
		cs.mu.Lock()
		cs.calls = append(cs.calls, call)
		cs.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"answers":{"q":{"type":"noul","noul":0.5}}}`))
	}))
	t.Cleanup(cs.Close)
	return cs
}

func (cs *captureServer) decide(t *testing.T, opts provider.DecisionCallOptions) (*provider.DecisionResult, error) {
	t.Helper()
	p := New(Config{APIKey: "k", BaseURL: cs.URL})
	model, _ := p.DecisionModel("jev-latest")
	if opts.Questions == nil {
		opts.Questions = map[string]provider.DecisionQuestion{"q": {Type: "boolean", Instructions: "i"}}
	}
	return model.DoDecide(t.Context(), opts)
}

func (cs *captureServer) last() capturedCall {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.calls[len(cs.calls)-1]
}

func (cs *captureServer) count() int {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return len(cs.calls)
}

func imageFile(mediaType, b64 string, opts map[string]interface{}) provider.DecisionStatePart {
	f := types.FileContent{MediaType: mediaType, FileData: types.FileData{Type: types.FileDataTypeData, DataString: b64}}
	if opts != nil {
		f.ProviderOptions = map[string]interface{}{"typesafe": opts}
	}
	return provider.DecisionFilePart(f)
}

func tsOpts(m map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{"typesafe": m}
}

func TestLongContext_HeaderOnlyWhenTrue(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts map[string]interface{}
		want string
	}{{"true", tsOpts(map[string]interface{}{"longContext": true}), "true"}, {"false", tsOpts(map[string]interface{}{"longContext": false}), ""}, {"unset", nil, ""}} {
		t.Run(tc.name, func(t *testing.T) {
			cs := newCaptureServer(t)
			res, err := cs.decide(t, provider.DecisionCallOptions{State: "x", ProviderOptions: tc.opts})
			if err != nil {
				t.Fatal(err)
			}
			if got := cs.last().header.Get("TypeSafe-Danger-Zone-Long-Context"); got != tc.want {
				t.Fatalf("header = %q, want %q", got, tc.want)
			}
			if len(res.Warnings) != 0 {
				t.Fatalf("warnings = %v", res.Warnings)
			}
		})
	}
}

func TestLongContext_CombinesWithImagesAndWarnsOnlyForUnknown(t *testing.T) {
	cs := newCaptureServer(t)
	res, err := cs.decide(t, provider.DecisionCallOptions{
		State:           []provider.DecisionStatePart{imageFile("image/png", "iVBORw==", nil)},
		ProviderOptions: tsOpts(map[string]interface{}{"longContext": true, "temperature": 0}),
		Headers:         map[string]string{"x-custom": "custom"},
	})
	if err != nil {
		t.Fatal(err)
	}
	h := cs.last().header
	if h.Get("TypeSafe-Danger-Zone-Images") != "true" || h.Get("TypeSafe-Danger-Zone-Long-Context") != "true" || h.Get("x-custom") != "custom" {
		t.Fatalf("headers = %v", h)
	}
	want := []types.Warning{{Type: "unsupported", Feature: "providerOptions.typesafe.temperature"}}
	if !reflect.DeepEqual(res.Warnings, want) {
		t.Fatalf("warnings = %v", res.Warnings)
	}
}

func TestLongContext_RejectsInvalidValues(t *testing.T) {
	for _, v := range []interface{}{"true", 1, nil} {
		cs := newCaptureServer(t)
		_, err := cs.decide(t, provider.DecisionCallOptions{State: "x", ProviderOptions: tsOpts(map[string]interface{}{"longContext": v})})
		var ia *providererrors.InvalidArgumentError
		if !asInvalidArgument(err, &ia) || cs.count() != 0 {
			t.Fatalf("%v: err=%v calls=%d", v, err, cs.count())
		}
	}
}

func TestImages_ExplicitReferenceInAssetsStateAndPartName(t *testing.T) {
	for _, ref := range []string{"[animal-photo]", "__proto__", "constructor", "request"} {
		cs := newCaptureServer(t)
		res, err := cs.decide(t, provider.DecisionCallOptions{
			State: []provider.DecisionStatePart{
				{Type: "file", File: &types.FileContent{Filename: "unrelated-name.png", MediaType: "image/png",
					FileData:        types.FileData{Type: types.FileDataTypeData, DataString: "iVBORw=="},
					ProviderOptions: tsOpts(map[string]interface{}{"reference": ref})}},
				imageFile("image/jpeg", "/9j/4A==", nil),
			},
		})
		if err != nil {
			t.Fatalf("%s: %v", ref, err)
		}
		call := cs.last()
		assets, _ := call.json["assets"].(map[string]interface{})
		if len(assets) != 2 {
			t.Fatalf("%s: assets = %v", ref, assets)
		}
		var generated string
		for k := range assets {
			if k != ref {
				generated = k
			}
		}
		if generated == "" || call.json["state"] != ref+"\n"+generated {
			t.Fatalf("%s: state = %v assets=%v", ref, call.json["state"], assets)
		}
		if !reflect.DeepEqual(assets[ref], map[string]interface{}{"type": "image", "part": "image-0"}) {
			t.Fatalf("%s: asset = %v", ref, assets[ref])
		}
		if call.files["image-0"].filename != "unrelated-name.png" || call.files["image-1"].filename != "image-1.jpeg" {
			t.Fatalf("filenames = %v", call.files)
		}
		if len(res.Warnings) != 0 {
			t.Fatalf("warnings = %v", res.Warnings)
		}
	}
}

func TestImages_RejectsInvalidReferencesBeforeHTTP(t *testing.T) {
	for _, ref := range []interface{}{"", " \t\n", 123, true, nil} {
		cs := newCaptureServer(t)
		_, err := cs.decide(t, provider.DecisionCallOptions{State: []provider.DecisionStatePart{
			imageFile("image/png", "iVBORw==", map[string]interface{}{"reference": ref}),
		}})
		var ia *providererrors.InvalidArgumentError
		if !asInvalidArgument(err, &ia) || cs.count() != 0 {
			t.Fatalf("%#v: err=%v calls=%d", ref, err, cs.count())
		}
	}
}

func TestImages_RejectsDuplicateReferences(t *testing.T) {
	cs := newCaptureServer(t)
	ref := map[string]interface{}{"reference": "[photo]"}
	_, err := cs.decide(t, provider.DecisionCallOptions{State: []provider.DecisionStatePart{
		imageFile("image/png", "iVBORw==", ref), imageFile("image/png", "/9j/4A==", ref),
	}})
	var ia *providererrors.InvalidArgumentError
	if !asInvalidArgument(err, &ia) || ia.Field != "providerOptions.typesafe.reference" || ia.Message != "Duplicate TypeSafe image reference: [photo]" || cs.count() != 0 {
		t.Fatalf("err=%v calls=%d", err, cs.count())
	}
}

func TestImages_MixedPartsMapToReferencedAssetsAndMultipart(t *testing.T) {
	cs := newCaptureServer(t)
	_, err := cs.decide(t, provider.DecisionCallOptions{State: []provider.DecisionStatePart{
		provider.DecisionTextPart("Compare these images."),
		imageFile("image/png", base64.StdEncoding.EncodeToString([]byte{137, 80, 78, 71}), nil),
		provider.DecisionJSONPart(map[string]interface{}{"label": "second"}),
		imageFile("image/jpeg", "/9j/4A==", nil),
	}})
	if err != nil {
		t.Fatal(err)
	}
	call := cs.last()
	assets := call.json["assets"].(map[string]interface{})
	var refs [2]string
	for k, v := range assets {
		switch v.(map[string]interface{})["part"] {
		case "image-1":
			refs[0] = k
		case "image-3":
			refs[1] = k
		}
	}
	if refs[0] == "" || refs[1] == "" || refs[0] == refs[1] || len(assets) != 2 {
		t.Fatalf("assets = %v", assets)
	}
	if want := "Compare these images.\n" + refs[0] + "\n{\"label\":\"second\"}\n" + refs[1]; call.json["state"] != want {
		t.Fatalf("state = %q, want %q", call.json["state"], want)
	}
	if !reflect.DeepEqual(call.order, []string{"request", "image-1", "image-3"}) {
		t.Fatalf("parts = %v", call.order)
	}
	if call.files["request"].contentType != "application/json" || call.files["request"].filename != "request.json" {
		t.Fatalf("request part = %+v", call.files["request"])
	}
	if f := call.files["image-1"]; f.contentType != "image/png" || f.filename != "image-1.png" || !reflect.DeepEqual(f.data, []byte{137, 80, 78, 71}) {
		t.Fatalf("image-1 = %+v", f)
	}
	if f := call.files["image-3"]; f.contentType != "image/jpeg" || !reflect.DeepEqual(f.data, []byte{255, 216, 255, 224}) {
		t.Fatalf("image-3 = %+v", f)
	}
	if !strings.HasPrefix(call.header.Get("Content-Type"), "multipart/form-data; boundary=") || call.header.Get("TypeSafe-Danger-Zone-Images") != "true" {
		t.Fatalf("headers = %v", call.header)
	}
}

func TestImages_TextOnlyHasNoAssetsOrHeader(t *testing.T) {
	cs := newCaptureServer(t)
	if _, err := cs.decide(t, provider.DecisionCallOptions{State: "hello"}); err != nil {
		t.Fatal(err)
	}
	call := cs.last()
	if _, ok := call.json["assets"]; ok || call.multi || call.header.Get("TypeSafe-Danger-Zone-Images") != "" {
		t.Fatalf("call = %+v", call)
	}
}

func TestImages_FreshReferencesPerRequest(t *testing.T) {
	cs := newCaptureServer(t)
	opts := provider.DecisionCallOptions{State: []provider.DecisionStatePart{imageFile("image/png", "iVBORw0KGgo=", nil)}}
	for range 2 {
		if _, err := cs.decide(t, opts); err != nil {
			t.Fatal(err)
		}
	}
	if cs.calls[0].json["state"] == cs.calls[1].json["state"] {
		t.Fatal("references were reused across requests")
	}
}

func TestImages_RejectsUnsupportedMediaAndData(t *testing.T) {
	for _, mt := range []string{"application/pdf", "image/webp", "image/gif", "image"} {
		cs := newCaptureServer(t)
		_, err := cs.decide(t, provider.DecisionCallOptions{State: []provider.DecisionStatePart{imageFile(mt, "AAAA", nil)}})
		if !providererrors.IsUnsupportedFunctionalityError(err) || cs.count() != 0 {
			t.Fatalf("%s: err=%v calls=%d", mt, err, cs.count())
		}
	}
	for _, fd := range []types.FileData{
		{Type: types.FileDataTypeURL, URL: "https://example.com/image.png"},
		{Type: types.FileDataTypeReference, Reference: map[string]string{"typesafe": "file-id"}},
		{Type: types.FileDataTypeText, Text: "not an image"},
	} {
		cs := newCaptureServer(t)
		_, err := cs.decide(t, provider.DecisionCallOptions{State: []provider.DecisionStatePart{
			provider.DecisionFilePart(types.FileContent{MediaType: "image/png", FileData: fd}),
		}})
		if !providererrors.IsUnsupportedFunctionalityError(err) || cs.count() != 0 {
			t.Fatalf("%s: err=%v calls=%d", fd.Type, err, cs.count())
		}
	}
}

// TS #23404: insertReference and duplicate-reference avoidance.

func TestImages_KeepsJSONStateAsObjectWhenItContainsTheReference(t *testing.T) {
	cs := newCaptureServer(t)
	_, err := cs.decide(t, provider.DecisionCallOptions{State: []provider.DecisionStatePart{
		provider.DecisionJSONPart(map[string]interface{}{"photo": "[photo]", "note": "Box arrived wet."}),
		imageFile("image/png", "iVBORw==", map[string]interface{}{"reference": "[photo]"}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	call := cs.last()
	if !reflect.DeepEqual(call.json["state"], map[string]interface{}{"photo": "[photo]", "note": "Box arrived wet."}) {
		t.Fatalf("state = %v", call.json["state"])
	}
	if !reflect.DeepEqual(call.json["assets"], map[string]interface{}{"[photo]": map[string]interface{}{"type": "image", "part": "image-1"}}) {
		t.Fatalf("assets = %v", call.json["assets"])
	}
	if !reflect.DeepEqual(call.order, []string{"request", "image-1"}) {
		t.Fatalf("parts = %v", call.order)
	}
}

func TestImages_LeavesTextStateUnchangedWhenItContainsTheReference(t *testing.T) {
	cs := newCaptureServer(t)
	_, err := cs.decide(t, provider.DecisionCallOptions{State: []provider.DecisionStatePart{
		provider.DecisionTextPart("Is [photo] damaged?"),
		imageFile("image/jpeg", "/9j/4A==", map[string]interface{}{"reference": "[photo]"}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := cs.last().json["state"]; got != "Is [photo] damaged?" {
		t.Fatalf("state = %v", got)
	}
}

func TestImages_FindsReferencesInNestedJSONStringsButNotKeys(t *testing.T) {
	cs := newCaptureServer(t)
	value := map[string]interface{}{
		"frames":  []interface{}{map[string]interface{}{"seconds_ago": 0.0, "frame": "Latest frame: [frame]"}},
		"[label]": "A key is not a reference.",
	}
	_, err := cs.decide(t, provider.DecisionCallOptions{State: []provider.DecisionStatePart{
		provider.DecisionJSONPart(value),
		imageFile("image/png", "iVBORw==", map[string]interface{}{"reference": "[frame]"}),
		imageFile("image/png", "iVBORw==", map[string]interface{}{"reference": "[label]"}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(value)
	if want := string(encoded) + "\n[label]"; cs.last().json["state"] != want {
		t.Fatalf("state = %q, want %q", cs.last().json["state"], want)
	}
}

func TestImages_InsertReferenceFalseSendsQuestionOnlyImage(t *testing.T) {
	cs := newCaptureServer(t)
	_, err := cs.decide(t, provider.DecisionCallOptions{
		State: []provider.DecisionStatePart{
			provider.DecisionJSONPart(map[string]interface{}{"photo": "[found]"}),
			imageFile("image/jpeg", "/9j/4A==", map[string]interface{}{"reference": "[found]"}),
			imageFile("image/jpeg", "/9j/4A==", map[string]interface{}{"reference": "[poster]", "insertReference": false}),
		},
		Questions: map[string]provider.DecisionQuestion{"same": {Type: "boolean", Instructions: "Is the dog in `photo` the dog in [poster]?"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	call := cs.last()
	if !reflect.DeepEqual(call.json["state"], map[string]interface{}{"photo": "[found]"}) {
		t.Fatalf("state = %v", call.json["state"])
	}
	assets := call.json["assets"].(map[string]interface{})
	if len(assets) != 2 || assets["[found]"] == nil || assets["[poster]"] == nil {
		t.Fatalf("assets = %v", assets)
	}
}

func TestImages_InsertReferenceTrueInsertsEvenWhenPresent(t *testing.T) {
	cs := newCaptureServer(t)
	_, err := cs.decide(t, provider.DecisionCallOptions{State: []provider.DecisionStatePart{
		provider.DecisionTextPart("Before: [photo]"),
		imageFile("image/png", "iVBORw==", map[string]interface{}{"reference": "[photo]", "insertReference": true}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := cs.last().json["state"]; got != "Before: [photo]\n[photo]" {
		t.Fatalf("state = %v", got)
	}
}

func TestImages_InsertsOnlyReferencesNotAlreadyPlaced(t *testing.T) {
	cs := newCaptureServer(t)
	_, err := cs.decide(t, provider.DecisionCallOptions{State: []provider.DecisionStatePart{
		provider.DecisionTextPart("Compare [before] with the new photo."),
		imageFile("image/png", "iVBORw==", map[string]interface{}{"reference": "[before]", "insertReference": false}),
		imageFile("image/png", "iVBORw==", map[string]interface{}{"reference": "[after]"}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	call := cs.last()
	if got := call.json["state"]; got != "Compare [before] with the new photo.\n[after]" {
		t.Fatalf("state = %v", got)
	}
	want := map[string]interface{}{
		"[before]": map[string]interface{}{"type": "image", "part": "image-1"},
		"[after]":  map[string]interface{}{"type": "image", "part": "image-2"},
	}
	if !reflect.DeepEqual(call.json["assets"], want) {
		t.Fatalf("assets = %v", call.json["assets"])
	}
}

func TestImages_InsertReferenceFalseNeedsExplicitReference(t *testing.T) {
	cs := newCaptureServer(t)
	_, err := cs.decide(t, provider.DecisionCallOptions{State: []provider.DecisionStatePart{
		imageFile("image/png", "iVBORw==", map[string]interface{}{"insertReference": false}),
	}})
	var ia *providererrors.InvalidArgumentError
	if !asInvalidArgument(err, &ia) || ia.Field != "providerOptions.typesafe.reference" || cs.count() != 0 {
		t.Fatalf("err=%v calls=%d", err, cs.count())
	}
}

func TestModelIDsAreSentUnchanged(t *testing.T) {
	for _, id := range []string{"jev-1.13.0", "jev-1.14.0", "jev-latest", "jev-preview"} {
		cs := newCaptureServer(t)
		p := New(Config{APIKey: "k", BaseURL: cs.URL})
		model, _ := p.DecisionModel(id)
		_, err := model.DoDecide(t.Context(), provider.DecisionCallOptions{State: "x", Questions: map[string]provider.DecisionQuestion{"q": {Type: "boolean", Instructions: "i"}}})
		if err != nil || cs.last().json["model"] != id {
			t.Fatalf("%s: err=%v body=%v", id, err, cs.last().json)
		}
	}
}

func asInvalidArgument(err error, target **providererrors.InvalidArgumentError) bool {
	return errors.As(err, target)
}

// JSON.stringify leaves <, > and & literal; the state text and the request
// bytes must too (json.Marshal would write < etc.).
func TestJoinedStateAndRequestKeepHTMLCharactersLiteral(t *testing.T) {
	value := map[string]interface{}{"note": "a<b>&c"}
	cases := map[string][]provider.DecisionStatePart{
		"multipart": {
			provider.DecisionJSONPart(value),
			provider.DecisionTextPart("x<y"),
			imageFile("image/png", "iVBORw==", map[string]interface{}{"reference": "[p]"}),
		},
		"json": {
			provider.DecisionJSONPart(value),
			provider.DecisionTextPart("x<y"),
		},
	}
	for name, state := range cases {
		t.Run(name, func(t *testing.T) {
			var raw []byte
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mediaType, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
				if strings.HasPrefix(mediaType, "multipart/") {
					mr := multipart.NewReader(r.Body, params["boundary"])
					for {
						part, err := mr.NextPart()
						if err != nil {
							break
						}
						if part.FormName() == "request" {
							raw, _ = io.ReadAll(part)
						}
					}
				} else {
					raw, _ = io.ReadAll(r.Body)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"answers":{"q":{"type":"noul","noul":0.5}}}`))
			}))
			defer srv.Close()
			cs := &captureServer{Server: srv}
			if _, err := cs.decide(t, provider.DecisionCallOptions{State: state}); err != nil {
				t.Fatal(err)
			}
			got := string(raw)
			if strings.Contains(got, "\\u003c") || strings.Contains(got, "\\u003e") || strings.Contains(got, "\\u0026") {
				t.Fatalf("request body HTML-escaped: %s", got)
			}
			if !strings.Contains(got, `a<b>&c`) || !strings.Contains(got, `x<y`) {
				t.Fatalf("request body = %s", got)
			}
		})
	}
}
