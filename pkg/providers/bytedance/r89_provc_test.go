package bytedance

import (
	"errors"
	"net/http"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS ByteDanceVideoModel has no doGenerate, so TS generateVideo always drives it through the
// core start/status flow. The model marks itself for ai.GenerateVideo with
// VideoStartStatusPollDefaults, returning the v0.5.2 pollIntervalMs /
// pollTimeoutMs provider options (0 = the core default).
func TestR89provcVideoStartStatusPollDefaults(t *testing.T) {
	var model interface {
		VideoStartStatusPollDefaults(map[string]interface{}) (int, int)
	} = r89provcByteDanceModel(t)
	if i, to := model.VideoStartStatusPollDefaults(map[string]interface{}{}); i != 0 || to != 0 {
		t.Fatalf("defaults without options = %d, %d; want 0, 0", i, to)
	}
	i, to := model.VideoStartStatusPollDefaults(map[string]interface{}{"bytedance": map[string]interface{}{"pollIntervalMs": float64(250), "pollTimeoutMs": float64(90000)}})
	if i != 250 || to != 90000 {
		t.Fatalf("defaults = %d, %d; want 250, 90000", i, to)
	}
}

func r89provcByteDanceModel(t *testing.T) *VideoModel {
	t.Helper()
	prov, err := New(Config{APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	return newVideoModel(prov, "seedance-1-0-pro-250528")
}

// TS byteDanceFailedResponseHandler gives an APICallError with
// responseHeaders, responseBody and data; Go's *Error unwraps to that
// ProviderError so the core retry honors retry-after.
func TestR89provcErrorCarriesResponseHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "1")
	body := []byte(`{"error":{"code":"RateLimit","message":"slow"}}`)
	err := error(newHTTPError(429, h, body))
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("no ProviderError in %v", err)
	}
	if pe.StatusCode != 429 || pe.ResponseHeaders["Retry-After"] != "1" || pe.ResponseBody != string(body) || pe.Data == nil {
		t.Fatalf("provider error = %+v", pe)
	}
	if errors.As(error(NewError(400, "x", "")), &pe) {
		t.Fatal("a non-HTTP error must not unwrap to a ProviderError")
	}
}
