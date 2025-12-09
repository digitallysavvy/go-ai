# Anthropic PDF Support

Demonstrates Claude's ability to analyze and extract information from PDF documents.

## Features

- **PDF analysis** - Understand document structure and content
- **Information extraction** - Pull specific data from PDFs
- **Summarization** - Condense long documents
- **Multi-page support** - Handle documents of any length
- **Native PDF parsing** - No external tools needed

## Prerequisites

- Anthropic API key
- Claude 3 Sonnet or Opus (PDF support)
- PDF files for testing

## Setup

```bash
export ANTHROPIC_API_KEY=sk-ant-...
```

## Running

```bash
cd examples/providers/anthropic/pdf-support
# Place PDF files named example.pdf, invoice.pdf, or report.pdf in this directory
go run main.go
```

## Supported Operations

1. **Document Analysis** - Understand content and structure
2. **Data Extraction** - Pull specific fields (invoices, forms)
3. **Summarization** - Create concise summaries
4. **Q&A** - Answer questions about document content
5. **Translation** - Extract and translate text

## Best Practices

1. **Clear instructions** - Specify what to extract
2. **Structured output** - Request JSON for data extraction
3. **Multi-turn** - Ask follow-up questions about same PDF
4. **Page limits** - Consider splitting very large documents

## Limitations

- Max file size: 10MB per PDF
- Text-based PDFs work best
- Scanned PDFs may have OCR limitations

## Documentation

- [Anthropic PDF Support](https://docs.anthropic.com/claude/docs/vision#document-support)
- [Go AI SDK Docs](../../../../docs)
