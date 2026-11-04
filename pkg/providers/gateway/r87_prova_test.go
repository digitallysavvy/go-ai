package gateway

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
)

// TS acea2b5ec5 removed xiaomi/mimo-v2.5 and -pro from GatewayModelId: they
// stay as deprecated constants and leave the suggested catalog.
func TestR87provaMimoV25Deprecated(t *testing.T) {
	if GatewayLanguageModelXiaomiMimoV25 != "xiaomi/mimo-v2.5" || GatewayLanguageModelXiaomiMimoV25Pro != "xiaomi/mimo-v2.5-pro" {
		t.Fatal("deprecated constants changed value")
	}
	for _, v := range catalogs()["language"] {
		if v == "xiaomi/mimo-v2.5" || v == "xiaomi/mimo-v2.5-pro" {
			t.Fatalf("%q still in the language catalog", v)
		}
	}
}

// r87provaGenB64 mirrors genB64 in the recorder (scratch scan.mjs, recorded
// with TS provider-utils at 3fab50aa9b).
func r87provaGenB64(n, seed int, mut string) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	b := make([]byte, n)
	for i := range b {
		b[i] = byte((i*131 + seed*17 + (i >> 8)) & 255)
	}
	s := base64.StdEncoding.EncodeToString(b)
	switch mut {
	case "bits":
		p := strings.TrimRight(s, "=")
		c := p[len(p)-1]
		s = p[:len(p)-1] + string(alphabet[(strings.IndexByte(alphabet, c)+1)%64]) + s[len(p):]
	case "nopad":
		s = strings.TrimRight(s, "=")
	case "midpad":
		s = s[:101] + "=" + s[102:]
	case "escape":
		s = s[:50] + `\/` + s[51:]
	case "extrapad":
		s += "="
	case "trailchar":
		s += "A"
	}
	return s
}

var r87provaTemplateArg = regexp.MustCompile(`@B\((\d+),(\d+),(\w+)\)`)

func r87provaBuild(tpl string) string {
	return r87provaTemplateArg.ReplaceAllStringFunc(tpl, func(m string) string {
		sub := r87provaTemplateArg.FindStringSubmatch(m)
		n, _ := strconv.Atoi(sub[1])
		seed, _ := strconv.Atoi(sub[2])
		return `"` + r87provaGenB64(n, seed, sub[3]) + `"`
	})
}

var r87provaRef = regexp.MustCompile(`; ref \d+-\d+\]`)

