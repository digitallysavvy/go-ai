package replicate

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestReplicateLanguageModelBuildConvertAndStream(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "ver-1")
	temp := float64(0.4)
	maxTokens := 20
	topP := float64(0.8)
	body := m.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hello"}}}},
			System:   "sys",
		},
		Temperature: &temp,
		MaxTokens:   &maxTokens,
		TopP:        &topP,
	})
	if body["version"] != "ver-1" {
		t.Fatalf("version = %#v", body["version"])
	}
	input := body["input"].(map[string]interface{})
	if input["temperature"] != temp || input["max_tokens"] != maxTokens || input["top_p"] != topP {
		t.Fatalf("input options mismatch: %#v", input)
	}

	res := m.convertResponse(replicatePrediction{Output: "hello"})
	if res.Text != "hello" || res.FinishReason != types.FinishReasonStop {
		t.Fatalf("convert string mismatch: %#v", res)
	}
	res2 := m.convertResponse(replicatePrediction{Output: []interface{}{"a", "b"}})
	if res2.Text != "ab" {
		t.Fatalf("convert array mismatch: %#v", res2)
	}

	stream := &replicateStream{result: &types.GenerateResult{Text: "abcdefghijk", FinishReason: types.FinishReasonStop}}
	chunk1, err := stream.Next()
	if err != nil || chunk1.Type != provider.ChunkTypeText || chunk1.Text != "abcdefghij" {
		t.Fatalf("chunk1 mismatch: chunk=%#v err=%v", chunk1, err)
	}
	chunk2, err := stream.Next()
	if err != nil || chunk2.Text != "k" {
		t.Fatalf("chunk2 mismatch: chunk=%#v err=%v", chunk2, err)
	}
	finish, err := stream.Next()
	if err != nil || finish.Type != provider.ChunkTypeFinish {
		t.Fatalf("finish chunk mismatch: chunk=%#v err=%v", finish, err)
	}
}

func TestReplicateImageModelBuildAndConvertErrors(t *testing.T) {
	m := NewImageModel(New(Config{APIKey: "k"}), "img-ver")
	n := 3
	body := m.buildRequestBody(&provider.ImageGenerateOptions{Prompt: "cat", N: &n, Size: "640x480"})
	if body["version"] != "img-ver" {
		t.Fatalf("version = %#v", body["version"])
	}
	input := body["input"].(map[string]interface{})
	if input["prompt"] != "cat" || input["num_outputs"] != 3 || input["width"] != 640 || input["height"] != 480 {
		t.Fatalf("input mismatch: %#v", input)
	}

	if _, err := m.convertResponse(t.Context(), replicateImagePrediction{Output: []interface{}{}}); err == nil {
		t.Fatal("expected no image URL error")
	}
}

func TestReplicateVideoModelBuildPollOptionsAndConvertErrors(t *testing.T) {
	m := NewVideoModel(New(Config{APIKey: "k"}), "vid-ver")
	duration := 5.0
	fps := 24
	seed := 7
	body := m.buildPredictionRequest(&provider.VideoModelV3CallOptions{
		Prompt:      "run",
		AspectRatio: "16:9",
		Duration:    &duration,
		FPS:         &fps,
		Seed:        &seed,
		Image:       &provider.VideoModelV3File{Type: "url", URL: "https://example.com/img.png"},
		ProviderOptions: map[string]interface{}{
			"replicate": map[string]interface{}{
				"pollIntervalMs": 500,
				"pollTimeoutMs":  10000,
				"quality":        "high",
			},
		},
	})
	input := body["input"].(map[string]interface{})
	if input["prompt"] != "run" || input["aspect_ratio"] != "16:9" || input["duration"] != 5.0 || input["fps"] != 24 || input["seed"] != 7 || input["image"] != "https://example.com/img.png" || input["quality"] != "high" {
		t.Fatalf("input mismatch: %#v", input)
	}
	if _, ok := input["pollIntervalMs"]; ok {
		t.Fatalf("pollIntervalMs should not be forwarded: %#v", input)
	}

	pollOpts := m.getPollOptions(map[string]interface{}{"replicate": map[string]interface{}{"pollIntervalMs": 250, "pollTimeoutMs": 9000}})
	if pollOpts.PollIntervalMs != 250 || pollOpts.PollTimeoutMs != 9000 {
		t.Fatalf("poll options mismatch: %#v", pollOpts)
	}

	if _, err := m.convertResponse(t.Context(), &replicateVideoPrediction{}); err == nil {
		t.Fatal("expected no video generated error")
	}
	if _, err := m.convertResponse(t.Context(), &replicateVideoPrediction{Output: []interface{}{map[string]interface{}{"x": 1}}}); err == nil {
		t.Fatal("expected unexpected output format error")
	}
}
