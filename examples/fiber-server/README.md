# Fiber Server Example

High-performance AI server using the Fiber web framework.

## Features

- Ultra-fast performance
- Built-in middleware
- Easy routing

## Quick Start

```bash
export OPENAI_API_KEY=sk-...
go run main.go
```

## Usage

```bash
curl -X POST http://localhost:8080/generate \
  -H "Content-Type: application/json" \
  -d '{"prompt": "Hello world"}'
```
