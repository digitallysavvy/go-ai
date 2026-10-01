//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"

	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
)

func main() {
	prov := xai.New(xai.Config{})
	model := xai.NewVideoModel(prov, "grok-imagine-video")

	resp, err := model.Extend(context.Background(), xai.VideoExtendOptions{
		Video:  "https://example.com/source.mp4",
		Prompt: "Continue the motion naturally",
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(resp.Videos[0].URL)
}
