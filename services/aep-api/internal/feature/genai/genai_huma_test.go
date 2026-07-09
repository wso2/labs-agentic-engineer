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

package genai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
)

// statusOf extracts the HTTP status a mapped error renders as.
func statusOf(t *testing.T, err error) int {
	t.Helper()
	var se huma.StatusError
	if !errors.As(err, &se) {
		t.Fatalf("mapped error %v is not a huma.StatusError", err)
	}
	return se.GetStatus()
}

// TestMapTurnError_Table pins the turn error mapping table — including the new
// 503 arm for an unusable org skills repo and the opaque-500 default. The table
// must stay intact as arms are added.
func TestMapTurnError_Table(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"project repo not found", ErrProjectRepoNotFound, 404},
		{"turn not found", ErrTurnNotFound, 404},
		{"invalid use case", ErrInvalidUseCase, 400},
		{"invalid conversation id", ErrInvalidConversationID, 400},
		{"empty instruction", ErrEmptyInstruction, 400},
		{"no anthropic key", ErrNoAnthropicKey, 400},
		{"buffer truncated", ErrTurnBufferTruncated, 409},
		{"skills repo unavailable", fmt.Errorf("%w: resolve head: boom", ErrSkillsRepoUnavailable), 503},
		{"wrapped skills unavailable", fmt.Errorf("start turn: %w", ErrSkillsRepoUnavailable), 503},
		{"unmapped default", errors.New("some unexpected failure"), 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := statusOf(t, mapTurnError(ctx, tc.err)); got != tc.want {
				t.Errorf("mapTurnError(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}

	// The in-progress guard renders its pinned 409 conflict body.
	if got := statusOf(t, mapTurnError(ctx, &TurnInProgressError{ActiveTurnID: "t1"})); got != 409 {
		t.Errorf("turn-in-progress = %d, want 409", got)
	}
	// The design gate renders its pinned 409 conflict body.
	if got := statusOf(t, mapTurnError(ctx, ErrRequirementsNotApproved)); got != 409 {
		t.Errorf("requirements-not-approved = %d, want 409", got)
	}
}

// captureLogs redirects slog to a buffer for the test's duration.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// TestInternalError_LogsCause proves the opaque-500 exit logs the underlying
// cause (the whole point of the helper — the client sees only "internal error",
// so the cause must reach the logs). Both default-500 mapper branches route
// through internalError.
func TestInternalError_LogsCause(t *testing.T) {
	buf := captureLogs(t)

	cause := errors.New("workspace exploded")
	if got := statusOf(t, mapTurnError(context.Background(), cause)); got != 500 {
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
	if got := statusOf(t, mapRehydrateError(errors.New("rehydrate blew up"))); got != 500 {
		t.Fatalf("rehydrate default arm status = %d, want 500", got)
	}
	if !strings.Contains(buf.String(), "rehydrate blew up") {
		t.Errorf("rehydrate internal-error log did not carry the cause; log = %q", buf.String())
	}
}

// TestSkillsUnavailable_LogsCause proves the 503 arm logs the wrapped cause —
// the incident's observability gap was exactly a skills failure with no log.
func TestSkillsUnavailable_LogsCause(t *testing.T) {
	buf := captureLogs(t)
	err := fmt.Errorf("%w: resolve head: git ref not found", ErrSkillsRepoUnavailable)
	if got := statusOf(t, mapTurnError(context.Background(), err)); got != 503 {
		t.Fatalf("skills-unavailable status = %d, want 503", got)
	}
	if !strings.Contains(buf.String(), "git ref not found") {
		t.Errorf("503 log did not carry the cause; log = %q", buf.String())
	}
}
