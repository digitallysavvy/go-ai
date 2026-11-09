package bedrock

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TestR89provaNullAmazonBedrockKeyFallsBack: TS reads
// `parseProviderOptions('amazonBedrock') ?? parseProviderOptions('bedrock')`,
// and parseProviderOptions returns undefined for a null key, so
// `{amazonBedrock: null, bedrock: {...}}` uses the legacy key (TS
// amazon-bedrock-embedding-model.ts / amazon-bedrock-image-model.ts doEmbed /
// doGenerate). Validation already took the same fallback.
func TestR89provaNullAmazonBedrockKeyFallsBack(t *testing.T) {
	providerOptions := map[string]interface{}{
		"amazonBedrock": nil,
		"bedrock":       map[string]interface{}{"dimensions": 512.0, "negativeText": "blurry"},
	}

	if err := validateEmbeddingProviderOptions(map[string]interface{}{
		"amazonBedrock": nil, "bedrock": map[string]interface{}{"dimensions": 512.0},
	}); err != nil {
		t.Fatalf("validate: %v", err)
	}
	emb := bedrockEmbeddingOptions(providerOptions)
	if emb == nil || emb.TitanOptions == nil || emb.TitanOptions.Dimensions == nil || *emb.TitanOptions.Dimensions != 512 {
		t.Fatalf("embedding options = %#v", emb)
	}

	body, _, err := NewImageModel(nil, "amazon.nova-canvas-v1:0").buildRequestBody(&provider.ImageGenerateOptions{
		Prompt:          "a cat",
		ProviderOptions: map[string]interface{}{"amazonBedrock": nil, "bedrock": map[string]interface{}{"negativeText": "blurry"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	params, _ := body["textToImageParams"].(map[string]interface{})
	if params["negativeText"] != "blurry" {
		t.Fatalf("textToImageParams = %#v", params)
	}
}

// TestR89provaAPIErrorData: TS createJsonErrorResponseHandler with
// AmazonBedrockErrorSchema puts the parsed error ({message, type?}, unknown
// keys stripped) on APICallError.data next to the response headers and body
// (TS amazon-bedrock-error.ts); Go left Data empty.
func TestR89provaAPIErrorData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"slow","type":"ThrottlingException","extra":1}`))
	}))
	defer srv.Close()
	p := New(Config{AWSAccessKeyID: "AKID", AWSSecretAccessKey: "secret-key", Region: "us-east-1", BaseURL: srv.URL})
	_, err := NewEmbeddingModel(p, "amazon.titan-embed-text-v2:0").DoEmbed(context.Background(), "hi", nil)
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v", err)
	}
	if pe.Message != "ThrottlingException: slow" || pe.StatusCode != 429 || pe.ResponseHeaders["Retry-After"] != "1" {
		t.Fatalf("error = %+v headers=%v", pe, pe.ResponseHeaders)
	}
	want := map[string]interface{}{"message": "slow", "type": "ThrottlingException"}
	if !reflect.DeepEqual(pe.Data, want) {
		t.Fatalf("data = %#v, want %#v", pe.Data, want)
	}
	nullType := bedrockErrorData([]byte(`{"message":"m","type":null}`), bedrockErrorBody{Message: r89provaStr("m")})
	if !reflect.DeepEqual(nullType, map[string]interface{}{"message": "m", "type": nil}) {
		t.Fatalf("null type data = %#v", nullType)
	}
}

func r89provaStr(s string) *string { return &s }
