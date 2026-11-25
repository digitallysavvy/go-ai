package anthropic

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type r94provaClosingReader struct {
	io.Reader
	closed bool
}

func (r *r94provaClosingReader) Close() error { r.closed = true; return nil }

// TestR94provaFilesStreamUploadUnsupported: TS uploadFile runs
// convertInlineFileDataToUint8Array, which cancels a stream upload and throws
// UnsupportedFunctionalityError('streaming file upload') before any request.
func TestR94provaFilesStreamUploadUnsupported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s", r.URL.Path)
	}))
	defer srv.Close()
	p := New(Config{BaseURL: srv.URL, APIKey: "k"})
	stream := &r94provaClosingReader{Reader: strings.NewReader("data")}
	_, err := p.Files().UploadFile(context.Background(), types.UploadFileOptions{
		Data:      types.FileData{Type: types.FileDataTypeStream, Stream: stream},
		MediaType: "text/plain",
	})
	var unsupported *providererrors.UnsupportedFunctionalityError
	if !errors.As(err, &unsupported) || unsupported.Functionality != "streaming file upload" ||
		err.Error() != "'streaming file upload' functionality not supported." {
		t.Fatalf("err = %T %v", err, err)
	}
	if !stream.closed {
		t.Fatal("stream not released")
	}
}

// TestR94provaSkillEmptyStrings: TS tests the skill's nullish fields with
// `!= null`: a present empty latest_version still fetches the version
// metadata, and the version's empty name / description win over the skill's.
func TestR94provaSkillEmptyStrings(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			_, _ = io.WriteString(w, `{"id":"skill_1","name":"skill-name","description":"skill-desc","latest_version":"","source":"custom","created_at":"c","updated_at":"u"}`)
			return
		}
		_, _ = io.WriteString(w, `{"type":"skill_version","skill_id":"skill_1","name":"","description":""}`)
	}))
	defer srv.Close()
	p := New(Config{BaseURL: srv.URL, APIKey: "k"})
	res, err := p.Skills().(*SkillsAPI).UploadSkill(context.Background(), types.UploadSkillOptions{
		Files: []types.UploadSkillFile{{Path: "s/SKILL.md", Data: types.FileData{Type: types.FileDataTypeText, Text: "x"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 2 || paths[1] != "GET /skills/skill_1/versions/" {
		t.Fatalf("requests = %v", paths)
	}
	if res.Name != "" || res.Description != "" || res.LatestVersion != "" {
		t.Fatalf("result = %+v", res)
	}
}

// TestR94provaMessageStartEmptyStopReason: TS sets the stream-wide finish
// reason when message_start carries a stop_reason that is `!= null`, so an
// empty one replaces the previous message's reason.
func TestR94provaMessageStartEmptyStopReason(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"m1","model":"c","usage":{"input_tokens":1,"output_tokens":0}}}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":1}}`,
		`data: {"type":"message_stop"}`,
		`data: {"type":"message_start","message":{"id":"m2","model":"c","stop_reason":"","usage":{"input_tokens":1,"output_tokens":0}}}`,
		`data: {"type":"message_stop"}`,
	}, "\n\n") + "\n\n"
	s := newAnthropicStream(io.NopCloser(strings.NewReader(sse)), false)
	var finishes []string
	for {
		chunk, err := s.Next()
		if err != nil {
			break
		}
		if chunk.Type == "finish" {
			finishes = append(finishes, string(chunk.FinishReason)+"/"+chunk.RawFinishReason)
		}
	}
	if len(finishes) != 2 || finishes[0] != "stop/end_turn" || finishes[1] != "other/" {
		t.Fatalf("finishes = %v", finishes)
	}
}

// TestR94provaUnfollowed3xxFails (cr-r94-provg-to-prova): TS getFromApi /
// postFormDataToApi / deleteFromApi fail on !response.ok, so an unfollowed
// 300 runs the failed-response handler instead of parsing a success body.
func TestR94provaUnfollowed3xxFails(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ok := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusMultipleChoices)
			_, _ = io.WriteString(w, body)
		}
	}
	fileBody := `{"id":"file_1","type":"file","filename":"a.txt"}`
	mux.HandleFunc("/files", ok(fileBody))
	mux.HandleFunc("/files/file_1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			ok(`{"id":"file_1","type":"file_deleted"}`)(w, r)
			return
		}
		ok(fileBody)(w, r)
	})
	mux.HandleFunc("/skills", ok(`{"id":"skill_1","source":"custom","created_at":"c","updated_at":"u"}`))
	mux.HandleFunc("/messages/batches/b1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"b1","type":"message_batch","processing_status":"ended","request_counts":{"processing":0,"succeeded":1,"errored":0,"canceled":0,"expired":0},"created_at":"2024-01-01T00:00:00Z","expires_at":"2024-01-02T00:00:00Z","results_url":"`+srv.URL+`/results"}`)
	})
	mux.HandleFunc("/results", ok(`{"custom_id":"ok","result":{"type":"canceled"}}`))

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	ref := types.ProviderReference{"anthropic": "file_1"}
	want300 := func(name string, err error) {
		t.Helper()
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.StatusCode != http.StatusMultipleChoices {
			t.Errorf("%s: err = %T %v, want a status 300 provider error", name, err, err)
		}
	}
	files := p.Files().(*FilesAPI)
	_, err := files.UploadFile(context.Background(), types.UploadFileOptions{Data: types.FileData{Type: types.FileDataTypeData, Data: []byte("x")}, MediaType: "text/plain"})
	want300("UploadFile", err)
	_, err = files.GetFileMetadata(context.Background(), provider.FileMetadataOptions{File: ref})
	want300("GetFileMetadata", err)
	_, err = files.DeleteFile(context.Background(), provider.DeleteFileOptions{File: ref})
	want300("DeleteFile", err)
	_, err = p.Skills().(*SkillsAPI).UploadSkill(context.Background(), types.UploadSkillOptions{
		Files: []types.UploadSkillFile{{Path: "s/SKILL.md", Data: types.FileData{Type: types.FileDataTypeText, Text: "x"}}},
	})
	want300("UploadSkill", err)
	_, err = p.ExperimentalBatch().DoGetBatchResults(context.Background(), provider.BatchV4OperationOptions{BatchID: "b1"})
	want300("DoGetBatchResults", err)
}

// TestR94provaSkillVersion3xxFails: the version metadata getFromApi also
// fails on an unfollowed 300.
func TestR94provaSkillVersion3xxFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			_, _ = io.WriteString(w, `{"id":"skill_1","latest_version":"1","source":"custom","created_at":"c","updated_at":"u"}`)
			return
		}
		w.WriteHeader(http.StatusMultipleChoices)
		_, _ = io.WriteString(w, `{"type":"skill_version","skill_id":"skill_1","name":"n"}`)
	}))
	defer srv.Close()
	p := New(Config{BaseURL: srv.URL, APIKey: "k"})
	_, err := p.Skills().(*SkillsAPI).UploadSkill(context.Background(), types.UploadSkillOptions{
		Files: []types.UploadSkillFile{{Path: "s/SKILL.md", Data: types.FileData{Type: types.FileDataTypeText, Text: "x"}}},
	})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.StatusCode != http.StatusMultipleChoices {
		t.Fatalf("err = %T %v", err, err)
	}
}
