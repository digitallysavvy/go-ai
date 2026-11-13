package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r92provcSpacedBase64 is "hi" in base64 with ASCII whitespace and no
// padding: TS convertBase64ToUint8Array (atob's forgiving decode) accepts it.
const r92provcSpacedBase64 = "aG\n k"

// TestR92provcForgivingBase64 (r92 provc-04): transcribe's string audio
// (TS convertDataContentToUint8Array), video prompt images and references
// and base64 generated videos (TS convertBase64ToUint8Array) decode with the
// forgiving rules of atob.
func TestR92provcForgivingBase64(t *testing.T) {
	var got []byte
	model := &testutil.MockTranscriptionModel{DoTranscribeFunc: func(_ context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
		got = opts.Audio
		return &types.TranscriptionResult{Text: "ok"}, nil
	}}
	zero := 0
	if _, err := Transcribe(context.Background(), TranscribeOptions{Model: model, AudioBase64: r92provcSpacedBase64, MaxRetries: &zero}); err != nil || string(got) != "hi" {
		t.Fatalf("transcribe audio = %q err=%v", got, err)
	}

	if data, _, err := decodeVideoPromptImageDataString(r92provcSpacedBase64); err != nil || string(data) != "hi" {
		t.Fatalf("prompt image = %q err=%v", data, err)
	}
	if data, _, err := decodeVideoReferenceDataString("data:image/png;base64,"+r92provcSpacedBase64, ""); err != nil || string(data) != "hi" {
		t.Fatalf("reference = %q err=%v", data, err)
	}
	file, err := convertVideoData(context.Background(), provider.VideoModelV3VideoData{Type: "base64", Data: r92provcSpacedBase64, MediaType: "video/mp4"}, nil, nil)
	if err != nil || string(file.Data) != "hi" {
		t.Fatalf("generated video = %+v err=%v", file, err)
	}
}
