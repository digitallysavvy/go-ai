package tui

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

var errInterrupted = errors.New("Interrupted")

const (
	colorDim       = "\x1b[2m"
	colorUser      = "\x1b[96m"
	colorAssistant = "\x1b[92m"
	colorReasoning = "\x1b[94m"
	colorTool      = "\x1b[95m"
	colorError     = "\x1b[91m"

	activeControls              = "↑/↓ · PgUp/PgDn · Esc/Ctrl+C"
	doneControls                = "↑/↓ · PgUp/PgDn · q/Esc/Ctrl+C"
	processingStatus            = "Processing input... " + activeControls
	processingToolResultsStatus = "Processing tool results... " + activeControls
	streamingStatus             = "Streaming... " + activeControls
	executingToolsStatus        = "Executing tools... " + activeControls
	inputCursorBlink            = 500 * time.Millisecond
)

// TerminalRendererOptions configures TerminalRenderer.
type TerminalRendererOptions struct {
	Input              io.Reader
	Output             io.Writer
	FrameBuffer        *TerminalFrameBuffer
	Tools              TerminalPartDisplayMode
	Reasoning          TerminalPartDisplayMode
	ResponseStatistics ResponseStatisticsMode
	ContextSize        int
	Columns            int
	Rows               int
}

// TerminalSessionOptions configures a prompt or render session.
type TerminalSessionOptions struct {
	Title              string
	TitleSet           bool
	InitialPrompt      string
	SubmittedPrompt    string
	SubmittedPromptSet bool
	WaitForExit        bool
	WaitForExitSet     bool
	ContinueSession    bool
	Tools              TerminalPartDisplayMode
	Reasoning          TerminalPartDisplayMode
	ResponseStatistics ResponseStatisticsMode
	ContextSize        int
}

// TerminalRenderer renders agent streams as ANSI terminal frames.
type TerminalRenderer struct {
	mu                 sync.Mutex
	input              io.Reader
	output             io.Writer
	frameBuffer        *TerminalFrameBuffer
	interactive        bool
	tools              TerminalPartDisplayMode
	reasoning          TerminalPartDisplayMode
	responseStatistics ResponseStatisticsMode
	defaultContextSize int
	columns            int
	rows               int
	columnsFixed       bool
	rowsFixed          bool
	keyReader          *bufio.Reader
	scrollOffset       int
	restoreRawMode     func()
	stopResizeWatcher  func()

	sections       []chatSection
	title          string
	status         string
	totalTokens    *int64
	outputTokens   *int64
	outputTokensPS *float64
	lastPaint      paintState
	streamCounter  int
	lastStreamID   string
}

type paintState struct {
	input         string
	inputActive   bool
	cursorVisible bool
	contextSize   int
}

type chatSectionKind string

const (
	sectionUser      chatSectionKind = "user"
	sectionAssistant chatSectionKind = "assistant"
	sectionReasoning chatSectionKind = "reasoning"
	sectionTool      chatSectionKind = "tool"
	sectionError     chatSectionKind = "error"
)

type chatSection struct {
	kind       chatSectionKind
	title      string
	rightTitle string
	content    string
	collapsed  bool
	id         string
}

type toolSectionState struct {
	id               string
	toolName         string
	title            string
	input            map[string]interface{}
	inputText        string
	output           interface{}
	errorText        string
	deniedReason     string
	status           string
	providerExecuted bool
}

func NewTerminalRenderer(options TerminalRendererOptions) *TerminalRenderer {
	input := options.Input
	if input == nil {
		input = os.Stdin
	}
	output := options.Output
	if output == nil {
		output = os.Stdout
	}
	tools := options.Tools
	if tools == "" {
		tools = defaultTerminalPartDisplayMode
	}
	reasoning := options.Reasoning
	if reasoning == "" {
		reasoning = defaultTerminalPartDisplayMode
	}
	stats := options.ResponseStatistics
	if stats == "" {
		stats = defaultResponseStatisticsMode
	}
	frameBuffer := options.FrameBuffer
	if frameBuffer == nil {
		frameBuffer = NewTerminalFrameBuffer(output)
	}
	columns := options.Columns
	rows := options.Rows
	return &TerminalRenderer{
		input:              input,
		output:             output,
		frameBuffer:        frameBuffer,
		tools:              tools,
		reasoning:          reasoning,
		responseStatistics: stats,
		defaultContextSize: options.ContextSize,
		columns:            columns,
		rows:               rows,
		columnsFixed:       options.Columns > 0,
		rowsFixed:          options.Rows > 0,
		status:             streamingStatus,
	}
}

