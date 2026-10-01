//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
)

func main() {
	prov := xai.New(xai.Config{})
	model := xai.NewVideoModel(prov, "grok-imagine-video")

	resp, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "Animate this concept with the references",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{
				"mode":               "reference-to-video",
				"referenceImageUrls": []string{"https://example.com/ref1.png", "https://example.com/ref2.png"},
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(resp.Videos[0].URL)
}
