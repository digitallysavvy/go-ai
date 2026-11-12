package google

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r94provgFilesServer serves the resumable start, the upload (with the given
// status and body) and nothing else.
func r94provgFilesServer(t *testing.T, startStatus, uploadStatus int, uploadBody string) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/upload/v1beta/files":
			w.Header().Set("X-Goog-Upload-URL", srv.URL+"/data")
			w.WriteHeader(startStatus)
		case "/data":
			w.WriteHeader(uploadStatus)
			_, _ = w.Write([]byte(uploadBody))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func r94provgUpload(t *testing.T, srv *httptest.Server) error {
	t.Helper()
	p := New(Config{APIKey: "k", BaseURL: srv.URL + "/v1beta"})
	_, err := p.Files().UploadFile(context.Background(), types.UploadFileOptions{Data: types.FileData{Type: types.FileDataTypeData, Data: []byte{1}}, MediaType: "application/pdf"})
	return err
}

// google-files.ts checks `response.ok` on both raw fetch steps: an unfollowed
// 3xx (no Location) fails the upload like a 4xx.
func TestR94provgFilesNon2xxFails(t *testing.T) {
	done := `{"file":{"name":"files/a","mimeType":"application/pdf","uri":"u","state":"ACTIVE"}}`
	err := r94provgUpload(t, r94provgFilesServer(t, 304, 200, done))
	if err == nil || !strings.Contains(err.Error(), "Failed to initiate resumable upload: 304") {
		t.Fatalf("start 304: err = %v", err)
	}
	err = r94provgUpload(t, r94provgFilesServer(t, 200, 300, done))
	if err == nil || !strings.Contains(err.Error(), "Failed to upload file data: 300") {
		t.Fatalf("upload 300: err = %v", err)
	}
	if err := r94provgUpload(t, r94provgFilesServer(t, 200, 200, done)); err != nil {
		t.Fatalf("2xx: %v", err)
	}
}

// TS reads uploadResult.file.state: an upload body without a file object is
// the TypeError, not an empty result.
func TestR94provgFilesUploadBodyWithoutFile(t *testing.T) {
	for body, want := range map[string]string{
		`{}`:            "Cannot read properties of undefined (reading 'state')",
		`{"file":null}`: "Cannot read properties of null (reading 'state')",
		`null`:          "Cannot read properties of null (reading 'file')",
		`[]`:            "Cannot read properties of undefined (reading 'state')",
	} {
		err := r94provgUpload(t, r94provgFilesServer(t, 200, 200, body))
		if err == nil || err.Error() != want {
			t.Errorf("body %s: err = %v, want %q", body, err, want)
		}
	}
}

// The batch upload and the results download are TS postToApi / getFromApi
// (`!response.ok` runs the failed-response handler): an unfollowed 3xx is
// the Google API error, not a success.
func TestR94provgBatchNon2xxFails(t *testing.T) {
	for _, step := range []string{"start", "upload", "download"} {
		var srv *httptest.Server
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fail := func() {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(300)
				_, _ = w.Write([]byte(`{"error":{"code":300,"message":"moved","status":"X"}}`))
			}
			switch {
			case r.URL.Path == "/upload/v1beta/files":
				if step == "start" {
					fail()
					return
				}
				w.Header().Set("x-goog-upload-url", srv.URL+"/session")
			case r.URL.Path == "/session":
				if step == "upload" {
					fail()
					return
				}
				_, _ = w.Write([]byte(`{"file":{"name":"files/in"}}`))
			case strings.HasPrefix(r.URL.Path, "/download/"):
				fail()
			default:
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"name":"batches/b","done":true,"metadata":{"state":"BATCH_STATE_SUCCEEDED","output":{"responsesFile":"files/out"}}}`))
			}
		}))
		p := New(Config{APIKey: "k", BaseURL: srv.URL + "/v1beta"})
		b := NewBatch(p)
		var err error
		if step == "download" {
			_, err = b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "batches/b"})
		} else {
			_, err = b.uploadBatchInputFile(t.Context(), []byte("{}"), "d", nil)
		}
		srv.Close()
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.StatusCode != 300 || pe.Message != "moved" {
			t.Errorf("%s: err = %T %v, want the 300 API error", step, err, err)
		}
	}
}

// r94provgFirstFrame serves one WebSocket and returns the first text frame
// the client sends, as sent.
func r94provgFirstFrame(t *testing.T) (*httptest.Server, <-chan string) {
	t.Helper()
	got := make(chan string, 1)
	srv := httptest.NewServer(websocket.Server{
		Handler: func(conn *websocket.Conn) {
			var raw string
			if websocket.Message.Receive(conn, &raw) == nil {
				got <- raw
			}
			_ = websocket.Message.Receive(conn, &raw)
		},
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
	})
	t.Cleanup(srv.Close)
	return srv, got
}

func r94provgWait(t *testing.T, got <-chan string) string {
	t.Helper()
	select {
	case s := <-got:
		return s
	case <-time.After(5 * time.Second):
		t.Fatal("no frame")
		return ""
	}
}

// cr-r94-carry-to-provg class 1 sweep: TS sends the Live setup frames, the
// client secret and Files / batch upload bodies, and the realtime client
// events with JSON.stringify, so <, > and & stay literal.
func TestR94provgJSONStringifyText(t *testing.T) {
	srv, got := r94provgFirstFrame(t)
	tm := newLiveTestModel(t, srv.URL)
	res, err := tm.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream([]byte{1}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
		ProviderOptions:  map[string]interface{}{"google": map[string]interface{}{"customVocabulary": []interface{}{"a<b&c"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if f := r94provgWait(t, got); !strings.Contains(f, `"a<b&c"`) {
		t.Errorf("transcription setup = %s", f)
	}
	_ = res.Stream.Close()

	srv2, got2 := r94provgFirstFrame(t)
	st := NewSpeechTranslationModel(New(Config{APIKey: "k", BaseURL: srv2.URL}), "x<y")
	res2, err := st.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            newChanAudioStream([]byte{1}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
		TargetLanguage:   "fr",
	})
	if err != nil {
		t.Fatal(err)
	}
	if f := r94provgWait(t, got2); !strings.Contains(f, `x<y`) {
		t.Errorf("speech translation setup = %s", f)
	}
	_ = res2.Stream.Close()

	// Realtime client event.
	rmAny, err := New(Config{APIKey: "k"}).RealtimeModel("gemini-live")
	if err != nil {
		t.Fatal(err)
	}
	rm := rmAny.(*GoogleRealtimeModel)
	instr := "a<b&c"
	raw, err := rm.SerializeClientEvent(provider.RealtimeClientEvent{Type: "session-update", Config: provider.RealtimeSessionConfig{Instructions: &instr}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "a<b&c") {
		t.Errorf("realtime event = %s", raw)
	}

	// Realtime client secret body.
	var secretBody string
	ssrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		secretBody = string(b)
		_, _ = w.Write([]byte(`{"name":"auth_tokens/t","expireTime":"2030-01-01T00:00:00Z"}`))
	}))
	defer ssrv.Close()
	srmAny, err := New(Config{APIKey: "k", BaseURL: ssrv.URL + "/v1beta"}).RealtimeModel("gemini-live")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = srmAny.(*GoogleRealtimeModel).DoCreateClientSecret(t.Context(), provider.ClientSecretOptions{SessionConfig: &provider.RealtimeSessionConfig{Instructions: &instr}})
	if !strings.Contains(secretBody, "a<b&c") {
		t.Errorf("client secret body = %s", secretBody)
	}

	// Files upload start body and batch upload start body.
	var bodies []string
	var bsrv *httptest.Server
	bsrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.URL.Path == "/upload/v1beta/files" {
			bodies = append(bodies, string(b))
			w.Header().Set("x-goog-upload-url", bsrv.URL+"/session")
			return
		}
		_, _ = w.Write([]byte(`{"file":{"name":"files/a","mimeType":"m","uri":"u","state":"ACTIVE"}}`))
	}))
	defer bsrv.Close()
	p := New(Config{APIKey: "k", BaseURL: bsrv.URL + "/v1beta"})
	if _, err := p.Files().UploadFile(context.Background(), types.UploadFileOptions{Data: types.FileData{Type: types.FileDataTypeData, Data: []byte{1}}, MediaType: "m",
		ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"displayName": "a<b"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewBatch(p).uploadBatchInputFile(t.Context(), []byte("{}"), "a<b", nil); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 || !strings.Contains(bodies[0], `"a<b"`) || !strings.Contains(bodies[1], `"a<b"`) {
		t.Errorf("upload start bodies = %q", bodies)
	}
}

type r94provgCloser struct {
	io.Reader
	closed bool
}

func (c *r94provgCloser) Close() error { c.closed = true; return nil }

// cr-r94-prova-to-provg: TS convertInlineFileDataToUint8Array cancels a
// stream and throws UnsupportedFunctionalityError('streaming file upload'),
// after parseProviderOptions and before getHeaders resolves the API key.
func TestR94provgFilesStreamUploadUnsupported(t *testing.T) {
	t.Setenv("GOOGLE_GENERATIVE_AI_API_KEY", "")
	stream := &r94provgCloser{Reader: strings.NewReader("x")}
	_, err := New(Config{}).Files().UploadFile(context.Background(), types.UploadFileOptions{Data: types.FileData{Type: types.FileDataTypeStream, Stream: stream}, MediaType: "m"})
	var unsupported *providererrors.UnsupportedFunctionalityError
	if !errors.As(err, &unsupported) || unsupported.Functionality != "streaming file upload" || !stream.closed {
		t.Fatalf("err = %T %v, closed %v", err, err, stream.closed)
	}
	// Invalid options fail first; the stream is not touched.
	stream2 := &r94provgCloser{Reader: strings.NewReader("x")}
	_, err = New(Config{APIKey: "k"}).Files().UploadFile(context.Background(), types.UploadFileOptions{Data: types.FileData{Type: types.FileDataTypeStream, Stream: stream2}, MediaType: "m",
		ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"pollIntervalMs": -1}}})
	var argErr *providererrors.InvalidArgumentError
	if !errors.As(err, &argErr) || stream2.closed {
		t.Fatalf("invalid options: err = %T %v, closed %v", err, err, stream2.closed)
	}
}

// TS googleUploadUrlResponseHandler throws only when the header is absent
// (== null); a present empty x-goog-upload-url is fetched as "" and fails
// to parse.
func TestR94provgBatchEmptyUploadURLHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header()["X-Goog-Upload-Url"] = []string{""}
	}))
	defer srv.Close()
	_, err := NewBatch(New(Config{APIKey: "k", BaseURL: srv.URL + "/v1beta"})).uploadBatchInputFile(t.Context(), []byte("{}"), "d", nil)
	if err == nil || err.Error() != "Failed to parse URL from " {
		t.Fatalf("err = %T %v", err, err)
	}
}

// cr-r94-prova-to-provg 2 sweep: the Live transcription and speech
// translation streams fail with the caller's cancel cause (TS
// abortSignal.reason).
func TestR94provgAbortCause(t *testing.T) {
	reason := errors.New("r94provg user stop")
	check := func(name string, next func() error, cancel context.CancelCauseFunc) {
		t.Helper()
		time.Sleep(50 * time.Millisecond)
		cancel(reason)
		done := make(chan error, 1)
		go func() {
			for {
				if err := next(); err != nil {
					done <- err
					return
				}
			}
		}()
		select {
		case err := <-done:
			if !errors.Is(err, reason) {
				t.Errorf("%s: err = %v, want the cancel cause", name, err)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("%s: no error after cancel", name)
		}
	}
	srv, _ := r94provgFirstFrame(t)
	ctx, cancel := context.WithCancelCause(context.Background())
	res, err := newLiveTestModel(t, srv.URL).DoStream(ctx, &provider.TranscriptionStreamOptions{
		Audio: newBlockingAudioStream(), InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Stream.Close() //nolint:errcheck
	check("transcription", func() error { _, err := res.Stream.Next(); return err }, cancel)

	srv2, _ := r94provgFirstFrame(t)
	ctx2, cancel2 := context.WithCancelCause(context.Background())
	res2, err := NewSpeechTranslationModel(New(Config{APIKey: "k", BaseURL: srv2.URL}), "m").DoStream(ctx2, &provider.SpeechTranslationStreamOptions{
		Audio: newBlockingAudioStream(), InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)}, TargetLanguage: "fr",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Stream.Close() //nolint:errcheck
	check("speech translation", func() error { _, err := res2.Stream.Next(); return err }, cancel2)
}

func r94provgInteractionsBody(t *testing.T, google map[string]interface{}, msgs []types.Message) (map[string]interface{}, []types.Warning, error) {
	t.Helper()
	m := NewInteractionsLanguageModel(New(Config{APIKey: "k"}), "gemini-3-flash-preview")
	args, warnings, _, err := m.buildArgs(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: msgs}, AllowSystemInMessages: true,
		ProviderOptions: map[string]interface{}{"google": google},
	}, false)
	if err != nil {
		return nil, nil, err
	}
	b, _ := json.Marshal(args)
	var body map[string]interface{}
	_ = json.Unmarshal(b, &body)
	return body, warnings, nil
}

// TS reads the Interactions string options with `?? undefined`: a present ""
// systemInstruction is sent (and conflicts with a system message), a present
// "" previousInteractionId is sent and compacts the history; an enum option
// given as "" fails parseProviderOptions.
func TestR94provgInteractionsEmptyStringOptions(t *testing.T) {
	user := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}
	body, _, err := r94provgInteractionsBody(t, map[string]interface{}{"systemInstruction": "", "previousInteractionId": ""}, user)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := body["system_instruction"]; !ok || v != "" {
		t.Errorf("system_instruction = %#v (present %v)", v, ok)
	}
	if v, ok := body["previous_interaction_id"]; !ok || v != "" {
		t.Errorf("previous_interaction_id = %#v (present %v)", v, ok)
	}
	sys := append([]types.Message{{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "s"}}}}, user...)
	_, warnings, err := r94provgInteractionsBody(t, map[string]interface{}{"systemInstruction": ""}, sys)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range warnings {
		found = found || strings.Contains(w.Message, "both AI SDK system message")
	}
	if !found {
		t.Errorf("no conflict warning: %+v", warnings)
	}
	for _, k := range []string{"mediaResolution", "serviceTier", "thinkingLevel", "thinkingSummaries"} {
		if _, _, err := r94provgInteractionsBody(t, map[string]interface{}{k: ""}, user); err == nil {
			t.Errorf("%s: \"\" accepted", k)
		}
	}
}

// TS `response.service_tier ?? responseHeaders?.['x-gemini-service-tier']`:
// a present empty header is the service tier.
func TestR94provgInteractionsEmptyServiceTierHeader(t *testing.T) {
	m := NewInteractionsLanguageModel(New(Config{APIKey: "k"}), "gemini-3-flash-preview")
	res := m.convertResponse(interactionsResponse{Status: "completed"}, &internalhttp.Response{Headers: http.Header{"X-Gemini-Service-Tier": {""}}, Body: []byte(`{}`)}, interactionsRequest{}, nil)
	meta, _ := res.ProviderMetadata["google"].(map[string]interface{})
	if v, ok := meta["serviceTier"]; !ok || v != "" {
		t.Fatalf("serviceTier = %#v (present %v)", v, ok)
	}
}

func r94provgStr(s string) *string { return &s }

// TS getGoogleInteractionsOutputTokensByModality keeps an entry whose
// modality is != null, so a present empty modality is a key.
func TestR94provgOutputTokensByEmptyModality(t *testing.T) {
	var u interactionsUsage
	if err := json.Unmarshal([]byte(`{"output_tokens_by_modality":[{"modality":"","tokens":3},{"tokens":4},{"modality":null,"tokens":5}]}`), &u); err != nil {
		t.Fatal(err)
	}
	got := outputTokensByModalityMap(&u)
	if len(got) != 1 || got[""] != int64(3) {
		t.Fatalf("by modality = %#v", got)
	}
}

// With a present empty previousInteractionId, TS compacts only assistant
// turns whose part providerOptions.google.interactionId === "": a turn with
// no interactionId is kept.
func TestR94provgInteractionsEmptyPreviousIDCompaction(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "kept"}}, ToolCalls: []types.ToolCall{{ID: "c1", ToolName: "t"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "dropped", ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"interactionId": ""}}}}},
	}
	body, _, err := r94provgInteractionsBody(t, map[string]interface{}{"previousInteractionId": ""}, msgs)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(body["input"])
	if !strings.Contains(string(b), "kept") || strings.Contains(string(b), "dropped") {
		t.Fatalf("input = %s", b)
	}
}
