package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Multi-round-trip request limits (TS mcp-client.ts, c3af430640).
const (
	maxInputRequiredRounds  = 10
	inputRequiredRetryDelay = 250 * time.Millisecond
)

// inputRequiredRetryParams are the params of a retried request: the original
// params plus `inputResponses` and the server's unchanged `requestState`,
// rebuilt from the original request every round so answers never leak from
// one round into the next.
type inputRequiredRetryParams struct {
	base           interface{}
	inputResponses map[string]ElicitResult
	requestState   *string
}

func (p inputRequiredRetryParams) MarshalJSON() ([]byte, error) {
	merged := map[string]interface{}{}
	if p.base != nil {
		data, err := json.Marshal(p.base)
		if err != nil {
			return nil, err
		}
		if len(data) > 0 && string(data) != "null" {
			if err := json.Unmarshal(data, &merged); err != nil {
				return nil, err
			}
		}
	}
	if len(p.inputResponses) > 0 {
		merged["inputResponses"] = p.inputResponses
	}
	if p.requestState != nil {
		merged["requestState"] = *p.requestState
	}
	return json.Marshal(merged)
}

// inputRequiredResult is TS InputRequiredResultSchema.
type inputRequiredResult struct {
	InputRequests map[string]inputRequest
	// keys are the inputRequests keys in JSON order (TS Object.entries).
	keys         []string
	RequestState *string
}

type inputRequest struct {
	Method string
	Params json.RawMessage
}

