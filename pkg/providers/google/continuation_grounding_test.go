package google

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports TS #23513: automatically continued responses expose each request's
// grounding snapshot in providerMetadata.google.groundingMetadataPerRequest.

const (
	contGrounding = `{"webSearchQueries":["ADA accessibility standards"],"groundingChunks":[{"web":{"uri":"https://aoda.ca","title":"aoda.ca"}}]}`
	contFirst     = `{"candidates":[{"content":{"role":"model","parts":[{"text":"Chapter 1"}]},"finishReason":"CONTINUATION","continuationToken":"checkpoint-1","groundingMetadata":` + contGrounding + `}],"usageMetadata":{"promptTokenCount":363,"candidatesTokenCount":32794,"totalTokenCount":33157}}`
	contLastPlain = `{"candidates":[{"content":{"role":"model","parts":[{"text":" more"}]},"finishReason":"MAX_TOKENS","continuationToken":"checkpoint-2"}],"usageMetadata":{"promptTokenCount":32883,"candidatesTokenCount":512,"totalTokenCount":33395}}`
	contLastGnd   = `{"candidates":[{"content":{"role":"model","parts":[{"text":" more"}]},"finishReason":"MAX_TOKENS","continuationToken":"checkpoint-2","groundingMetadata":` + contGrounding + `}],"usageMetadata":{"promptTokenCount":32883,"candidatesTokenCount":512,"totalTokenCount":33395}}`
)

func perRequestOf(t *testing.T, meta map[string]interface{}) []interface{} {
	t.Helper()
	per, ok := meta["groundingMetadataPerRequest"].([]interface{})
	if !ok {
		t.Fatalf("groundingMetadataPerRequest missing or wrong type: %#v", meta["groundingMetadataPerRequest"])
	}
	return per
}

func TestContinuationGrounding_Generate(t *testing.T) {
	var want interface{}
	_ = json.Unmarshal([]byte(contGrounding), &want)
	for _, tc := range []struct {
		name     string
		last     string
		wantLast interface{}
	}{
		{"terminal without grounding", contLastPlain, nil},
		{"terminal with grounding", contLastGnd, want},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := scriptedServer(t, scriptedReply{body: contFirst}, scriptedReply{body: tc.last})
			mt := 33280
			res, err := longDecodeModel(srv).DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, MaxTokens: &mt})
			if err != nil {
				t.Fatal(err)
			}
			meta := googleMeta(t, mustJSON(t, res.ProviderMetadata))
			if got := perRequestOf(t, meta); !reflect.DeepEqual(got, []interface{}{want, tc.wantLast}) {
				t.Errorf("groundingMetadataPerRequest = %#v", got)
			}
			if !reflect.DeepEqual(meta["groundingMetadata"], tc.wantLast) {
				t.Errorf("groundingMetadata = %#v, want %#v", meta["groundingMetadata"], tc.wantLast)
			}
			if *res.Usage.InputTokens != 33246 || *res.Usage.OutputTokens != 33306 {
				t.Errorf("usage in=%d out=%d", *res.Usage.InputTokens, *res.Usage.OutputTokens)
			}
		})
	}
}

func TestContinuationGrounding_NotContinuedOmitsField(t *testing.T) {
	srv, _ := scriptedServer(t, scriptedReply{body: contLastGnd})
	res, err := longDecodeModel(srv).DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := googleMeta(t, mustJSON(t, res.ProviderMetadata))["groundingMetadataPerRequest"]; ok {
		t.Error("groundingMetadataPerRequest must be absent without continuation")
	}
}

func TestContinuationGrounding_Stream(t *testing.T) {
	var want interface{}
	_ = json.Unmarshal([]byte(contGrounding), &want)
	for _, tc := range []struct {
		name     string
		last     string
		wantLast interface{}
	}{
		{"terminal without grounding", contLastPlain, nil},
		{"terminal with grounding", contLastGnd, want},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := scriptedServer(t,
				scriptedReply{sse: true, body: sseEvents(contFirst)},
				scriptedReply{sse: true, body: sseEvents(tc.last)},
			)
			mt := 33280
			s, err := longDecodeModel(srv).DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, MaxTokens: &mt})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close() //nolint:errcheck
			fin := finishOf(t, drain(t, s))
			meta := googleMeta(t, fin.ProviderMetadata)
			if got := perRequestOf(t, meta); !reflect.DeepEqual(got, []interface{}{want, tc.wantLast}) {
				t.Errorf("groundingMetadataPerRequest = %#v", got)
			}
			if *fin.Usage.InputTokens != 33246 || *fin.Usage.OutputTokens != 33306 {
				t.Errorf("usage in=%d out=%d", *fin.Usage.InputTokens, *fin.Usage.OutputTokens)
			}
		})
	}
}
