package xai

import (
	"errors"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r74 provb open item 2: an array that has a non-string element and exceeds
// its .max(n) reports both issues, element issue first, as zod v4 (issue
// lists produced by zod 4.4.3 for the same args).
func TestR74Integ_XAIToolArgsElementAndMaxItems(t *testing.T) {
	handles := make([]interface{}, 0, 21)
	for i := 0; i < 20; i++ {
		handles = append(handles, "h")
	}
	handles = append(handles, 7.0)
	domains := []interface{}{1.0, "a", "b", "c", "d", "e"}
	for _, tc := range []struct {
		name string
		tool types.Tool
		want string
	}{
		{"x search", types.Tool{Name: "xai.x_search", Type: types.ToolTypeProviderDefined, ProviderID: "xai.x_search", ProviderArgs: map[string]interface{}{"allowedXHandles": handles}}, `[
  {
    "expected": "string",
    "code": "invalid_type",
    "path": [
      "allowedXHandles",
      20
    ],
    "message": "Invalid input: expected string, received number"
  },
  {
    "origin": "array",
    "code": "too_big",
    "maximum": 20,
    "inclusive": true,
    "path": [
      "allowedXHandles"
    ],
    "message": "Too big: expected array to have <=20 items"
  }
]`},
		{"web search", types.Tool{Name: "xai.web_search", Type: types.ToolTypeProviderDefined, ProviderID: "xai.web_search", ProviderArgs: map[string]interface{}{
			"allowedDomains": domains, "enableImageSearch": "x", "excludedDomains": []interface{}{"a", "b", "c", "d", "e", "f"},
		}}, `[
  {
    "expected": "string",
    "code": "invalid_type",
    "path": [
      "allowedDomains",
      0
    ],
    "message": "Invalid input: expected string, received number"
  },
  {
    "origin": "array",
    "code": "too_big",
    "maximum": 5,
    "inclusive": true,
    "path": [
      "allowedDomains"
    ],
    "message": "Too big: expected array to have <=5 items"
  },
  {
    "origin": "array",
    "code": "too_big",
    "maximum": 5,
    "inclusive": true,
    "path": [
      "excludedDomains"
    ],
    "message": "Too big: expected array to have <=5 items"
  },
  {
    "expected": "boolean",
    "code": "invalid_type",
    "path": [
      "enableImageSearch"
    ],
    "message": "Invalid input: expected boolean, received string"
  }
]`},
	} {
		err := validateXAIResponsesToolArgs([]types.Tool{tc.tool})
		var tv *providererrors.TypeValidationError
		if !errors.As(err, &tv) {
			t.Fatalf("%s: err = %v, want a TypeValidationError", tc.name, err)
		}
		if msg := err.Error(); !strings.HasSuffix(msg, ".\nError message: "+tc.want) {
			t.Errorf("%s: err =\n%s\nwant issues\n%s", tc.name, msg, tc.want)
		}
	}
}
