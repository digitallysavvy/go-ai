// Command goai-docs-mcp is a Model Context Protocol server that lets coding
// agents search and read the Go AI SDK documentation.
//
// It speaks MCP over stdio and answers from the documentation sources embedded
// in the binary (package github.com/digitallysavvy/go-ai/docs), so it needs no
// network access and the docs always match the installed version.
//
// Install and register it with Claude Code:
//
//	go install github.com/digitallysavvy/go-ai/cmd/goai-docs-mcp@latest
//	claude mcp add go-ai-docs -- goai-docs-mcp
package main

import (
	"flag"
	"fmt"
	"os"

	godocs "github.com/digitallysavvy/go-ai/docs"

	"github.com/digitallysavvy/go-ai/cmd/goai-docs-mcp/internal/docs"
)

// version is overridden at release time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("goai-docs-mcp", version)
		return
	}
	pages, err := docs.Build(godocs.FS)
	if err != nil {
		fmt.Fprintln(os.Stderr, "goai-docs-mcp: cannot load the embedded docs:", err)
		os.Exit(1)
	}
	srv := NewServer(docs.NewIndex(pages), version)
	// stdout carries protocol messages only; diagnostics go to stderr.
	if err := srv.Serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "goai-docs-mcp:", err)
		os.Exit(1)
	}
}
