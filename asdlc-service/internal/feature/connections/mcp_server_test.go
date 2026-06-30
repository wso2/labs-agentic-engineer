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

package connections

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMCPHandler_NilResourceTypes verifies that NewMCPHandler with a nil
// resourceTypes does NOT panic and returns a success response with an empty
// resourceTypes list when list_platform_resource_types is called.
func TestMCPHandler_NilResourceTypes(t *testing.T) {
	// Registry must be non-nil (the handler's first check), but we never call
	// any DB method here — passing a zero-value Registry is sufficient.
	reg := &Registry{}
	h := NewMCPHandler(reg, nil, nil) // nil catalog + nil resourceTypes

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_platform_resource_types","arguments":{}}}`
	req := httptest.NewRequest(http.MethodPost, "/internal/organizations/default/mcp", strings.NewReader(body))
	req.SetPathValue("orgHandle", "default")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp jsonrpcResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %+v", resp.Error)
	}

	// The result must have content[0].text that is a JSON object with resourceTypes=[].
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result is not an object: %T %+v", resp.Result, resp.Result)
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("expected non-empty content array, got %+v", result["content"])
	}
	textBlock, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("content[0] is not an object: %T", content[0])
	}
	textStr, ok := textBlock["text"].(string)
	if !ok {
		t.Fatalf("content[0].text is not a string: %T", textBlock["text"])
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(textStr), &payload); err != nil {
		t.Fatalf("unmarshal text payload: %v", err)
	}
	rtRaw, ok := payload["resourceTypes"]
	if !ok {
		t.Fatalf("payload missing resourceTypes key: %+v", payload)
	}
	rtList, ok := rtRaw.([]any)
	if !ok {
		t.Fatalf("resourceTypes is not an array: %T", rtRaw)
	}
	if len(rtList) != 0 {
		t.Fatalf("expected empty resourceTypes, got %v", rtList)
	}
}
