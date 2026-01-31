package mcp

import (
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
	if msg.Params == nil || len(msg.Params) == 0 {
		return nil
	}

	return json.Unmarshal(msg.Params, target)
}

// ParseResult parses the result from a message into the target type
func ParseResult(msg *MCPMessage, target interface{}) error {
	if msg.Result == nil || len(msg.Result) == 0 {
		return nil
	}

	return json.Unmarshal(msg.Result, target)
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