func r87provaSHA(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func r87provaNormalize(v interface{}) interface{} {
	switch x := v.(type) {
	case string:
		return r87provaRef.ReplaceAllString(x, "; ref R]")
	case []interface{}:
		for i := range x {
			x[i] = r87provaNormalize(x[i])
		}
	case map[string]interface{}:
		for k := range x {
			x[k] = r87provaNormalize(x[k])
		}
	}
	return v
}

func r87provaDescribe(v interface{}, binaries map[string][]byte) interface{} {
	switch x := v.(type) {
	case string:
		if b, ok := binaries[x]; ok {
			return map[string]interface{}{"bytes": r87provaSHA(b), "n": float64(len(b))}
		}
		return map[string]interface{}{"string": r87provaSHA([]byte(x)), "n": float64(len(utf16.Encode([]rune(x))))}
	case []interface{}:
		out := make([]interface{}, len(x))
		for i := range x {
			out[i] = r87provaDescribe(x[i], binaries)
		}
		return out
	case map[string]interface{}:
		out := map[string]interface{}{}
		for k, item := range x {
			out[k] = r87provaDescribe(item, binaries)
		}
		return out
	}
	return v
}

// Differential test against TS createJsonResponseHandler with
// experimental_binaryFields ['images[*]'] (89aff56165): 26 bodies (threshold,
// padding, trailing bits, escapes, non-path and escaped keys, BOM, multi-byte
// text, truncation, duplicate keys) x 5 chunk sizes. The skeleton parses to
// the TS rawValue, the replaced value has the same bytes and strings, and a
// body TS rejects has the same skeleton text.
func TestR87provaBinaryFieldScannerMatchesTS(t *testing.T) {
	f, err := os.Open("testdata/r87_binary_field_scanner_ts.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Tpl   string      `json:"tpl"`
		Chunk int         `json:"chunk"`
		Raw   *string     `json:"raw"`
		Value interface{} `json:"value"`
		Error string      `json:"error"`
		Body  string      `json:"body"`
	}
	if err := json.NewDecoder(zr).Decode(&cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 130 {
		t.Fatalf("cases = %d", len(cases))
	}
	paths, err := compileBinaryFieldPaths([]string{"images[*]"})
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range cases {
		body := []byte(r87provaBuild(c.Tpl))
		scanner := newJSONBinaryFieldScanner(paths, binaryFieldMinBytes)
		if c.Chunk == 0 {
			scanner.push(body)
		} else {
			for at := 0; at < len(body); at += c.Chunk {
				scanner.push(body[at:min(at+c.Chunk, len(body))])
			}
		}
		text, binaries := scanner.finish()
		var parsed interface{}
		parseErr := json.Unmarshal([]byte(text), &parsed)
		if c.Error != "" {
			if parseErr == nil {
				t.Errorf("case %d (%.40s, chunk %d): TS failed (%s), Go parsed", i, c.Tpl, c.Chunk, c.Error)
			}
			if got := r87provaSHA([]byte(r87provaRef.ReplaceAllString(text, "; ref R]"))); got != c.Body {
				t.Errorf("case %d (%.40s, chunk %d): error body differs from TS", i, c.Tpl, c.Chunk)
			}
			continue
		}
		if parseErr != nil {
			t.Errorf("case %d (%.40s, chunk %d): %v", i, c.Tpl, c.Chunk, parseErr)
			continue
		}
		var want interface{}
		_ = json.Unmarshal([]byte(*c.Raw), &want)
		if got := r87provaNormalize(r87provaCopy(parsed)); !reflect.DeepEqual(got, want) {
			t.Errorf("case %d (%.40s, chunk %d): raw value differs from TS", i, c.Tpl, c.Chunk)
		}
		wantValue, _ := json.Marshal(c.Value)
		var wantNorm interface{}
		_ = json.Unmarshal(wantValue, &wantNorm)
		if got := r87provaDescribe(parsed, binaries); !reflect.DeepEqual(got, wantNorm) {
			gotJSON, _ := json.Marshal(got)
			t.Errorf("case %d (%.40s, chunk %d): value = %.300s, want %.300s", i, c.Tpl, c.Chunk, gotJSON, wantValue)
		}
	}
}

func r87provaCopy(v interface{}) interface{} {
	b, _ := json.Marshal(v)
	var out interface{}
	_ = json.Unmarshal(b, &out)
	return out
}

// r87provaBytesOf is bytesOf in TS gateway-image-model.test.ts (89aff56165).
func r87provaBytesOf(length int, seed uint32) []byte {
	b := make([]byte, length)
	state := seed
	for i := range b {
		state = state*1103515245 + 12345
		b[i] = byte(state >> 24)
	}
	return b
}

type r87provaImageCall struct {
	body map[string]interface{}
}

func r87provaImageServer(t *testing.T, respond func(w http.ResponseWriter)) (*ImageModel, *r87provaImageCall) {
	t.Helper()
	call := &r87provaImageCall{}
	serverURL, closeServer := newGatewayIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call.body = nil
		_ = json.NewDecoder(r.Body).Decode(&call.body)
		respond(w)
	}))
	t.Cleanup(closeServer)
	p, err := New(Config{APIKey: "k", BaseURL: serverURL})
	if err != nil {
		t.Fatal(err)
	}
	return NewImageModel(p, "openai/gpt-image-1"), call
}

func r87provaJSONResponse(v interface{}) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
}

