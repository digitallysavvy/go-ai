# MCP HTTP Example

Model Context Protocol server over HTTP REST API.

## Quick Start

```bash
export OPENAI_API_KEY=sk-...
go run main.go
```

## Endpoints

- `GET /tools` - List available tools
- `POST /generate` - Generate completion with tools

## Example

```bash
curl http://localhost:8080/tools
curl -X POST http://localhost:8080/generate \
  -H "Content-Type: application/json" \
  -d '{"prompt": "Calculate 12 * 34"}'
```
