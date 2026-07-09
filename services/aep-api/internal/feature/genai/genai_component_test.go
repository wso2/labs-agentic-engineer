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

// Component tier for the committed-truth turn surface (shared-volume-clone
// Phase 4 exit gate): the REAL Huma handler (componenttest) fronting the REAL
// genai service — turn repository semantics faked in memory (the D18 guard's
// DB tier is covered by dbtest), the workspace engine REAL over real file://
// origins (workspacetest), and a scripted fake agents SSE server (incl. the
// D14 manifest frames) recording the exact TurnRequest the BFF dispatches.
package genai_test

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wso2/aep/aep-api/internal/api"
	"github.com/wso2/aep/aep-api/internal/clients/agentsvc"
	"github.com/wso2/aep/aep-api/internal/credentials"
	"github.com/wso2/aep/aep-api/internal/feature/genai"
	"github.com/wso2/aep/aep-api/internal/feature/gitrepo"
	"github.com/wso2/aep/aep-api/internal/platform/componenttest"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs/workspacetest"
	"github.com/wso2/aep/aep-api/internal/platform/gittest"
	"github.com/wso2/aep/aep-api/models"
)

const (
	testOrg  = "acme-org"
	testProj = "widgets"
	convUUID = "550e8400-e29b-41d4-a716-446655440000"
)

func turnsPath(uuid string) string {
	return "/api/v1/projects/" + testProj + "/agents/" + uuid + "/messages"
}
func turnPath(turnID string) string {
	return "/api/v1/projects/" + testProj + "/turns/" + turnID
}
func convPath(uuid string) string {
	return "/api/v1/projects/" + testProj + "/agents/" + uuid + "/messages"
}

// ---- SSE script helpers ------------------------------------------------------

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func addFilePart(path, content string) string {
	b, _ := json.Marshal(map[string]any{
		"type": "tool-call", "toolCallId": "t-" + path, "toolName": "addFile",
		"input": map[string]string{"path": path, "content": content},
	})
	return string(b)
}

func editFilePart(path, oldString, newString string) string {
	b, _ := json.Marshal(map[string]any{
		"type": "tool-call", "toolCallId": "e-" + path, "toolName": "editFile",
		"input": map[string]string{"path": path, "oldString": oldString, "newString": newString},
	})
	return string(b)
}

func textPart(text string) string {
	b, _ := json.Marshal(map[string]any{"type": "text-delta", "id": "txt", "delta": text})
	return string(b)
}

// manifestPart builds the terminal manifest frame over FINAL contents.
func manifestPart(files map[string]string, deleted []string) string {
	hashes := map[string]string{}
	for p, c := range files {
		hashes[p] = sha256Hex(c)
	}
	if deleted == nil {
		deleted = []string{}
	}
	b, _ := json.Marshal(map[string]any{"type": "manifest", "files": hashes, "deleted": deleted})
	return string(b)
}

// ---- fake agents service -----------------------------------------------------

// fakeAgents streams a scripted turn: `parts` in order (gating after
// gateAfter parts when gated), then the manifest (unless nil), then [DONE]
// (unless sever). It records the exact TurnRequest + headers of every POST.
type fakeAgents struct {
	*httptest.Server
	mu sync.Mutex

	parts    []string
	manifest *string
	sever    bool // close without manifest/[DONE]

	gated   bool
	entered chan struct{}
	release chan struct{}

	turnStatus int // non-200 → pre-stream failure
	turnBody   string
	convStatus int
	convBody   string

	turnCount    int
	requests     []recordedTurn
	lastConvPath string
}

type recordedTurn struct {
	path    string
	headers http.Header
	req     agentsvc.TurnRequest
}

func newFakeAgents(t *testing.T) *fakeAgents {
	t.Helper()
	f := &fakeAgents{
		turnStatus: 200,
		convStatus: 200,
		convBody:   `{"messages":[{"role":"user","content":"hi"}]}`,
		entered:    make(chan struct{}, 1),
		release:    make(chan struct{}),
	}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/turns"):
			f.handleTurn(w, r)
		case r.Method == http.MethodGet:
			f.mu.Lock()
			f.lastConvPath = r.URL.Path
			status, body := f.convStatus, f.convBody
			f.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = fmt.Fprint(w, body)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(func() {
		f.mu.Lock()
		if f.gated {
			select {
			case <-f.release:
			default:
				close(f.release)
			}
		}
		f.mu.Unlock()
		f.Close()
	})
	return f
}

func (f *fakeAgents) handleTurn(w http.ResponseWriter, r *http.Request) {
	var req agentsvc.TurnRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	f.mu.Lock()
	f.turnCount++
	f.requests = append(f.requests, recordedTurn{path: r.URL.Path, headers: r.Header.Clone(), req: req})
	parts, manifest, sever, gated := f.parts, f.manifest, f.sever, f.gated
	status, body := f.turnStatus, f.turnBody
	f.mu.Unlock()

	if status != 200 {
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, body)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	fl, _ := w.(http.Flusher)
	emit := func(data string) {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		if fl != nil {
			fl.Flush()
		}
	}
	for _, p := range parts {
		emit(p)
	}
	if gated {
		select {
		case f.entered <- struct{}{}:
		default:
		}
		<-f.release
	}
	if sever {
		return // connection closes mid-turn: no manifest, no [DONE]
	}
	if manifest != nil {
		emit(*manifest)
	}
	emit("[DONE]")
}