func (r *TerminalRenderer) ReadPrompt(ctx context.Context, options TerminalSessionOptions) (string, bool, error) {
	inputText := options.InitialPrompt
	contextSize := effectiveContextSize(options.ContextSize, r.defaultContextSize)
	cursorVisible := true
	r.mu.Lock()
	r.startLocked(options)
	r.status = "Type a prompt and press Enter · " + activeControls
	r.paintLocked(inputText, true, cursorVisible, contextSize)
	r.mu.Unlock()
	blinkDone := make(chan struct{})
	defer close(blinkDone)
	go func() {
		ticker := time.NewTicker(inputCursorBlink)
		defer ticker.Stop()
		for {
			select {
			case <-blinkDone:
				return
			case <-ticker.C:
				r.mu.Lock()
				select {
				case <-blinkDone:
					r.mu.Unlock()
					return
				default:
				}
				cursorVisible = !cursorVisible
				r.paintLocked(inputText, true, cursorVisible, contextSize)
				r.mu.Unlock()
			}
		}
	}()

	for {
		key, err := readTerminalKey(ctx, r.input, &r.keyReader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "", false, nil
			}
			return "", false, err
		}
		switch key.Type {
		case TerminalKeyCharacter:
			r.mu.Lock()
			inputText += key.Value
			cursorVisible = true
			r.paintLocked(inputText, true, cursorVisible, contextSize)
			r.mu.Unlock()
		case TerminalKeyBackspace:
			r.mu.Lock()
			inputText = dropLastRune(inputText)
			cursorVisible = true
			r.paintLocked(inputText, true, cursorVisible, contextSize)
			r.mu.Unlock()
		case TerminalKeyEnter:
			r.mu.Lock()
			cursorVisible = true
			r.status = processingStatus
			r.addUserSectionLocked(inputText, contextSize)
			r.paintLocked("", false, cursorVisible, contextSize)
			r.mu.Unlock()
			return inputText, true, nil
		case TerminalKeyUp:
			r.mu.Lock()
			r.handleScroll("up", 1)
			r.paintLocked(inputText, true, cursorVisible, contextSize)
			r.mu.Unlock()
		case TerminalKeyDown:
			r.mu.Lock()
			r.handleScroll("down", 1)
			r.paintLocked(inputText, true, cursorVisible, contextSize)
			r.mu.Unlock()
		case TerminalKeyPageUp:
			r.mu.Lock()
			r.handleScroll("up", r.bodyContentHeight())
			r.paintLocked(inputText, true, cursorVisible, contextSize)
			r.mu.Unlock()
		case TerminalKeyPageDown:
			r.mu.Lock()
			r.handleScroll("down", r.bodyContentHeight())
			r.paintLocked(inputText, true, cursorVisible, contextSize)
			r.mu.Unlock()
		case TerminalKeyCtrlL:
			r.mu.Lock()
			r.frameBuffer.Reset()
			r.paintLocked(inputText, true, cursorVisible, contextSize)
			r.mu.Unlock()
		case TerminalKeyEscape, TerminalKeyCtrlC:
			r.mu.Lock()
			r.stopLocked()
			r.mu.Unlock()
			return "", false, errInterrupted
		}
	}
}

func (r *TerminalRenderer) RenderStream(ctx context.Context, result AgentTUIStreamResult, options TerminalSessionOptions) ([]types.Message, error) {
	if result.Stream == nil {
		return nil, fmt.Errorf("stream is required")
	}
	contextSize := effectiveContextSize(options.ContextSize, r.defaultContextSize)
	r.mu.Lock()
	r.startLocked(options)
	r.status = processingStatus
	r.totalTokens = nil
	r.outputTokens = nil
	r.outputTokensPS = nil
	if options.SubmittedPromptSet || options.SubmittedPrompt != "" {
		r.addSubmittedPromptLocked(options.SubmittedPrompt, contextSize)
	}
	streamID := r.nextStreamSectionIDLocked(options)
	r.paintLocked("", false, true, contextSize)
	r.mu.Unlock()

	display := displayModes{
		tools:              effectivePartMode(options.Tools, r.tools),
		reasoning:          effectivePartMode(options.Reasoning, r.reasoning),
		responseStatistics: effectiveStatsMode(options.ResponseStatistics, r.responseStatistics),
	}
	model := newRenderModel(display, streamID)
	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()
	interrupt := make(chan struct{}, 1)
	if _, ok := r.input.(TerminalKeyReader); ok || isTerminalInput(r.input) {
		go r.listenForStreamKeys(streamCtx, cancelStream, interrupt, options)
	}

	for {
		chunk, err := result.Stream.Next(streamCtx)
		if err == io.EOF {
			break
		}
		if err != nil {
			select {
			case <-interrupt:
				if result.Abort != nil {
					result.Abort()
				}
				r.mu.Lock()
				r.status = "Interrupted"
				r.paintLocked("", false, true, contextSize)
				if !options.ContinueSession {
					r.stopLocked()
				}
				r.mu.Unlock()
				return nil, errInterrupted
			default:
			}
			if errors.Is(err, context.Canceled) {
				if result.Abort != nil {
					result.Abort()
				}
				r.mu.Lock()
				r.status = "Interrupted"
				r.paintLocked("", false, true, contextSize)
				if !options.ContinueSession {
					r.stopLocked()
				}
				r.mu.Unlock()
				return nil, errInterrupted
			}
			r.mu.Lock()
			r.addErrorSectionLocked("Error", err.Error())
			r.mu.Unlock()
			break
		}
		if chunk == nil {
			continue
		}
		r.mu.Lock()
		r.observeStatus(chunk)
		model.apply(*chunk)
		if (chunk.Type == provider.ChunkTypeFinish || chunk.Type == provider.ChunkTypeUsage) && chunk.Usage != nil {
			r.applyUsage(*chunk.Usage)
		}
		if chunk.Type == provider.ChunkTypeError {
			r.addErrorSectionLocked("Error", streamChunkErrorText(*chunk))
		}
		r.renderModel(model, display)
		r.paintLocked("", false, true, contextSize)
		r.mu.Unlock()
	}

	finalStep := result.Stream.FinalStep()
	r.mu.Lock()
	if finalStep.Usage.TotalTokens != nil || finalStep.Usage.OutputTokens != nil {
		r.applyUsage(finalStep.Usage)
	}
	if finalStep.Performance.OutputTokensPerSecond != nil {
		r.outputTokensPS = finalStep.Performance.OutputTokensPerSecond
	}
	r.renderModel(model, display)
	r.status = finalStatus(options.ContinueSession)
	r.paintLocked("", false, true, contextSize)
	r.mu.Unlock()
	cancelStream()
	if err := r.waitForExit(ctx, options); err != nil {
		if errors.Is(err, errInterrupted) || errors.Is(err, context.Canceled) {
			if result.Abort != nil {
				result.Abort()
			}
			r.mu.Lock()
			r.status = "Interrupted"
			r.paintLocked("", false, true, contextSize)
			if !options.ContinueSession {
				r.stopLocked()
			}
			r.mu.Unlock()
			return nil, errInterrupted
		}
		return nil, err
	}
	if !options.ContinueSession {
		r.mu.Lock()
		r.stopLocked()
		r.mu.Unlock()
	}
	return result.Stream.ResponseMessages(), nil
}

