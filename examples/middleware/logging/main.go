package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// LogEntry represents a structured log entry
type LogEntry struct {
	Timestamp    time.Time              `json:"timestamp"`
	Provider     string                 `json:"provider"`
	Model        string                 `json:"model"`
	Prompt       string                 `json:"prompt"`
	Response     string                 `json:"response"`
	InputTokens  int64                  `json:"inputTokens"`
	OutputTokens int64                  `json:"outputTokens"`
	TotalTokens  int64                  `json:"totalTokens"`
	Duration     time.Duration          `json:"duration"`
	FinishReason types.FinishReason     `json:"finishReason"`
	Error        string                 `json:"error,omitempty"`
	Extra        map[string]interface{} `json:"extra,omitempty"`
}

// Logger interface for different logging strategies
type Logger interface {
	Log(entry LogEntry)
	Flush() error
}

// ConsoleLogger logs to stdout
type ConsoleLogger struct {
	Verbose bool
}

func (l *ConsoleLogger) Log(entry LogEntry) {
	if l.Verbose {
		// Detailed logging
		fmt.Printf("\n[%s] %s/%s\n", entry.Timestamp.Format(time.RFC3339), entry.Provider, entry.Model)
		fmt.Printf("Prompt: %s\n", truncate(entry.Prompt, 80))
		fmt.Printf("Response: %s\n", truncate(entry.Response, 80))
		fmt.Printf("Tokens: %d in, %d out, %d total\n", entry.InputTokens, entry.OutputTokens, entry.TotalTokens)
		fmt.Printf("Duration: %v\n", entry.Duration)
		if entry.Error != "" {
			fmt.Printf("Error: %s\n", entry.Error)
		}
	} else {
		// Concise logging
		status := "✓"
		if entry.Error != "" {
			status = "✗"
		}
		fmt.Printf("[%s] %s %s: %d tokens in %v\n",
			entry.Timestamp.Format("15:04:05"),
			status,
			entry.Model,
			entry.TotalTokens,
			entry.Duration,
		)
	}
}

func (l *ConsoleLogger) Flush() error {
	return nil
}

// JSONLogger logs to a JSON file
type JSONLogger struct {
	filename string
	file     *os.File
	encoder  *json.Encoder
}

func NewJSONLogger(filename string) (*JSONLogger, error) {
	file, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	return &JSONLogger{
		filename: filename,
		file:     file,
		encoder:  json.NewEncoder(file),
	}, nil
}

func (l *JSONLogger) Log(entry LogEntry) {
	if err := l.encoder.Encode(entry); err != nil {
		log.Printf("Failed to write log entry: %v", err)
	}
}

func (l *JSONLogger) Flush() error {
	return l.file.Sync()
}

func (l *JSONLogger) Close() error {
	return l.file.Close()
}

// LoggingMiddleware wraps AI operations with logging
type LoggingMiddleware struct {
	logger Logger
}

func NewLoggingMiddleware(logger Logger) *LoggingMiddleware {
	return &LoggingMiddleware{logger: logger}
}

