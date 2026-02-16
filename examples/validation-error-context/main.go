package main

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Example demonstrating the enhanced ValidationError with context
func main() {
	fmt.Println("=== Enhanced ValidationError Examples ===")

	// Example 1: Validation error with full context (field path, entity name, and ID)
	fmt.Println("Example 1: Full context with field path and entity information")
	err1 := errors.NewValidationErrorWithContext(
		map[string]string{"foo": "bar"},
		"Invalid type",
		nil,
		&errors.ValidationContext{
			Field:      "message.parts[3].data",
			EntityName: "tool_call",
			EntityID:   "call_abc123",
		},
	)
	fmt.Printf("%v\n\n", err1)

	// Example 2: Validation error with only field path
	fmt.Println("Example 2: Context with only field path")
	err2 := errors.NewValidationErrorWithContext(
		"invalid@",
		"Invalid email format",
		nil,
		&errors.ValidationContext{
			Field: "user.email",
		},
	)
	fmt.Printf("%v\n\n", err2)

	// Example 3: Validation error with only entity information
	fmt.Println("Example 3: Context with only entity information")
	err3 := errors.NewValidationErrorWithContext(
		nil,
		"Missing required field",
		nil,
		&errors.ValidationContext{
			EntityName: "message",
			EntityID:   "msg_123",
		},
	)
	fmt.Printf("%v\n\n", err3)

	// Example 4: Wrapping an error with context
	fmt.Println("Example 4: Wrapping an error with context")
	baseErr := errors.NewValidationError("data", "schema mismatch", nil)
	wrappedErr := errors.WrapValidationError(
		map[string]interface{}{"invalid": true},
		baseErr,
		&errors.ValidationContext{
			Field:      "data.items[0].name",
			EntityName: "item",
			EntityID:   "item_456",
		},
	)
	fmt.Printf("%v\n\n", wrappedErr)

	// Example 5: Backward compatibility - old style still works
	fmt.Println("Example 5: Backward compatible old-style ValidationError")
	err5 := errors.NewValidationError("temperature", "must be between 0 and 2", nil)
	fmt.Printf("%v\n\n", err5)

	// Example 6: Complex nested field path
	fmt.Println("Example 6: Complex nested field path")
	err6 := errors.NewValidationErrorWithContext(
		map[string]interface{}{"endpoint": "invalid://"},
		"Invalid URL scheme",
		nil,
		&errors.ValidationContext{
			Field:      "config.api.endpoints[2].url",
			EntityName: "api_config",
			EntityID:   "cfg_001",
		},
	)
	fmt.Printf("%v\n\n", err6)
}
