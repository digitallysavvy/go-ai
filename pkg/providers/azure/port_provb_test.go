package azure

import (
	"context"
	stdhttp "net/http"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports of the TS tests added by 3a48c5dca4 (azure-mai-image-model.test.ts,
// azure-mai-image-generate-image.test.ts): MAI-Image-2.7 models and the
// background option.

func portprovbBackground(background string, api string) map[string]interface{} {
	azure := map[string]interface{}{"background": background}
	if api != "" {
		azure["api"] = api
	}
	return map[string]interface{}{"azure": azure}
}

var portprovbMaiWarning = types.Warning{
	Type: "unsupported", Feature: "providerOptions.azure.background",
	Details: "The MAI image API supports 'transparent' and 'opaque' backgrounds.",
}

// TS 'API routing' it.each: MAI-Image-2.7 and mai-image-2.7-flash route to mai.
func TestPortprovbMaiImage27RoutesToMai(t *testing.T) {
	for _, id := range []string{"MAI-Image-2.7", "mai-image-2.7-flash"} {
		server, c := newMaiServer(t, nil)
		if _, err := maiImageModel(t, server, id).DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "A red apple"}); err != nil {
			t.Fatal(err)
		}
		if len(c.paths) != 1 || c.paths[0] != "/mai/v1/images/generations" {
			t.Errorf("%s: paths = %v", id, c.paths)
		}
		if !isMAIImageModel(id) {
			t.Errorf("isMAIImageModel(%q) = false", id)
		}
	}
}

// TS 'sends background %s' (generations) and the generateImage integration
// 'sends a transparent background for MAI-Image-2.7 models'.
func TestPortprovbMaiImageSendsBackground(t *testing.T) {
	for _, background := range []string{"transparent", "opaque"} {
		server, c := newMaiServer(t, nil)
		result, err := maiImageModel(t, server, "MAI-Image-2.7").DoGenerate(context.Background(), &provider.ImageGenerateOptions{
			Prompt: "A red apple", ProviderOptions: portprovbBackground(background, ""),
		})
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]interface{}{"model": "MAI-Image-2.7", "prompt": "A red apple", "background": background}
		if !reflect.DeepEqual(c.jsonBody, want) {
			t.Errorf("%s: body = %#v, want %#v", background, c.jsonBody, want)
		}
		if len(result.Warnings) != 0 {
			t.Errorf("%s: warnings = %#v", background, result.Warnings)
		}
	}
}

// TS 'drops background auto for %s (api=%s) with a warning'.
func TestPortprovbMaiImageDropsBackgroundAuto(t *testing.T) {
	for _, tc := range []struct{ id, api string }{{"MAI-Image-2.7-Flash", ""}, {"my-mai-deployment", "mai"}} {
		server, c := newMaiServer(t, nil)
		result, err := maiImageModel(t, server, tc.id).DoGenerate(context.Background(), &provider.ImageGenerateOptions{
			Prompt: "A red apple", ProviderOptions: portprovbBackground("auto", tc.api),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(c.paths) != 1 || c.paths[0] != "/mai/v1/images/generations" {
			t.Errorf("%s: paths = %v", tc.id, c.paths)
		}
		if _, has := c.jsonBody["background"]; has {
			t.Errorf("%s: background sent: %#v", tc.id, c.jsonBody)
		}
		if !reflect.DeepEqual(result.Warnings, []types.Warning{portprovbMaiWarning}) {
			t.Errorf("%s: warnings = %#v", tc.id, result.Warnings)
		}
	}
}

// TS 'rejects invalid background values': nothing is fetched.
func TestPortprovbInvalidBackgroundRejected(t *testing.T) {
	for _, value := range []interface{}{"translucent", 1, nil, true} {
		server, c := newMaiServer(t, nil)
		_, err := maiImageModel(t, server, "MAI-Image-2.7").DoGenerate(context.Background(), &provider.ImageGenerateOptions{
			Prompt:          "A red apple",
			ProviderOptions: map[string]interface{}{"azure": map[string]interface{}{"background": value}},
		})
		if err == nil {
			t.Errorf("background %v: no error", value)
		}
		if len(c.paths) != 0 {
			t.Errorf("background %v: fetched %v", value, c.paths)
		}
	}
}

// TS 'sends background as a form field' and 'drops background auto from
// edits with a warning'.
func TestPortprovbMaiImageEditsBackground(t *testing.T) {
	files := []provider.ImageFile{{Type: "file", Data: []byte{1}, MediaType: "image/png"}}
	server, c := newMaiServer(t, nil)
	if _, err := maiImageModel(t, server, "MAI-Image-2.7-Flash").DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "edit", Files: files, ProviderOptions: portprovbBackground("transparent", ""),
	}); err != nil {
		t.Fatal(err)
	}
	if c.paths[0] != "/mai/v1/images/edits" || len(c.form["background"]) != 1 || c.form["background"][0] != "transparent" {
		t.Errorf("paths = %v form = %v", c.paths, c.form)
	}

	server, c = newMaiServer(t, nil)
	result, err := maiImageModel(t, server, "MAI-Image-2.7").DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "edit", Files: files, ProviderOptions: portprovbBackground("auto", ""),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, has := c.form["background"]; has {
		t.Errorf("form = %v", c.form)
	}
	found := false
	for _, w := range result.Warnings {
		if reflect.DeepEqual(w, portprovbMaiWarning) {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %#v", result.Warnings)
	}
}

// TS 'sends %s (api=%s) background %s to the OpenAI API without warnings'
// and 'sends background to OpenAI image edits without warnings'.
func TestPortprovbOpenAIImageBackgroundWithoutWarnings(t *testing.T) {
	for _, tc := range []struct{ id, api, background string }{
		{"gpt-image-1", "", "auto"}, {"gpt-image-1", "", "transparent"}, {"MAI-Image-2.7", "openai", "auto"}, {"MAI-Image-2.7", "openai", "opaque"},
	} {
		server, c := newMaiServer(t, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"b64_json":"aGk="}]}`))
		})
		result, err := maiImageModel(t, server, tc.id).DoGenerate(context.Background(), &provider.ImageGenerateOptions{
			Prompt: "A red apple", ProviderOptions: portprovbBackground(tc.background, tc.api),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(c.paths) != 1 || c.paths[0] != "/openai/images/generations" || c.jsonBody["background"] != tc.background || len(result.Warnings) != 0 {
			t.Errorf("%+v: paths = %v body = %#v warnings = %#v", tc, c.paths, c.jsonBody, result.Warnings)
		}
	}

	server, c := newMaiServer(t, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"aGk="}]}`))
	})
	result, err := maiImageModel(t, server, "gpt-image-1").DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "edit", Files: []provider.ImageFile{{Type: "file", Data: []byte{137, 80, 78, 71}, MediaType: "image/png"}},
		ProviderOptions: portprovbBackground("auto", ""),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.form["background"]) != 1 || c.form["background"][0] != "auto" || len(result.Warnings) != 0 {
		t.Errorf("form = %v warnings = %#v", c.form, result.Warnings)
	}
}
