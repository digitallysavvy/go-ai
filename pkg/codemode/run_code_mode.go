package codemode

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/internal/third_party/qjs"
)

// RunCodeMode runs code-mode JavaScript directly, without wrapping it as an
// AI SDK tool. The source runs as the body of an async function, so
// top-level `await`/`return` are supported; the source is wrapped in a
// fresh QuickJS sandbox with input.Options' execution limits applied.
//
// Mirrors TypeScript's experimental_runCodeMode (code-mode/src/
// run-code-mode.ts), except for the continuation/interrupt inputs -- see
// the package doc for what is deferred.
func RunCodeMode(ctx context.Context, input RunInput) (interface{}, error) {
	var options *Options
	if input.Options != nil {
		options = input.Options
	}
	var policyInput *ExecutionPolicy
	if options != nil {
		policyInput = options.ExecutionPolicy
	}
	policy, perr := resolveExecutionPolicy(policyInput)
	if perr != nil {
		return nil, perr
	}

	if err := assertSourceSize(input.JS, policy.MaxSourceBytes); err != nil {
		return nil, err
	}

	outerToolCall := "code-mode-1"
	if input.ToolExecutionOptions != nil && input.ToolExecutionOptions.ToolCallID != "" {
		outerToolCall = input.ToolExecutionOptions.ToolCallID
	}

	bridge := newToolBridge(ctx, input, options, policy, outerToolCall)
	source := wrapCodeModeSource(stripTypeScriptAnnotations(input.JS))

	resultJSON, isUndefined, err := runInSandbox(ctx, policy, source, func(jsCtx *qjs.Context) error {
		return bindCodeModeDispatch(jsCtx, bridge)
	})
	if err != nil {
		bridge.mu.Lock()
		preserved := bridge.lastCodeModeErr
		bridge.mu.Unlock()
		if preserved != nil {
			return nil, preserved
		}
		return nil, err
	}
	if isUndefined {
		return nil, nil
	}

	if serr := assertJSONPayloadSize(resultJSON, policy.MaxResultBytes, "Code mode result"); serr != nil {
		return nil, serr
	}
	return fromJSONPayload(resultJSON)
}

func assertSourceSize(source string, maxBytes int) error {
	bytes := len([]byte(source))
	if maxBytes > 0 && bytes > maxBytes {
		return NewSourceTooLargeError(bytes, maxBytes)
	}
	return nil
}

// wrapCodeModeSource wraps (type-stripped) user JS as the body of an async
// IIFE, exposing `tools` as a Proxy that forwards every property access to
// the single __codeModeDispatch host function bound by
// bindCodeModeDispatch. Mirrors TypeScript's createCodeModeSource; this
// port uses one dispatch function plus a real JS Proxy instead of TS's
// numbered per-tool-name bindings, since the Go host-function bridge below
// already resolves unknown tool names into a proper "Unknown tool: x"
// CodeModeError (see toolBridge.invoke) without needing to know the tool
// set up front.
func wrapCodeModeSource(js string) string {
	return "(async()=>{\n" +
		"const tools=new Proxy(Object.create(null),{get(_t,name){return (input)=>__codeModeDispatch(String(name),input);}});\n" +
		js + "\n" +
		"})()"
}

// bindCodeModeDispatch installs the single global host function that every
// `tools.<name>(input)` call is routed through by the Proxy in
// wrapCodeModeSource.
func bindCodeModeDispatch(jsCtx *qjs.Context, bridge *toolBridge) error {
	dispatch := jsCtx.Function(func(this *qjs.This) (*qjs.Value, error) {
		args := this.Args()
		if len(args) == 0 {
			return nil, NewProtocolError("code-mode dispatch called without a tool name.", nil)
		}
		toolName := args[0].String()

		inputJSON := ""
		if len(args) > 1 && !args[1].IsUndefined() {
			js, err := args[1].JSONStringify()
			if err != nil {
				return nil, err
			}
			inputJSON = js
		}

		outJSON, ierr := bridge.invoke(toolName, inputJSON)
		if ierr != nil {
			return nil, ierr
		}
		if outJSON == "" {
			return jsCtx.NewUndefined(), nil
		}
		return jsCtx.ParseJSON(outJSON), nil
	})
	jsCtx.Global().SetPropertyStr("__codeModeDispatch", dispatch)
	return nil
}
