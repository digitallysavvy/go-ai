package mcp

import (
	"errors"
	"runtime"
	"strings"
	"testing"
)

const shellEchoCommand = `echo '{"jsonrpc":"2.0","id":1,"result":{"ok":true}}'`

// TS #19430: StdioConfig.shell. A shell-only command line (quotes, pipes)
// runs through the shell and only with Shell set.
func TestStdioTransportShellOption(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell command line")
	}

	t.Run("without shell the command line is not parsed", func(t *testing.T) {
		tr := NewStdioTransport(StdioTransportConfig{Command: shellEchoCommand})
		if err := tr.Connect(t.Context()); err == nil {
			_ = tr.Close()
			t.Fatal("expected start failure without Shell")
		}
	})

	for name, cfg := range map[string]StdioTransportConfig{
		"Shell":     {Command: shellEchoCommand, Shell: true},
		"ShellPath": {Command: shellEchoCommand, ShellPath: "/bin/sh"},
	} {
		t.Run(name, func(t *testing.T) {
			tr := NewStdioTransport(cfg)
			if err := tr.Connect(t.Context()); err != nil {
				t.Fatalf("Connect: %v", err)
			}
			defer tr.Close() //nolint:errcheck
			msg, err := tr.Receive(t.Context())
			if err != nil || !strings.Contains(string(mustJSON(msg)), `"ok":true`) {
				t.Fatalf("msg=%v err=%v", msg, err)
			}
		})
	}
}

// TS #19430: the SSE start error carries status, url and response body.
func TestSSETransportStartErrorCarriesResponseBody(t *testing.T) {
	tr := NewSSETransport(SSETransportConfig{
		URL:       "http://localhost:3000/sse",
		SSEClient: &staticSSEClient{status: 500, contentType: "text/plain", body: strings.NewReader("Internal Server Error")},
	})
	err := tr.Connect(t.Context())
	var ce *MCPClientError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v", err)
	}
	if ce.StatusCode != 500 || ce.URL != "http://localhost:3000/sse" || ce.ResponseBody != "Internal Server Error" || !strings.Contains(ce.Error(), "500 Internal Server Error: Internal Server Error") {
		t.Fatalf("err = %+v", ce)
	}
}