// Ported from gateway-image-model.test.ts "large images with experimental
// streaming decoding" (TS 89aff56165).
func TestR87provaGatewayStreamingImageDecoding(t *testing.T) {
	large := r87provaBytesOf(70_000, 1)
	largeBase64 := base64.StdEncoding.EncodeToString(large)
	small := r87provaBytesOf(1_000, 2)
	smallBase64 := base64.StdEncoding.EncodeToString(small)
	n := 2
	opts := func(po map[string]interface{}) *provider.ImageGenerateOptions {
		return &provider.ImageGenerateOptions{Prompt: "Test prompt", N: &n, ProviderOptions: po}
	}
	on := func() map[string]interface{} {
		return map[string]interface{}{"gateway": map[string]interface{}{"experimental_streamingImageDecoding": true}}
	}

	t.Run("option unset or false keeps base64 results", func(t *testing.T) {
		for _, po := range []map[string]interface{}{{}, {"gateway": map[string]interface{}{"experimental_streamingImageDecoding": false}}} {
			model, _ := r87provaImageServer(t, r87provaJSONResponse(map[string]interface{}{"images": []string{largeBase64, smallBase64}}))
			result, err := model.DoGenerate(context.Background(), opts(po))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(result.Base64Images, []string{largeBase64, smallBase64}) {
				t.Fatalf("Base64Images = %d entries", len(result.Base64Images))
			}
		}
	})

	t.Run("client option removed, service options kept, caller map unchanged", func(t *testing.T) {
		for _, enabled := range []bool{true, false} {
			model, call := r87provaImageServer(t, r87provaJSONResponse(map[string]interface{}{"images": []string{largeBase64}}))
			gw := map[string]interface{}{"experimental_streamingImageDecoding": enabled, "tags": []interface{}{"images"}}
			po := map[string]interface{}{"gateway": gw, "openai": map[string]interface{}{"quality": "hd"}}
			if _, err := model.DoGenerate(context.Background(), opts(po)); err != nil {
				t.Fatal(err)
			}
			want := map[string]interface{}{"gateway": map[string]interface{}{"tags": []interface{}{"images"}}, "openai": map[string]interface{}{"quality": "hd"}}
			if !reflect.DeepEqual(call.body["providerOptions"], want) {
				t.Fatalf("%v: providerOptions = %#v", enabled, call.body["providerOptions"])
			}
			if gw["experimental_streamingImageDecoding"] != enabled {
				t.Fatalf("caller options modified")
			}
		}
	})

	t.Run("providerOptions omitted when the option is its only entry; empty gateway dropped", func(t *testing.T) {
		for _, enabled := range []bool{true, false} {
			model, call := r87provaImageServer(t, r87provaJSONResponse(map[string]interface{}{"images": []string{smallBase64}}))
			if _, err := model.DoGenerate(context.Background(), opts(map[string]interface{}{"gateway": map[string]interface{}{"experimental_streamingImageDecoding": enabled}})); err != nil {
				t.Fatal(err)
			}
			if _, ok := call.body["providerOptions"]; ok {
				t.Fatalf("%v: providerOptions sent: %#v", enabled, call.body["providerOptions"])
			}
			if _, err := model.DoGenerate(context.Background(), opts(map[string]interface{}{
				"gateway": map[string]interface{}{"experimental_streamingImageDecoding": enabled},
				"openai":  map[string]interface{}{"quality": "hd"},
			})); err != nil {
				t.Fatal(err)
			}
			if want := map[string]interface{}{"openai": map[string]interface{}{"quality": "hd"}}; !reflect.DeepEqual(call.body["providerOptions"], want) {
				t.Fatalf("%v: providerOptions = %#v", enabled, call.body["providerOptions"])
			}
		}
		// The typed GatewayProviderOptions form is stripped too.
		model, call := r87provaImageServer(t, r87provaJSONResponse(map[string]interface{}{"images": []string{smallBase64}}))
		yes := true
		if _, err := model.DoGenerate(context.Background(), opts(GatewayProviderOptions{ExperimentalStreamingImageDecoding: &yes}.ToProviderOptions())); err != nil {
			t.Fatal(err)
		}
		if _, ok := call.body["providerOptions"]; ok {
			t.Fatalf("typed: providerOptions sent: %#v", call.body["providerOptions"])
		}
	})

	t.Run("images of 64 KiB and more are bytes", func(t *testing.T) {
		model, _ := r87provaImageServer(t, r87provaJSONResponse(map[string]interface{}{"images": []string{largeBase64}}))
		result, err := model.DoGenerate(context.Background(), opts(on()))
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Images) != 1 || !bytes.Equal(result.Images[0], large) || len(result.Base64Images) != 0 || result.Base64Image != "" || !bytes.Equal(result.Image, large) {
			t.Fatalf("result = %d images, %d base64", len(result.Images), len(result.Base64Images))
		}
	})

	t.Run("images below 64 KiB stay base64 strings", func(t *testing.T) {
		model, _ := r87provaImageServer(t, r87provaJSONResponse(map[string]interface{}{"images": []string{smallBase64, smallBase64}}))
		result, err := model.DoGenerate(context.Background(), opts(on()))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(result.Base64Images, []string{smallBase64, smallBase64}) {
			t.Fatalf("Base64Images = %v", len(result.Base64Images))
		}
	})

	t.Run("mixed large and small: every image is bytes", func(t *testing.T) {
		model, _ := r87provaImageServer(t, r87provaJSONResponse(map[string]interface{}{"images": []string{smallBase64, largeBase64, smallBase64}}))
		result, err := model.DoGenerate(context.Background(), opts(on()))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(result.Images, [][]byte{small, large, small}) || result.Base64Images != nil {
			t.Fatalf("images mismatch")
		}
	})

	base64URL := strings.NewReplacer("+", "-", "/", "_").Replace(largeBase64)
	t.Run("non-canonical strings stay strings", func(t *testing.T) {
		model, _ := r87provaImageServer(t, r87provaJSONResponse(map[string]interface{}{"images": []string{base64URL, smallBase64}}))
		result, err := model.DoGenerate(context.Background(), opts(on()))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(result.Base64Images, []string{base64URL, smallBase64}) {
			t.Fatalf("Base64Images mismatch")
		}
	})

	t.Run("non-canonical large strings decoded like the regular path when mixed", func(t *testing.T) {
		model, _ := r87provaImageServer(t, r87provaJSONResponse(map[string]interface{}{"images": []string{base64URL, largeBase64}}))
		result, err := model.DoGenerate(context.Background(), opts(on()))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(result.Images, [][]byte{large, large}) || result.Base64Images != nil {
			t.Fatalf("images mismatch")
		}
	})

	t.Run("other response fields unchanged", func(t *testing.T) {
		model, _ := r87provaImageServer(t, r87provaJSONResponse(map[string]interface{}{
			"images":           []string{largeBase64},
			"warnings":         []interface{}{map[string]interface{}{"type": "other", "message": "careful"}},
			"providerMetadata": map[string]interface{}{"gateway": map[string]interface{}{"images": []interface{}{map[string]interface{}{}}, "cost": "0.04"}},
			"usage":            map[string]interface{}{"inputTokens": 1, "outputTokens": 2, "totalTokens": 3},
		}))
		result, err := model.DoGenerate(context.Background(), opts(on()))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(result.Images[0], large) || len(result.Warnings) != 1 || result.Warnings[0].Message != "careful" ||
			!reflect.DeepEqual(result.ProviderMetadata, map[string]interface{}{"gateway": map[string]interface{}{"images": []interface{}{map[string]interface{}{}}, "cost": "0.04"}}) ||
			result.Usage.InputTokens != 1 || result.Usage.OutputTokens != 2 || result.Usage.TotalTokens != 3 {
			t.Fatalf("result = %#v", result.Warnings)
		}
	})

	t.Run("body in small chunks", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{"images": []string{largeBase64, smallBase64}})
		model, _ := r87provaImageServer(t, func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/json")
			flusher, _ := w.(http.Flusher)
			for at := 0; at < len(body); at += 1999 {
				_, _ = w.Write(body[at:min(at+1999, len(body))])
				if flusher != nil {
					flusher.Flush()
				}
			}
		})
		result, err := model.DoGenerate(context.Background(), opts(on()))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(result.Images, [][]byte{large, small}) {
			t.Fatalf("images mismatch")
		}
	})

	t.Run("images that are not strings fail without the image in the error", func(t *testing.T) {
		model, _ := r87provaImageServer(t, r87provaJSONResponse(map[string]interface{}{"images": []interface{}{largeBase64, 5}}))
		_, err := model.DoGenerate(context.Background(), opts(on()))
		if err == nil {
			t.Fatal("expected an error")
		}
		var responseErr *gatewayerrors.GatewayResponseError
		if !errors.As(err, &responseErr) {
			t.Fatalf("error = %T %v", err, err)
		}
		if len(err.Error()) >= 2000 {
			t.Fatalf("error message carries the image: %d bytes", len(err.Error()))
		}
	})
}
