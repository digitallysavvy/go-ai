package zai

import (
	"strings"
	"testing"
)

// TS zaiLanguageModelChatOptions: requestId z.string().min(6).max(64) counts
// UTF-16 code units, so 30 CJK characters (90 bytes) are valid.
func TestR82provbZaiRequestIDLengthInUTF16Units(t *testing.T) {
	id := strings.Repeat("编", 30)
	got, err := decodeZaiOptions(map[string]interface{}{"requestId": id, "userId": strings.Repeat("用", 100)})
	if err != nil {
		t.Fatal(err)
	}
	if got.requestID != id {
		t.Fatalf("requestID = %q", got.requestID)
	}
	if _, err := decodeZaiOptions(map[string]interface{}{"requestId": strings.Repeat("😀", 33)}); err == nil {
		t.Fatal("66 UTF-16 units accepted; max is 64")
	}
}
