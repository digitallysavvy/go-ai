package tencent

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS signs `host: new URL(url).host`: the WHATWG host is lower-cased and a
// default port is dropped.
func TestR98provcCloudAPISignsWHATWGHost(t *testing.T) {
	srv, calls := cloudServer(t, `{"Response":{"TaskId":"task-1"}}`)
	port := srv.URL[strings.LastIndex(srv.URL, ":")+1:]
	p := New(Config{VodBaseURL: "http://LOCALHOST:" + port})
	if _, err := p.postTencentCloudAPI(context.Background(), "vod", "CreateAigcVideoTask", "2018-07-17", cloudCreds, cloudBody(), vodCreateTaskResponseSchema, nil, 1794120000); err != nil {
		t.Fatal(err)
	}
	want := createTC3Authorization(cloudCreds.secretID, cloudCreds.secretKey, "vod", 1794120000,
		`{"SubAppId":1500000000,"ModelName":"VS","ModelVersion":"2.0","Prompt":"A cat"}`,
		map[string]string{"content-type": tc3ContentType, "host": "localhost:" + port, "x-tc-action": "createaigcvideotask"})
	if got := calls()[0].headers.Get("Authorization"); got != want {
		t.Errorf("Authorization = %q, want %q", got, want)
	}
}

// TS new URL(url) throws TypeError "Invalid URL" before any request.
func TestR98provcCloudAPIInvalidBaseURL(t *testing.T) {
	p := New(Config{VodBaseURL: "http://exa mple.com"})
	_, err := p.postTencentCloudAPI(context.Background(), "vod", "DescribeTaskDetail", "2018-07-17", cloudCreds, newOrderedBody("TaskId", "t"), vodCreateTaskResponseSchema, nil, 1794120000)
	if err == nil || err.Error() != "Invalid URL" {
		t.Fatalf("err = %v", err)
	}
}

// APICallError.data is the parsed envelope: Response is loose, but its Error
// is a z.object, so undeclared Error keys are stripped.
func TestR98provcCloudAPIErrorDataStripsErrorKeys(t *testing.T) {
	srv, _ := cloudServer(t, `{"Response":{"Error":{"Code":"AuthFailure","Message":"m","Extra":1},"RequestId":"r","Other":2},"Top":3}`)
	p := New(Config{VodBaseURL: srv.URL})
	_, err := p.postTencentCloudAPI(context.Background(), "vod", "CreateAigcVideoTask", "2018-07-17", cloudCreds, cloudBody(), vodCreateTaskResponseSchema, nil, 1794120000)
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) {
		t.Fatalf("err = %T %v", err, err)
	}
	want := map[string]interface{}{"Response": map[string]interface{}{
		"Error": map[string]interface{}{"Code": "AuthFailure", "Message": "m"}, "RequestId": "r", "Other": float64(2),
	}}
	if !reflect.DeepEqual(perr.Data, want) {
		t.Errorf("data = %#v", perr.Data)
	}
}
