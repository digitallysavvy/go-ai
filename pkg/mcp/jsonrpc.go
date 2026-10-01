package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
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
// message shaped as a request, notification, response, or error object,
// mirroring TS validateJSONRPCMessage / JSONRPCMessageSchema
// (json-rpc-message.ts, hash 3c30eb4). The id, when present, must be a
// string or an integer (matching z.union([z.string(), z.number().int()])).
func ValidateJSONRPCMessage(raw []byte) (*MCPMessage, error) {
	var generic struct {
		JSONRpc string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, fmt.Errorf("invalid JSON-RPC message: %w", err)
	}
	if generic.JSONRpc != "2.0" {
		return nil, fmt.Errorf(`invalid JSON-RPC message: jsonrpc must be "2.0"`)
	}

	hasID := len(generic.ID) > 0 && string(generic.ID) != "null"
	var id interface{}
	if hasID {
		var decoded interface{}
		dec := json.NewDecoder(bytes.NewReader(generic.ID))
		dec.UseNumber()
		if err := dec.Decode(&decoded); err != nil {
			return nil, fmt.Errorf("invalid JSON-RPC message: invalid id")
		}
		switch v := decoded.(type) {
		case string:
			id = v
		case json.Number:
			n, err := v.Int64()
			if err != nil {
				return nil, fmt.Errorf("invalid JSON-RPC message: id must be a string or integer")
			}
			id = n
		default:
			return nil, fmt.Errorf("invalid JSON-RPC message: id must be a string or integer")
		}
	}

	hasMethod := generic.Method != ""
	hasResult := len(generic.Result) > 0 && string(generic.Result) != "null"
	hasError := len(generic.Error) > 0 && string(generic.Error) != "null"

	switch {
	case hasMethod && hasID:
		return &MCPMessage{JSONRpc: "2.0", ID: id, Method: generic.Method, Params: generic.Params}, nil
	case hasMethod && !hasID:
		return &MCPMessage{JSONRpc: "2.0", Method: generic.Method, Params: generic.Params}, nil
	case hasError:
		var errObj MCPError
		if err := json.Unmarshal(generic.Error, &errObj); err != nil {
			return nil, fmt.Errorf("invalid JSON-RPC message: invalid error object: %w", err)
		}
		if errObj.Message == "" {
			return nil, fmt.Errorf("invalid JSON-RPC message: error.message is required")
		}
		msg := &MCPMessage{JSONRpc: "2.0", Error: &errObj}
		if hasID {
			msg.ID = id
		}
		return msg, nil
	case hasResult && hasID:
		return &MCPMessage{JSONRpc: "2.0", ID: id, Result: generic.Result}, nil
	default:
		return nil, fmt.Errorf("invalid JSON-RPC message: does not match request, notification, response, or error shape")
	}
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