func (m *LoggingMiddleware) GenerateText(
	ctx context.Context,
	model provider.LanguageModel,
	prompt string,
	opts ai.GenerateTextOptions,
) (*ai.GenerateTextResult, error) {
	start := time.Now()
	entry := LogEntry{
		Timestamp: start,
		Model:     model.ModelID(),
		Provider:  model.Provider(),
		Prompt:    prompt,
	}

	result, err := ai.GenerateText(ctx, opts)
	entry.Duration = time.Since(start)

	if err != nil {
		entry.Error = err.Error()
		m.logger.Log(entry)
		return result, err
	}

	entry.Response = result.Text
	entry.InputTokens = result.Usage.GetInputTokens()
	entry.OutputTokens = result.Usage.GetOutputTokens()
	entry.TotalTokens = result.Usage.GetTotalTokens()
	entry.FinishReason = result.FinishReason

	m.logger.Log(entry)
	return result, nil
}

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	p := openai.New(openai.Config{
		APIKey: apiKey,
	})

	model, err := p.LanguageModel("gpt-4")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	fmt.Println("=== Logging Middleware Example ===")

	// Example 1: Console logging (verbose)
	fmt.Println("1. Verbose Console Logging")
	fmt.Println("   " + string(make([]byte, 40, 40)))

	consoleLogger := &ConsoleLogger{Verbose: true}
	loggingMiddleware := NewLoggingMiddleware(consoleLogger)

	result, err := loggingMiddleware.GenerateText(ctx, model, "What is the capital of France?", ai.GenerateTextOptions{
		Model:  model,
		Prompt: "What is the capital of France?",
	})
	if err != nil {
		log.Fatal(err)
	}

	// Example 2: Console logging (concise)
	fmt.Println("\n2. Concise Console Logging")
	fmt.Println("   " + string(make([]byte, 40, 40)))

	consoleLogger.Verbose = false

	prompts := []string{
		"What is 2+2?",
		"Name a famous scientist",
		"What is the largest planet?",
		"Who wrote Hamlet?",
	}

	for _, prompt := range prompts {
		loggingMiddleware.GenerateText(ctx, model, prompt, ai.GenerateTextOptions{
			Model:  model,
			Prompt: prompt,
		})
		time.Sleep(500 * time.Millisecond)
	}

	// Example 3: JSON file logging
	fmt.Println("\n3. JSON File Logging")
	fmt.Println("   " + string(make([]byte, 40, 40)))

	jsonLogger, err := NewJSONLogger("ai-requests.jsonl")
	if err != nil {
		log.Fatal(err)
	}
	defer jsonLogger.Close()

	jsonMiddleware := NewLoggingMiddleware(jsonLogger)

	queries := []string{
		"Explain photosynthesis briefly",
		"What is quantum entanglement?",
		"Describe the water cycle",
	}

	for i, query := range queries {
		fmt.Printf("   Request %d: %s\n", i+1, truncate(query, 40))
		result, err = jsonMiddleware.GenerateText(ctx, model, query, ai.GenerateTextOptions{
			Model:  model,
			Prompt: query,
		})
		if err != nil {
			log.Printf("Error: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}

	if err := jsonLogger.Flush(); err != nil {
		log.Printf("Failed to flush logs: %v", err)
	}

	fmt.Println("\n   ✓ Logs written to ai-requests.jsonl")

	// Example 4: Multi-logger (console + file)
	fmt.Println("\n4. Multi-Logger (Console + File)")
	fmt.Println("   " + string(make([]byte, 40, 40)))

	multiLogger := &MultiLogger{
		loggers: []Logger{
			&ConsoleLogger{Verbose: false},
			jsonLogger,
		},
	}

	multiMiddleware := NewLoggingMiddleware(multiLogger)

	result, err = multiMiddleware.GenerateText(ctx, model, "What is machine learning?", ai.GenerateTextOptions{
		Model:  model,
		Prompt: "What is machine learning?",
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("\n   Response: %s\n", truncate(result.Text, 100))

	// Show log file preview
	fmt.Println("\n5. Log File Preview")
	fmt.Println("   " + string(make([]byte, 40, 40)))

	showLogPreview("ai-requests.jsonl", 3)
}

// MultiLogger sends logs to multiple loggers
type MultiLogger struct {
	loggers []Logger
}

func (m *MultiLogger) Log(entry LogEntry) {
	for _, logger := range m.loggers {
		logger.Log(entry)
	}
}

func (m *MultiLogger) Flush() error {
	var lastErr error
	for _, logger := range m.loggers {
		if err := logger.Flush(); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func showLogPreview(filename string, lines int) {
	file, err := os.Open(filename)
	if err != nil {
		fmt.Printf("   Failed to open log file: %v\n", err)
		return
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	count := 0

	fmt.Printf("   Last %d entries:\n\n", lines)

	var entries []LogEntry
	for decoder.More() {
		var entry LogEntry
		if err := decoder.Decode(&entry); err != nil {
			break
		}
		entries = append(entries, entry)
	}

	start := len(entries) - lines
	if start < 0 {
		start = 0
	}

	for i := start; i < len(entries); i++ {
		entry := entries[i]
		count++
		fmt.Printf("   %d. [%s] %s\n", count, entry.Timestamp.Format("15:04:05"), entry.Model)
		fmt.Printf("      Prompt: %s\n", truncate(entry.Prompt, 60))
		fmt.Printf("      Tokens: %d | Duration: %v\n", entry.TotalTokens, entry.Duration)
		fmt.Println()
	}
}