func (f *fakeAgents) sentTurn(t *testing.T, i int) recordedTurn {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) <= i {
		t.Fatalf("agents saw %d turn request(s), want > %d", len(f.requests), i)
	}
	return f.requests[i]
}

func (f *fakeAgents) turns(t *testing.T) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.turnCount
}

// ---- in-memory turn repository ------------------------------------------------

// memTurnRepo mirrors the agent_turns semantics in memory for the component
// tier (the real partial-unique-index guard is covered by dbtest).
type memTurnRepo struct {
	mu   sync.Mutex
	rows []*models.AgentTurn
}

func (m *memTurnRepo) TryStart(_ context.Context, t *models.AgentTurn) (*models.AgentTurn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows {
		if r.OrgID == t.OrgID && r.ProjectID == t.ProjectID && r.Status == "running" {
			cp := *r
			return &cp, genai.ErrTurnActive
		}
	}
	t.ID = uuid.NewString()
	t.Status = "running"
	now := time.Now().UTC()
	t.CreatedAt, t.UpdatedAt, t.HeartbeatAt = now, now, now
	cp := *t
	m.rows = append(m.rows, &cp)
	return t, nil
}

func (m *memTurnRepo) Heartbeat(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows {
		if r.ID == id && r.Status == "running" {
			r.HeartbeatAt = time.Now().UTC()
		}
	}
	return nil
}

func (m *memTurnRepo) Finish(_ context.Context, id string, term genai.TurnTerminal) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows {
		if r.ID == id && r.Status == "running" {
			r.Status = term.Status
			r.CommitSHA = term.CommitSHA
			r.Reason = term.Reason
			r.NoChanges = term.NoChanges
			r.Message = term.Message
			if len(term.Paths) > 0 {
				b, _ := json.Marshal(term.Paths)
				r.Paths = string(b)
			}
			r.UpdatedAt = time.Now().UTC()
			return true, nil
		}
	}
	return false, nil
}