func parseInputRequiredResult(raw json.RawMessage) (inputRequiredResult, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return inputRequiredResult{}, err
	}
	var out inputRequiredResult
	if r, ok := fields["inputRequests"]; ok {
		var requests map[string]json.RawMessage
		if err := json.Unmarshal(r, &requests); err != nil || requests == nil {
			return out, errors.New("inputRequests must be an object")
		}
		out.InputRequests = make(map[string]inputRequest, len(requests))
		out.keys = orderedObjectKeys(r)
		for key, rawReq := range requests {
			var req struct {
				Method *string         `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			var obj map[string]json.RawMessage
			if json.Unmarshal(rawReq, &obj) != nil || obj == nil || json.Unmarshal(rawReq, &req) != nil || req.Method == nil {
				return out, fmt.Errorf("inputRequests.%s must be a request with a string method", key)
			}
			// TS z.optional(BaseParamsSchema): an explicit null fails.
			if len(req.Params) > 0 {
				var p map[string]json.RawMessage
				if json.Unmarshal(req.Params, &p) != nil || p == nil {
					return out, fmt.Errorf("inputRequests.%s.params must be an object", key)
				}
			}
			out.InputRequests[key] = inputRequest{Method: *req.Method, Params: req.Params}
		}
	}
	if r, ok := fields["requestState"]; ok {
		// TS z.string().optional(): null is not a string (json.Unmarshal
		// would leave the string empty without an error).
		var state string
		if len(r) == 0 || r[0] != '"' || json.Unmarshal(r, &state) != nil {
			return out, errors.New("requestState must be a string")
		}
		out.RequestState = &state
	}
	if out.InputRequests == nil && out.RequestState == nil {
		return out, errors.New("input_required must include inputRequests or requestState")
	}
	return out, nil
}

// parseFormElicitation is TS InputRequiredElicitationSchema: an
// elicitation request whose params carry a string message, an optional
// mode "form" and a requestedSchema object with type "object".
func parseFormElicitation(params json.RawMessage) (ElicitationRequest, error) {
	req, invalid, _ := parseElicitationRequestParams(params)
	if invalid != "" {
		return req, errors.New(invalid)
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(params, &fields)
	if m, ok := fields["mode"]; ok {
		var mode string
		if json.Unmarshal(m, &mode) != nil || mode != "form" {
			return req, errors.New(`params.mode must be "form"`)
		}
	}
	schema, ok := req.RequestedSchema.(map[string]interface{})
	if !ok || schema["type"] != "object" {
		return req, errors.New(`params.requestedSchema must be an object schema with type "object"`)
	}
	return req, nil
}

// inputRequestMode returns params.mode when it is present and not null.
func inputRequestMode(params json.RawMessage) (string, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(params, &fields) != nil {
		return "", false
	}
	raw, ok := fields["mode"]
	if !ok || string(raw) == "null" {
		return "", false
	}
	var mode string
	if json.Unmarshal(raw, &mode) != nil {
		return string(raw), true
	}
	return mode, true
}

// callWithInputRequired is TS request() for modern-era tools/call,
// prompts/get and resources/read (c3af430640): an `input_required` result
// is answered through the OnElicitationRequest handler (form elicitation
// only) and the request is retried with `inputResponses` and the unchanged
// `requestState`, up to 10 rounds; a round without answers waits 250 ms.
// Each round has its own timeout; MaxTotalTimeoutMS bounds them all.
func (c *MCPClient) callWithInputRequired(ctx context.Context, method string, params interface{}, result interface{}) error {
	if err := ctx.Err(); err != nil {
		return &MCPClientError{Message: "Request was aborted", Cause: err}
	}
	ro, _ := requestOptionsFrom(ctx)
	startedAt := time.Now()
	nextParams := params

	for round := 0; ; round++ {
		// TS getEffectiveTimeout({timeout, maxTotalTimeout: remaining}); Go
		// falls back to the client RequestTimeoutMS when neither is set.
		var timeout time.Duration
		timeoutSet := false
		if ro.TimeoutMS > 0 {
			timeout, timeoutSet = time.Duration(ro.TimeoutMS)*time.Millisecond, true
		}
		if ro.MaxTotalTimeoutMS > 0 {
			// Whole milliseconds, like TS Date.now() arithmetic.
			remaining := time.Duration(ro.MaxTotalTimeoutMS)*time.Millisecond - time.Since(startedAt).Truncate(time.Millisecond)
			if remaining < 0 {
				remaining = 0
			}
			if !timeoutSet || remaining < timeout {
				timeout = remaining
			}
			timeoutSet = true
		}
		if !timeoutSet && c.config.RequestTimeoutMS > 0 {
			timeout, timeoutSet = time.Duration(c.config.RequestTimeoutMS)*time.Millisecond, true
		}
		if timeoutSet && timeout <= 0 {
			// TS: `if (timeout === 0) throw timeoutError`.
			return NewMCPClientError(0, "Request timed out after 0ms", nil)
		}

		done, err := c.inputRequiredRound(ctx, method, params, &nextParams, result, round, timeout, timeoutSet)
		if err != nil || done {
			return err
		}
	}
}

// inputRequiredRound runs one round; done reports a final result.
func (c *MCPClient) inputRequiredRound(ctx context.Context, method string, params interface{}, nextParams *interface{}, result interface{}, round int, timeout time.Duration, timeoutSet bool) (done bool, err error) {
	roundCtx := ctx
	if timeoutSet {
		var cancel context.CancelFunc
		roundCtx, cancel = context.WithTimeoutCause(ctx, timeout, errRequestTimeout)
		defer cancel()
	}
	// aborted maps a cancellation like TS's catch: the caller's own abort is
	// "Request was aborted", the round timeout is the timeout error, and a
	// closed client is "Connection closed".
	aborted := func() error {
		if err := ctx.Err(); err != nil {
			return &MCPClientError{Message: "Request was aborted", Cause: err}
		}
		if errors.Is(context.Cause(roundCtx), errRequestTimeout) {
			return &TimeoutError{Operation: method, TimeoutMS: int(timeout / time.Millisecond)}
		}
		return NewMCPClientError(0, "Connection closed", nil)
	}
	closed := func() bool {
		select {
		case <-c.ctx.Done():
			return true
		case <-c.connLost:
			return true
		default:
			return false
		}
	}
	wrap := func(err error) error {
		if roundCtx.Err() != nil || closed() {
			return aborted()
		}
		var mcpErr *MCPClientError
		var timeoutErr *TimeoutError
		if errors.As(err, &mcpErr) || errors.As(err, &timeoutErr) {
			return err
		}
		return &MCPClientError{Message: "Failed to process server response", Cause: err}
	}

	var raw json.RawMessage
	if err := c.callOnce(roundCtx, method, *nextParams, &raw, callOnceOptions{allowInputRequired: true, timeoutSet: true}); err != nil {
		return false, wrap(err)
	}
	if !isInputRequiredResult(raw) {
		if err := validateMCPResultShape(method, raw); err != nil {
			return false, err
		}
		if result != nil && raw != nil {
			if err := unmarshalSafeJSON(raw, result); err != nil {
				return false, &MCPClientError{Message: "Failed to parse server response", Cause: err}
			}
		}
		return true, nil
	}

	parsed, err := parseInputRequiredResult(raw)
	if err != nil {
		return false, wrap(err)
	}
	if round == maxInputRequiredRounds {
		return false, NewMCPClientError(0, fmt.Sprintf("Request still requires input after %d rounds", maxInputRequiredRounds), nil)
	}

	responses := map[string]ElicitResult{}
	for _, key := range parsed.keys {
		if roundCtx.Err() != nil || closed() {
			return false, aborted()
		}
		req := parsed.InputRequests[key]
		if mode, ok := inputRequestMode(req.Params); req.Method != "elicitation/create" || (ok && mode != "form") {
			return false, NewMCPClientError(0, fmt.Sprintf("Unsupported input request: %s. Only form elicitation is supported.", req.Method), nil)
		}
		c.elicitationMu.RLock()
		handler := c.elicitationHandler
		c.elicitationMu.RUnlock()
		if handler == nil {
			return false, NewMCPClientError(0, "Server requested input, but no handler is registered. Use onElicitationRequest to handle elicitation.", nil)
		}
		elicitation, err := parseFormElicitation(req.Params)
		if err != nil {
			return false, wrap(err)
		}
		answer, err := c.waitForElicitation(roundCtx, handler, elicitation)
		if err != nil {
			return false, wrap(err)
		}
		// TS ElicitResultSchema.parse(answer): the ZodError message.
		encoded, encErr := json.Marshal(answer)
		if encErr != nil {
			return false, wrap(encErr)
		}
		if message, _ := zodIssues(elicitResultSchema, encoded); message != "" {
			return false, wrap(errors.New(message))
		}
		responses[key] = answer
	}

	if len(responses) == 0 {
		t := time.NewTimer(inputRequiredRetryDelay)
		select {
		case <-t.C:
		case <-roundCtx.Done():
			t.Stop()
			return false, aborted()
		case <-c.ctx.Done():
			t.Stop()
			return false, aborted()
		case <-c.connLost:
			t.Stop()
			return false, aborted()
		}
	}
	if roundCtx.Err() != nil || closed() {
		return false, aborted()
	}
	*nextParams = inputRequiredRetryParams{base: params, inputResponses: responses, requestState: parsed.RequestState}
	return false, nil
}

// waitForElicitation runs the handler and stops waiting when the round is
// cancelled or the client closes (TS waitForAbort).
func (c *MCPClient) waitForElicitation(ctx context.Context, handler func(context.Context, ElicitationRequest) (ElicitResult, error), req ElicitationRequest) (ElicitResult, error) {
	type answer struct {
		result ElicitResult
		err    error
	}
	ch := make(chan answer, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				ch <- answer{err: fmt.Errorf("%v", r)}
			}
		}()
		res, err := handler(ctx, req)
		ch <- answer{res, err}
	}()
	select {
	case a := <-ch:
		return a.result, a.err
	case <-ctx.Done():
		return ElicitResult{}, ctx.Err()
	case <-c.ctx.Done():
		return ElicitResult{}, errClosedClient()
	case <-c.connLost:
		return ElicitResult{}, errClosedClient()
	}
}

// orderedObjectKeys returns the member names of a JSON object in document
// order, keeping only the last occurrence of a duplicate (as JSON.parse does
// for values, though it keeps the first position; duplicates are not
// expected here).
func orderedObjectKeys(raw json.RawMessage) []string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil
	}
	var keys []string
	seen := map[string]bool{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return keys
		}
		key, _ := tok.(string)
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return keys
		}
	}
	return keys
}
