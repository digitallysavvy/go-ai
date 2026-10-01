package ai

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"testing"
)

// crossLanguageApprovalFixtures were computed with the TypeScript SDK
// algorithm (packages/ai/src/util/canonical-hash.ts and
// generate-text/tool-approval-signature.ts at ai@7.0.113, executed verbatim
// under Node 24 WebCrypto) using secret "test-secret-key-for-hmac-signing".
// Inputs are JSON so Go decodes them into the same value model as JS.
var crossLanguageApprovalFixtures = []struct {
	name       string
	legacy     bool
	approvalID string
	toolCallID string
	toolName   string
	inputJSON  string
	canonical  string
	digest     string
	signature  string
}{
	{
		name:       "basic",
		approvalID: "approval-1",
		toolCallID: "call-1",
		toolName:   "deleteFile",
		inputJSON:  `{"path":"/tmp/cache"}`,
		canonical:  `{"path":"/tmp/cache"}`,
		digest:     "z6DfQ9QDa112neoYXpvHbdMcFAzyM-8SXyI_2NogPUA",
		signature:  "KhhdqErvPwq9PCD-YYlVKgPDJ_tby36fpqptjJpmYhE",
	},
	{
		// JS JSON.stringify does not escape HTML characters or U+2028/U+2029;
		// Go's encoding/json does by default. Byte-equality is required.
		name:       "unicode-separators-and-html",
		approvalID: "a<b>& ",
		toolCallID: "call -1",
		toolName:   "tool\"\\",
		inputJSON:  `{"html":"<script>&amp;</script>","ls":"  ","ctl":"\u0001\b\f\n\r\t\u001f\u007f","emoji":"😀é","z":1,"a":[1.5,-0,1e21,1e-7,0.1,123456789012,true,null,{"b":2,"a":"x"}]}`,
		canonical:  "{\"a\":[1.5,0,1e+21,1e-7,0.1,123456789012,true,null,{\"a\":\"x\",\"b\":2}],\"ctl\":\"\\u0001\\b\\f\\n\\r\\t\\u001f\u007f\",\"emoji\":\"😀é\",\"html\":\"<script>&amp;</script>\",\"ls\":\"  \",\"z\":1}",
		digest:     "aSK54wsIy5t-OtZDz9KAOg47Qv4rblTgrDWwABtha2E",
		signature:  "PmuQHaQO9W7DYBczRUFKB5qHALnvIGRcp17p6_bbFpI",
	},
	{
		name:       "newline-in-toolname",
		approvalID: "approval-1",
		toolCallID: "call-1",
		toolName:   "searchDocs\ndeleteFile",
		inputJSON:  `{"path":"/tmp/target"}`,
		canonical:  `{"path":"/tmp/target"}`,
		digest:     "niy2F8anppLBE90LymGBpNCQna4pfgUfLj_vX-rtd3c",
		signature:  "S2bb3Xdoe27mJHciGfC8t0dRE7imDW8G_LAph_Ehh-Q",
	},
	{
		// JS sorts keys by UTF-16 code units: U+1F600 (surrogate D83D) sorts
		// before U+FFFF, the opposite of UTF-8 byte order.
		name:       "key-order-utf16",
		approvalID: "id",
		toolCallID: "c",
		toolName:   "t",
		inputJSON:  `{"￿":1,"😀":2,"b":3,"B":4}`,
		canonical:  "{\"B\":4,\"b\":3,\"😀\":2,\"￿\":1}",
		digest:     "wFqFE4LUoaLR7k5ZXsKcgjs1P1qTxHP_SsVctomv-l4",
		signature:  "C0Aq6ekrtrGDxUx2xXzQJtCB_asAGWMEDuwXnR_izcg",
	},
	{
		name:       "legacy-basic",
		legacy:     true,
		approvalID: "approval-1",
		toolCallID: "call-1",
		toolName:   "deleteFile",
		inputJSON:  `{"path":"/tmp/cache"}`,
		canonical:  `{"path":"/tmp/cache"}`,
		digest:     "z6DfQ9QDa112neoYXpvHbdMcFAzyM-8SXyI_2NogPUA",
		signature:  "m_3hnkZy4Mx-__NmCA7J5NTNUwAk3bhbUtXD7PDAF_k",
	},
}

