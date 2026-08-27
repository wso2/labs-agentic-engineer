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

package genaiturns

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// Port of the genai_huma mapper pins onto the strict-server mappers
// (handlers_genai.go): the status table, the pinned 409 conflict bodies, and
// the cause-logging guarantees. statusOf is defined below.

// TestMapGenAITurnError_Table pins the turn error mapping table — including
// the 503 arm for an unusable org skills repo and the opaque-500 default. The
// table must stay intact as arms are added.
func TestMapGenAITurnError_Table(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"project repo not found", spec.ErrProjectRepoNotFound, 404},
		{"turn not found", spec.ErrTurnNotFound, 404},
		{"invalid conversation id", spec.ErrInvalidConversationID, 400},
		{"empty instruction", spec.ErrEmptyInstruction, 400},
		{"no anthropic key", spec.ErrNoAnthropicKey, 400},
		{"buffer truncated", spec.ErrTurnBufferTruncated, 409},
		{"skills repo unavailable", fmt.Errorf("%w: resolve head: boom", spec.ErrSkillsRepoUnavailable), 503},
		{"wrapped skills unavailable", fmt.Errorf("start turn: %w", spec.ErrSkillsRepoUnavailable), 503},
		{"disk admission", fmt.Errorf("ensure repo snapshot: %w", gitfs.ErrDiskAdmission), 503},
		{"wrapped disk admission", fmt.Errorf("start turn: %w", gitfs.ErrDiskAdmission), 503},
		{"unmapped default", errors.New("some unexpected failure"), 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := statusOf(t, mapGenAITurnError(ctx, tc.err)); got != tc.want {
				t.Errorf("mapGenAITurnError(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

// TestTurnConflictOf_PinnedBodies pins the StartTurn conflict rejection's
// wire shape — 409 with {"code":"turn_in_progress","activeTurnId":…} (NOT the
// flat {code,message} envelope; declared in the contract as TurnConflict and
// served via the generated type). Field set + values are the contract; JSON
// key order is not.
func TestTurnConflictOf_PinnedBodies(t *testing.T) {
	resp, ok := turnConflictOf(fmt.Errorf("start turn: %w", &spec.TurnInProgressError{ActiveTurnID: "t1"}))
	if !ok {
		t.Fatal("turn-in-progress not recognized")
	}
	rec := httptest.NewRecorder()
	if err := resp.VisitCreateTurnResponse(rec); err != nil {
		t.Fatalf("visit: %v", err)
	}
	if rec.Code != 409 {
		t.Errorf("turn-in-progress status = %d, want 409", rec.Code)
	}
	var inProgress map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &inProgress); err != nil {
		t.Fatalf("turn-in-progress body not JSON: %v", err)
	}
	if !reflect.DeepEqual(inProgress, map[string]string{"code": "turn_in_progress", "activeTurnId": "t1"}) {
		t.Errorf("turn-in-progress body = %s", rec.Body.String())
	}

	if _, ok := turnConflictOf(spec.ErrTurnNotFound); ok {
		t.Error("non-conflict error must stay on the envelope path")
	}
}

// captureGenAILogs redirects slog to a buffer for the test's duration.
func captureGenAILogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// TestGenAIInternalError_LogsCause proves the opaque-500 exit logs the
// underlying cause (the whole point of the helper — the client sees only
// "internal error", so the cause must reach the logs). Both default-500
// mapper branches route through genaiInternalError.
func TestGenAIInternalError_LogsCause(t *testing.T) {
	buf := captureGenAILogs(t)

	cause := errors.New("workspace exploded")
	if got := statusOf(t, mapGenAITurnError(context.Background(), cause)); got != 500 {
		t.Fatalf("default arm status = %d, want 500", got)
	}
	if !strings.Contains(buf.String(), "workspace exploded") {
		t.Errorf("internal-error log did not carry the cause; log = %q", buf.String())
	}
	if !strings.Contains(buf.String(), "genai turn") {
		t.Errorf("internal-error log did not carry the scope; log = %q", buf.String())
	}

	// The rehydrate mapper's default arm logs too.
	buf.Reset()
	if got := statusOf(t, mapGenAIRehydrateError(errors.New("rehydrate blew up"))); got != 500 {
		t.Fatalf("rehydrate default arm status = %d, want 500", got)
	}
	if !strings.Contains(buf.String(), "rehydrate blew up") {
		t.Errorf("rehydrate internal-error log did not carry the cause; log = %q", buf.String())
	}
}

// TestGenAISkillsUnavailable_LogsCause proves the 503 arm logs the wrapped
// cause — the incident's observability gap was exactly a skills failure with
// no log.
func TestGenAISkillsUnavailable_LogsCause(t *testing.T) {
	buf := captureGenAILogs(t)
	err := fmt.Errorf("%w: resolve head: git ref not found", spec.ErrSkillsRepoUnavailable)
	if got := statusOf(t, mapGenAITurnError(context.Background(), err)); got != 503 {
		t.Fatalf("skills-unavailable status = %d, want 503", got)
	}
	if !strings.Contains(buf.String(), "git ref not found") {
		t.Errorf("503 log did not carry the cause; log = %q", buf.String())
	}
}

func TestGenAIDiskAdmission_Is503NotInternalError(t *testing.T) {
	buf := captureGenAILogs(t)
	err := fmt.Errorf("ensure repo snapshot: %w (usage=92%%)", gitfs.ErrDiskAdmission)
	mapped := mapGenAITurnError(context.Background(), err)
	var ae *apierr.Error
	if !errors.As(mapped, &ae) {
		t.Fatalf("expected an *apierr.Error, got %T (%v)", mapped, mapped)
	}
	if ae.Status != 503 {
		t.Fatalf("disk-admission status = %d, want 503", ae.Status)
	}
	if ae.Message == "internal error" {
		t.Fatalf("message = %q, want a sentence the console can show", ae.Message)
	}
	if !strings.Contains(buf.String(), "disk admission") && !strings.Contains(buf.String(), "usage=92") {
		t.Errorf("503 log did not carry the cause; log = %q", buf.String())
	}
}

// statusOf casts a transport error to its wire status, failing the test if it
// is not an *apierr.Error (the slice's error type).
func statusOf(t *testing.T, err error) int {
	t.Helper()
	var ae *apierr.Error
	if !errors.As(err, &ae) {
		t.Fatalf("expected an *apierr.Error, got %T (%v)", err, err)
	}
	return ae.Status
}
