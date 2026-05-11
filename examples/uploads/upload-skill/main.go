//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func main() {
	p := openai.New(openai.Config{APIKey: os.Getenv("OPENAI_API_KEY")})
	uploaded, err := ai.UploadSkill(context.Background(), ai.UploadSkillOptions{
		API: p,
		Files: []ai.UploadSkillFile{
			{Path: "index.ts", Data: []byte("export default async function run() { return 'ok'; }")},
			{Path: "README.md", Data: []byte("# Example skill")},
		},
		DisplayTitle: "Example Skill",
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("uploaded skill reference: %#v\n", uploaded.ProviderReference)
}
