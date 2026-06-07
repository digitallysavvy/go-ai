package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/agent"
)

// TerminalPartDisplayMode controls how terminal UI sections for stream parts are displayed.
//
//   - "full": show the section header and full content.
//   - "collapsed": show only the section header.
//   - "auto-collapsed": show the latest section expanded until another visible section appears.
//   - "hidden": omit the section entirely.
type TerminalPartDisplayMode string

const (
	TerminalPartDisplayFull          TerminalPartDisplayMode = "full"
	TerminalPartDisplayCollapsed     TerminalPartDisplayMode = "collapsed"
	TerminalPartDisplayAutoCollapsed TerminalPartDisplayMode = "auto-collapsed"
	TerminalPartDisplayHidden        TerminalPartDisplayMode = "hidden"
)

// ResponseStatisticsMode controls which response statistic is shown.
type ResponseStatisticsMode string

const (
	ResponseStatisticsOutputTokenCount      ResponseStatisticsMode  = "outputTokenCount"
	ResponseStatisticsOutputTokensPerSecond ResponseStatisticsMode  = "outputTokensPerSecond"
	defaultResponseStatisticsMode           ResponseStatisticsMode  = ResponseStatisticsOutputTokensPerSecond
	defaultTerminalPartDisplayMode          TerminalPartDisplayMode = TerminalPartDisplayAutoCollapsed
)

// RunAgentTUIOptions configures RunAgentTUI.
type RunAgentTUIOptions struct {
	// Agent is the agent to run.
	Agent agent.Agent

	// Title is shown in the terminal UI header.
	Title string

	// Tools controls how tool calls render. Defaults to "auto-collapsed".
	Tools TerminalPartDisplayMode

	// Reasoning controls how reasoning parts render. Defaults to "auto-collapsed".
	Reasoning TerminalPartDisplayMode

	// ResponseStatistics controls which response statistic is shown.
	// Defaults to "outputTokensPerSecond".
	ResponseStatistics ResponseStatisticsMode

	// ContextSize is the model context window size in tokens. When > 0, the UI
	// shows total token usage as a percentage of this context window.
	ContextSize int

	// Input and Output are optional test hooks. When unset, os.Stdin/os.Stdout are used.
	Input  io.Reader
	Output io.Writer
}

// RunAgentTUI runs an agent in the default terminal UI until input ends,
// the context is cancelled, or the process receives an interrupt signal.
func RunAgentTUI(ctx context.Context, opts RunAgentTUIOptions) error {
	if opts.Agent == nil {
		return fmt.Errorf("agent is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.Input == nil {
		opts.Input = os.Stdin
	}
	if opts.Output == nil {
		opts.Output = os.Stdout
	}
	normalizeRunOptions(&opts)

	runner := NewAgentTUIRunner(AgentTUIRunnerOptions{
		Agent:              opts.Agent,
		Title:              opts.Title,
		Tools:              opts.Tools,
		Reasoning:          opts.Reasoning,
		ResponseStatistics: opts.ResponseStatistics,
		ContextSize:        opts.ContextSize,
		Renderer: NewTerminalRenderer(TerminalRendererOptions{
			Input:              opts.Input,
			Output:             opts.Output,
			Tools:              opts.Tools,
			Reasoning:          opts.Reasoning,
			ResponseStatistics: opts.ResponseStatistics,
			ContextSize:        opts.ContextSize,
		}),
		Input: opts.Input,
	})

	err := runner.Run(ctx)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func normalizeRunOptions(opts *RunAgentTUIOptions) {
	if opts.Tools == "" {
		opts.Tools = defaultTerminalPartDisplayMode
	}
	if opts.Reasoning == "" {
		opts.Reasoning = defaultTerminalPartDisplayMode
	}
	if opts.ResponseStatistics == "" {
		opts.ResponseStatistics = defaultResponseStatisticsMode
	}
}
