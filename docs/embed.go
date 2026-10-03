// Package docs embeds the Go AI SDK documentation sources for tools such as
// goai-docs-mcp. It embeds every file under docs/ (files and directories whose
// names start with "_" or "." are skipped by go:embed); consumers filter out
// contributor-only content, see cmd/goai-docs-mcp/internal/docs.
package docs

import "embed"

// FS holds the documentation sources, rooted at docs/.
//
//go:embed *
var FS embed.FS
