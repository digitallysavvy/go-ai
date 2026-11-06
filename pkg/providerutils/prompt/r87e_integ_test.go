package prompt

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS googleVideoMetadataSchema.parse throws zod's message; fps 0 fails gt(0)
// with inclusive false (node run of zod/v4, TS 3fab50aa9b;
// cr-r87e-prova-to-provb CR 1 follow-up).
func TestR87eintegVideoMetadataFPSExclusiveBound(t *testing.T) {
	f := types.FileContent{MediaType: "video/mp4", Data: []byte{1}}
	f.ProviderOptions = map[string]interface{}{"google": map[string]interface{}{"videoMetadata": map[string]interface{}{"fps": 0.0}}}
	_, err := ConvertToGoogleMessages([]types.Message{{Role: types.RoleUser, Content: []types.ContentPart{f}}}, GoogleMessagesOptions{})
	want := "[\n  {\n    \"origin\": \"number\",\n    \"code\": \"too_small\",\n    \"minimum\": 0,\n    \"inclusive\": false,\n    \"path\": [\n      \"fps\"\n    ],\n    \"message\": \"Too small: expected number to be >0\"\n  }\n]"
	if err == nil || !strings.HasSuffix(err.Error(), "Error message: "+want) {
		t.Fatalf("err = %v", err)
	}
}
