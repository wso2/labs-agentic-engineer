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
	"testing"
)

// recordingUpstream records what is forwarded.
type recordingUpstream struct {
	method string
	params string
}

func (u *recordingUpstream) Call(_ context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	u.method, u.params = method, string(params)
	return json.RawMessage(`{"content":[]}`), nil
}

func TestServer_ToolsListIsTheLocalDescriptors(t *testing.T) {
	up := &recordingUpstream{}
	out, err := Server{Upstream: up}.Call(context.Background(), "tools/list", nil)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(map[string]any{"tools": Tools()})
	if string(out) != string(want) {
		t.Fatalf("tools/list = %s", out)
	}
	if up.method != "" {
		t.Fatalf("forwarded %s", up.method)
	}
}

// Only name and arguments are forwarded; absent arguments are an empty object.
func TestServer_ForwardsNameAndArgumentsOnly(t *testing.T) {
	up := &recordingUpstream{}
	s := Server{Upstream: up}
	if _, err := s.Call(context.Background(), "tools/call", json.RawMessage(`{"name":"list_groups","_meta":{"x":1}}`)); err != nil {
		t.Fatal(err)
	}
	if up.method != "tools/call" || up.params != `{"name":"list_groups","arguments":{}}` {
		t.Fatalf("forwarded %s %s", up.method, up.params)
	}
	if _, err := s.Call(context.Background(), "tools/call", json.RawMessage(`{"name":"fetch_openapi_spec","arguments":{"url":"https://x"}}`)); err != nil {
		t.Fatal(err)
	}
	if up.params != `{"name":"fetch_openapi_spec","arguments":{"url":"https://x"}}` {
		t.Fatalf("forwarded %s", up.params)
	}
	// The guardrail catalog is aep-api's, read for the token's org.
	if _, err := s.Call(context.Background(), "tools/call", json.RawMessage(`{"name":"list_guardrail_policies"}`)); err != nil {
		t.Fatal(err)
	}
	if up.params != `{"name":"list_guardrail_policies","arguments":{}}` {
		t.Fatalf("forwarded %s", up.params)
	}
}

func TestServer_RefusalsAreJSONRPCErrors(t *testing.T) {
	up := &recordingUpstream{}
	s := Server{Upstream: up}
	for name, c := range map[string]struct {
		method, params string
		code           int
	}{
		"unknown method":   {"ping", `{}`, codeMethodNotFound},
		"unlisted tool":    {"tools/call", `{"name":"drop_database"}`, codeInvalidParams},
		"no name":          {"tools/call", `{"arguments":{}}`, codeInvalidParams},
		"params not a map": {"tools/call", `[1]`, codeInvalidParams},
		"args not a map":   {"tools/call", `{"name":"get_remote_git_file_contents","arguments":[1]}`, codeInvalidParams},
		"forwarded args":   {"tools/call", `{"name":"list_groups","arguments":"x"}`, codeInvalidParams},
		"args not strings": {"tools/call", `{"name":"search_remote_git_code","arguments":{"owner":1}}`, codeInvalidParams},
	} {
		_, err := s.Call(context.Background(), c.method, json.RawMessage(c.params))
		var rpc *RPCError
		if !errors.As(err, &rpc) || rpc.Code != c.code {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
	if up.method != "" {
		t.Fatalf("a refused call was forwarded: %s", up.params)
	}
}
