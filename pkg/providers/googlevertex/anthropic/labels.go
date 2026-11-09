package anthropic

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// LabelsHeader is the HTTP header Vertex AI reads request labels from.
const LabelsHeader = "x-vertex-ai-labels"

// transformRequestHeaders forwards providerOptions.vertex.labels as the
// base64-encoded JSON x-vertex-ai-labels header (TS #23348). An explicit
// request header of the same name (any casing, even empty) wins. Invalid labels return an
// InvalidArgumentError before any request is sent. Note that Go map keys are
// marshaled in sorted order, unlike TS which keeps insertion order.
func transformRequestHeaders(headers map[string]string, providerOptions map[string]interface{}) (map[string]string, error) {
	raw, ok := providerOptions["vertex"]
	if !ok || raw == nil {
		return headers, nil
	}
	vertexOptions, ok := raw.(map[string]interface{})
	if !ok {
		return nil, invalidLabels("providerOptions.vertex must be an object")
	}
	rawLabels, present := vertexOptions["labels"]
	if !present {
		return headers, nil
	}
	labels, err := parseLabels(rawLabels)
	if err != nil {
		return nil, err
	}
	for name := range headers {
		if strings.EqualFold(name, LabelsHeader) {
			return headers, nil
		}
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // match JSON.stringify
	if err := enc.Encode(labels); err != nil {
		return nil, invalidLabels(err.Error())
	}
	encoded := base64.StdEncoding.EncodeToString(bytes.TrimRight(buf.Bytes(), "\n"))

	out := make(map[string]string, len(headers)+1)
	for k, v := range headers {
		out[k] = v
	}
	out[LabelsHeader] = encoded
	return out, nil
}

// generatedLabelsHeader marks a labels header generated from
// providerOptions.vertex.labels, so the auth transport can let a labels
// header from HeadersResolver (a provider header in TS) replace it while a
// call header keeps winning. The transport removes it before sending.
const generatedLabelsHeader = "X-Go-Ai-Generated-Vertex-Labels"

// transformRequestHeadersWith is transformRequestHeaders with the provider's
// static headers: TS transformRequestHeaders sees the provider headers
// combined with the call headers, so a configured labels header also keeps
// the generated one out (the labels are still validated first).
func transformRequestHeadersWith(static map[string]string) func(map[string]string, map[string]interface{}) (map[string]string, error) {
	return func(headers map[string]string, providerOptions map[string]interface{}) (map[string]string, error) {
		out, err := transformRequestHeaders(headers, providerOptions)
		if err != nil {
			return nil, err
		}
		_, generated := out[LabelsHeader]
		for name := range headers {
			if strings.EqualFold(name, LabelsHeader) {
				generated = false
			}
		}
		if !generated {
			return out, nil
		}
		for name := range static {
			if strings.EqualFold(name, LabelsHeader) {
				return headers, nil
			}
		}
		out[generatedLabelsHeader] = "1"
		return out, nil
	}
}

func parseLabels(raw interface{}) (map[string]string, error) {
	switch v := raw.(type) {
	case map[string]string:
		return v, nil
	case map[string]interface{}:
		out := make(map[string]string, len(v))
		for k, val := range v {
			s, ok := val.(string)
			if !ok {
				return nil, invalidLabels("labels values must be strings")
			}
			out[k] = s
		}
		return out, nil
	default:
		return nil, invalidLabels("labels must be an object of string values")
	}
}

func invalidLabels(msg string) error {
	return providererrors.NewArgumentError("providerOptions.vertex.labels", "Invalid Google Vertex Anthropic provider options: "+msg, nil)
}