func (r *TerminalRenderer) ReadToolApproval(ctx context.Context, request AgentTUIToolApprovalRequest, options TerminalSessionOptions) (AgentTUIToolApprovalResponse, error) {
	contextSize := effectiveContextSize(options.ContextSize, r.defaultContextSize)
	r.mu.Lock()
	r.startLocked(options)
	r.status = "Approve " + formatToolApprovalTitle(request) + "? y/n · " + activeControls
	r.paintLocked("", false, true, contextSize)
	r.mu.Unlock()

	for {
		key, err := readTerminalKey(ctx, r.input, &r.keyReader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return AgentTUIToolApprovalResponse{Approved: false, Reason: "Denied by user."}, nil
			}
			return AgentTUIToolApprovalResponse{}, err
		}
		switch key.Type {
		case TerminalKeyCharacter:
			switch strings.ToLower(key.Value) {
			case "y":
				r.mu.Lock()
				r.status = "Approved · " + processingStatus
				r.paintLocked("", false, true, contextSize)
				r.mu.Unlock()
				return AgentTUIToolApprovalResponse{Approved: true}, nil
			case "n":
				r.mu.Lock()
				r.status = "Denied · " + processingStatus
				r.paintLocked("", false, true, contextSize)
				r.mu.Unlock()
				return AgentTUIToolApprovalResponse{Approved: false, Reason: "Denied by user."}, nil
			}
		case TerminalKeyUp:
			r.mu.Lock()
			r.handleScroll("up", 1)
			r.paintLocked("", false, true, contextSize)
			r.mu.Unlock()
		case TerminalKeyDown:
			r.mu.Lock()
			r.handleScroll("down", 1)
			r.paintLocked("", false, true, contextSize)
			r.mu.Unlock()
		case TerminalKeyPageUp:
			r.mu.Lock()
			r.handleScroll("up", r.bodyContentHeight())
			r.paintLocked("", false, true, contextSize)
			r.mu.Unlock()
		case TerminalKeyPageDown:
			r.mu.Lock()
			r.handleScroll("down", r.bodyContentHeight())
			r.paintLocked("", false, true, contextSize)
			r.mu.Unlock()
		case TerminalKeyCtrlL:
			r.mu.Lock()
			r.frameBuffer.Reset()
			r.paintLocked("", false, true, contextSize)
			r.mu.Unlock()
		case TerminalKeyEscape, TerminalKeyCtrlC:
			r.mu.Lock()
			r.stopLocked()
			r.mu.Unlock()
			return AgentTUIToolApprovalResponse{}, errInterrupted
		}
	}
}

func (r *TerminalRenderer) Stop() {
	r.mu.Lock()
	r.stopLocked()
	r.mu.Unlock()
}

func (r *TerminalRenderer) startLocked(options TerminalSessionOptions) {
	if options.TitleSet || options.Title != "" {
		r.title = options.Title
	}
	if r.interactive {
		return
	}
	r.interactive = true
	r.frameBuffer.Reset()
	_, _ = io.WriteString(r.output, "\x1b[?1049h\x1b[?25l")
	if r.restoreRawMode == nil {
		r.restoreRawMode = enableRawTerminalMode(r.input)
	}
	if r.stopResizeWatcher == nil {
		r.stopResizeWatcher = startTerminalResizeWatcher(r.output, func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.frameBuffer.Reset()
			r.repaintLastLocked()
		})
	}
}

func (r *TerminalRenderer) stopLocked() {
	if !r.interactive {
		return
	}
	if r.restoreRawMode != nil {
		r.restoreRawMode()
		r.restoreRawMode = nil
	}
	if r.stopResizeWatcher != nil {
		r.stopResizeWatcher()
		r.stopResizeWatcher = nil
	}
	_, _ = io.WriteString(r.output, "\x1b[?25h\x1b[?1049l")
	r.frameBuffer.Reset()
	r.interactive = false
}

