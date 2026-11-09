package baseten

import (
	"errors"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// basetenErrorSchema is TS basetenErrorSchema with errorToMessage:
// {error: string | {message: string, object?: string | null, type?: string
// | null, param?: any, code?: string | number | null}}. data is the zod
// output (undeclared keys stripped).
func basetenErrorSchema(body interface{}) (string, interface{}, bool) {
	obj, ok := body.(map[string]interface{})
	if !ok {
		return "", nil, false
	}
	switch e := obj["error"].(type) {
	case string:
		return e, map[string]interface{}{"error": e}, true
	case map[string]interface{}:
		message, ok := e["message"].(string)
		if !ok {
			return "", nil, false
		}
		for _, key := range []string{"object", "type"} {
			if v, present := e[key]; present && v != nil {
				if _, isString := v.(string); !isString {
					return "", nil, false
				}
			}
		}
		if v, present := e["code"]; present && v != nil {
			switch v.(type) {
			case string, float64:
			default:
				return "", nil, false
			}
		}
		return message, map[string]interface{}{"error": providerutils.PickSchemaKeys(e, "message", "object", "type", "param", "code")}, true
	}
	return "", nil, false
}

// basetenErrorStructure is the openai.Config.CompatErrorStructure of the
// Baseten models (TS basetenErrorStructure).
func basetenErrorStructure(provider string, err error) (*providererrors.ProviderError, bool) {
	var hs providererrors.HTTPStatusDetails
	if !errors.As(err, &hs) {
		return nil, false
	}
	return providererrors.NewSchemaProviderError(provider, err, basetenErrorSchema), true
}
