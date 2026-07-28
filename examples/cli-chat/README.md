# CLI Chat Example

An interactive command-line chat application built with the Go AI SDK. This example demonstrates how to build a conversational AI interface with streaming responses and conversation history.

## Features

- 🤖 Real-time streaming responses
- 💬 Conversation history management  
- 🎨 Colored terminal output
- ⌨️ Special commands (/exit, /clear, /help)
- 📊 Token usage tracking

## Prerequisites

- Go 1.21 or higher
- OpenAI API key

## Setup

1. Set your OpenAI API key:

\`\`\`bash
export OPENAI_API_KEY=sk-...
\`\`\`

2. Run the chat application:

\`\`\`bash
go run main.go
\`\`\`

## Usage

Simply type your messages and press Enter. The AI will respond with streaming text.

### Available Commands

- \`/exit\` - Exit the chat application
- \`/clear\` - Clear conversation history
- \`/help\` - Show available commands

## What You'll Learn

- Building interactive CLI applications with Go
- Managing conversation state and history
- Streaming AI responses in real-time
- Handling user input with bufio
- Adding colors to terminal output
- Implementing command systems

## Code Highlights

### Conversation History

The application maintains a slice of messages representing the full conversation:

\`\`\`go
messages := []types.Message{}

// Add user message
messages = append(messages, types.Message{
    Role: types.RoleUser,
    Content: []types.ContentPart{
        types.TextContent{Text: userInput},
    },
})

// Add assistant response
messages = append(messages, types.Message{
    Role: types.RoleAssistant,
    Content: []types.ContentPart{
        types.TextContent{Text: fullResponse.String()},
    },
})
\`\`\`

### Streaming Response

Responses stream in real-time using stream chunks:

\`\`\`go
for chunk := range stream.Chunks() {
    fmt.Print(chunk.Text)
    fullResponse.WriteString(chunk.Text)
}
\`\`\`

### Terminal Colors

ANSI escape codes provide colored output:

\`\`\`go
fmt.Print("\\033[1;32mYou:\\033[0m ")      // Green for user
fmt.Print("\\033[1;34mAssistant:\\033[0m ") // Blue for assistant
\`\`\`

## Extending the Example

Ideas for enhancements:

1. **Add More Commands**
   - \`/save\` - Save conversation to file
   - \`/load\` - Load previous conversation
   - \`/model\` - Switch models
   - \`/system\` - Set system prompt

2. **Improve UI**
   - Add markdown rendering
   - Show typing indicator
   - Display conversation stats

3. **Add Features**
   - Tool calling support
   - Multi-turn reasoning
   - Context window management
   - Cost tracking

4. **Persistence**
   - Save/load conversations from disk
   - SQLite database storage
   - Export to various formats

## Notes

- Conversation history grows with each exchange - consider implementing pruning for long conversations
- Token usage is shown after each response
- Use Ctrl+C or \`/exit\` to quit gracefully
- The application uses OpenAI's GPT-4 by default - modify the model name in \`main.go\` to use different models