func TestToolApprovalSignatureCrossLanguageFixtures(t *testing.T) {
	secret := []byte("test-secret-key-for-hmac-signing")
	for _, fx := range crossLanguageApprovalFixtures {
		t.Run(fx.name, func(t *testing.T) {
			var input interface{}
			if err := json.Unmarshal([]byte(fx.inputJSON), &input); err != nil {
				t.Fatalf("decode input: %v", err)
			}
			canonical, err := canonicalJSON(input)
			if err != nil {
				t.Fatalf("canonicalJSON: %v", err)
			}
			if string(canonical) != fx.canonical {
				t.Fatalf("canonicalJSON = %q, want %q", canonical, fx.canonical)
			}
			digest, err := hashCanonical(input)
			if err != nil || digest != fx.digest {
				t.Fatalf("hashCanonical = %q (%v), want %q", digest, err, fx.digest)
			}

			// TS-generated signature verifies in Go.
			valid, err := VerifyToolApprovalSignature(secret, fx.signature, fx.approvalID, fx.toolCallID, fx.toolName, input)
			if err != nil || !valid {
				t.Fatalf("VerifyToolApprovalSignature(TS signature) = %v, %v", valid, err)
			}

			if fx.legacy {
				return
			}
			// Go-generated signature is byte-identical to TS.
			signature, err := SignToolApproval(secret, fx.approvalID, fx.toolCallID, fx.toolName, input)
			if err != nil {
				t.Fatalf("SignToolApproval: %v", err)
			}
			if signature != fx.signature {
				t.Fatalf("SignToolApproval = %q, want TS %q", signature, fx.signature)
			}
		})
	}
}

func TestCanonicalJSONNormalizesGoNumericAndStructTypes(t *testing.T) {
	type payload struct {
		Path  string `json:"path"`
		Count int    `json:"count"`
	}
	got, err := canonicalJSON(map[string]interface{}{
		"s": payload{Path: "<x>", Count: 3},
		"i": int64(7),
		"f": float32(1.5),
		// A literal -0.0 constant is indistinguishable from 0.0 in Go (no
		// negative zero for untyped constants); math.Copysign forces an
		// actual IEEE-754 negative zero at runtime, matching what TS's
		// JSON.stringify(-0) === "0" test is really exercising.
		"neg": math.Copysign(0, -1),
		"n":   json.Number("2.50"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"f":1.5,"i":7,"n":2.5,"neg":0,"s":{"count":3,"path":"<x>"}}`
	if string(got) != want {
		t.Fatalf("canonicalJSON = %s, want %s", got, want)
	}
}

// Port of TS "should not collide when a newline in toolName is retupled into toolCallId".
func TestToolApprovalSignatureRejectsNewlineRetupling(t *testing.T) {
	secret := []byte("test-secret-key-for-hmac-signing")
	input := map[string]interface{}{"path": "/tmp/target"}
	signature, err := SignToolApproval(secret, "approval-1", "call-1", "searchDocs\ndeleteFile", input)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := VerifyToolApprovalSignature(secret, signature, "approval-1", "call-1\nsearchDocs", "deleteFile", input)
	if err != nil {
		t.Fatal(err)
	}
	if valid {
		t.Fatal("retupled approval verified")
	}
}

// Port of TS legacy-format tests: a legacy signature is accepted only when no
// field contains the newline delimiter.
func TestToolApprovalSignatureLegacyFallbackGuardedOnNewlines(t *testing.T) {
	secret := []byte("test-secret-key-for-hmac-signing")
	input := map[string]interface{}{"path": "/tmp/target"}
	digest, _ := hashCanonical(input)
	legacySign := func(approvalID, toolCallID, toolName string) string {
		mac := hmacSHA256(secret, []byte(approvalID+"\n"+toolCallID+"\n"+toolName+"\n"+digest))
		return base64.RawURLEncoding.EncodeToString(mac)
	}

	sig := legacySign("approval-1", "call-1", "deleteFile")
	if ok, err := VerifyToolApprovalSignature(secret, sig, "approval-1", "call-1", "deleteFile", input); err != nil || !ok {
		t.Fatalf("legacy signature rejected: %v %v", ok, err)
	}

	// The legacy-format collision: signed for toolName "searchDocs\ndeleteFile"
	// retupled into toolCallId must not verify.
	collided := legacySign("approval-1", "call-1", "searchDocs\ndeleteFile")
	if ok, _ := VerifyToolApprovalSignature(secret, collided, "approval-1", "call-1\nsearchDocs", "deleteFile", input); ok {
		t.Fatal("legacy retupled signature verified")
	}
	if ok, _ := VerifyToolApprovalSignature(secret, collided, "approval-1", "call-1", "searchDocs\ndeleteFile", input); ok {
		t.Fatal("legacy signature with newline field verified")
	}
}

func TestVerifyToolApprovalSignatureAcceptsPaddedAndStandardBase64(t *testing.T) {
	secret := []byte("test-secret-key-for-hmac-signing")
	input := map[string]interface{}{"path": "/tmp/cache"}
	for _, sig := range []string{
		"KhhdqErvPwq9PCD-YYlVKgPDJ_tby36fpqptjJpmYhE",
		"KhhdqErvPwq9PCD-YYlVKgPDJ_tby36fpqptjJpmYhE=",
		"KhhdqErvPwq9PCD+YYlVKgPDJ/tby36fpqptjJpmYhE=",
	} {
		ok, err := VerifyToolApprovalSignature(secret, sig, "approval-1", "call-1", "deleteFile", input)
		if err != nil || !ok {
			t.Fatalf("signature %q: ok=%v err=%v", sig, ok, err)
		}
	}
}
