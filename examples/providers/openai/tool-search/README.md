# OpenAI Tool Search Example

Demonstrates the OpenAI Responses API `tool_search` tool, which lets the
model search across a large catalog of deferred tools instead of loading
every tool definition into context.

## Execution Modes

- **Server mode (default)** — OpenAI resolves tool matches internally. No
  `tool_search_call` event is emitted; the tool config is just sent with the
  request.
- **Client mode** — The model emits a `tool_search_call` event. The client's
  `Execute` function receives the search arguments and returns matching tool
  names.

## What This Example Shows

1. Building a server-mode `tool_search` tool and inspecting its wire format.
2. Building a client-mode `tool_search` tool with a custom `Execute` function
   and inspecting its wire format.
3. Simulating a `tool_search_call` event by invoking `Execute` directly.

This example runs entirely offline — it only builds tool definitions and
prints their JSON wire format, so no API key or network access is required.

## Usage

```bash
go run main.go
```