func (r *TerminalRenderer) observeStatus(chunk *provider.StreamChunk) {
	switch chunk.Type {
	case provider.ChunkTypeToolResult:
		r.status = processingToolResultsStatus
	case provider.ChunkTypeToolCall:
		r.status = executingToolsStatus
	case provider.ChunkTypeTextStart, provider.ChunkTypeText, provider.ChunkTypeReasoningStart, provider.ChunkTypeReasoning, provider.ChunkTypeToolInputStart, provider.ChunkTypeToolInputDelta:
		r.status = streamingStatus
	}
}

func (r *TerminalRenderer) listenForStreamKeys(ctx context.Context, cancelStream context.CancelFunc, interrupt chan<- struct{}, options TerminalSessionOptions) {
	for {
		key, err := readTerminalKey(ctx, r.input, &r.keyReader)
		if err != nil {
			return
		}
		switch key.Type {
		case TerminalKeyUp:
			r.mu.Lock()
			r.handleScroll("up", 1)
			r.paintLocked("", false, true, effectiveContextSize(options.ContextSize, r.defaultContextSize))
			r.mu.Unlock()
		case TerminalKeyDown:
			r.mu.Lock()
			r.handleScroll("down", 1)
			r.paintLocked("", false, true, effectiveContextSize(options.ContextSize, r.defaultContextSize))
			r.mu.Unlock()
		case TerminalKeyPageUp:
			r.mu.Lock()
			r.handleScroll("up", r.bodyContentHeight())
			r.paintLocked("", false, true, effectiveContextSize(options.ContextSize, r.defaultContextSize))
			r.mu.Unlock()
		case TerminalKeyPageDown:
			r.mu.Lock()
			r.handleScroll("down", r.bodyContentHeight())
			r.paintLocked("", false, true, effectiveContextSize(options.ContextSize, r.defaultContextSize))
			r.mu.Unlock()
		case TerminalKeyCtrlL:
			r.mu.Lock()
			r.frameBuffer.Reset()
			r.paintLocked("", false, true, effectiveContextSize(options.ContextSize, r.defaultContextSize))
			r.mu.Unlock()
		case TerminalKeyEscape, TerminalKeyCtrlC:
			select {
			case interrupt <- struct{}{}:
			default:
			}
			cancelStream()
			return
		}
	}
}

