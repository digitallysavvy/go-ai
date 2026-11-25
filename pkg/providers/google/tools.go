package google

import "github.com/digitallysavvy/go-ai/pkg/providers/gemini"

// Tool constructors are defined once in the gemini package and re-exported here
// so callers can import from either google or googlevertex without caring which
// package owns the implementation.

// GoogleSearchTool creates a Google web search grounding tool.
// Requires Gemini 2.0 or newer.
var GoogleSearchTool = gemini.GoogleSearchTool

// EnterpriseWebSearchTool creates an enterprise web search grounding tool.
// For highly-regulated industries. Requires Gemini 2.0 or newer.
var EnterpriseWebSearchTool = gemini.EnterpriseWebSearchTool

// GoogleMapsTool creates a Google Maps grounding tool.
// Requires Gemini 2.0 or newer.
var GoogleMapsTool = gemini.GoogleMapsTool

// UrlContextTool creates a URL context fetching tool.
// Requires Gemini 2.0 or newer.
var UrlContextTool = gemini.UrlContextTool

// FileSearchTool creates a Gemini file search (RAG) tool.
// Requires Gemini 2.5 or newer.
var FileSearchTool = gemini.FileSearchTool

// CodeExecutionTool creates a code execution tool.
// Requires Gemini 2.0 or newer.
var CodeExecutionTool = gemini.CodeExecutionTool

// VertexRagStoreTool creates a Vertex RAG store retrieval tool.
var VertexRagStoreTool = gemini.VertexRagStoreTool

// MCPServerTool connects a remote MCP server so the model can call its tools.
// Only available with the Interactions API. Headers carry connector
// credentials and are redacted from telemetry.
var MCPServerTool = gemini.MCPServerTool
