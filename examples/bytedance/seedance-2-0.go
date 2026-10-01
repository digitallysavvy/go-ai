//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/bytedance"
)

func main() {
	prov, err := bytedance.New(bytedance.Config{})
	if err != nil {
		log.Fatal(err)
	}

	model, err := prov.VideoModel(string(bytedance.ModelDreaminaSeedance20))
	if err != nil {
		log.Fatal(err)
	}

	resp, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt:      "A vibrant market at golden hour",
		AspectRatio: "16:9",
		ProviderOptions: map[string]interface{}{
			"bytedance": map[string]interface{}{"watermark": false, "pollTimeoutMs": 600000},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(resp.Videos[0].URL)
}
