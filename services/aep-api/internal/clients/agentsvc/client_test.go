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

package agentsvc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

// recordingServer captures the last request and replays a fixed response.
type recordingServer struct {
	*httptest.Server
	method  string
	path    string
	headers http.Header
	body    string
}

func newRecordingServer(t *testing.T, status int, respBody, contentType string) *recordingServer {
	t.Helper()
	rs := &recordingServer{}
	rs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rs.method, rs.path, rs.headers, rs.body = r.Method, r.URL.Path, r.Header.Clone(), string(b)
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(rs.Close)
	return rs
}

func TestTurn_SendsExactRequestAndHeaders(t *testing.T) {
	const sse = "data: {\"type\":\"start\"}\n\n: keep-alive\n\ndata: [DONE]\n\n"
	srv := newRecordingServer(t, http.StatusOK, sse, "text/event-stream")

	c := New(Config{BaseURL: srv.URL, Secret: "shh", Audience: "agents-service", Issuer: "aep-bff"})
	req := TurnRequest{
		Turn: TurnSpec{Kind: TurnKindFlow, Skill: "design"},
		Workspace: WorkspaceRef{
			ConversationID: "org_o--proj_p--design-generate--uuid1",
			TurnID:         "turn-1",
			RepoSlug:       "o-r",
			Ref:            strings.Repeat("a", 40),
			SkillsRef:      strings.Repeat("b", 40),
		},
		FilesChangedExternally: true,
		Surface:                SurfaceConsole,
	}
	body, err := c.Turn(context.Background(), "org_o--proj_p--design-generate--uuid1", "org-o", "sk-ant-123", req)
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	defer body.Close()
	got, _ := io.ReadAll(body)
	if string(got) != sse {
		t.Errorf("stream not passed through verbatim:\n got %q\nwant %q", string(got), sse)
	}

	if srv.method != http.MethodPost {
		t.Errorf("method = %s, want POST", srv.method)
	}
	if srv.path != "/conversations/org_o--proj_p--design-generate--uuid1/turns" {
		t.Errorf("path = %s", srv.path)
	}
	if ct := srv.headers.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	if ac := srv.headers.Get("Accept"); ac != "text/event-stream" {
		t.Errorf("Accept = %q", ac)
	}
	if k := srv.headers.Get("X-Anthropic-Key"); k != "sk-ant-123" {
		t.Errorf("X-Anthropic-Key = %q", k)
	}
	if o := srv.headers.Get("X-Org-Id"); o != "org-o" {
		t.Errorf("X-Org-Id = %q", o)
	}

	// Authorization bearer is a valid HS256 token with the configured aud/iss.
	auth := srv.headers.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		t.Fatalf("Authorization = %q, want Bearer prefix", auth)
	}
	tok, err := jwt.Parse(strings.TrimPrefix(auth, "Bearer "), func(*jwt.Token) (any, error) { return []byte("shh"), nil })
	if err != nil || !tok.Valid {
		t.Fatalf("token parse/verify failed: %v", err)
	}
	aud, _ := tok.Claims.GetAudience()
	if len(aud) != 1 || aud[0] != "agents-service" {
		t.Errorf("aud = %v", aud)
	}
	iss, _ := tok.Claims.GetIssuer()
	if iss != "aep-bff" {
		t.Errorf("iss = %q", iss)
	}

	// Body is the exact TurnRequest JSON with the pinned camelCase workspace
	// field names (the agents-side resolveWorkspace contract) and NO
	// files/skills keys — the snapshot is read from the mount, never inlined.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(srv.body), &raw); err != nil {
		t.Fatalf("unmarshal sent body: %v", err)
	}
	for _, banned := range []string{"files", "skills"} {
		if _, ok := raw[banned]; ok {
			t.Errorf("body carries banned key %q: %s", banned, srv.body)
		}
	}
	var ws map[string]string
	if err := json.Unmarshal(raw["workspace"], &ws); err != nil {
		t.Fatalf("unmarshal workspace: %v", err)
	}
	want := map[string]string{
		"conversationId": req.Workspace.ConversationID,
		"turnId":         "turn-1",
		"repoSlug":       "o-r",
		"ref":            strings.Repeat("a", 40),
		"skillsRef":      strings.Repeat("b", 40),
	}
	for k, v := range want {
		if ws[k] != v {
			t.Errorf("workspace[%q] = %q, want %q", k, ws[k], v)
		}
	}
	// The surface rides the wire under the name the agents service validates
	// (#580); a typo here is a 400 on every turn, not a quiet loss of narration.
	if got := string(raw["surface"]); got != `"console"` {
		t.Errorf("surface = %s, want \"console\"", got)
	}
	var sent TurnRequest
	if err := json.Unmarshal([]byte(srv.body), &sent); err != nil {
		t.Fatalf("unmarshal sent body: %v", err)
	}
	if sent.Turn.Kind != req.Turn.Kind || sent.Turn.Skill != req.Turn.Skill || !sent.FilesChangedExternally {
		t.Errorf("turn/flag mismatch: %+v", sent)
	}
	if sent.Surface != SurfaceConsole {
		t.Errorf("surface = %q, want %q", sent.Surface, SurfaceConsole)
	}
}

