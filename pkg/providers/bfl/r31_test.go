package bfl

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS: url.searchParams.has('id') keeps an `id=` pair (even empty) and set()
// appends in place instead of sorting the query.
func TestEnsurePollingURLHasID(t *testing.T) {
	for in, want := range map[string]string{
		"https://api.bfl.ai/v1/get_result?z=1":      "https://api.bfl.ai/v1/get_result?z=1&id=req+1",
		"https://api.bfl.ai/v1/get_result":          "https://api.bfl.ai/v1/get_result?id=req+1",
		"https://api.bfl.ai/v1/get_result?id=":      "https://api.bfl.ai/v1/get_result?id=",
		"https://api.bfl.ai/v1/get_result?id=x#f":   "https://api.bfl.ai/v1/get_result?id=x#f",
		"https://api.bfl.ai/v1/get_result?a=1#frag": "https://api.bfl.ai/v1/get_result?a=1&id=req+1#frag",
		// URLSearchParams.set re-serializes the whole query (Node 24 output).
		"https://h/p?q=a%20b&flag":          "https://h/p?q=a+b&flag=&id=req+1",
		"https://h/p?x=%zz&y=%C3%A9&&k=a+b": "https://h/p?x=%25zz&y=%C3%A9&k=a+b&id=req+1",
		"https://h/p?a=*~!()&id2=1":         "https://h/p?a=*%7E%21%28%29&id2=1&id=req+1",
		"https://h/p?q=a%20b&id=1":          "https://h/p?q=a%20b&id=1",
	} {
		got, err := ensurePollingURLHasID(in, "req 1")
		if err != nil || got != want {
			t.Errorf("ensurePollingURLHasID(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestImageTooManyInputImagesMessageMatchesTS(t *testing.T) {
	m := NewImageModel(New(Config{APIKey: "k"}), "flux-kontext-pro")
	files := make([]provider.ImageFile, 11)
	_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p", Files: files})
	if err == nil || err.Error() != "Black Forest Labs supports up to 10 input images." {
		t.Fatalf("err = %v", err)
	}
}

// TS throws InvalidArgumentError (argument "duration") for 3+ untimed
// keyframes without a duration; the v0.5.2 ValidationError stays reachable.
func TestVideoUntimedKeyframesNeedDurationIsInvalidArgumentError(t *testing.T) {
	m := NewVideoModel(New(Config{APIKey: "k"}), "flux-3-video")
	_, _, err := m.buildVideoArgs(&provider.VideoModelV3CallOptions{
		Prompt: "p",
		ProviderOptions: map[string]interface{}{
			"blackForestLabs": map[string]interface{}{"keyframes": []interface{}{"a", "b", "c"}},
		},
	})
	var ia *providererrors.InvalidArgumentError
	if !errors.As(err, &ia) || ia.Field != "duration" {
		t.Fatalf("err = %T %v, want InvalidArgumentError for duration", err, err)
	}
	var ve *providererrors.ValidationError
	if !errors.As(err, &ve) {
		t.Errorf("v0.5.2 ValidationError no longer reachable")
	}
	want := "Invalid argument for parameter duration: FLUX 3 video requires an explicit duration when 3 or more keyframes are sent without a timestamp."
	if err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
}

// TS getArgs parses the provider options before it checks the input image
// count, so an invalid option wins over the 10-image limit.
func TestImageOptionValidationBeforeInputImageLimit(t *testing.T) {
	m := NewImageModel(New(Config{APIKey: "k"}), "flux-kontext-pro")
	files := make([]provider.ImageFile, 11)
	_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "p", Files: files,
		ProviderOptions: map[string]interface{}{"blackForestLabs": map[string]interface{}{"steps": -1}},
	})
	if err == nil || strings.Contains(err.Error(), "up to 10 input images") {
		t.Fatalf("want option validation error, got %v", err)
	}
}

func TestBFLSizeAspectRatioMatchesTSNumber(t *testing.T) {
	for in, want := range map[string]string{
		"1024x768": "4:3", "1024x768abc": "", "0x5": "", "axb": "", "1024": "", "1.5x1": "3:2",
	} {
		if got := bflSizeAspectRatio(in); got != want {
			t.Errorf("bflSizeAspectRatio(%q) = %q, want %q", in, got, want)
		}
	}
}
