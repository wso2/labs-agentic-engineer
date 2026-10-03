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

package turnstest

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
)

// TestGolden_MatchesSpec: every golden line is exactly one of the spec's
// frame shapes, and each stream ends with its one result frame.
func TestGolden_MatchesSpec(t *testing.T) {
	_, self, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(self), "../../../../../../../..")
	doc, err := openapi3.NewLoader().LoadFromFile(filepath.Join(root, "packages/contracts/sockets/ae-studio/turn/openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	frames := []string{"TaskOpFrame", "KeepAliveFrame", "ResultFrame"}
	names, err := filepath.Glob(filepath.Join(root, GoldenDir, "*.ndjson"))
	if err != nil || len(names) == 0 {
		t.Fatalf("no golden streams: %v", err)
	}
	for _, name := range names {
		lines := Golden(t, filepath.Base(name))
		for i, line := range lines {
			var v any
			if err := json.Unmarshal([]byte(line), &v); err != nil {
				t.Fatalf("%s:%d not JSON: %v", filepath.Base(name), i+1, err)
			}
			var matched []string
			for _, f := range frames {
				if doc.Components.Schemas[f].Value.VisitJSON(v) == nil {
					matched = append(matched, f)
				}
			}
			if len(matched) != 1 {
				t.Fatalf("%s:%d matches %v, want exactly one frame shape", filepath.Base(name), i+1, matched)
			}
			isResult := matched[0] == "ResultFrame"
			if last := i == len(lines)-1; isResult != last {
				t.Fatalf("%s:%d: the result frame must be the last line, and only the last", filepath.Base(name), i+1)
			}
		}
	}
}

// TestFake_TurnRulesOfTheSpec: the fake keeps the spec's turn rules, so the
// callers' tests exercise them.
func TestFake_TurnRulesOfTheSpec(t *testing.T) {
	s := New(t, Script{Events: []string{`{"type":"keep-alive"}`}, Hold: 300 * time.Millisecond})
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", s.Path())
	}}}
	t.Cleanup(client.CloseIdleConnections)
	post := func(id string) (*http.Response, string) {
		t.Helper()
		resp, err := client.Post("http://turn/turns", "application/json",
			strings.NewReader(`{"turnId":"`+id+`","project":"greeter","kind":"start","credit":{"userId":"u","name":"n","email":"e"}}`))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return resp, string(b)
	}
	const a, b = "11111111-1111-5111-8111-111111111111", "22222222-2222-5222-8222-222222222222"

	// A caller that leaves at once does not stop the turn.
	resp, err := client.Post("http://turn/turns", "application/json",
		strings.NewReader(`{"turnId":"`+a+`","project":"greeter","kind":"start","credit":{"userId":"u","name":"n","email":"e"}}`))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("start: %v %v", resp, err)
	}
	_ = resp.Body.Close()

	if resp, body := post(b); resp.StatusCode != http.StatusConflict ||
		body != `{"activeTurnId":"`+a+`","code":"turn_in_progress"}`+"\n" {
		t.Fatalf("different turnId while running: %d %s", resp.StatusCode, body)
	}
	// Same turnId: reattached, the whole stream to the result.
	if resp, body := post(a); resp.StatusCode != http.StatusOK || body != `{"type":"keep-alive"}`+"\n"+CompletedResult+"\n" {
		t.Fatalf("reattach while running: %d %q", resp.StatusCode, body)
	}
	// After it ended: the finished stream again, never a second turn.
	if resp, body := post(a); resp.StatusCode != http.StatusOK || !strings.HasSuffix(body, CompletedResult+"\n") {
		t.Fatalf("reattach after the end: %d %q", resp.StatusCode, body)
	}
	if s.Started() != 1 {
		t.Fatalf("started = %d, want 1", s.Started())
	}
	// The project is free again for a new turn.
	if resp, _ := post(b); resp.StatusCode != http.StatusOK || s.Started() != 2 {
		t.Fatalf("next turn: %d, started %d", resp.StatusCode, s.Started())
	}
}
