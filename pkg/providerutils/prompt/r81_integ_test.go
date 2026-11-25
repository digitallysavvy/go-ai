package prompt

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// cr-r81-prova-to-integrator item 1: TS convert-to-google-messages.ts sends
// data: convertToBase64(part.data.data), which returns a string unchanged,
// and detectMediaType decodes it with convertBase64ToUint8Array for a bare
// type. Go re-encoded a string StdEncoding accepted (rewriting a newline)
// and could not sniff a base64url or unpadded string.
func TestR81integGoogleDataStringForwarded(t *testing.T) {
	for _, tc := range []struct {
		name, mediaType, data, wantData, wantMime string
	}{
		{"newline kept, bare type sniffed", "image", "iVBO\nRw==", "iVBO\nRw==", "image/png"},
		{"base64url sent as is", "image/png", "-_8", "-_8", "image/png"},
		{"unpadded sniffed", "image/*", "iVBORw0KGgo", "iVBORw0KGgo", "image/png"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msgs := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
				types.FileContent{MediaType: tc.mediaType, FileData: types.FileData{Type: types.FileDataTypeData, DataString: tc.data}},
			}}}
			got, err := ConvertToGoogleMessages(msgs, GoogleMessagesOptions{})
			if err != nil {
				t.Fatal(err)
			}
			b, _ := json.Marshal(got)
			wantData, _ := json.Marshal(tc.wantData)
			if !strings.Contains(string(b), `"data":`+string(wantData)) || !strings.Contains(string(b), `"mimeType":"`+tc.wantMime+`"`) {
				t.Fatalf("prompt = %s, want data %s and mimeType %s", b, wantData, tc.wantMime)
			}
		})
	}
}
