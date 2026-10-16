package bfl

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func statusOnce(t *testing.T, body string) (*provider.VideoModelV3OperationStatusResult, error) {
	t.Helper()
	model, server := newVideoHTTPModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
	defer server.Close()
	op, _ := json.Marshal(bflVideoOperation{RequestID: "req-1", PollingURL: server.URL + "/get_result"})
	return model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
}

// TS bflVideoPollSchema: a mistyped status/cost is "Invalid JSON response".
func TestVideoDoStatusInvalidPollBodyIsInvalidJSONResponse(t *testing.T) {
	for _, body := range []string{`{"status":5}`, `{"status":"Ready","cost":"x"}`, `{"status":null}`, `[]`, `{}`} {
		_, err := statusOnce(t, body)
		var pErr *providererrors.ProviderError
		if !errors.As(err, &pErr) || pErr.Message != "Invalid JSON response" || pErr.ResponseBody != body {
			t.Errorf("body %s: err = %#v", body, err)
		}
	}
}

// `status ?? state`: an empty status is present and wins over state.
func TestVideoDoStatusEmptyStatusWinsOverState(t *testing.T) {
	res, err := statusOnce(t, `{"status":"","state":"Ready","result":{"sample":"https://x.example/v.mp4"}}`)
	if err != nil || res.Status != provider.VideoOperationStatusPending {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

// TS getFromApi lets a transport failure propagate unchanged.
func TestVideoDoStatusTransportFailureIsNotWrapped(t *testing.T) {
	model, server := newVideoHTTPModel(t, func(w http.ResponseWriter, r *http.Request) {})
	op, _ := json.Marshal(bflVideoOperation{RequestID: "req-1", PollingURL: server.URL + "/get_result"})
	server.Close()
	_, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err == nil || strings.Contains(err.Error(), "failed to check status") {
		t.Fatalf("err = %v", err)
	}
}
