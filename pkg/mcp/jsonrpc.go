package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"sync/atomic"
)

// JSON-RPC 2.0 implementation for MCP

// IDGenerator generates unique IDs for JSON-RPC requests
type IDGenerator struct {
	counter uint64
}

// NewIDGenerator creates a new ID generator
func NewIDGenerator() *IDGenerator {
	return &IDGenerator{counter: 0}
}

// Next generates the next ID
func (g *IDGenerator) Next() interface{} {
	id := atomic.AddUint64(&g.counter, 1)
	return id
}

// CreateRequest creates a JSON-RPC 2.0 request
func CreateRequest(id interface{}, method string, params interface{}) (*MCPMessage, error) {
	var paramsRaw json.RawMessage
	if params != nil {
		paramBytes, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal params: %w", err)
		}
		paramsRaw = paramBytes
	}

	return &MCPMessage{
		JSONRpc: "2.0",
		ID:      id,
		Method:  method,
		Params:  paramsRaw,
	}, nil
}

// CreateNotification creates a JSON-RPC 2.0 notification (request without ID)
func CreateNotification(method string, params interface{}) (*MCPMessage, error) {
	var paramsRaw json.RawMessage
	if params != nil {
		paramBytes, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal params: %w", err)
		}
		paramsRaw = paramBytes
	}

	return &MCPMessage{
		JSONRpc: "2.0",
		Method:  method,
		Params:  paramsRaw,
	}, nil
}

// CreateResponse creates a JSON-RPC 2.0 response
func CreateResponse(id interface{}, result interface{}) (*MCPMessage, error) {
	var resultRaw json.RawMessage
	if result != nil {
		resultBytes, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal result: %w", err)
		}
		resultRaw = resultBytes
	}

	return &MCPMessage{
		JSONRpc: "2.0",
		ID:      id,
		Result:  resultRaw,
	}, nil
}

// CreateErrorResponse creates a JSON-RPC 2.0 error response
func CreateErrorResponse(id interface{}, code int, message string, data interface{}) *MCPMessage {
	return &MCPMessage{
		JSONRpc: "2.0",
		ID:      id,
		Error: &MCPError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
}

// IsRequest returns true if the message is a request (has method and ID)
func IsRequest(msg *MCPMessage) bool {
	return msg.Method != "" && msg.ID != nil
}

// IsNotification returns true if the message is a notification (has method but no ID)
func IsNotification(msg *MCPMessage) bool {
	return msg.Method != "" && msg.ID == nil
}

// IsResponse returns true if the message is a response (has result or error)
func IsResponse(msg *MCPMessage) bool {
	return (msg.Result != nil || msg.Error != nil) && msg.ID != nil
}

// IsError returns true if the message is an error response
func IsError(msg *MCPMessage) bool {
	return msg.Error != nil
}

// ParseParams parses the params from a message into the target type
func ParseParams(msg *MCPMessage, target interface{}) error {
	if len(msg.Params) == 0 {
		return nil
	}

	return unmarshalSafeJSON(msg.Params, target)
}

// ParseResult parses the result from a message into the target type
func ParseResult(msg *MCPMessage, target interface{}) error {
	if len(msg.Result) == 0 {
		return nil
	}

	return unmarshalSafeJSON(msg.Result, target)
}

// ValidateJSONRPCMessage validates that raw is a well-formed JSON-RPC 2.0
// message, mirroring TS validateJSONRPCMessage / JSONRPCMessageSchema
// (json-rpc-message.ts): a union of four zod `.strict()` object schemas, so
// any key outside the matched shape's key set is rejected (keys are
// case-sensitive).
//
//	request       {jsonrpc, id, method, params?}
//	notification  {jsonrpc, method, params?}
//	response      {jsonrpc, id, result}
//	error         {jsonrpc, id?, error: {code, message, data?}}
//
// jsonrpc must be "2.0"; id is a string or a safe integer (null is
// rejected); method is any string; params and result are objects whose
// optional _meta is an object (and result.resultType, when present, a
// string); error.code is a safe integer and error.message a string.
func ValidateJSONRPCMessage(raw []byte) (*MCPMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("invalid JSON-RPC message: %w", err)
	}
	if obj == nil {
		return nil, fmt.Errorf("invalid JSON-RPC message: expected an object")
	}
	var version string
	if v, ok := obj["jsonrpc"]; !ok || json.Unmarshal(v, &version) != nil || version != "2.0" {
		return nil, fmt.Errorf(`invalid JSON-RPC message: jsonrpc must be "2.0"`)
	}

	_, hasIDKey := obj["id"]
	_, hasMethod := obj["method"]
	_, hasResult := obj["result"]
	_, hasError := obj["error"]
	allow := func(keys ...string) bool {
		for k := range obj {
			ok := false
			for _, allowed := range keys {
				if k == allowed {
					ok = true
					break
				}
			}
			if !ok {
				return false
			}
		}
		return true
	}
	shapeErr := fmt.Errorf("invalid JSON-RPC message: does not match request, notification, response, or error shape")

	var id interface{}
	if hasIDKey {
		var err error
		if id, err = parseJSONRPCID(obj["id"]); err != nil {
			return nil, err
		}
	}

	switch {
	case hasMethod:
		var method string
		if err := json.Unmarshal(obj["method"], &method); err != nil {
			return nil, fmt.Errorf("invalid JSON-RPC message: method must be a string")
		}
		if hasResult || hasError || !allow("jsonrpc", "id", "method", "params") {
			return nil, shapeErr
		}
		if p, ok := obj["params"]; ok {
			if err := checkJSONRPCObject(p, "params"); err != nil {
				return nil, err
			}
		}
		return &MCPMessage{JSONRpc: "2.0", ID: id, Method: method, Params: obj["params"]}, nil
	case hasError:
		if hasResult || !allow("jsonrpc", "id", "error") {
			return nil, shapeErr
		}
		errObj, err := parseJSONRPCErrorObject(obj["error"])
		if err != nil {
			return nil, err
		}
		return &MCPMessage{JSONRpc: "2.0", ID: id, Error: errObj}, nil
	case hasResult:
		if !hasIDKey || !allow("jsonrpc", "id", "result") {
			return nil, shapeErr
		}
		if err := checkJSONRPCObject(obj["result"], "result"); err != nil {
			return nil, err
		}
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(obj["result"], &fields)
		if rt, ok := fields["resultType"]; ok && !isJSONString(rt) {
			return nil, fmt.Errorf("invalid JSON-RPC message: result.resultType must be a string")
		}
		return &MCPMessage{JSONRpc: "2.0", ID: id, Result: obj["result"]}, nil
	default:
		return nil, shapeErr
	}
}