// A caller that names no surface sends no key at all — the agents service then
// composes a prompt byte-identical to one from before surfaces existed, which
// is what keeps a local run reading the shared skills unchanged.
func TestTurn_UnsetSurfaceIsAbsentFromTheBody(t *testing.T) {
	srv := newRecordingServer(t, http.StatusOK, "data: [DONE]\n\n", "text/event-stream")
	c := New(Config{BaseURL: srv.URL, Secret: "shh"})
	body, err := c.Turn(context.Background(), "org_o--proj_p--general--uuid1", "org-o", "sk-ant-123", TurnRequest{
		Turn:      TurnSpec{Kind: TurnKindChat, Text: "hi"},
		Workspace: WorkspaceRef{ConversationID: "org_o--proj_p--general--uuid1"},
	})
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	defer body.Close()
	io.ReadAll(body)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(srv.body), &raw); err != nil {
		t.Fatalf("unmarshal sent body: %v", err)
	}
	if _, ok := raw["surface"]; ok {
		t.Errorf("body carries a surface key when none was set: %s", srv.body)
	}
}

func TestTurn_UpstreamErrorIsTyped(t *testing.T) {
	srv := newRecordingServer(t, http.StatusConflict, `{"error":"turn in progress"}`, "application/json")
	c := New(Config{BaseURL: srv.URL, Secret: "shh"})

	_, err := c.Turn(context.Background(), "cid", "org", "key", TurnRequest{Turn: TurnSpec{Kind: TurnKindChat, Text: "x"}})
	var ue *UpstreamError
	if !errors.As(err, &ue) {
		t.Fatalf("want *UpstreamError, got %v", err)
	}
	if ue.StatusCode != http.StatusConflict {
		t.Errorf("StatusCode = %d, want 409", ue.StatusCode)
	}
}

func TestGetConversation(t *testing.T) {
	srv := newRecordingServer(t, http.StatusOK, `{"messages":[{"role":"user"}]}`, "application/json")
	c := New(Config{BaseURL: srv.URL, Secret: "shh"})

	raw, err := c.GetConversation(context.Background(), "cid-1", "org-o")
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if !strings.Contains(string(raw), `"messages"`) {
		t.Errorf("body = %s", raw)
	}
	if srv.path != "/conversations/cid-1" {
		t.Errorf("path = %s", srv.path)
	}

	srv2 := newRecordingServer(t, http.StatusNotFound, `{}`, "application/json")
	c2 := New(Config{BaseURL: srv2.URL, Secret: "shh"})
	_, err = c2.GetConversation(context.Background(), "missing", "org")
	var ue *UpstreamError
	if !errors.As(err, &ue) || ue.StatusCode != http.StatusNotFound {
		t.Fatalf("want *UpstreamError 404, got %v", err)
	}
}
