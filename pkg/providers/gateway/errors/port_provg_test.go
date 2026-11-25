package errors

import (
	stderrors "errors"
	"reflect"
	"testing"
)

// TS create-gateway-error.test.ts "access_policy_denied" (b8c01eb49d).
func TestPortprovgAccessPolicyDenied(t *testing.T) {
	const message = "Access policy denied: model openai/gpt-5 is not allowed by the access policies that apply to this request (policies: ap_1)."
	create := func(body string) error {
		return CreateGatewayErrorFromResponse([]byte(body), 403, "default", nil, "")
	}
	denied := func(t *testing.T, err error) *GatewayAccessPolicyDeniedError {
		t.Helper()
		var target *GatewayAccessPolicyDeniedError
		if !stderrors.As(err, &target) {
			t.Fatalf("error %T is not a GatewayAccessPolicyDeniedError", err)
		}
		var forbidden *GatewayForbiddenError
		if stderrors.As(err, &forbidden) {
			t.Fatal("a policy denial must not be a GatewayForbiddenError")
		}
		return target
	}

	t.Run("creates the error with the policy ids and removed values", func(t *testing.T) {
		err := create(`{"error":{"message":"` + message + `","type":"access_policy_denied","param":{"policyIds":["ap_1","ap_2"],"removedModels":["openai/gpt-5"],"removedProviders":["openai"]}},"generationId":"gen_123"}`)
		got := denied(t, err)
		if got.GetType() != "access_policy_denied" || got.GetStatusCode() != 403 || got.IsRetryable() || got.GetGenerationID() != "gen_123" {
			t.Fatalf("error = %+v", got)
		}
		if got.Error() != message+" [gen_123]" {
			t.Fatalf("Error() = %q", got.Error())
		}
		if !reflect.DeepEqual(got.PolicyIDs, []string{"ap_1", "ap_2"}) || !reflect.DeepEqual(got.RemovedModels, []string{"openai/gpt-5"}) || !reflect.DeepEqual(got.RemovedProviders, []string{"openai"}) {
			t.Fatalf("fields = %v %v %v", got.PolicyIDs, got.RemovedModels, got.RemovedProviders)
		}
		if !IsGatewayError(err) || GatewayErrorDetails(got).GetRawType() != "access_policy_denied" {
			t.Fatalf("not a gateway error with details: %v", err)
		}
	})

	t.Run("ignores extra keys in param", func(t *testing.T) {
		got := denied(t, create(`{"error":{"message":"m","type":"access_policy_denied","param":{"name":"AccessPolicyDeniedError","statusCode":403,"policyIds":["ap_1"],"removedModels":[],"removedProviders":["openai"]}}}`))
		if !reflect.DeepEqual(got.PolicyIDs, []string{"ap_1"}) || got.RemovedModels == nil || len(got.RemovedModels) != 0 || !reflect.DeepEqual(got.RemovedProviders, []string{"openai"}) {
			t.Fatalf("fields = %v %v %v", got.PolicyIDs, got.RemovedModels, got.RemovedProviders)
		}
	})

	t.Run("leaves the arrays nil when the response has no param", func(t *testing.T) {
		got := denied(t, create(`{"error":{"message":"`+message+`","type":"access_policy_denied"}}`))
		if got.GetStatusCode() != 403 || got.Error() != message || got.PolicyIDs != nil || got.RemovedModels != nil || got.RemovedProviders != nil {
			t.Fatalf("error = %+v", got)
		}
	})

	t.Run("keeps the arrays that are present when others are missing", func(t *testing.T) {
		got := denied(t, create(`{"error":{"message":"m","type":"access_policy_denied","param":{"policyIds":["ap_1"]}}}`))
		if !reflect.DeepEqual(got.PolicyIDs, []string{"ap_1"}) || got.RemovedModels != nil || got.RemovedProviders != nil {
			t.Fatalf("fields = %v %v %v", got.PolicyIDs, got.RemovedModels, got.RemovedProviders)
		}
	})

	for name, param := range map[string]string{
		"a string":                               `"model"`,
		"null":                                   `null`,
		"an array field with non-string entries": `{"policyIds":[1,2]}`,
		"a mix of valid and invalid fields":      `{"policyIds":["ap_1"],"removedModels":[1]}`,
	} {
		t.Run("leaves the arrays nil when param is "+name, func(t *testing.T) {
			got := denied(t, create(`{"error":{"message":"m","type":"access_policy_denied","param":`+param+`}}`))
			if got.GetStatusCode() != 403 || got.PolicyIDs != nil || got.RemovedModels != nil || got.RemovedProviders != nil {
				t.Fatalf("error = %+v", got)
			}
		})
	}

	t.Run("defaults", func(t *testing.T) {
		got := NewGatewayAccessPolicyDeniedError("", 0, nil, "", nil, nil, nil)
		if got.Error() != "Access policy denied" || got.GetStatusCode() != 403 {
			t.Fatalf("defaults = %q %d", got.Error(), got.GetStatusCode())
		}
	})
}
