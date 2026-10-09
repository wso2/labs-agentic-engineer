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

package turns

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/wso2/aep/ae-studio-tools/internal/turns/turnstest"
)

func TestClient_StartForwardsTheBodyUnchanged(t *testing.T) {
	// The request is forwarded byte for byte: re-encoding the generated
	// model would add an empty "scope" object to every start turn.
	body := turnBody(testTurnID)
	var got, method, path, ct string
	sock := turnstest.ServeHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got, method, path, ct = string(b), r.Method, r.URL.Path, r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"code":"turn_in_progress"}`)
	}))
	rc, status, err := NewClient(sock).Start(context.Background(), Body(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	answer, _ := io.ReadAll(rc)
	if status != http.StatusConflict || string(answer) != `{"code":"turn_in_progress"}` {
		t.Fatalf("got %d %s", status, answer)
	}
	if got != body || method != http.MethodPost || path != "/turns" || ct != "application/json" {
		t.Fatalf("sent %s %s %q %s", method, path, ct, got)
	}
}

func TestClient_UnreachableSocket(t *testing.T) {
	_, _, err := NewClient(filepath.Join(t.TempDir(), "absent.sock")).Start(context.Background(), Body(turnBody(testTurnID)))
	if !errors.Is(err, ErrSocketUnavailable) {
		t.Fatalf("err = %v, want ErrSocketUnavailable", err)
	}
}
