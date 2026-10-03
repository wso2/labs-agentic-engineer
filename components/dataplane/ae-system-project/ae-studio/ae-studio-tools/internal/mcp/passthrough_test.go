// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wso2/aep/ae-studio-tools/internal/gen/aepapi"
)

// aepAPIStub is aep-api's /internal/v1/mcp: it records the request and
// answers status and body.
func aepAPIStub(t *testing.T, status int, body string) (Upstream, *http.Request, *[]byte) {
	t.Helper()
	var got http.Request
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = *r
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	c, err := aepapi.NewClientWithResponses(srv.URL + "/internal/v1")
	if err != nil {
		t.Fatal(err)
	}
	return NewAEPAPIUpstream(c), &got, &gotBody
}

func TestAEPAPIUpstream_SendsOneJSONRPCRequestAndAnswersTheResult(t *testing.T) {
	up, req, body := aepAPIStub(t, http.StatusOK, `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"[]"}]}}`)
	out, err := up.Call(context.Background(), "tools/call", json.RawMessage(`{"name":"list_groups","arguments":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"content":[{"type":"text","text":"[]"}]}` {
		t.Fatalf("result = %s", out)
	}
	if req.Method != http.MethodPost || req.URL.Path != "/internal/v1/mcp" || req.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("request = %s %s %s", req.Method, req.URL.Path, req.Header.Get("Content-Type"))
	}
	var sent struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int             `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(*body, &sent); err != nil {
		t.Fatal(err)
	}
	if sent.JSONRPC != "2.0" || sent.ID == 0 || sent.Method != "tools/call" || string(sent.Params) != `{"name":"list_groups","arguments":{}}` {
		t.Fatalf("sent = %s", *body)
	}
}

func TestAEPAPIUpstream_RelaysAJSONRPCError(t *testing.T) {
	up, _, _ := aepAPIStub(t, http.StatusOK, `{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"invalid params"}}`)
	_, err := up.Call(context.Background(), "tools/call", json.RawMessage(`{}`))
	var rpc *RPCError
	if !errors.As(err, &rpc) || rpc.Code != -32602 || rpc.Message != "invalid params" {
		t.Fatalf("err = %v", err)
	}
}

func TestAEPAPIUpstream_FailuresAreUnavailable(t *testing.T) {
	for name, c := range map[string]struct {
		status int
		body   string
	}{
		"5xx":       {http.StatusInternalServerError, `{}`},
		"401":       {http.StatusUnauthorized, `{"error":"unauthorized"}`},
		"no result": {http.StatusOK, `{"jsonrpc":"2.0","id":1}`},
		"not json":  {http.StatusOK, `<html>`},
	} {
		up, _, _ := aepAPIStub(t, c.status, c.body)
		if _, err := up.Call(context.Background(), "tools/call", json.RawMessage(`{}`)); !errors.Is(err, ErrUpstreamUnavailable) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
	c, err := aepapi.NewClientWithResponses("http://127.0.0.1:1/internal/v1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewAEPAPIUpstream(c).Call(context.Background(), "tools/call", json.RawMessage(`{}`)); !errors.Is(err, ErrUpstreamUnavailable) {
		t.Fatalf("unreachable: err = %v", err)
	}
}
