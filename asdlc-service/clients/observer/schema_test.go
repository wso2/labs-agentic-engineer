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

package observer

import "testing"

func TestParseProgressLine_PhaseRoundTrip(t *testing.T) {
	raw := `{"schemaVersion":1,"ts":"2026-05-07T14:30:12.413Z","seq":47,"kind":"phase","phase":"workspace_ready"}`
	ev := ParseProgressLine(raw)
	if ev.Kind != "phase" {
		t.Fatalf("expected kind=phase, got %q", ev.Kind)
	}
	if ev.SchemaVersion != 1 {
		t.Fatalf("expected schemaVersion=1, got %d", ev.SchemaVersion)
	}
	if ev.Phase != "workspace_ready" {
		t.Fatalf("expected phase=workspace_ready, got %q", ev.Phase)
	}
	if ev.Seq != 47 {
		t.Fatalf("expected seq=47, got %d", ev.Seq)
	}
}

func TestParseProgressLine_GitPush(t *testing.T) {
	raw := `{"schemaVersion":1,"ts":"2026-05-07T14:32:08.910Z","seq":51,"kind":"git_push","sha":"b3c4f2a","branch":"task/jwt-9a3"}`
	ev := ParseProgressLine(raw)
	if ev.Kind != "git_push" {
		t.Fatalf("expected kind=git_push, got %q", ev.Kind)
	}
	if ev.SHA != "b3c4f2a" {
		t.Fatalf("expected sha=b3c4f2a, got %q", ev.SHA)
	}
	if ev.Branch != "task/jwt-9a3" {
		t.Fatalf("expected branch=task/jwt-9a3, got %q", ev.Branch)
	}
}

func TestParseProgressLine_NonJSON(t *testing.T) {
	raw := "stdout: build started"
	ev := ParseProgressLine(raw)
	if ev.Kind != "log" {
		t.Fatalf("non-JSON should fall back to log, got %q", ev.Kind)
	}
	if ev.Summary != raw {
		t.Fatalf("summary should preserve raw line, got %q", ev.Summary)
	}
}

func TestParseProgressLine_BadJSON(t *testing.T) {
	raw := `{"schemaVersion":`
	ev := ParseProgressLine(raw)
	if ev.Kind != "log" {
		t.Fatalf("malformed JSON should fall back to log, got %q", ev.Kind)
	}
}

func TestParseProgressLine_WrongSchemaVersion(t *testing.T) {
	raw := `{"schemaVersion":99,"ts":"2026-05-07T14:30:12.413Z","seq":1,"kind":"phase","phase":"x"}`
	ev := ParseProgressLine(raw)
	if ev.Kind != "log" {
		t.Fatalf("unsupported schemaVersion should fall back to log, got %q", ev.Kind)
	}
}

func TestParseProgressLine_EmptyKind(t *testing.T) {
	raw := `{"schemaVersion":1,"ts":"2026-05-07T14:30:12.413Z","seq":1}`
	ev := ParseProgressLine(raw)
	if ev.Kind != "log" {
		t.Fatalf("missing kind should fall back to log, got %q", ev.Kind)
	}
}

func TestParseProgressLine_Empty(t *testing.T) {
	ev := ParseProgressLine("")
	if ev.Kind != "log" {
		t.Fatalf("empty string should be a log event, got %q", ev.Kind)
	}
}
