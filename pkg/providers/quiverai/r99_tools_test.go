package quiverai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS quiveraiErrorSchema status is z.number().int(), zod v4's "safeint"
// format: a whole number beyond ±Number.MAX_SAFE_INTEGER fails the schema
// (node: safeParse({status: 1e16, ...}).success === false), so the message is
// the status text and there is no data. Go accepted any whole number and
// converted it to int.
func TestR99toolsErrorStatusMustBeSafeInteger(t *testing.T) {
	for _, body := range []string{
		`{"status":1e16,"code":"c","message":"m","request_id":"r"}`,
		`{"status":-1e20,"code":"c","message":"m","request_id":"r"}`,
	} {
		err := quiverAIFailedResponseHandler(&internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(body)})
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.Message != "Bad Request" || pe.Data != nil {
			t.Fatalf("%s: got %#v", body, err)
		}
	}
	// The largest safe integer still matches, and its value is kept exactly
	// (an int conversion overflows on 32-bit targets).
	err := quiverAIFailedResponseHandler(&internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(`{"status":9007199254740991,"code":"c","message":"m","request_id":"r"}`)})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.Message != "m" || pe.Data != (quiverAPIError{Status: 9007199254740991, Code: "c", Message: "m", RequestID: "r"}) {
		t.Fatalf("safe integer: got %#v", err)
	}
}

// svgGenerationResponseSchema accepts any safe integer for the usage,
// credits and animation timings. On a 32-bit target, values above 2^31-1 made
// the decode into int fail, so a valid response was an error.
func TestR99toolsSVGResponseLargeIntegers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"i","created":1,"data":[{"svg":"<svg/>","mime_type":"image/svg+xml","loop_period_ms":3000000000,"opening_animation_ms":5}],"usage":{"total_tokens":3000000002,"input_tokens":1,"output_tokens":3000000001},"credits":4000000000}`))
	}))
	defer srv.Close()
	m := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), ModelArrow2)
	res, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	meta := res.ProviderMetadata["quiverai"].(map[string]interface{})
	images := meta["images"].([]map[string]interface{})
	if got := r99toolsAsInt64(images[0]["loopPeriodMs"]); got != 3000000000 {
		t.Errorf("loopPeriodMs = %v", images[0]["loopPeriodMs"])
	}
	if images[0]["openingAnimationMs"] != 5 {
		t.Errorf("openingAnimationMs = %#v, want int 5", images[0]["openingAnimationMs"])
	}
	if got := r99toolsAsInt64(meta["credits"]); got != 4000000000 {
		t.Errorf("credits = %v", meta["credits"])
	}
	if res.Usage.InputTokens != 1 || res.Usage.OutputTokens <= 0 || res.Usage.TotalTokens <= 0 {
		t.Errorf("usage = %+v", res.Usage)
	}
}

func r99toolsAsInt64(v interface{}) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	}
	return -1
}
