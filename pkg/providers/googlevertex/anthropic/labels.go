package anthropic

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// LabelsHeader is the HTTP header Vertex AI reads request labels from.
const LabelsHeader = "x-vertex-ai-labels"

// transformRequestHeaders forwards providerOptions.vertex.labels as the
// base64-encoded JSON x-vertex-ai-labels header (TS #23348). An explicit
// request header of the same name (any casing, even empty) wins. Invalid
// labels return TS parseProviderOptions' InvalidArgumentError before any
// request is sent. Note that Go map keys are
// marshaled in sorted order, unlike TS which keeps insertion order.
func transformRequestHeaders(headers map[string]string, providerOptions map[string]interface{}) (map[string]string, error) {
	// TS parseProviderOptions({provider: 'vertex', schema:
	// googleVertexAnthropicProviderOptions}): labels is
	// z.record(z.string(), z.string()).optional(); a mismatch is the
	// InvalidArgumentError "invalid vertex provider options".
	vertexOptions, err := providerutils.ParseProviderOptions("vertex", providerOptions, providerutils.OptionSchema{
		"labels": providerutils.OptRecord(nil, providerutils.OptString),
	})
	if err != nil {
		return nil, err
	}
	rawLabels, present := vertexOptions["labels"]
	if !present {
		return headers, nil
	}
	labels := parseLabels(rawLabels)
	for name := range headers {
		if strings.EqualFold(name, LabelsHeader) {
			return headers, nil
		}
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // match JSON.stringify
	if err := enc.Encode(labels); err != nil {
		return nil, err
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
	labelled := transformLabelsHeaderWith(static)
	return func(headers map[string]string, providerOptions map[string]interface{}) (map[string]string, error) {
		out, err := labelled(headers, providerOptions)
		if err != nil {
			return nil, err
		}
		return markCallDefaults(headers, out), nil
	}
}

// noCallUserAgentHeader and noCallContentTypeHeader mark a request whose
// call headers set no user agent / content type, so the auth transport can
// let a provider header (TS options.headers, combined before the call
// headers) take that place: TS sends the provider user agent followed by
// the runtime tag, and the provider content type over postJsonToApi's
// default. The transport removes both before sending.
const (
	noCallUserAgentHeader   = "X-Go-Ai-Vertex-No-Call-User-Agent"
	noCallContentTypeHeader = "X-Go-Ai-Vertex-No-Call-Content-Type"
)

// markCallDefaults adds the no-call markers to out for the names the call
// headers do not set (any spelling).
func markCallDefaults(headers, out map[string]string) map[string]string {
	hasUA, hasCT := false, false
	for name := range headers {
		hasUA = hasUA || strings.EqualFold(name, "user-agent")
		hasCT = hasCT || strings.EqualFold(name, "content-type")
	}
	if hasUA && hasCT {
		return out
	}
	marked := make(map[string]string, len(out)+2)
	for k, v := range out {
		marked[k] = v
	}
	if !hasUA {
		marked[noCallUserAgentHeader] = "1"
	}
	if !hasCT {
		marked[noCallContentTypeHeader] = "1"
	}
	return marked
}

// transformLabelsHeaderWith adds the labels header of transformRequestHeaders
// unless a call or static provider header already carries one.
func transformLabelsHeaderWith(static map[string]string) func(map[string]string, map[string]interface{}) (map[string]string, error) {
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

// parseLabels reads the validated labels record.
func parseLabels(raw interface{}) map[string]string {
	switch v := raw.(type) {
	case map[string]string:
		return v
	case map[string]interface{}:
		out := make(map[string]string, len(v))
		for k, val := range v {
			out[k], _ = val.(string)
		}
		return out
	}
	out := map[string]string{}
	if encoded, err := json.Marshal(raw); err == nil {
		_ = json.Unmarshal(encoded, &out)
	}
	return out
}