func (m *memTurnRepo) Get(_ context.Context, orgID, projectID, turnID string) (*models.AgentTurn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows {
		if r.OrgID == orgID && r.ProjectID == projectID && r.ID == turnID {
			cp := *r
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *memTurnRepo) GetActive(_ context.Context, orgID, projectID string) (*models.AgentTurn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows {
		if r.OrgID == orgID && r.ProjectID == projectID && r.Status == "running" {
			cp := *r
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *memTurnRepo) LastTerminal(_ context.Context, orgID, projectID, conversationID string) (*models.AgentTurn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var last *models.AgentTurn
	for _, r := range m.rows { // insertion order == creation order
		if r.OrgID == orgID && r.ProjectID == projectID && r.ConversationID == conversationID &&
			(r.Status == "completed" || r.Status == "failed") {
			last = r
		}
	}
	if last == nil {
		return nil, nil
	}
	cp := *last
	return &cp, nil
}

func (m *memTurnRepo) SweepStale(_ context.Context, olderThan time.Time) ([]models.AgentTurn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var swept []models.AgentTurn
	for _, r := range m.rows {
		if r.Status == "running" && r.HeartbeatAt.Before(olderThan) {
			r.Status = "failed"
			r.Reason = "stream-died"
			r.Message = "replica crashed or hung"
			swept = append(swept, *r)
		}
	}
	return swept, nil
}

func (m *memTurnRepo) row(t *testing.T, id string) models.AgentTurn {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows {
		if r.ID == id {
			return *r
		}
	}
	t.Fatalf("no row %s", id)
	return models.AgentTurn{}
}

// ---- faked credential edges ---------------------------------------------------

type stubRepoResolver struct{ rec *models.GitRepository }

func (s stubRepoResolver) GetRepo(_ context.Context, _, _ string) (*models.GitRepository, error) {
	if s.rec == nil {
		return nil, gitrepo.ErrRepoNotFound
	}
	return s.rec, nil
}

type stubCred struct{}

func (stubCred) Token(context.Context) (string, time.Time, error) {
	return "test-token", time.Time{}, nil
}
func (stubCred) Identity() credentials.Identity {
	return credentials.Identity{Name: "Bot", Email: "bot@aep.dev", Login: "bot"}
}
func (stubCred) RepoOwner() string                            { return "acme" }
func (stubCred) WebhookStrategy() credentials.WebhookStrategy { return credentials.WebhookPlatform }

type stubResolver struct{}

func (stubResolver) Resolve(context.Context, string) (credentials.Credential, error) {
	return stubCred{}, nil
}

// ---- harness -------------------------------------------------------------------

type genaiRig struct {
	h            *componenttest.Harness
	fx           *workspacetest.Fixture
	skillsOrigin *gittest.Remote
	fake         *fakeAgents
	turns        *memTurnRepo
	broker       *genai.TurnBroker
	// knobs read at request time
	key string
}

// rigOption tweaks the rig before the service is wired.
type rigOption func(*rigConfig)

type rigConfig struct {
	client     agentsvc.Client // overrides the default real-over-fake-HTTP client
	skillsRepo genai.SkillsRepoResolver
}

// withAgentsClient swaps the agents client (e.g. a panicking fake) — everything
// else in the rig stays real.
func withAgentsClient(c agentsvc.Client) rigOption {
	return func(rc *rigConfig) { rc.client = c }
}

// withSkillsRepo overrides the skills-repo resolver (e.g. to hand back a row
// whose backing repo is gone, exercising the skills-unavailable 503 path).
func withSkillsRepo(fn genai.SkillsRepoResolver) rigOption {
	return func(rc *rigConfig) { rc.skillsRepo = fn }
}

// newGenaiRig wires the real genai service over a real engine + origins and
// the scripted fake agents service.
func newGenaiRig(t *testing.T, seed map[string]string, opts ...rigOption) *genaiRig {
	t.Helper()
	var cfg rigConfig
	for _, o := range opts {
		o(&cfg)
	}
	fx := workspacetest.New(t, seed)
	skillsOrigin := gittest.NewRemote(t, gittest.WithSeed(map[string]string{
		"skills/high-level-architecture/SKILL.md": "---\nname: high-level-architecture\ndescription: d\nmetadata:\n  aep:\n    kind: platform\n---\nbody",
	}, "seed skills"))

	rec := &models.GitRepository{
		OrgID:         testOrg,
		ProjectID:     testProj,
		RepoURL:       fx.Origin.URL(),
		DefaultBranch: "main",
		Status:        "ready",
		RepoSlug:      workspacetest.DefaultSlug,
	}
	skillsRow := &models.GitRepository{
		OrgID:         testOrg,
		ProjectID:     models.SkillsRepoSentinelProjectID,
		RepoURL:       skillsOrigin.URL(),
		DefaultBranch: "main",
		Status:        "ready",
		RepoSlug:      "org-skills",
	}

	fake := newFakeAgents(t)
	turns := &memTurnRepo{}
	broker := genai.NewTurnBroker()
	rig := &genaiRig{fx: fx, skillsOrigin: skillsOrigin, fake: fake, turns: turns, broker: broker, key: "sk-ant-test"}

	var client agentsvc.Client = agentsvc.New(agentsvc.Config{BaseURL: fake.URL})
	if cfg.client != nil {
		client = cfg.client
	}
	skillsRepo := genai.SkillsRepoResolver(func(context.Context, string) (*models.GitRepository, error) {
		return skillsRow, nil
	})
	if cfg.skillsRepo != nil {
		skillsRepo = cfg.skillsRepo
	}
	svc := genai.NewService(genai.ServiceDeps{
		Repos:      stubRepoResolver{rec: rec},
		Git:        gitrepo.NewGitOpsService(stubResolver{}, fx.Engine),
		Keys:       func(context.Context, string) (string, error) { return rig.key, nil },
		Client:     client,
		Turns:      turns,
		Broker:     broker,
		Snapshots:  fx.Engine,
		SkillsRepo: skillsRepo,
	})
	rig.h = componenttest.New(t, componenttest.Options{Deps: api.HumaDeps{GenAISvc: svc}})
	return rig
}

func (r *genaiRig) post(t *testing.T, uuid, useCase, instruction string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"useCase": useCase, "instruction": instruction})
	return r.h.AsOrg(testOrg).Post(turnsPath(uuid), string(body))
}

// startTurn POSTs and returns the turnId from the 202 body.
func (r *genaiRig) startTurn(t *testing.T, uuid, useCase, instruction string) string {
	t.Helper()
	rec := r.post(t, uuid, useCase, instruction)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST turn: code %d, want 202 (%s)", rec.Code, rec.Body.String())
	}
	var out struct {
		TurnID string `json:"turnId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.TurnID == "" {
		t.Fatalf("202 body = %s (err %v)", rec.Body.String(), err)
	}
	return out.TurnID
}

// waitTerminal polls the status GET until the turn leaves running.
func (r *genaiRig) waitTerminal(t *testing.T, turnID string) genai.TurnStatus {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		rec := r.h.AsOrg(testOrg).Get(turnPath(turnID))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET turn: code %d (%s)", rec.Code, rec.Body.String())
		}
		var st genai.TurnStatus
		if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
			t.Fatalf("status body: %v (%s)", err, rec.Body.String())
		}
		if st.Status != "running" {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("turn never reached a terminal state")
	return genai.TurnStatus{}
}

// originGit runs a git command against the origin repo.
func (r *genaiRig) originGit(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"--git-dir", r.fx.Origin.Dir()}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// streamEvents GETs the stream (post-terminal — the recorder is synchronous)
// and parses it into (id, data) pairs plus whether [DONE] arrived.
func (r *genaiRig) streamEvents(t *testing.T, turnID, query string, hdr map[string]string) (events []sseEvent, done bool, code int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, turnPath(turnID)+"/stream"+query, nil)
	k, v := componenttest.ClaimsHeader(t, testOrg)
	req.Header.Set(k, v)
	for hk, hv := range hdr {
		req.Header.Set(hk, hv)
	}
	rec := httptest.NewRecorder()
	r.h.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		return nil, false, rec.Code
	}
	return parseSSE(t, rec.Body.String()), strings.Contains(rec.Body.String(), "data: [DONE]"), rec.Code
}

type sseEvent struct {
	id   int
	data string
}

func parseSSE(t *testing.T, body string) []sseEvent {
	t.Helper()
	var events []sseEvent
	cur := sseEvent{id: -1}
	sc := bufio.NewScanner(strings.NewReader(body))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "id: "):
			fmt.Sscanf(line, "id: %d", &cur.id)
		case strings.HasPrefix(line, "data: "):
			cur.data = strings.TrimPrefix(line, "data: ")
		case line == "":
			if cur.data != "" && cur.data != "[DONE]" {
				events = append(events, cur)
			}
			cur = sseEvent{id: -1}
		}
	}
	return events
}

// ---- exit-gate tests ------------------------------------------------------------

// Test202Flow_CommitLandsAndStreamReplays is the happy path: 202 → detached
// turn → fold verified against the manifest → ONE commit on origin with
// author=user / committer=bot → stream replay carries every part + the
// terminal (and never the manifest).
func Test202Flow_CommitLandsAndStreamReplays(t *testing.T) {
	r := newGenaiRig(t, map[string]string{"specs/requirements/requirements.md": "# Reqs\n"})
	baseRef := r.fx.Origin.HeadSHA(t)

	final := map[string]string{
		"specs/requirements/notes.md":        "# Notes\nBody\n",
		"specs/requirements/requirements.md": "# Requirements\n",
	}
	r.fake.parts = []string{
		textPart("working"),
		addFilePart("specs/requirements/notes.md", "# Notes\nBody\n"),
		editFilePart("specs/requirements/requirements.md", "# Reqs\n", "# Requirements\n"),
	}
	m := manifestPart(final, nil)
	r.fake.manifest = &m

	turnID := r.startTurn(t, convUUID, "requirements-chat", "tidy the requirements")
	st := r.waitTerminal(t, turnID)
	if st.Status != "completed" || st.NoChanges {
		t.Fatalf("terminal = %+v, want completed with changes", st)
	}
	if st.ConversationID != convUUID || st.UseCase != "requirements-chat" {
		t.Errorf("status identity fields = %+v", st)
	}

	// Commit visible on origin, exactly the folded content.
	head := r.fx.Origin.HeadSHA(t)
	if head == baseRef {
		t.Fatal("origin head did not advance")
	}
	if st.CommitSHA != head {
		t.Errorf("commitSha = %s, want origin head %s", st.CommitSHA, head)
	}
	for path, want := range final {
		if got := r.fx.Origin.FileAt(t, "main", path); got != want {
			t.Errorf("origin %s = %q, want %q", path, got, want)
		}
	}
	// D20 identities: author = prompting user, committer = bot (credential).
	if id := r.originGit(t, "log", "-1", "--format=%an|%ae|%cn|%ce"); id != "componenttest-user|componenttest-user@users.noreply.aep.dev|Bot|bot@aep.dev" {
		t.Errorf("commit identities = %q", id)
	}
	// D20 message convention.
	if msg := r.originGit(t, "log", "-1", "--format=%s"); msg != "chat(requirements): tidy the requirements" {
		t.Errorf("commit message = %q", msg)
	}

	// Dispatch carried the workspace shape (ref = base, skills head) and the
	// load-bearing X-Org-Id.
	sent := r.fake.sentTurn(t, 0)
	if sent.req.Workspace.Ref != baseRef {
		t.Errorf("dispatched ref = %s, want %s", sent.req.Workspace.Ref, baseRef)
	}
	if sent.req.Workspace.SkillsRef != r.skillsOrigin.HeadSHA(t) {
		t.Errorf("dispatched skillsRef = %s", sent.req.Workspace.SkillsRef)
	}
	if sent.req.Workspace.TurnID != turnID {
		t.Errorf("dispatched turnId = %s, want %s", sent.req.Workspace.TurnID, turnID)
	}
	wantConv := "org_" + testOrg + "--proj_" + testProj + "--requirements-chat--" + convUUID
	if sent.req.Workspace.ConversationID != wantConv || !strings.Contains(sent.path, wantConv) {
		t.Errorf("namespaced conversation = %q (path %q), want %q", sent.req.Workspace.ConversationID, sent.path, wantConv)
	}
	if o := sent.headers.Get("X-Org-Id"); o != testOrg {
		t.Errorf("X-Org-Id = %q", o)
	}
	if k := sent.headers.Get("X-Anthropic-Key"); k != "sk-ant-test" {
		t.Errorf("X-Anthropic-Key = %q", k)
	}
	if sent.req.FilesChangedExternally {
		t.Error("first turn must not carry filesChangedExternally")
	}
	if !strings.HasPrefix(sent.req.Instruction, "tidy the requirements") {
		t.Errorf("instruction = %q", sent.req.Instruction)
	}

	// Stream replay: every non-manifest part + terminal + [DONE], id-stamped.
	events, done, code := r.streamEvents(t, turnID, "", nil)
	if code != http.StatusOK || !done {
		t.Fatalf("stream: code %d done %v", code, done)
	}
	if len(events) != 4 { // 3 parts + terminal
		t.Fatalf("stream events = %d, want 4: %+v", len(events), events)
	}
	for i, ev := range events {
		if ev.id != i {
			t.Errorf("event %d has id %d", i, ev.id)
		}
		if strings.Contains(ev.data, `"manifest"`) {
			t.Errorf("manifest leaked into the client stream: %s", ev.data)
		}
	}
	var terminal struct {
		Type      string `json:"type"`
		CommitSHA string `json:"commitSha"`
		NoChanges bool   `json:"noChanges"`
	}
	if err := json.Unmarshal([]byte(events[3].data), &terminal); err != nil ||
		terminal.Type != "turn-committed" || terminal.CommitSHA != head || terminal.NoChanges {
		t.Errorf("terminal event = %s", events[3].data)
	}

	// Replay from an index (and via Last-Event-ID) trims the head.
	fromEvents, done2, _ := r.streamEvents(t, turnID, "?from=2", nil)
	if !done2 || len(fromEvents) != 2 || fromEvents[0].id != 2 {
		t.Errorf("from=2 replay = %+v done=%v", fromEvents, done2)
	}
	lastEvents, done3, _ := r.streamEvents(t, turnID, "", map[string]string{"Last-Event-ID": "1"})
	if !done3 || len(lastEvents) != 2 || lastEvents[0].id != 2 {
		t.Errorf("Last-Event-ID=1 replay = %+v done=%v", lastEvents, done3)
	}
	// from wins over Last-Event-ID.
	winEvents, _, _ := r.streamEvents(t, turnID, "?from=3", map[string]string{"Last-Event-ID": "0"})
	if len(winEvents) != 1 || winEvents[0].id != 3 {
		t.Errorf("from-wins replay = %+v", winEvents)
	}

	// Unknown turn id → 404 pre-stream.
	if _, _, code := r.streamEvents(t, uuid.NewString(), "", nil); code != http.StatusNotFound {
		t.Errorf("unknown turn stream: code %d, want 404", code)
	}
	if rec := r.h.AsOrg(testOrg).Get(turnPath(uuid.NewString())); rec.Code != http.StatusNotFound {
		t.Errorf("unknown turn status: code %d, want 404", rec.Code)
	}
}

// TestManifestGate_MismatchSeveredEmpty pins the D14 outcomes: hash mismatch →
// fold-parity (no commit); severed stream → stream-died (no commit); empty
// manifest → completed noChanges (no commit).
func TestManifestGate_MismatchSeveredEmpty(t *testing.T) {
	t.Run("hash mismatch", func(t *testing.T) {
		r := newGenaiRig(t, map[string]string{"specs/requirements/requirements.md": "# Reqs\n"})
		base := r.fx.Origin.HeadSHA(t)
		r.fake.parts = []string{addFilePart("specs/requirements/notes.md", "# Notes\n")}
		m := manifestPart(map[string]string{"specs/requirements/notes.md": "CORRUPTED content"}, nil)
		r.fake.manifest = &m

		st := r.waitTerminalOf(t, r.startTurn(t, convUUID, "requirements-chat", "x"))
		if st.Status != "failed" || st.Reason != "fold-parity" {
			t.Fatalf("terminal = %+v, want failed fold-parity", st)
		}
		if r.fx.Origin.HeadSHA(t) != base {
			t.Error("origin must be untouched on fold-parity failure")
		}
	})

	t.Run("severed stream", func(t *testing.T) {
		r := newGenaiRig(t, map[string]string{"specs/requirements/requirements.md": "# Reqs\n"})
		base := r.fx.Origin.HeadSHA(t)
		r.fake.parts = []string{addFilePart("specs/requirements/notes.md", "# Notes\n")}
		r.fake.sever = true

		st := r.waitTerminalOf(t, r.startTurn(t, convUUID, "requirements-chat", "x"))
		if st.Status != "failed" || st.Reason != "stream-died" {
			t.Fatalf("terminal = %+v, want failed stream-died", st)
		}
		if r.fx.Origin.HeadSHA(t) != base {
			t.Error("origin must be untouched on a severed stream")
		}
	})

	t.Run("empty manifest", func(t *testing.T) {
		r := newGenaiRig(t, map[string]string{"specs/requirements/requirements.md": "# Reqs\n"})
		base := r.fx.Origin.HeadSHA(t)
		r.fake.parts = []string{textPart("chat only")}
		m := manifestPart(nil, nil)
		r.fake.manifest = &m

		st := r.waitTerminalOf(t, r.startTurn(t, convUUID, "requirements-chat", "x"))
		if st.Status != "completed" || !st.NoChanges {
			t.Fatalf("terminal = %+v, want completed no-changes", st)
		}
		if st.CommitSHA != base {
			t.Errorf("no-changes commitSha = %s, want base %s", st.CommitSHA, base)
		}
		if r.fx.Origin.HeadSHA(t) != base {
			t.Error("no-changes turn must not commit")
		}
	})
}

func (r *genaiRig) waitTerminalOf(t *testing.T, turnID string) genai.TurnStatus {
	t.Helper()
	return r.waitTerminal(t, turnID)
}

// TestD15_ConcurrentBaseMovement pins disjoint→rebase, overlap→fail: an
// unrelated commit landing mid-turn does not kill the generation; a commit
// touching a folded path fails the turn with the conflicting path listed.
func TestD15_ConcurrentBaseMovement(t *testing.T) {
	t.Run("unrelated path rebases", func(t *testing.T) {
		r := newGenaiRig(t, map[string]string{"specs/requirements/requirements.md": "# Reqs\n"})
		r.fake.parts = []string{addFilePart("specs/requirements/notes.md", "# Notes\n")}
		m := manifestPart(map[string]string{"specs/requirements/notes.md": "# Notes\n"}, nil)
		r.fake.manifest = &m
		r.fake.gated = true

		turnID := r.startTurn(t, convUUID, "requirements-chat", "x")
		<-r.fake.entered
		// A concurrent save to an UNRELATED path lands while the turn streams.
		r.fx.Origin.Seed(t, map[string]string{"docs/unrelated.md": "external\n"}, "external apply")
		close(r.fake.release)

		st := r.waitTerminal(t, turnID)
		if st.Status != "completed" {
			t.Fatalf("terminal = %+v, want completed (disjoint rebase)", st)
		}
		// Both changes are on main.
		if got := r.fx.Origin.FileAt(t, "main", "docs/unrelated.md"); got != "external\n" {
			t.Errorf("external change lost: %q", got)
		}
		if got := r.fx.Origin.FileAt(t, "main", "specs/requirements/notes.md"); got != "# Notes\n" {
			t.Errorf("turn change lost: %q", got)
		}
	})

	t.Run("overlapping path fails base-moved", func(t *testing.T) {
		r := newGenaiRig(t, map[string]string{"specs/requirements/requirements.md": "# Reqs\n"})
		r.fake.parts = []string{editFilePart("specs/requirements/requirements.md", "# Reqs\n", "# Agent version\n")}
		m := manifestPart(map[string]string{"specs/requirements/requirements.md": "# Agent version\n"}, nil)
		r.fake.manifest = &m
		r.fake.gated = true

		turnID := r.startTurn(t, convUUID, "requirements-chat", "x")
		<-r.fake.entered
		// A concurrent save to the SAME path the fold touched.
		r.fx.Origin.Seed(t, map[string]string{"specs/requirements/requirements.md": "# Human version\n"}, "concurrent human edit")
		humanHead := r.fx.Origin.HeadSHA(t)
		close(r.fake.release)

		st := r.waitTerminal(t, turnID)
		if st.Status != "failed" || st.Reason != "base-moved" {
			t.Fatalf("terminal = %+v, want failed base-moved", st)
		}
		if len(st.Paths) != 1 || st.Paths[0] != "specs/requirements/requirements.md" {
			t.Errorf("conflicting paths = %v", st.Paths)
		}
		// The human edit survives; the agent version never landed.
		if r.fx.Origin.HeadSHA(t) != humanHead {
			t.Error("origin advanced past the human edit")
		}
	})
}

// TestD18_OneActiveTurnPerProject pins the guard: a second POST during a run
// 409s with the active turn id; after the terminal a new turn is admitted.
func TestD18_OneActiveTurnPerProject(t *testing.T) {
	r := newGenaiRig(t, map[string]string{"specs/requirements/requirements.md": "# Reqs\n"})
	r.fake.parts = []string{textPart("thinking")}
	m := manifestPart(nil, nil)
	r.fake.manifest = &m
	r.fake.gated = true

	turnID := r.startTurn(t, convUUID, "requirements-chat", "first")
	<-r.fake.entered

	// Second POST (any conversation, any use case) → 409 {turn_in_progress}.
	rec := r.post(t, "other-conv-uuid", "requirements-generate", "second")
	if rec.Code != http.StatusConflict {
		t.Fatalf("second POST: code %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
	var conflict struct {
		Code         string `json:"code"`
		ActiveTurnID string `json:"activeTurnId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &conflict); err != nil ||
		conflict.Code != "turn_in_progress" || conflict.ActiveTurnID != turnID {
		t.Fatalf("409 body = %s", rec.Body.String())
	}

	// GET /turns/active while running → 200 with the same shape.
	active := r.h.AsOrg(testOrg).Get(turnPath("active"))
	if active.Code != http.StatusOK {
		t.Fatalf("active: code %d (%s)", active.Code, active.Body.String())
	}
	var activeSt genai.TurnStatus
	_ = json.Unmarshal(active.Body.Bytes(), &activeSt)
	if activeSt.TurnID != turnID || activeSt.Status != "running" {
		t.Errorf("active = %+v", activeSt)
	}

	close(r.fake.release)
	r.waitTerminal(t, turnID)

	// Guard released: active → 204, next POST admitted.
	if rec := r.h.AsOrg(testOrg).Get(turnPath("active")); rec.Code != http.StatusNoContent {
		t.Errorf("active after terminal: code %d, want 204", rec.Code)
	}
	r.fake.mu.Lock()
	r.fake.gated = false
	r.fake.mu.Unlock()
	next := r.startTurn(t, convUUID, "requirements-chat", "second try")
	if st := r.waitTerminal(t, next); st.Status != "completed" {
		t.Errorf("post-release turn = %+v", st)
	}
}

// TestD19_DesignGateAndHeadRead pins the design gate: no requirements v-tag →
// 409 requirements_not_approved and agents never dispatched; with a tag the
// turn proceeds reading HEAD (not the tagged sha) and stamps SpecTag.
func TestD19_DesignGateAndHeadRead(t *testing.T) {
	t.Run("no tag → 409", func(t *testing.T) {
		r := newGenaiRig(t, map[string]string{"specs/requirements/requirements.md": "# Reqs\n"})
		rec := r.post(t, convUUID, "design-generate", "design it")
		if rec.Code != http.StatusConflict {
			t.Fatalf("no-tag design POST: code %d, want 409 (%s)", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"requirements_not_approved"`) {
			t.Errorf("409 body = %s", rec.Body.String())
		}
		if r.fake.turns(t) != 0 {
			t.Error("agents dispatched despite failed gate")
		}
	})

	t.Run("tagged → proceeds at HEAD with SpecTag", func(t *testing.T) {
		r := newGenaiRig(t, map[string]string{"specs/requirements/requirements.md": "# Approved reqs\n"})
		r.fx.Origin.Tag(t, "v1", "Requirements v1")
		// HEAD moves past the approved tag — the turn reads HEAD (D19).
		r.fx.Origin.Seed(t, map[string]string{"specs/requirements/requirements.md": "# Newer than v1\n"}, "post-approval edit")
		head := r.fx.Origin.HeadSHA(t)

		r.fake.parts = []string{textPart("designing")}
		m := manifestPart(nil, nil)
		r.fake.manifest = &m

		turnID := r.startTurn(t, convUUID, "design-generate", "design it")
		st := r.waitTerminal(t, turnID)
		if st.Status != "completed" {
			t.Fatalf("terminal = %+v", st)
		}
		if got := r.fake.sentTurn(t, 0).req.Workspace.Ref; got != head {
			t.Errorf("design turn dispatched ref %s, want HEAD %s (no tag pinning)", got, head)
		}
		if row := r.turns.row(t, turnID); row.SpecTag != "v1" {
			t.Errorf("SpecTag = %q, want v1", row.SpecTag)
		}
	})
}

// TestD20_FilesChangedExternallyAndDivergenceNote pins the server-derived
// flag: a second turn in the same conversation after main moved externally
// dispatches filesChangedExternally=true; after a FAILED turn the divergence
// note is prepended.
func TestD20_FilesChangedExternallyAndDivergenceNote(t *testing.T) {
	r := newGenaiRig(t, map[string]string{"specs/requirements/requirements.md": "# Reqs\n"})
	m := manifestPart(nil, nil)
	r.fake.parts = []string{textPart("ok")}
	r.fake.manifest = &m

	// Turn 1 completes (no changes).
	r.waitTerminal(t, r.startTurn(t, convUUID, "requirements-chat", "one"))

	// An external Apply advances main behind the conversation's back.
	r.fx.Origin.Seed(t, map[string]string{"specs/requirements/requirements.md": "# Externally edited\n"}, "external")

	// Turn 2, same conversation → filesChangedExternally=true, no note.
	r.waitTerminal(t, r.startTurn(t, convUUID, "requirements-chat", "two"))
	second := r.fake.sentTurn(t, 1)
	if !second.req.FilesChangedExternally {
		t.Error("second dispatch must carry filesChangedExternally=true")
	}
	if strings.Contains(second.req.Instruction, "were NOT applied") {
		t.Error("completed prior turn must not add the divergence note")
	}

	// Turn 3 fails (severed) → turn 4 carries the divergence note.
	r.fake.mu.Lock()
	r.fake.sever = true
	r.fake.mu.Unlock()
	if st := r.waitTerminal(t, r.startTurn(t, convUUID, "requirements-chat", "three")); st.Status != "failed" {
		t.Fatalf("turn 3 = %+v, want failed", st)
	}
	r.fake.mu.Lock()
	r.fake.sever = false
	r.fake.mu.Unlock()
	r.waitTerminal(t, r.startTurn(t, convUUID, "requirements-chat", "four"))
	fourth := r.fake.sentTurn(t, 3)
	if !strings.HasPrefix(fourth.req.Instruction, "Note: your previous turn's changes were NOT applied; the workspace reflects the repository state.") {
		t.Errorf("divergence note missing: %q", fourth.req.Instruction)
	}
}

// TestLiveAttach_SecondViewerTails attaches a real SSE client mid-stream (the
// D16/D18 viewer): it receives the already-buffered parts, live-tails the
// rest, and sees the terminal + [DONE].
func TestLiveAttach_SecondViewerTails(t *testing.T) {
	r := newGenaiRig(t, map[string]string{"specs/requirements/requirements.md": "# Reqs\n"})
	r.fake.parts = []string{textPart("before-gate")}
	m := manifestPart(nil, nil)
	r.fake.manifest = &m
	r.fake.gated = true

	turnID := r.startTurn(t, convUUID, "requirements-chat", "x")
	<-r.fake.entered

	srv := httptest.NewServer(r.h.Handler)
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+turnPath(turnID)+"/stream", nil)
	k, v := componenttest.ClaimsHeader(t, testOrg)
	req.Header.Set(k, v)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("attach: code %d", resp.StatusCode)
	}

	// Read the replayed part, then release the gate and read to [DONE].
	lines := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	waitLine := func(want string) {
		t.Helper()
		deadline := time.After(10 * time.Second)
		for {
			select {
			case line, ok := <-lines:
				if !ok {
					t.Fatalf("stream closed before %q", want)
				}
				if strings.Contains(line, want) {
					return
				}
			case <-deadline:
				t.Fatalf("timed out waiting for %q", want)
			}
		}
	}
	waitLine("before-gate")
	close(r.fake.release)
	waitLine("turn-committed")
	waitLine("[DONE]")
}

// TestPre202Failures pins the pre-202 4xx contract: bad use case / bad
// conversation id / missing Anthropic key — agents is never dispatched, no
// turn row is created.
func TestPre202Failures(t *testing.T) {
	r := newGenaiRig(t, map[string]string{"specs/requirements/requirements.md": "# Reqs\n"})

	if rec := r.post(t, convUUID, "bogus", "x"); rec.Code != http.StatusBadRequest && rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid useCase: code %d, want 4xx", rec.Code)
	}
	if rec := r.post(t, "a--b", "requirements-chat", "x"); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid conversation id: code %d, want 400", rec.Code)
	}
	r.key = ""
	if rec := r.post(t, convUUID, "requirements-generate", "x"); rec.Code != http.StatusBadRequest {
		t.Errorf("missing key: code %d, want 400", rec.Code)
	}
	if r.fake.turns(t) != 0 {
		t.Error("agents dispatched despite pre-202 failures")
	}
	if rec := r.h.AsOrg(testOrg).Get(turnPath("active")); rec.Code != http.StatusNoContent {
		t.Errorf("no rows should exist: active = %d, want 204", rec.Code)
	}
}

// TestTurnStatus_CrossOrg404 pins the row fence: another org cannot read a
// foreign turn's status or stream.
func TestTurnStatus_CrossOrg404(t *testing.T) {
	r := newGenaiRig(t, map[string]string{"specs/requirements/requirements.md": "# Reqs\n"})
	m := manifestPart(nil, nil)
	r.fake.parts = []string{textPart("ok")}
	r.fake.manifest = &m
	turnID := r.startTurn(t, convUUID, "requirements-chat", "x")
	r.waitTerminal(t, turnID)

	if rec := r.h.AsOrg("other-org").Get(turnPath(turnID)); rec.Code != http.StatusNotFound {
		t.Errorf("foreign status: code %d, want 404", rec.Code)
	}
	req := httptest.NewRequest(http.MethodGet, turnPath(turnID)+"/stream", nil)
	k, v := componenttest.ClaimsHeader(t, "other-org")
	req.Header.Set(k, v)
	rec := httptest.NewRecorder()
	r.h.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("foreign stream: code %d, want 404", rec.Code)
	}
}

// ---- rehydrate (unchanged surface) ----------------------------------------------

func TestRehydrate_ChatMessages(t *testing.T) {
	r := newGenaiRig(t, map[string]string{"specs/requirements/requirements.md": "# Reqs\n"})
	rec := r.h.AsOrg(testOrg).Get(convPath(convUUID))
	if rec.Code != http.StatusOK {
		t.Fatalf("rehydrate code %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"messages"`) {
		t.Errorf("rehydrate body = %s", rec.Body.String())
	}
	// Rehydrate reconstructs the id under the requirements-chat use case.
	wantPath := "/conversations/org_" + testOrg + "--proj_" + testProj + "--requirements-chat--" + convUUID
	r.fake.mu.Lock()
	gotPath := r.fake.lastConvPath
	r.fake.mu.Unlock()
	if gotPath != wantPath {
		t.Errorf("rehydrate path = %q, want %q", gotPath, wantPath)
	}
}

func TestRehydrate_CrossTenantOrUnknown_404(t *testing.T) {
	r := newGenaiRig(t, map[string]string{"specs/requirements/requirements.md": "# Reqs\n"})
	r.fake.mu.Lock()
	r.fake.convStatus = http.StatusNotFound
	r.fake.mu.Unlock()
	if rec := r.h.AsOrg(testOrg).Get(convPath(convUUID)); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown conversation: code %d, want 404", rec.Code)
	}
}

func TestGenAI_NoAuth_401(t *testing.T) {
	r := newGenaiRig(t, map[string]string{"specs/requirements/requirements.md": "# Reqs\n"})
	if rec := r.h.NoAuth().Get(convPath(convUUID)); rec.Code != http.StatusUnauthorized {
		t.Errorf("no-auth rehydrate: code %d, want 401", rec.Code)
	}
	if rec := r.h.NoAuth().Get(turnPath("active")); rec.Code != http.StatusUnauthorized {
		t.Errorf("no-auth active: code %d, want 401", rec.Code)
	}
}

// ---- review fixes (Go G1) -------------------------------------------------------

// panicClient is an agents client whose Turn panics on the detached turn path —
// it exercises the runTurn panic barrier without needing a real fold/parser to
// blow up.
type panicClient struct{}

func (panicClient) Turn(context.Context, string, string, string, agentsvc.TurnRequest) (io.ReadCloser, error) {
	panic("boom on the turn path")
}
func (panicClient) GetConversation(context.Context, string, string) (json.RawMessage, error) {
	return nil, nil
}

// TestPanicBarrier_TurnFailsAndGuardReleases pins the detached-goroutine panic
// barrier: a panic on the turn path does NOT crash the process — the turn is
// failed (reason "internal", message "turn runner panicked"), main is untouched,
// the D18 one-active guard releases, and a subsequent turn on the same project
// is admitted (and its own panic is likewise contained).
func TestPanicBarrier_TurnFailsAndGuardReleases(t *testing.T) {
	r := newGenaiRig(t, map[string]string{"specs/requirements/requirements.md": "# Reqs\n"},
		withAgentsClient(panicClient{}))
	base := r.fx.Origin.HeadSHA(t)

	turnID := r.startTurn(t, convUUID, "requirements-chat", "trigger a panic")
	st := r.waitTerminal(t, turnID)
	if st.Status != "failed" || st.Reason != "internal" {
		t.Fatalf("terminal = %+v, want failed internal", st)
	}
	if st.Message != "turn runner panicked" {
		t.Errorf("message = %q, want %q", st.Message, "turn runner panicked")
	}
	if r.fx.Origin.HeadSHA(t) != base {
		t.Error("origin must be untouched after a panicked turn")
	}

	// Guard released: a fresh POST on the same project is admitted (startTurn
	// fatals on anything but 202), and the barrier contains its panic too.
	next := r.startTurn(t, convUUID, "requirements-chat", "after the panic")
	if st := r.waitTerminal(t, next); st.Status != "failed" {
		t.Errorf("second turn = %+v, want failed (barrier repeatable)", st)
	}
}

// TestEmptyInstruction_400NoRow pins the synchronous empty-instruction reject: a
// whitespace-only instruction is 400 pre-202 — agents is never dispatched, no
// turn row is created, and the D18 guard is untaken (active → 204).
func TestEmptyInstruction_400NoRow(t *testing.T) {
	r := newGenaiRig(t, map[string]string{"specs/requirements/requirements.md": "# Reqs\n"})

	if rec := r.post(t, convUUID, "requirements-chat", "  \t\n  "); rec.Code != http.StatusBadRequest {
		t.Fatalf("whitespace instruction: code %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
	if r.fake.turns(t) != 0 {
		t.Error("agents dispatched despite an empty instruction")
	}
	if rec := r.h.AsOrg(testOrg).Get(turnPath("active")); rec.Code != http.StatusNoContent {
		t.Errorf("no row should exist: active = %d, want 204", rec.Code)
	}
}
