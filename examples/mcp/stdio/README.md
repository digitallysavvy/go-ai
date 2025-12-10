# MCP Stdio Example

Model Context Protocol (MCP) server and client communicating over stdio (standard input/output).

## Features

- ✅ MCP protocol implementation
- ✅ JSON-RPC 2.0 over stdio
- ✅ Tool registration and calling
- ✅ AI completion generation
- ✅ Server/client architecture

## What is MCP?

The Model Context Protocol (MCP) is an open protocol that standardizes how applications provide context to LLMs. It enables:

- **Universal Integration**: Connect any AI model to any data source
- **Tool Calling**: Expose tools that models can use
- **Context Management**: Efficient context sharing
- **Interoperability**: Standard protocol across implementations

## Quick Start

### Build

```bash
# Build server
cd server && go build -o server

# Build client
cd client && go build -o client
```

### Run

```bash
# Terminal 1: Start server
export OPENAI_API_KEY=sk-...
./server/server

# Terminal 2: Run client
./client/client
```

## Protocol Methods

### initialize
Initialize the MCP connection.

### tools/list
List available tools.

### tools/call
Execute a specific tool.

### completion/generate
Generate AI completion with tool access.

## Use Cases

- IDE integrations (VS Code, Cursor, etc.)
- CLI tools with AI capabilities
- Process-to-process AI communication
- Isolated AI services

## Related

- [mcp/http](../http) - HTTP-based MCP server
- [agents](../../agents) - AI agents with tools
