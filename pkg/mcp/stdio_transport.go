package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

// StdioTransport implements the Transport interface for stdio-based communication
// This transport launches a command and communicates via stdin/stdout
type StdioTransport struct {
	// Command to execute
	command string
	args    []string

	// Process
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	// Reader/Writer
	reader *bufio.Scanner
	writer *bufio.Writer

	// State
	connected bool
	mu        sync.Mutex

	// Configuration
	config TransportConfig
}

// StdioTransportConfig contains configuration for stdio transport
type StdioTransportConfig struct {
	// Command is the command to execute
	Command string

	// Args are the arguments to pass to the command
	Args []string

	// Env are additional environment variables to set
	Env []string

	// WorkingDir is the working directory for the command
	WorkingDir string

	// Config is the base transport configuration
	Config TransportConfig
}

// NewStdioTransport creates a new stdio transport
func NewStdioTransport(config StdioTransportConfig) *StdioTransport {
	return &StdioTransport{
		command: config.Command,
		args:    config.Args,
		config:  config.Config,
	}
}

// Connect establishes a connection by starting the command
func (t *StdioTransport) Connect(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.connected {
		return fmt.Errorf("already connected")
	}

	// Create command
	t.cmd = exec.CommandContext(ctx, t.command, t.args...)

	// Get stdin, stdout, stderr pipes
	var err error
	t.stdin, err = t.cmd.StdinPipe()
	if err != nil {
		return NewTransportError("failed to create stdin pipe", err)
	}

	t.stdout, err = t.cmd.StdoutPipe()
	if err != nil {
		return NewTransportError("failed to create stdout pipe", err)
	}

	t.stderr, err = t.cmd.StderrPipe()
	if err != nil {
		return NewTransportError("failed to create stderr pipe", err)
	}

	// Start the command
	if err := t.cmd.Start(); err != nil {
		return NewTransportError("failed to start command", err)
	}

	// Create reader and writer
	t.reader = bufio.NewScanner(t.stdout)
	// Increase buffer size for large messages
	buf := make([]byte, 0, 64*1024)
	t.reader.Buffer(buf, 1024*1024) // 1MB max

	t.writer = bufio.NewWriter(t.stdin)

	// Start stderr logger
	if t.config.EnableLogging {
		go t.logStderr()
	}

	t.connected = true
	return nil
}

// Close closes the connection and terminates the command
func (t *StdioTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.connected {
		return nil
	}

	// Close pipes
	if t.stdin != nil {
		t.stdin.Close()
	}
	if t.stdout != nil {
		t.stdout.Close()
	}
	if t.stderr != nil {
		t.stderr.Close()
	}

	// Kill process
	if t.cmd != nil && t.cmd.Process != nil {
		t.cmd.Process.Kill()
		t.cmd.Wait()
	}

	t.connected = false
	return nil
}

// Send sends a message to the MCP server
func (t *StdioTransport) Send(ctx context.Context, message *MCPMessage) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.connected {
		return NewTransportError("not connected", nil)
	}

	// Marshal message to JSON
	data, err := json.Marshal(message)
	if err != nil {
		return NewTransportError("failed to marshal message", err)
	}

	if t.config.EnableLogging {
		fmt.Printf("MCP Send: %s\n", string(data))
	}

	// Write message followed by newline
	if _, err := t.writer.Write(data); err != nil {
		return NewTransportError("failed to write message", err)
	}
	if err := t.writer.WriteByte('\n'); err != nil {
		return NewTransportError("failed to write newline", err)
	}

	// Flush
	if err := t.writer.Flush(); err != nil {
		return NewTransportError("failed to flush", err)
	}

	return nil
}

// Receive receives a message from the MCP server
func (t *StdioTransport) Receive(ctx context.Context) (*MCPMessage, error) {
	if !t.connected {
		return nil, NewTransportError("not connected", nil)
	}

	// Read line
	if !t.reader.Scan() {
		err := t.reader.Err()
		if err == nil {
			err = io.EOF
		}
		return nil, err
	}

	line := t.reader.Bytes()

	if t.config.EnableLogging {
		fmt.Printf("MCP Receive: %s\n", string(line))
	}

	// Parse JSON
	var message MCPMessage
	if err := json.Unmarshal(line, &message); err != nil {
		return nil, NewTransportError("failed to unmarshal message", err)
	}

	return &message, nil
}

// IsConnected returns true if the transport is connected
func (t *StdioTransport) IsConnected() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.connected
}

// logStderr logs stderr output from the command
func (t *StdioTransport) logStderr() {
	scanner := bufio.NewScanner(t.stderr)
	for scanner.Scan() {
		fmt.Printf("MCP stderr: %s\n", scanner.Text())
	}
}
