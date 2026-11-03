package errors

import (
	"encoding/json"
	stderrors "errors"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
)

// r83provaGatewayErrorToTS renders a Go gateway error with the TS error
// fields the create-gateway-error tests read.
func r83provaGatewayErrorToTS(err error) map[string]interface{} {
	out := map[string]interface{}{"message": err.Error()}
	t := reflect.TypeOf(err)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	out["name"] = t.Name()
	var ge GatewayError
	if stderrors.As(err, &ge) {
		out["statusCode"] = ge.GetStatusCode()
		out["type"] = ge.GetType()
		out["isRetryable"] = ge.IsRetryable()
		if id := ge.GetGenerationID(); id != "" {
			out["generationId"] = id
		}
	}
	if cause := stderrors.Unwrap(err); cause != nil {
		out["cause"] = map[string]interface{}{} // a TS Error serializes as {}
	}
	switch e := err.(type) {
	case *GatewayModelNotFoundError:
		if e.ModelID != "" {
			out["modelId"] = e.ModelID
		}
	case *GatewayForbiddenError:
		if e.RuleID != "" {
			out["ruleId"] = e.RuleID
		}
	case *GatewayResponseError:
		out["response"] = e.Response
		if e.ValidationError != nil {
			out["validationError"] = e.ValidationError.Error()
		}
	}
	return out
}

// TestTSCasesCreateGatewayError replays errors/create-gateway-error.test.ts
// (createGatewayErrorFromResponse).
func TestTSCasesCreateGatewayError(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/create_gateway_error.json.gz")
	call := func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		body, err := json.Marshal(in["response"])
		if err != nil {
			return nil, nil, err
		}
		status := 0
		if n, ok := in["statusCode"].(float64); ok {
			status = int(n)
		}
		defaultMessage, ok := in["defaultMessage"].(string)
		if !ok {
			defaultMessage = "Gateway request failed" // TS default
		}
		var cause error
		if c, present := in["cause"]; present {
			if m, _ := c.(map[string]interface{}); m["__undef"] != true {
				cause = stderrors.New("cause")
			}
		}
		authMethod, _ := in["authMethod"].(string)
		got := r83provaGatewayErrorToTS(CreateGatewayErrorFromResponse(body, status, defaultMessage, cause, authMethod))
		// const responseError = error as GatewayResponseError
		return got, map[string]interface{}{"responseError": got}, nil
	}
	tsfixture.RunFn(t, cases, call, nil)
	// TS toBe(object) is reference equality, which the harness leaves out;
	// compare the values here (the response the error keeps, the cause).
	checked := 0
	for _, c := range cases {
		for _, a := range c.Asserts {
			if a.Matcher != "toBe" {
				continue
			}
			switch a.Arg.(type) {
			case map[string]interface{}, []interface{}:
			default:
				continue
			}
			got, _, _ := call(c.FnArgs)
			var field string
			switch a.Expr {
			case "responseError.response":
				field = "response"
			case "error.cause":
				field = "cause"
			default:
				t.Errorf("%s: unhandled object toBe %s", c.Test, a.Expr)
				continue
			}
			checked++
			if d := tsfixture.DiffValues(a.Arg, got.(map[string]interface{})[field]); d != "" {
				t.Errorf("%s: %s: %s", c.Test, a.Expr, d)
			}
		}
	}
	if checked != 7 {
		t.Errorf("object toBe assertions = %d, want 7", checked)
	}
}
