// Command toolwebfetch demonstrates pkg/toolwebfetch: a provider-independent
// web fetch tool that converts HTML pages to markdown.
//
// This example needs no API key or network access: a stub transport serves a
// page for a public-looking hostname. In real use, leave Options.Transport
// unset so requests use the SDK's SSRF-safe transport, which validates DNS
// results at connect time.
//
// Run with: go run ./examples/toolwebfetch
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/toolwebfetch"
)

const pageHTML = `<!doctype html><html><head><title>Guide | Example</title></head><body>
<nav><a href="/">Home</a></nav>
<main><h1>Guide</h1><p>Read the <a href="/docs/start">start page</a> first.</p></main>
<footer>Copyright Example Inc.</footer></body></html>`

// stubTransport answers every request with pageHTML.
type stubTransport struct{}

func (stubTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		Body:       io.NopCloser(strings.NewReader(pageHTML)),
	}, nil
}

func main() {
	maxLength := 2_000
	webFetch, err := toolwebfetch.CreateWebFetchTool(toolwebfetch.Options{
		AllowedDomains:          []string{"example.com"}, // subdomains are included
		RequireURLInUserMessage: true,                    // the model can only fetch URLs the user wrote
		MaxLength:               &maxLength,
		Transport:               stubTransport{},
	})
	if err != nil {
		log.Fatal(err)
	}

	// Pass the tool to a model with GenerateTextOptions{Tools: []types.Tool{webFetch}}.
	// Here the model's tool call is simulated by calling Execute directly.
	messages := []types.Message{{
		Role:    types.RoleUser,
		Content: []types.ContentPart{types.TextContent{Text: "Summarize https://example.com/guide"}},
	}}
	out, err := webFetch.Execute(context.Background(),
		map[string]interface{}{"url": "https://example.com/guide"},
		types.ToolExecutionOptions{ToolCallID: "call-1", Messages: messages})
	if err != nil {
		log.Fatal(err)
	}

	// ToModelOutput is what the model sees.
	modelOutput, err := webFetch.ToModelOutput(context.Background(), types.ToModelOutputOptions{ToolCallID: "call-1", Output: out})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(modelOutput.Value)
}