func (r *TerminalRenderer) waitForExit(ctx context.Context, options TerminalSessionOptions) error {
	if (options.WaitForExitSet && !options.WaitForExit) || options.ContinueSession || !supportsTerminalWaitInput(r.input) {
		return nil
	}
	for {
		key, err := readTerminalKey(ctx, r.input, &r.keyReader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		switch key.Type {
		case TerminalKeyUp:
			r.mu.Lock()
			r.handleScroll("up", 1)
			r.paintLocked("", false, true, effectiveContextSize(options.ContextSize, r.defaultContextSize))
			r.mu.Unlock()
		case TerminalKeyDown:
			r.mu.Lock()
			r.handleScroll("down", 1)
			r.paintLocked("", false, true, effectiveContextSize(options.ContextSize, r.defaultContextSize))
			r.mu.Unlock()
		case TerminalKeyPageUp:
			r.mu.Lock()
			r.handleScroll("up", r.bodyContentHeight())
			r.paintLocked("", false, true, effectiveContextSize(options.ContextSize, r.defaultContextSize))
			r.mu.Unlock()
		case TerminalKeyPageDown:
			r.mu.Lock()
			r.handleScroll("down", r.bodyContentHeight())
			r.paintLocked("", false, true, effectiveContextSize(options.ContextSize, r.defaultContextSize))
			r.mu.Unlock()
		case TerminalKeyCtrlL:
			r.mu.Lock()
			r.frameBuffer.Reset()
			r.paintLocked("", false, true, effectiveContextSize(options.ContextSize, r.defaultContextSize))
			r.mu.Unlock()
		case TerminalKeyEscape:
			return nil
		case TerminalKeyCtrlC:
			return nil
		case TerminalKeyCharacter:
			if key.Value == "q" {
				return nil
			}
		}
	}
}

func supportsTerminalWaitInput(input io.Reader) bool {
	if _, ok := input.(TerminalKeyReader); ok {
		return true
	}
	return isTerminalInput(input)
}

func (r *TerminalRenderer) nextStreamSectionIDLocked(options TerminalSessionOptions) string {
	if !options.SubmittedPromptSet && r.lastStreamID != "" {
		return r.lastStreamID
	}
	r.streamCounter++
	r.lastStreamID = "response-" + strconv.Itoa(r.streamCounter)
	return r.lastStreamID
}

func (r *TerminalRenderer) renderModel(model *renderModel, display displayModes) {
	previous := r.bodyLineCount()
	activeSectionIDs := map[string]bool{}
	for _, section := range model.sections() {
		if section.kind == sectionAssistant {
			section.rightTitle = formatResponseStatistics(r.totalTokens, r.outputTokens, r.outputTokensPS, display.responseStatistics)
		}
		if section.id != "" {
			activeSectionIDs[section.id] = true
		}
		r.upsertSection(section)
	}
	r.removeStaleAssistantSections(model.sectionIDPrefix(), activeSectionIDs)
	r.adjustScrollAfterBodyChange(previous)
}

func (r *TerminalRenderer) applyUsage(usage types.Usage) {
	total := usage.TotalTokens
	if total == nil && usage.InputTokens != nil && usage.OutputTokens != nil {
		value := *usage.InputTokens + *usage.OutputTokens
		total = &value
	}
	if total != nil {
		r.totalTokens = total
	}
	if usage.OutputTokens != nil {
		r.outputTokens = usage.OutputTokens
	}
}

func (r *TerminalRenderer) addSubmittedPromptLocked(prompt string, contextSize int) {
	if len(r.sections) > 0 {
		last := r.sections[len(r.sections)-1]
		if last.kind == sectionUser && last.content == prompt {
			return
		}
	}
	r.addUserSectionLocked(prompt, contextSize)
}

func (r *TerminalRenderer) addUserSectionLocked(prompt string, contextSize int) {
	previous := r.bodyLineCount()
	r.sections = append(r.sections, chatSection{kind: sectionUser, title: "User", content: prompt})
	r.adjustScrollAfterBodyChange(previous)
	r.paintLocked("", false, true, contextSize)
}

func (r *TerminalRenderer) addErrorSectionLocked(title, content string) {
	previous := r.bodyLineCount()
	r.sections = append(r.sections, chatSection{kind: sectionError, title: title, content: content})
	r.adjustScrollAfterBodyChange(previous)
}

func (r *TerminalRenderer) upsertSection(section chatSection) {
	if section.id != "" {
		for i := range r.sections {
			if r.sections[i].id == section.id {
				r.sections[i] = section
				return
			}
		}
	}
	r.sections = append(r.sections, section)
}

func (r *TerminalRenderer) removeStaleAssistantSections(prefix string, activeSectionIDs map[string]bool) {
	if prefix == "" {
		return
	}
	var next []chatSection
	for _, section := range r.sections {
		if section.id == "" || !strings.HasPrefix(section.id, prefix) || activeSectionIDs[section.id] {
			next = append(next, section)
		}
	}
	r.sections = next
}

func (r *TerminalRenderer) paintLocked(input string, inputActive bool, cursorVisible bool, contextSize int) {
	r.lastPaint = paintState{input: input, inputActive: inputActive, cursorVisible: cursorVisible, contextSize: contextSize}
	frame := renderScreenViewport(screenViewportState{
		width:            r.width(),
		height:           r.height(),
		title:            r.title,
		rightTitle:       formatTokenCount(r.totalTokens, contextSize),
		visibleBodyLines: r.visibleBodyLines(),
		input:            input,
		inputActive:      inputActive,
		cursorVisible:    cursorVisible,
		status:           r.status,
	})
	r.frameBuffer.Present(frame)
}

func (r *TerminalRenderer) repaintLastLocked() {
	state := r.lastPaint
	r.paintLocked(state.input, state.inputActive, state.cursorVisible, state.contextSize)
}

func (r *TerminalRenderer) visibleBodyLines() []string {
	if len(r.sections) == 0 {
		return []string{"Waiting for input..."}
	}
	contentHeight := r.bodyContentHeight()
	totalLineCount := r.bodyLineCount()
	start := maxInt(0, totalLineCount-contentHeight-r.scrollOffset)
	end := start + contentHeight
	var lines []string
	sectionStart := 0
	sectionWidth := r.width() - 4
	for _, section := range r.sections {
		sectionLines := renderSectionLines(section, sectionWidth)
		sectionEnd := sectionStart + len(sectionLines)
		if sectionEnd > start && sectionStart < end {
			from := maxInt(0, start-sectionStart)
			to := minInt(len(sectionLines), end-sectionStart)
			lines = append(lines, sectionLines[from:to]...)
		}
		if len(lines) >= contentHeight {
			break
		}
		sectionStart = sectionEnd
	}
	return lines
}

func (r *TerminalRenderer) bodyContentHeight() int {
	return maxInt(1, r.height()-5)
}

func (r *TerminalRenderer) bodyLineCount() int {
	if len(r.sections) == 0 {
		return 1
	}
	count := 0
	sectionWidth := r.width() - 4
	for _, section := range r.sections {
		count += len(renderSectionLines(section, sectionWidth))
	}
	return count
}

func (r *TerminalRenderer) width() int {
	if r.columnsFixed {
		return maxInt(20, r.columns)
	}
	if columns, _ := terminalOutputSize(r.output); columns > 0 {
		return maxInt(20, columns)
	}
	return maxInt(20, terminalColumns())
}

func (r *TerminalRenderer) height() int {
	if r.rowsFixed {
		return maxInt(8, r.rows)
	}
	if _, rows := terminalOutputSize(r.output); rows > 0 {
		return maxInt(8, rows)
	}
	return 24
}

func (r *TerminalRenderer) handleScroll(direction string, lines int) {
	delta := lines
	if direction == "down" {
		delta = -lines
	}
	r.scrollOffset = r.clampScrollOffset(r.scrollOffset + delta)
}

func (r *TerminalRenderer) adjustScrollAfterBodyChange(previousBodyLineCount int) {
	if r.scrollOffset == 0 {
		return
	}
	delta := r.bodyLineCount() - previousBodyLineCount
	r.scrollOffset = r.clampScrollOffset(r.scrollOffset + delta)
}

func (r *TerminalRenderer) clampScrollOffset(offset int) int {
	maxOffset := maxInt(0, r.bodyLineCount()-r.bodyContentHeight())
	return minInt(maxInt(0, offset), maxOffset)
}

type displayModes struct {
	tools              TerminalPartDisplayMode
	reasoning          TerminalPartDisplayMode
	responseStatistics ResponseStatisticsMode
}

type renderModel struct {
	display      displayModes
	streamID     string
	order        []string
	text         map[string]string
	reasoning    map[string]string
	tools        map[string]*toolSectionState
	activeTextID string
}

func newRenderModel(display displayModes, streamID string) *renderModel {
	return &renderModel{
		display:   display,
		streamID:  streamID,
		text:      map[string]string{},
		reasoning: map[string]string{},
		tools:     map[string]*toolSectionState{},
	}
}

func (m *renderModel) apply(chunk provider.StreamChunk) {
	switch chunk.Type {
	case provider.ChunkTypeTextStart:
		id := nonEmpty(chunk.ID, "text")
		m.ensureOrder("text:" + id)
		m.activeTextID = id
	case provider.ChunkTypeText:
		id := nonEmpty(chunk.ID, m.activeTextID)
		if id == "" {
			id = "text"
		}
		m.ensureOrder("text:" + id)
		m.activeTextID = id
		m.text[id] += chunk.Text
	case provider.ChunkTypeTextEnd:
		if chunk.ID == m.activeTextID {
			m.activeTextID = ""
		}
	case provider.ChunkTypeReasoningStart:
		id := nonEmpty(chunk.ID, "reasoning")
		m.ensureOrder("reasoning:" + id)
	case provider.ChunkTypeReasoning:
		id := nonEmpty(chunk.ID, "reasoning")
		m.ensureOrder("reasoning:" + id)
		m.reasoning[id] += chunk.Reasoning
	case provider.ChunkTypeToolInputStart:
		if chunk.ToolCall != nil {
			tool := m.ensureTool(chunk.ToolCall.ID, chunk.ToolCall.ToolName, chunk.ToolCall.Title)
			tool.status = "waiting"
		} else if chunk.ID != "" {
			tool := m.ensureTool(chunk.ID, "", "")
			tool.status = "waiting"
		}
	case provider.ChunkTypeToolInputDelta:
		if chunk.ToolCall != nil {
			tool := m.ensureTool(chunk.ToolCall.ID, chunk.ToolCall.ToolName, chunk.ToolCall.Title)
			tool.inputText += chunk.Text
			tool.status = "waiting"
		} else if chunk.ID != "" {
			tool := m.ensureTool(chunk.ID, "", "")
			tool.inputText += chunk.Text
			tool.status = "waiting"
		}
	case provider.ChunkTypeToolInputEnd:
		if chunk.ToolCall != nil {
			m.ensureTool(chunk.ToolCall.ID, chunk.ToolCall.ToolName, chunk.ToolCall.Title)
		} else if chunk.ID != "" {
			m.ensureTool(chunk.ID, "", "")
		}
	case provider.ChunkTypeToolCall:
		if chunk.ToolCall != nil {
			tool := m.ensureTool(chunk.ToolCall.ID, chunk.ToolCall.ToolName, chunk.ToolCall.Title)
			tool.input = chunk.ToolCall.Arguments
			tool.inputText = chunk.ToolCall.RawArguments
			tool.providerExecuted = chunk.ToolCall.ProviderExecuted
			tool.status = "executing"
		}
	case provider.ChunkTypeToolResult:
		if chunk.ToolResult != nil {
			tool := m.ensureTool(chunk.ToolResult.ToolCallID, chunk.ToolResult.ToolName, chunk.ToolResult.Title)
			tool.input = chunk.ToolResult.Input
			tool.output = chunk.ToolResult.Result
			tool.status = "done"
			if chunk.ToolResult.Error != nil {
				tool.errorText = chunk.ToolResult.Error.Error()
			}
			switch chunk.ToolResult.ApprovalStatus {
			case types.ToolApprovalStatusUserApproval:
				tool.status = "approval requested"
				tool.output = nil
			case types.ToolApprovalStatusApproved:
				if chunk.ToolResult.ProviderExecuted && chunk.ToolResult.Result == nil && chunk.ToolResult.Error == nil {
					tool.status = "executing"
				}
			case types.ToolApprovalStatusDenied:
				tool.status = "denied"
				if chunk.ToolResult.ApprovalReason != nil && *chunk.ToolResult.ApprovalReason != "" {
					tool.deniedReason = *chunk.ToolResult.ApprovalReason
				} else {
					tool.deniedReason = "denied"
				}
			}
			if chunk.ToolResult.ProviderExecuted && chunk.ToolResult.Result == nil && chunk.ToolResult.Error == nil && chunk.ToolResult.ApprovalStatus == "" {
				tool.status = "executing"
			}
		}
	}
}

func (m *renderModel) ensureTool(id, name, title string) *toolSectionState {
	id = nonEmpty(id, name)
	key := "tool:" + id
	m.ensureOrder(key)
	tool := m.tools[id]
	if tool == nil {
		tool = &toolSectionState{id: id, toolName: name, title: title, status: "waiting"}
		m.tools[id] = tool
	}
	if name != "" {
		tool.toolName = name
	}
	if title != "" {
		tool.title = title
	}
	return tool
}

func (m *renderModel) ensureOrder(key string) {
	for _, existing := range m.order {
		if existing == key {
			return
		}
	}
	m.order = append(m.order, key)
}

func (m *renderModel) sections() []chatSection {
	var sections []chatSection
	visibleIndexes := map[int]bool{}
	for i, key := range m.order {
		if m.isVisible(key) {
			visibleIndexes[i] = true
		}
	}
	for i, key := range m.order {
		if !visibleIndexes[i] {
			continue
		}
		collapsed := m.shouldCollapse(i, key)
		switch {
		case strings.HasPrefix(key, "text:"):
			id := strings.TrimPrefix(key, "text:")
			content := strings.TrimSpace(m.text[id])
			sections = append(sections, chatSection{id: m.sectionID("assistant", id), kind: sectionAssistant, title: "Assistant", content: content})
		case strings.HasPrefix(key, "reasoning:"):
			id := strings.TrimPrefix(key, "reasoning:")
			sections = append(sections, chatSection{id: m.sectionID("reasoning", id), kind: sectionReasoning, title: "Reasoning", content: strings.TrimSpace(m.reasoning[id]), collapsed: collapsed})
		case strings.HasPrefix(key, "tool:"):
			id := strings.TrimPrefix(key, "tool:")
			if tool := m.tools[id]; tool != nil {
				sections = append(sections, renderToolSection(m.sectionID("tool", tool.id), tool, collapsed))
			}
		}
	}
	return sections
}

func (m *renderModel) sectionID(kind, id string) string {
	return m.sectionIDPrefix() + kind + ":" + id
}

func (m *renderModel) sectionIDPrefix() string {
	if m.streamID == "" {
		return ""
	}
	return m.streamID + ":"
}

func (m *renderModel) isVisible(key string) bool {
	switch {
	case strings.HasPrefix(key, "text:"):
		return strings.TrimSpace(m.text[strings.TrimPrefix(key, "text:")]) != ""
	case strings.HasPrefix(key, "reasoning:"):
		return m.display.reasoning != TerminalPartDisplayHidden && strings.TrimSpace(m.reasoning[strings.TrimPrefix(key, "reasoning:")]) != ""
	case strings.HasPrefix(key, "tool:"):
		return m.display.tools != TerminalPartDisplayHidden
	}
	return false
}

func (m *renderModel) shouldCollapse(index int, key string) bool {
	mode := TerminalPartDisplayFull
	if strings.HasPrefix(key, "reasoning:") {
		mode = m.display.reasoning
	} else if strings.HasPrefix(key, "tool:") {
		mode = m.display.tools
	}
	switch mode {
	case TerminalPartDisplayCollapsed:
		return true
	case TerminalPartDisplayAutoCollapsed:
		for i := index + 1; i < len(m.order); i++ {
			if m.isVisible(m.order[i]) {
				return true
			}
		}
	}
	return false
}

func renderToolSection(sectionID string, tool *toolSectionState, collapsed bool) chatSection {
	name := nonEmpty(tool.title, tool.toolName)
	title := "Tool · " + name
	content := toolInputText(tool)
	if tool.output != nil {
		content += "\n\nOutput:\n" + formatValue(tool.output)
	}
	if tool.errorText != "" {
		return chatSection{id: sectionID, kind: sectionError, title: "Tool Error · " + name, rightTitle: "done", content: content + "\n\nError:\n" + tool.errorText, collapsed: collapsed}
	}
	if tool.status == "denied" {
		reason := nonEmpty(tool.deniedReason, "denied")
		return chatSection{id: sectionID, kind: sectionError, title: "Tool Denied · " + name, rightTitle: "denied", content: content + "\n\nReason: " + reason, collapsed: collapsed}
	}
	return chatSection{id: sectionID, kind: sectionTool, title: title, rightTitle: nonEmpty(tool.status, "waiting"), content: content, collapsed: collapsed}
}

func formatToolApprovalTitle(request AgentTUIToolApprovalRequest) string {
	title := request.Title
	if title == "" {
		title = request.ToolName
	}
	return "tool " + title
}

func toolInputText(tool *toolSectionState) string {
	if tool.input != nil {
		return "Input:\n" + formatValue(tool.input)
	}
	if tool.inputText != "" {
		return "Input:\n" + tool.inputText
	}
	return "Input: (streaming...)"
}

func renderSectionLines(section chatSection, width int) []string {
	width = maxInt(8, width)
	styleColor, border := sectionStyle(section.kind)
	contentWidth := maxInt(1, width-4)
	title := " " + section.title + " "
	right := ""
	if section.rightTitle != "" {
		right = " " + section.rightTitle + " "
	}
	borderWidth := maxInt(0, width-2-visibleLength(title)-visibleLength(right))
	top := styleColor + "╭" + title + strings.Repeat(border, borderWidth) + right + "╮" + ansiReset
	bottom := styleColor + "╰" + strings.Repeat(border, maxInt(0, width-2)) + "╯" + ansiReset
	if section.collapsed {
		return []string{top, bottom}
	}
	content := RenderMarkdown(section.content)
	if content == "" {
		content = colorDim + "(streaming...)" + ansiReset
	}
	var lines []string
	for _, line := range strings.Split(content, "\n") {
		for _, wrapped := range wrapVisibleLine(line, contentWidth) {
			lines = append(lines, sectionLine(wrapped, contentWidth, styleColor))
		}
	}
	return append(append([]string{top}, lines...), bottom)
}

func sectionLine(line string, width int, color string) string {
	visible := sliceVisible(line, width)
	padding := strings.Repeat(" ", maxInt(0, width-visibleLength(visible)))
	return color + "│" + ansiReset + " " + visible + padding + " " + color + "│" + ansiReset
}

func sectionStyle(kind chatSectionKind) (string, string) {
	switch kind {
	case sectionUser:
		return colorUser, "─"
	case sectionAssistant:
		return colorAssistant, "─"
	case sectionReasoning:
		return colorReasoning, "·"
	case sectionTool:
		return colorTool, "─"
	case sectionError:
		return colorError, "─"
	default:
		return "", "─"
	}
}

type screenViewportState struct {
	width            int
	height           int
	title            string
	rightTitle       string
	visibleBodyLines []string
	input            string
	inputActive      bool
	cursorVisible    bool
	status           string
}

func renderScreenViewport(state screenViewportState) string {
	width := maxInt(20, state.width)
	height := maxInt(8, state.height)
	bodyContentHeight := height - 5
	visible := append([]string(nil), state.visibleBodyLines...)
	if len(visible) > bodyContentHeight {
		visible = visible[:bodyContentHeight]
	}
	for len(visible) < bodyContentHeight {
		visible = append(visible, "")
	}
	lines := []string{topBorder(width, state.title, state.rightTitle)}
	for _, line := range visible {
		lines = append(lines, boxLine(line, width))
	}
	lines = append(lines, bottomBorder(width), topBorder(width, mapInputTitle(state.inputActive), ""), boxLine(statusLine(state), width), bottomBorder(width))
	return strings.Join(lines, "\n")
}

func mapInputTitle(active bool) string {
	if active {
		return "Input"
	}
	return "Status"
}

func statusLine(state screenViewportState) string {
	if state.inputActive {
		cursor := "█"
		if !state.cursorVisible {
			cursor = " "
		}
		return "> " + state.input + cursor
	}
	if state.status != "" {
		return state.status
	}
	return streamingStatus
}

func topBorder(width int, title, rightTitle string) string {
	contentWidth := maxInt(0, width-2)
	label := ""
	if title != "" {
		label = sliceVisible(" "+title+" ", contentWidth)
	}
	right := ""
	if rightTitle != "" {
		right = sliceVisible(" "+rightTitle+" ", maxInt(0, contentWidth-visibleLength(label)))
	}
	remaining := maxInt(0, contentWidth-visibleLength(label)-visibleLength(right))
	return "┌" + label + strings.Repeat("─", remaining) + right + "┐"
}

func bottomBorder(width int) string {
	return "└" + strings.Repeat("─", width-2) + "┘"
}

func boxLine(line string, width int) string {
	contentWidth := width - 4
	visible := sliceVisible(line, contentWidth)
	padding := strings.Repeat(" ", maxInt(0, contentWidth-visibleLength(visible)))
	return "│ " + visible + padding + " │"
}

func formatValue(value interface{}) string {
	if s, ok := value.(string); ok {
		return s
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(data)
}

func formatTokenCount(tokens *int64, contextSize int) string {
	if tokens == nil {
		return ""
	}
	count := formatInt(*tokens) + " tokens"
	if *tokens == 1 {
		count = "1 token"
	}
	if contextSize > 0 {
		percent := int64(math.Round((float64(*tokens) / float64(contextSize)) * 100))
		count += " " + formatInt(percent) + "%"
	}
	return count
}

func formatResponseStatistics(totalTokens, outputTokens *int64, outputTokensPS *float64, mode ResponseStatisticsMode) string {
	if mode == ResponseStatisticsOutputTokensPerSecond {
		if outputTokensPS == nil {
			return ""
		}
		return formatFloat(*outputTokensPS) + " tok/s"
	}
	return formatTokenCount(outputTokens, 0)
}

func formatFloat(value float64) string {
	if math.Trunc(value) == value {
		return formatInt(int64(value))
	}
	return strconv.FormatFloat(math.Round(value*10)/10, 'f', 1, 64)
}

func formatInt(value int64) string {
	sign := ""
	if value < 0 {
		sign = "-"
		value = -value
	}
	s := strconv.FormatInt(value, 10)
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	return sign + strings.Join(parts, ",")
}

func streamChunkErrorText(chunk provider.StreamChunk) string {
	if chunk.AbortReason != "" {
		return chunk.AbortReason
	}
	if chunk.Text != "" {
		return chunk.Text
	}
	return "stream error"
}

func terminalColumns() int {
	if value := os.Getenv("COLUMNS"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			return parsed
		}
	}
	return 80
}

func effectivePartMode(value, fallback TerminalPartDisplayMode) TerminalPartDisplayMode {
	if value != "" {
		return value
	}
	return fallback
}

func effectiveStatsMode(value, fallback ResponseStatisticsMode) ResponseStatisticsMode {
	if value != "" {
		return value
	}
	return fallback
}

func effectiveContextSize(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func finalStatus(continueSession bool) string {
	if continueSession {
		return "Done · Enter another prompt · " + activeControls
	}
	return "Done · " + doneControls
}

func nonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func dropLastRune(value string) string {
	if value == "" {
		return ""
	}
	runes := []rune(value)
	return string(runes[:len(runes)-1])
}
