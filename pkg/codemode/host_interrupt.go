package codemode

import "fmt"

// interruptSignal is returned as an error by RequestCodeModeInterrupt for a
// host tool's Execute function to return immediately. The toolBridge
// recognizes it (by type, not by message) and turns it into a pending
// Interrupt instead of a generic tool failure -- Go's closest equivalent to
// TypeScript's getHostFunctionContext().interrupt(payload), which throws a
// special HostFunctionInterruptSignal exception that unwinds the sandboxed
// script (see tool-invocation's handling in tool_invocation.go).
//
// interruptSignal deliberately does NOT implement the CodeModeError
// interface: TypeScript's HostFunctionInterruptSignal is a plain Error, not
// a CodeModeError subclass, and toolBridge relies on that distinction to
// tell a genuine interrupt request apart from every other tool failure.
type interruptSignal struct {
	payload InterruptPayload
}

func (s *interruptSignal) Error() string {
	return fmt.Sprintf("code mode interrupt requested (kind=%q)", s.payload.Kind())
}

// RequestCodeModeInterrupt is called from inside a host tool's Execute
// function to pause the enclosing code-mode invocation and surface payload
// to the caller as part of an Interrupt, to be resumed later with
// ContinueCodeModeInterrupt. A resumed call receives the resolution via
// types.ToolExecutionOptions.CodeModeInterrupt (a
// *InterruptExecutionContext); Execute should check that field first and
// only call RequestCodeModeInterrupt when it is nil:
//
//	Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
//		if resume, ok := opts.CodeModeInterrupt.(*codemode.InterruptExecutionContext); ok {
//			return resume.Resolution, nil
//		}
//		return nil, codemode.RequestCodeModeInterrupt(codemode.InterruptPayload{"kind": "authorization"})
//	}
//
// Mirrors TypeScript's experimental_requestCodeModeInterrupt
// (code-mode/src/host-interrupt.ts), which returns `never` because it
// always throws; the Go analog of "always throw" is "always return an
// error for the caller to return immediately", so RequestCodeModeInterrupt
// returns a non-nil error unconditionally, including when payload itself
// is invalid.
func RequestCodeModeInterrupt(payload InterruptPayload) error {
	if payload == nil {
		return NewProtocolError("Code mode interrupt payload must be an object.", nil)
	}
	if payload.Kind() == "" {
		return NewProtocolError("Code mode interrupt payload must include a string kind.", nil)
	}
	return &interruptSignal{payload: payload}
}