// parseJSONRPCID decodes a string or safe-integer id (TS
// z.union([z.string(), z.number().int()])); null and floats are rejected.
func parseJSONRPCID(raw json.RawMessage) (interface{}, error) {
	var decoded interface{}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("invalid JSON-RPC message: invalid id")
	}
	switch v := decoded.(type) {
	case string:
		return v, nil
	case json.Number:
		n, ok := safeJSONInteger(v)
		if !ok {
			return nil, fmt.Errorf("invalid JSON-RPC message: id must be a string or integer")
		}
		return n, nil
	default:
		return nil, fmt.Errorf("invalid JSON-RPC message: id must be a string or integer")
	}
}

// safeJSONInteger reports whether n is an integer-valued JSON number within
// the JS safe-integer range (JS parses 1.0 and 1e3 as integers).
func safeJSONInteger(n json.Number) (int64, bool) {
	f, err := strconv.ParseFloat(n.String(), 64)
	if err != nil || math.IsInf(f, 0) || math.IsNaN(f) || f != math.Trunc(f) || math.Abs(f) > 9007199254740991 {
		return 0, false
	}
	return int64(f), true
}

// checkJSONRPCObject requires raw to be a JSON object whose optional _meta is
// also an object (BaseParamsSchema: a loose object with _meta: loose object).
func checkJSONRPCObject(raw json.RawMessage, name string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return fmt.Errorf("invalid JSON-RPC message: %s must be an object", name)
	}
	if meta, ok := fields["_meta"]; ok && !isJSONObject(meta) {
		return fmt.Errorf("invalid JSON-RPC message: %s._meta must be an object", name)
	}
	return nil
}

func parseJSONRPCErrorObject(raw json.RawMessage) (*MCPError, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return nil, fmt.Errorf("invalid JSON-RPC message: error must be an object")
	}
	codeRaw, ok := fields["code"]
	if !ok {
		return nil, fmt.Errorf("invalid JSON-RPC message: error.code is required")
	}
	var num json.Number
	dec := json.NewDecoder(bytes.NewReader(codeRaw))
	dec.UseNumber()
	if err := dec.Decode(&num); err != nil {
		return nil, fmt.Errorf("invalid JSON-RPC message: error.code must be an integer")
	}
	code, ok := safeJSONInteger(num)
	if !ok {
		return nil, fmt.Errorf("invalid JSON-RPC message: error.code must be an integer")
	}
	var message string
	if m, ok := fields["message"]; !ok || !isJSONString(m) || json.Unmarshal(m, &message) != nil {
		return nil, fmt.Errorf("invalid JSON-RPC message: error.message must be a string")
	}
	out := &MCPError{Code: int(code), Message: message}
	if d, ok := fields["data"]; ok {
		if err := json.Unmarshal(d, &out.Data); err != nil {
			return nil, fmt.Errorf("invalid JSON-RPC message: invalid error.data: %w", err)
		}
	}
	return out, nil
}

// parseMCPMessage decodes one inbound JSON-RPC message the way TS
// parseJSONRPCMessage does: the text must be safe JSON (no prototype keys, the
// depth and field limits of unmarshalSafeJSON) and must match a JSON-RPC
// request, notification, response or error shape. A transport reports a
// failure through its error callback and keeps reading.
func parseMCPMessage(data []byte) (*MCPMessage, error) {
	var probe interface{}
	if err := unmarshalSafeJSON(data, &probe); err != nil {
		return nil, err
	}
	msg, err := ValidateJSONRPCMessage(data)
	if err != nil {
		return nil, err
	}
	// Transports have always delivered numeric ids as decoded into an
	// interface{} (float64); keep that so Transport consumers see no change.
	if id, ok := msg.ID.(int64); ok {
		msg.ID = float64(id)
	}
	return msg, nil
}

// GetError extracts the error from a message
func GetError(msg *MCPMessage) error {
	if msg.Error == nil {
		return nil
	}

	return &MCPClientError{
		Code:    msg.Error.Code,
		Message: msg.Error.Message,
		Data:    msg.Error.Data,
	}
}
