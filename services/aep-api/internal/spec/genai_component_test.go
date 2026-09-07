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

// Component tier for the committed-truth turn surface (shared-workspace-volume
// Phase 4 exit gate): the REAL contract-first strict handler (componenttest)
// fronting the REAL genai service — turn repository semantics faked in memory
// (the D18 guard's DB tier is covered by dbtest), the workspace engine REAL
// over real file:// origins (workspacetest), and a scripted fake agents SSE
// server (incl. the D14 manifest frames) recording the exact TurnRequest the
// BFF dispatches.
package spec_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wso2/aep/aep-api/internal/clients/agentsvc"
	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/edge"
	"github.com/wso2/aep/aep-api/internal/platform/componenttest"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs/workspacetest"
	"github.com/wso2/aep/aep-api/internal/platform/gittest"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
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

// The agents-side write ledger emits the verdict frame right behind every
// mutation tool-call, and the fold applies a write only on that verdict's
// ok:true (ADR-0021) — so each helper carries the pair: two `data:` frames in
// one script entry, exactly as the stream would.
func withVerdict(call []byte, id, tool, op, path string) string {
	v, _ := json.Marshal(map[string]any{
		"type": "tool-result", "toolCallId": id, "toolName": tool,
		"output": map[string]any{"ok": true, "path": path, "op": op, "status": "applied"},
	})
	return string(call) + "\n\ndata: " + string(v)
}

func addFilePart(path, content string) string {
	b, _ := json.Marshal(map[string]any{
		"type": "tool-call", "toolCallId": "t-" + path, "toolName": "addFile",
		"input": map[string]string{"path": path, "content": content},
	})
	return withVerdict(b, "t-"+path, "addFile", "add", path)
}

func editFilePart(path, oldString, newString string) string {
	b, _ := json.Marshal(map[string]any{
		"type": "tool-call", "toolCallId": "e-" + path, "toolName": "editFile",
		"input": map[string]string{"path": path, "oldString": oldString, "newString": newString},
	})
	return withVerdict(b, "e-"+path, "editFile", "edit", path)
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

	// preHeaderGate blocks the handler BEFORE any response header is written —
	// the shape of a slow time-to-first-token (a big PDF/image the model must
	// read before emitting anything). Dispatch is still blocked at this point.
	preHeaderGate    bool
	preHeaderEntered chan struct{}
	preHeaderRelease chan struct{}

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
		turnStatus:       200,
		convStatus:       200,
		convBody:         `{"messages":[{"role":"user","content":"hi"}]}`,
		entered:          make(chan struct{}, 1),
		release:          make(chan struct{}),
		preHeaderEntered: make(chan struct{}, 1),
		preHeaderRelease: make(chan struct{}),
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
	preHeader := f.preHeaderGate
	f.mu.Unlock()

	if preHeader {
		select {
		case f.preHeaderEntered <- struct{}{}:
		default:
		}
		<-f.preHeaderRelease
	}

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
	rows []*spec.AgentTurn
}

func (m *memTurnRepo) SumUsageByProject(context.Context, string) (map[string]contracts.StampedUsage, error) {
	return nil, nil
}

func (m *memTurnRepo) TryStart(_ context.Context, t *spec.AgentTurn) (*spec.AgentTurn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows {
		if r.OrgID == t.OrgID && r.ProjectID == t.ProjectID && r.Status == "running" {
			cp := *r
			return &cp, spec.ErrTurnActive
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

func (m *memTurnRepo) Finish(_ context.Context, id string, term spec.TurnTerminal) (bool, error) {
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

func (m *memTurnRepo) Get(_ context.Context, orgID, projectID, turnID string) (*spec.AgentTurn, error) {
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

func (m *memTurnRepo) GetActive(_ context.Context, orgID, projectID string) (*spec.AgentTurn, error) {
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

func (m *memTurnRepo) NewestCompletedFlow(_ context.Context, orgID, projectID, flow string) (*spec.AgentTurn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var newest *spec.AgentTurn
	for _, r := range m.rows { // insertion order == creation order
		if r.OrgID == orgID && r.ProjectID == projectID && r.Flow == flow && r.Status == "completed" {
			newest = r
		}
	}
	if newest == nil {
		return nil, nil
	}
	cp := *newest
	return &cp, nil
}

func (m *memTurnRepo) Newest(_ context.Context, orgID, projectID string) (*spec.AgentTurn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var newest *spec.AgentTurn
	for _, r := range m.rows { // insertion order == creation order
		if r.OrgID == orgID && r.ProjectID == projectID {
			newest = r
		}
	}
	if newest == nil {
		return nil, nil
	}
	cp := *newest
	return &cp, nil
}

func (m *memTurnRepo) LastTerminal(_ context.Context, orgID, projectID, conversationID string) (*spec.AgentTurn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var last *spec.AgentTurn
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

func (m *memTurnRepo) SweepStale(_ context.Context, olderThan time.Time) ([]spec.AgentTurn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var swept []spec.AgentTurn
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

func (m *memTurnRepo) row(t *testing.T, id string) spec.AgentTurn {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows {
		if r.ID == id {
			return *r
		}
	}
	t.Fatalf("no row %s", id)
	return spec.AgentTurn{}
}

// ---- faked credential edges ---------------------------------------------------

type stubRepoResolver struct{ rec *sourcecontrol.GitRepository }

func (s stubRepoResolver) GetRepo(_ context.Context, _, projectID string) (*sourcecontrol.GitRepository, error) {
	// Match on project id so a synthetic Marketplace id does not silently
	// receive the fixture repo (that harness lie hid the live 404).
	if s.rec == nil || projectID != s.rec.ProjectID {
		return nil, sourcecontrol.ErrRepoNotFound
	}
	return s.rec, nil
}

type stubCred struct{}

func (stubCred) Token(context.Context) (string, time.Time, error) {
	return "test-token", time.Time{}, nil
}
func (stubCred) Identity() secrets.Identity {
	return secrets.Identity{Name: "Bot", Email: "bot@aep.dev", Login: "bot"}
}
func (stubCred) RepoOwner() string                        { return "acme" }
func (stubCred) WebhookStrategy() secrets.WebhookStrategy { return secrets.WebhookPlatform }

type stubResolver struct{}

func (stubResolver) Resolve(context.Context, string) (secrets.Credential, error) {
	return stubCred{}, nil
}

// ---- harness -------------------------------------------------------------------

type genaiRig struct {
	h            *componenttest.Harness
	fx           *workspacetest.Fixture
	skillsOrigin *gittest.Remote
	fake         *fakeAgents
	turns        *memTurnRepo
	broker       *spec.TurnBroker
	svc          *spec.Service
	// knobs read at request time
	key string
}

// rigOption tweaks the rig before the service is wired.
type rigOption func(*rigConfig)

type rigConfig struct {
	client        agentsvc.Client // overrides the default real-over-fake-HTTP client
	skillsRepo    spec.SkillsRepoResolver
	mcpTokens     spec.MCPTokenMinter
	mcpBaseURL    string
	recorder      spec.TurnActivityRecorder
	conversations spec.ConversationRepository
	repos         spec.RepoResolver
	snapshots     sourcecontrol.SnapshotProvider
}

// withConversations wires the #430 thread store so the resolve/rotate endpoints
// and the conversation_rotated admission fence can be exercised (nil skips the
// fence, keeping the pre-#430 tests' arbitrary conversation uuids valid).
func withConversations(repo spec.ConversationRepository) rigOption {
	return func(c *rigConfig) { c.conversations = repo }
}

// withRepos swaps the project-repo resolver (e.g. a counting stub that 404s
// the synthetic Marketplace id while still serving testProj).
func withRepos(r spec.RepoResolver) rigOption {
	return func(c *rigConfig) { c.repos = r }
}

// withSnapshots swaps the snapshot engine (e.g. Ensure returns disk admission
// so StartTurn can be asserted as 503, not opaque 500).
func withSnapshots(p sourcecontrol.SnapshotProvider) rigOption {
	return func(c *rigConfig) { c.snapshots = p }
}

// withRecorder wires an activity recorder so a committed turn's spec_updated
// line can be asserted.
func withRecorder(r spec.TurnActivityRecorder) rigOption {
	return func(rc *rigConfig) { rc.recorder = r }
}

// withAgentsClient swaps the agents client (e.g. a panicking fake) — everything
// else in the rig stays real.
func withAgentsClient(c agentsvc.Client) rigOption {
	return func(rc *rigConfig) { rc.client = c }
}

// withSkillsRepo overrides the skills-repo resolver (e.g. to hand back a row
// whose backing repo is gone, exercising the skills-unavailable 503 path).
func withSkillsRepo(fn spec.SkillsRepoResolver) rigOption {
	return func(rc *rigConfig) { rc.skillsRepo = fn }
}

// MCP discovery wiring is opt-in: the default rig leaves MCPTokens/MCPBaseURL
// unset (matching a BFF whose internal MCP surface is unconfigured — every turn
// dispatches without an MCP block), and withMCP turns it on for the discovery
// gate tests.
const (
	testMCPBaseURL = "http://bff.internal"
	testMCPToken   = "mcp-stub-token"
)

// stubMinter is an MCPTokenMinter that always returns a fixed token.
type stubMinter struct{ token string }

func (m stubMinter) IssueMCPToken(string) (string, error) { return m.token, nil }

// withMCP wires the MCP discovery deps (a fixed-token minter + a base URL) so a
// dispatched turn's MCP block can be asserted.
func withMCP() rigOption {
	return func(rc *rigConfig) {
		rc.mcpTokens = stubMinter{token: testMCPToken}
		rc.mcpBaseURL = testMCPBaseURL
	}
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
		"skills/architecture/SKILL.md": "---\nname: architecture\ndescription: d\nmetadata:\n  aep:\n    kind: platform\n---\nbody",
	}, "seed skills"))

	rec := &sourcecontrol.GitRepository{
		OrgID:         testOrg,
		ProjectID:     testProj,
		RepoURL:       fx.Origin.URL(),
		DefaultBranch: "main",
		Status:        "ready",
		RepoSlug:      workspacetest.DefaultSlug,
	}
	skillsRow := &sourcecontrol.GitRepository{
		OrgID:         testOrg,
		ProjectID:     spec.SkillsRepoSentinelProjectID,
		RepoURL:       skillsOrigin.URL(),
		DefaultBranch: "main",
		Status:        "ready",
		RepoSlug:      "org-skills",
	}

	fake := newFakeAgents(t)
	turns := &memTurnRepo{}
	broker := spec.NewTurnBroker()
	rig := &genaiRig{fx: fx, skillsOrigin: skillsOrigin, fake: fake, turns: turns, broker: broker, key: "sk-ant-test"}

	var client agentsvc.Client = agentsvc.New(agentsvc.Config{BaseURL: fake.URL})
	if cfg.client != nil {
		client = cfg.client
	}
	skillsRepo := spec.SkillsRepoResolver(func(context.Context, string) (*sourcecontrol.GitRepository, error) {
		return skillsRow, nil
	})
	if cfg.skillsRepo != nil {
		skillsRepo = cfg.skillsRepo
	}
	repos := spec.RepoResolver(stubRepoResolver{rec: rec})
	if cfg.repos != nil {
		repos = cfg.repos
	}
	snapshots := sourcecontrol.SnapshotProvider(fx.Engine)
	if cfg.snapshots != nil {
		snapshots = cfg.snapshots
	}
	svc := spec.NewService(spec.ServiceDeps{
		Repos:         repos,
		Git:           sourcecontrol.NewGitOpsService(stubResolver{}, fx.Engine),
		Keys:          func(context.Context, string) (string, error) { return rig.key, nil },
		Client:        client,
		Turns:         turns,
		Broker:        broker,
		Snapshots:     snapshots,
		SkillsRepo:    skillsRepo,
		Conversations: cfg.conversations,
		MCPTokens:     cfg.mcpTokens,
		MCPBaseURL:    cfg.mcpBaseURL,
		Recorder:      cfg.recorder,
	})
	rig.svc = svc
	rig.h = componenttest.New(t, componenttest.Options{Deps: edge.Deps{Spec: mustSpecHandlers(t, spec.Deps{GenAI: svc})}})
	return rig
}

func (r *genaiRig) post(t *testing.T, uuid, _, instruction string) *httptest.ResponseRecorder {
	t.Helper()
	// The old per-flow "useCase" field is gone from the wire (#373) — flows
	// ride /<skill> instructions; the middle parameter is retained only to
	// keep call sites readable and is ignored.
	payload := map[string]any{"instruction": instruction}
	body, _ := json.Marshal(payload)
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
func (r *genaiRig) waitTerminal(t *testing.T, turnID string) spec.TurnStatus {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		rec := r.h.AsOrg(testOrg).Get(turnPath(turnID))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET turn: code %d (%s)", rec.Code, rec.Body.String())
		}
		var st spec.TurnStatus
		if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
			t.Fatalf("status body: %v (%s)", err, rec.Body.String())
		}
		if st.Status != "running" {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("turn never reached a terminal state")
	return spec.TurnStatus{}
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

// Test202Flow_PreviewOnlyAndStreamReplays is the happy path: 202 → detached
// turn → fold verified against the manifest → preview-only terminal (#373:
// genai turns never commit; room turns persist via the collab save path) →
// stream replay carries every part + the terminal (and never the manifest).
func Test202Flow_PreviewOnlyAndStreamReplays(t *testing.T) {
	r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"})
	baseRef := r.fx.Origin.HeadSHA(t)

	final := map[string]string{
		"specs/requirements/notes.md": "# Notes\nBody\n",
		"specs/requirements/prd.md":   "# Requirements\n",
	}
	r.fake.parts = []string{
		textPart("working"),
		addFilePart("specs/requirements/notes.md", "# Notes\nBody\n"),
		editFilePart("specs/requirements/prd.md", "# Reqs\n", "# Requirements\n"),
	}
	m := manifestPart(final, nil)
	r.fake.manifest = &m

	turnID := r.startTurn(t, convUUID, "", "tidy the requirements")
	st := r.waitTerminal(t, turnID)
	// Preview-only (#373): the fold streams for display, nothing is committed —
	// the terminal reports a no-op completion pinned to the base sha.
	if st.Status != "completed" || !st.NoChanges {
		t.Fatalf("terminal = %+v, want completed preview-only", st)
	}
	if st.ConversationID != convUUID || st.UseCase != "general" {
		t.Errorf("status identity fields = %+v", st)
	}
	if head := r.fx.Origin.HeadSHA(t); head != baseRef {
		t.Fatal("preview-only turn must not advance origin")
	}
	if st.CommitSHA != baseRef {
		t.Errorf("commitSha = %s, want base %s", st.CommitSHA, baseRef)
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
	// #580: every turn the BFF dispatches is read in the console, so it names
	// that surface and the agents service inlines the console's narration rules.
	// Unset, the agent narrates repo paths at someone who cannot see a file tree.
	if sent.req.Surface != agentsvc.SurfaceConsole {
		t.Errorf("dispatched surface = %q, want %q", sent.req.Surface, agentsvc.SurfaceConsole)
	}
	wantConv := "org_" + testOrg + "--proj_" + testProj + "--general--" + convUUID
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
	if sent.req.Turn.Kind != agentsvc.TurnKindChat || sent.req.Turn.Text != "tidy the requirements" {
		t.Errorf("turn = %+v, want the user's text as a chat turn", sent.req.Turn)
	}
	// The journal (#463) carries the raw client-sent text — what the sender's
	// UI rendered as the user bubble — so rehydrate shows the same thing.
	if sent.req.Journal == nil || sent.req.Journal.Text != "tidy the requirements" {
		t.Errorf("journal = %+v, want the raw instruction", sent.req.Journal)
	}

	// Stream replay: every non-manifest part + terminal + [DONE], id-stamped.
	events, done, code := r.streamEvents(t, turnID, "", nil)
	if code != http.StatusOK || !done {
		t.Fatalf("stream: code %d done %v", code, done)
	}
	if len(events) != 6 { // 3 parts + terminal
		t.Fatalf("stream events = %d, want 6 (each write rides with its verdict frame): %+v", len(events), events)
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
	if err := json.Unmarshal([]byte(events[5].data), &terminal); err != nil ||
		terminal.Type != "turn-committed" || terminal.CommitSHA != baseRef || !terminal.NoChanges {
		t.Errorf("terminal event = %s", events[5].data)
	}

	// Replay from an index trims the head.
	fromEvents, done2, _ := r.streamEvents(t, turnID, "?from=2", nil)
	if !done2 || len(fromEvents) != 4 || fromEvents[0].id != 2 {
		t.Errorf("from=2 replay = %+v done=%v", fromEvents, done2)
	}
	// Last-Event-ID names the last RECEIVED event — replay resumes at the
	// next index (the SSE auto-reconnect contract, declared as a header param).
	lastEvents, done3, _ := r.streamEvents(t, turnID, "", map[string]string{"Last-Event-ID": "1"})
	if !done3 || len(lastEvents) != 4 || lastEvents[0].id != 2 {
		t.Errorf("Last-Event-ID=1 replay = %+v done=%v", lastEvents, done3)
	}
	// from still wins over (the unread) Last-Event-ID.
	winEvents, _, _ := r.streamEvents(t, turnID, "?from=3", map[string]string{"Last-Event-ID": "0"})
	if len(winEvents) != 3 || winEvents[0].id != 3 {
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

// TestGenericTurn_NoUseCase pins the no-useCase path: an OMITTED "useCase" runs
// the internal general turn — generic steering (naming neither requirements nor
// design), the `--general--` conversation namespace, the served general use
// case — and, crucially, it COMMITS NOTHING: the fold streams for display but
// main never advances.
func TestGenericTurn_NoUseCase(t *testing.T) {
	r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"})
	baseRef := r.fx.Origin.HeadSHA(t)

	// The agent proposes a domain-model.md edit; the manifest vouches for it. A
	// committing use case would land it — the general turn must not.
	r.fake.parts = []string{
		textPart("working"),
		addFilePart("specs/design/domain-model.md", "# Design\n"),
	}
	m := manifestPart(map[string]string{"specs/design/domain-model.md": "# Design\n"}, nil)
	r.fake.manifest = &m

	// useCase "" → the post helper omits the field entirely.
	turnID := r.startTurn(t, convUUID, "", "touch the design")
	st := r.waitTerminal(t, turnID)

	// Completes WITHOUT committing: reported like a no-op (base sha pinned).
	if st.Status != "completed" || !st.NoChanges {
		t.Fatalf("terminal = %+v, want completed with noChanges", st)
	}
	if st.UseCase != "general" {
		t.Errorf("status useCase = %q, want general", st.UseCase)
	}
	if st.CommitSHA != baseRef {
		t.Errorf("commitSha = %s, want base %s (no commit)", st.CommitSHA, baseRef)
	}
	// The strongest proof: origin main never advanced.
	if head := r.fx.Origin.HeadSHA(t); head != baseRef {
		t.Errorf("origin head advanced to %s — a generic turn must not commit", head)
	}

	// Dispatch still carries the general namespace + generic steering.
	sent := r.fake.sentTurn(t, 0)
	wantConv := "org_" + testOrg + "--proj_" + testProj + "--general--" + convUUID
	if sent.req.Workspace.ConversationID != wantConv || !strings.Contains(sent.path, wantConv) {
		t.Errorf("namespaced conversation = %q (path %q), want %q", sent.req.Workspace.ConversationID, sent.path, wantConv)
	}
	// The BFF states facts and stops: no steering text of any kind rides the
	// wire now (the spec-paths rule is the agents service's to append). The
	// retired steers cannot come back through this door.
	if sent.req.Turn.Kind != agentsvc.TurnKindChat || sent.req.Turn.Text != "touch the design" {
		t.Errorf("turn = %+v, want the user's prose as a chat turn", sent.req.Turn)
	}
	if strings.Contains(sent.req.Turn.Text, "requirements draft") || strings.Contains(sent.req.Turn.Text, "list_org_endpoints") {
		t.Errorf("turn carries retired steering: %+v", sent.req.Turn)
	}
}

// TestNoDesignGate pins the design gate's removal (#373): a /design turn on a
// requirements-less repo is ACCEPTED — completeness is enforced at the v<N>
// tag (the build gate), not at turn start.
func TestNoDesignGate(t *testing.T) {
	r := newGenaiRig(t, map[string]string{"README.md": "no requirements yet\n"})
	r.fake.parts = []string{textPart("ok")}
	m := manifestPart(nil, nil)
	r.fake.manifest = &m

	if rec := r.post(t, convUUID, "", "/design"); rec.Code != http.StatusAccepted {
		t.Fatalf("/design on requirements-less repo: code %d, want 202 (%s)", rec.Code, rec.Body.String())
	}
}

// TestManifestGate_MismatchSeveredEmpty pins the D14 outcomes: hash mismatch →
// fold-parity (no commit); severed stream → stream-died (no commit); empty
// manifest → completed noChanges (no commit).
func TestManifestGate_MismatchSeveredEmpty(t *testing.T) {
	t.Run("hash mismatch", func(t *testing.T) {
		r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"})
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
		r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"})
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
		r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"})
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

func (r *genaiRig) waitTerminalOf(t *testing.T, turnID string) spec.TurnStatus {
	t.Helper()
	return r.waitTerminal(t, turnID)
}

// TestCollabTurn_RoomScopedDispatchNoCommit pins #86 phase 4's BFF half: a
// `collab: true` turn dispatches the agents service with the room + the
// caller's bearer, relays the stream, and lands NOTHING on git — the doc is
// the write surface (no fold, no manifest gate, no commit).
func TestCollabTurn_RoomScopedDispatchNoCommit(t *testing.T) {
	// MCP discovery is wired: a collab room-scoped turn must carry the BFF-minted
	// MCP block so the Spec-view architect can discover real org endpoints (the
	// regression pin for the invented-org-service-name bug).
	r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"}, withMCP())
	base := r.fx.Origin.HeadSHA(t)
	// Room-scheme (unprefixed) paths: in committed mode this would fail the
	// fold; room mode never folds — pinning that the fold is bypassed.
	r.fake.parts = []string{
		textPart("editing the shared doc"),
		editFilePart("requirements/requirements.md", "# Reqs\n", "# Requirements\n"),
	}
	m := manifestPart(map[string]string{"requirements/requirements.md": "# Requirements\n"}, nil)
	r.fake.manifest = &m

	body, _ := json.Marshal(map[string]any{
		"instruction": "edit the doc live",
		"collab":      true,
	})
	rec := r.h.AsOrg(testOrg).Post(turnsPath(convUUID), string(body))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST collab turn: code %d (%s)", rec.Code, rec.Body.String())
	}
	var out struct {
		TurnID string `json:"turnId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.TurnID == "" {
		t.Fatalf("202 body = %s (err %v)", rec.Body.String(), err)
	}

	st := r.waitTerminal(t, out.TurnID)
	if st.Status != "completed" || !st.NoChanges {
		t.Fatalf("terminal = %+v, want completed no-changes", st)
	}
	if st.CommitSHA != base {
		t.Errorf("collab turn commitSha = %s, want base pin %s", st.CommitSHA, base)
	}
	if r.fx.Origin.HeadSHA(t) != base {
		t.Error("collab turn must not commit — git is not the write surface")
	}

	sent := r.fake.sentTurn(t, 0)
	if sent.req.Collab == nil {
		t.Fatal("dispatch carried no collab block")
	}
	if want := "spec-" + testOrg + "-" + testProj; sent.req.Collab.RoomID != want {
		t.Errorf("collab roomId = %q, want %q", sent.req.Collab.RoomID, want)
	}
	if sent.req.Collab.Token != componenttest.TestBearer {
		t.Errorf("collab token = %q, want the caller's bearer", sent.req.Collab.Token)
	}
	// Regression pin: the collab turn carries the MCP discovery block so the
	// architect can call list_org_endpoints instead of inventing org-service
	// names (the whole reason this gate was widened past design-generate).
	if sent.req.MCP == nil {
		t.Fatal("collab turn dispatched without an MCP discovery block")
	}
	if want := testMCPBaseURL + "/internal/v1/mcp"; sent.req.MCP.URL != want {
		t.Errorf("MCP url = %q, want %q", sent.req.MCP.URL, want)
	}
	if sent.req.MCP.Token != testMCPToken {
		t.Errorf("MCP token = %q, want %q", sent.req.MCP.Token, testMCPToken)
	}
	// collabDepsSteer is retired (#373): the skills own dependency discovery,
	// and MCP + WebSearch attachment (asserted above) is what the collab turn
	// still guarantees.
	if strings.Contains(sent.req.Turn.Text, "list_org_endpoints") {
		t.Errorf("collab turn carries the retired dependency-discovery steer: %+v", sent.req.Turn)
	}
	// external-dependency-discovery: a collab room-scoped turn also carries
	// WebSearch:true, the same gate as MCP.
	if !sent.req.WebSearch {
		t.Error("collab turn dispatched without WebSearch:true")
	}
}

// TestMCPGate_AttachAndLeak pins mcpForTurn's widened gate: a collab turn with
// MCP UNWIRED carries no block (best-effort); a non-collab requirements-chat
// turn never gets MCP even when wired (the gate did not leak past design-generate
// + collab); a design-generate turn still attaches MCP.
func TestMCPGate_AttachAndLeak(t *testing.T) {
	t.Run("collab turn, MCP unwired → no block", func(t *testing.T) {
		r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"})
		r.fake.parts = []string{textPart("editing the shared doc")}
		m := manifestPart(nil, nil)
		r.fake.manifest = &m

		body, _ := json.Marshal(map[string]any{
			"instruction": "edit the doc live",
			"collab":      true,
		})
		rec := r.h.AsOrg(testOrg).Post(turnsPath(convUUID), string(body))
		if rec.Code != http.StatusAccepted {
			t.Fatalf("POST collab turn: code %d (%s)", rec.Code, rec.Body.String())
		}
		var out struct {
			TurnID string `json:"turnId"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		r.waitTerminal(t, out.TurnID)
		if sent := r.fake.sentTurn(t, 0); sent.req.MCP != nil {
			t.Errorf("collab turn carried an MCP block with MCP deps unwired: %+v", sent.req.MCP)
		}
	})

	t.Run("non-collab requirements-chat, MCP wired → no leak", func(t *testing.T) {
		r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"}, withMCP())
		r.fake.parts = []string{textPart("chatting")}
		m := manifestPart(nil, nil)
		r.fake.manifest = &m

		turnID := r.startTurn(t, convUUID, "requirements-chat", "just chat")
		r.waitTerminal(t, turnID)
		sent := r.fake.sentTurn(t, 0)
		if sent.req.MCP != nil {
			t.Errorf("plain requirements-chat turn leaked an MCP block: %+v", sent.req.MCP)
		}
		if strings.Contains(sent.req.Turn.Text, "list_org_endpoints") {
			t.Errorf("non-collab turn leaked the collab dependency-discovery steer: %+v", sent.req.Turn)
		}
	})

	t.Run("design flow, MCP wired → attaches", func(t *testing.T) {
		r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"}, withMCP())
		r.fake.parts = []string{textPart("designing")}
		m := manifestPart(nil, nil)
		r.fake.manifest = &m

		turnID := r.startTurn(t, convUUID, "", "/design")
		r.waitTerminal(t, turnID)
		sent := r.fake.sentTurn(t, 0)
		if sent.req.MCP == nil {
			t.Fatal("design-flow turn dispatched without an MCP discovery block")
		}
		if want := testMCPBaseURL + "/internal/v1/mcp"; sent.req.MCP.URL != want {
			t.Errorf("MCP url = %q, want %q", sent.req.MCP.URL, want)
		}
		if sent.req.MCP.Token != testMCPToken {
			t.Errorf("MCP token = %q, want %q", sent.req.MCP.Token, testMCPToken)
		}
	})
}

// TestWebSearchGate_AttachAndLeak pins the WebSearch flag's gate (external-
// dependency-discovery): it fires on the SAME condition as mcpForTurn
// (design-generate or any collab room-scoped turn), but — unlike MCP —
// WITHOUT needing the MCP token-minter/base-URL wired, since Anthropic's
// web_search provider tool needs no BFF-minted credential. None of these
// subtests call withMCP(), proving that independence.
func TestWebSearchGate_AttachAndLeak(t *testing.T) {
	t.Run("collab turn, MCP unwired → WebSearch still attaches", func(t *testing.T) {
		r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"})
		r.fake.parts = []string{textPart("editing the shared doc")}
		m := manifestPart(nil, nil)
		r.fake.manifest = &m

		body, _ := json.Marshal(map[string]any{
			"instruction": "edit the doc live",
			"collab":      true,
		})
		rec := r.h.AsOrg(testOrg).Post(turnsPath(convUUID), string(body))
		if rec.Code != http.StatusAccepted {
			t.Fatalf("POST collab turn: code %d (%s)", rec.Code, rec.Body.String())
		}
		var out struct {
			TurnID string `json:"turnId"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		r.waitTerminal(t, out.TurnID)
		if sent := r.fake.sentTurn(t, 0); !sent.req.WebSearch {
			t.Error("collab turn dispatched without WebSearch:true, even with MCP deps unwired")
		}
	})

	t.Run("non-collab requirements-chat → no WebSearch", func(t *testing.T) {
		r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"})
		r.fake.parts = []string{textPart("chatting")}
		m := manifestPart(nil, nil)
		r.fake.manifest = &m

		turnID := r.startTurn(t, convUUID, "requirements-chat", "just chat")
		r.waitTerminal(t, turnID)
		sent := r.fake.sentTurn(t, 0)
		if sent.req.WebSearch {
			t.Error("plain requirements-chat turn leaked WebSearch:true")
		}
	})

	t.Run("design-generate → WebSearch attaches", func(t *testing.T) {
		r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"})
		r.fake.parts = []string{textPart("designing")}
		m := manifestPart(nil, nil)
		r.fake.manifest = &m

		turnID := r.startTurn(t, convUUID, "", "/design")
		r.waitTerminal(t, turnID)
		sent := r.fake.sentTurn(t, 0)
		if !sent.req.WebSearch {
			t.Error("design-generate turn dispatched without WebSearch:true")
		}
	})
}

// TestD20_FilesChangedExternallyAndDivergenceNote pins the server-derived
// flag: a second turn in the same conversation after main moved externally
// dispatches filesChangedExternally=true; after a FAILED turn the divergence
// note is prepended.
func TestD20_FilesChangedExternallyAndDivergenceNote(t *testing.T) {
	r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"})
	m := manifestPart(nil, nil)
	r.fake.parts = []string{textPart("ok")}
	r.fake.manifest = &m

	// Turn 1 completes (no changes).
	r.waitTerminal(t, r.startTurn(t, convUUID, "requirements-chat", "one"))

	// An external Apply advances main behind the conversation's back.
	r.fx.Origin.Seed(t, map[string]string{"specs/requirements/prd.md": "# Externally edited\n"}, "external")

	// Turn 2, same conversation → filesChangedExternally=true, no note.
	r.waitTerminal(t, r.startTurn(t, convUUID, "requirements-chat", "two"))
	second := r.fake.sentTurn(t, 1)
	if !second.req.FilesChangedExternally {
		t.Error("second dispatch must carry filesChangedExternally=true")
	}
	if second.req.PreviousTurnFailed {
		t.Error("completed prior turn must not flag a previous failure")
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
	if !fourth.req.PreviousTurnFailed {
		t.Errorf("previousTurnFailed missing after a failed turn: %+v", fourth.req)
	}
}

// TestLiveAttach_SecondViewerTails attaches a real SSE client mid-stream (the
// D16/D18 viewer): it receives the already-buffered parts, live-tails the
// rest, and sees the terminal + [DONE].
func TestLiveAttach_SecondViewerTails(t *testing.T) {
	r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"})
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
	r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"})

	if rec := r.post(t, "a--b", "", "x"); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid conversation id: code %d, want 400", rec.Code)
	}
	r.key = ""
	if rec := r.post(t, convUUID, "", "x"); rec.Code != http.StatusBadRequest {
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
	r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"})
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
	r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"})
	rec := r.h.AsOrg(testOrg).Get(convPath(convUUID))
	if rec.Code != http.StatusOK {
		t.Fatalf("rehydrate code %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"messages"`) {
		t.Errorf("rehydrate body = %s", rec.Body.String())
	}
	// Rehydrate reconstructs the id under the general use case (the console
	// omits "useCase", so its turns are namespaced under useCaseGeneral).
	wantPath := "/conversations/org_" + testOrg + "--proj_" + testProj + "--general--" + convUUID
	r.fake.mu.Lock()
	gotPath := r.fake.lastConvPath
	r.fake.mu.Unlock()
	if gotPath != wantPath {
		t.Errorf("rehydrate path = %q, want %q", gotPath, wantPath)
	}
}

func TestRehydrate_CrossTenantOrUnknown_404(t *testing.T) {
	r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"})
	r.fake.mu.Lock()
	r.fake.convStatus = http.StatusNotFound
	r.fake.mu.Unlock()
	if rec := r.h.AsOrg(testOrg).Get(convPath(convUUID)); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown conversation: code %d, want 404", rec.Code)
	}
}

func TestGenAI_NoAuth_401(t *testing.T) {
	r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"})
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
	r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"},
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
	r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"})

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

// A turn whose FIRST model event is slow — the shape of a kickoff carrying a
// large PDF/image the model must read before emitting anything — must still
// heartbeat. The agents service opens its SSE stream lazily on that first
// event, so Dispatch blocks until then; starting the heartbeat only after
// Dispatch returns left heartbeat_at frozen at created_at, and the 60s sweep
// failed a perfectly healthy turn as "replica crashed or hung" (observed live
// on a project with an 848KB PDF + 334KB PNG attached).
func TestSlowFirstEvent_StillHeartbeats(t *testing.T) {
	// Seed something the agent does NOT write, so the fold has no parity clash.
	r := newGenaiRig(t, map[string]string{"README.md": "hi\n"})
	r.svc.SetHeartbeatEveryForTest(20 * time.Millisecond)
	r.fake.parts = []string{addFilePart("specs/requirements/prd.md", "# Reqs\n")}
	m := manifestPart(map[string]string{"specs/requirements/prd.md": "# Reqs\n"}, nil)
	r.fake.manifest = &m
	r.fake.preHeaderGate = true
	// Released via defer (sync.Once): a t.Fatalf before the explicit release
	// would otherwise leave the handler parked and deadlock server cleanup.
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(r.fake.preHeaderRelease) }) }
	defer release()

	turnID := r.startTurn(t, convUUID, "", "/start")

	// The handler is now blocked BEFORE writing headers: Dispatch has not
	// returned, and this is exactly the window the sweep used to kill.
	select {
	case <-r.fake.preHeaderEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("handler never reached the pre-header gate")
	}
	created := r.turns.row(t, turnID).HeartbeatAt
	time.Sleep(200 * time.Millisecond) // ≫ the 20ms cadence
	beat := r.turns.row(t, turnID).HeartbeatAt
	if !beat.After(created) {
		t.Fatalf("heartbeat frozen at %v during a slow first event — the sweep would fail this healthy turn", created)
	}

	release()
	if st := r.waitTerminal(t, turnID); st.Status != "completed" {
		t.Fatalf("turn = %+v, want completed once the first event arrives", st)
	}
}

// The bug this pins: attachments parsed fine, the journal carried their names,
// the POST answered 202 and the console rendered chips — and the agents service
// received NOTHING, because the dispatch never set TurnRequest.Attachments. The
// turn looked entirely successful while the agent truthfully reported seeing no
// file. Parsing tests could not catch it; only asserting the DISPATCHED body can.
func TestStartTurnDispatchesChatAttachments(t *testing.T) {
	r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"})
	convUUID := "11111111-2222-3333-4444-555555555555"

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("instruction", "Add this as a separate form")
	_ = w.WriteField("collab", "true")
	part, err := w.CreateFormFile("files", "2025-Motor Claim Form.pdf")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	_, _ = part.Write([]byte("%PDF-1.7 fake"))
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	rec := r.h.AsOrg(testOrg).PostRaw(turnsPath(convUUID), w.FormDataContentType(), buf.Bytes())
	if rec.Code != http.StatusAccepted {
		t.Fatalf("multipart POST: code %d, want 202 (%s)", rec.Code, rec.Body.String())
	}
	var out struct {
		TurnID string `json:"turnId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.TurnID == "" {
		t.Fatalf("202 body = %s (err %v)", rec.Body.String(), err)
	}
	r.waitTerminal(t, out.TurnID)

	sent := r.fake.sentTurn(t, 0)
	if len(sent.req.Attachments) != 1 {
		t.Fatalf("dispatched attachments = %+v, want exactly one", sent.req.Attachments)
	}
	got := sent.req.Attachments[0]
	// The name is sanitized to a bare base name but otherwise preserved — it is
	// the dedupe key the agents service matches against history.
	if got.Name != "2025-Motor Claim Form.pdf" {
		t.Errorf("name = %q", got.Name)
	}
	// A PDF is read natively as a document block, so its real media type rides.
	if got.MediaType != "application/pdf" {
		t.Errorf("mediaType = %q, want application/pdf", got.MediaType)
	}
	raw, err := base64.StdEncoding.DecodeString(got.Data)
	if err != nil {
		t.Fatalf("data is not base64: %v", err)
	}
	if string(raw) != "%PDF-1.7 fake" {
		t.Errorf("decoded bytes = %q", raw)
	}
	// The journal carries NAMES for the chips — a separate concern from the
	// bytes above, and having one without the other is exactly the failure.
	if sent.req.Journal == nil || len(sent.req.Journal.Attachments) != 1 ||
		sent.req.Journal.Attachments[0] != "2025-Motor Claim Form.pdf" {
		t.Errorf("journal attachments = %+v", sent.req.Journal)
	}
	if sent.req.Turn.Kind != agentsvc.TurnKindChat || sent.req.Turn.Text != "Add this as a separate form" {
		t.Errorf("turn = %+v", sent.req.Turn)
	}
}

// A JSON send must stay byte-identical to before the multipart arm existed.
func TestStartTurnJSONCarriesNoAttachments(t *testing.T) {
	r := newGenaiRig(t, map[string]string{"specs/requirements/prd.md": "# Reqs\n"})
	convUUID := "11111111-2222-3333-4444-666666666666"
	turnID := r.startTurn(t, convUUID, "", "tidy the requirements")
	r.waitTerminal(t, turnID)

	sent := r.fake.sentTurn(t, 0)
	if sent.req.Attachments != nil {
		t.Errorf("attachments = %+v, want nil for a JSON send", sent.req.Attachments)
	}
	if sent.req.Journal == nil || sent.req.Journal.Attachments != nil {
		t.Errorf("journal attachments = %+v, want nil", sent.req.Journal)
	}
}
