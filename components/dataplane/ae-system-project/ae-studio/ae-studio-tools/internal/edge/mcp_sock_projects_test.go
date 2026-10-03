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

package edge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wso2/aep/ae-studio-tools/internal/files"
	"github.com/wso2/aep/ae-studio-tools/internal/projects"
	"github.com/wso2/aep/ae-studio-tools/internal/projects/projectstest"
	"github.com/wso2/aep/ae-studio-tools/internal/repo"
	"github.com/wso2/aep/ae-studio-tools/internal/repo/repotest"
)

// lookupHarness is the MCP socket over a real engine, a file:// origin aep-api
// names acme/greeter, and a file:// org skills origin.
type lookupHarness struct {
	*mcpHarness
	root     string
	engine   *repo.Engine
	origin   *repotest.Origin
	skills   *repotest.Origin
	projects *projectstest.Fake
}

func newLookupHarness(t *testing.T) *lookupHarness {
	t.Helper()
	origin := repotest.NewOrigin(t, map[string]string{"specs/requirements/prd.md": "req v1\n"})
	skills := repotest.NewOrigin(t, map[string]string{"designer/SKILL.md": "# designer\n"})
	fake := projectstest.NewFake(map[string]projects.Repository{"greeter": {
		Owner: "acme", Repo: "greeter", DefaultBranch: repotest.Branch, CloneURL: origin.URL(),
	}})
	fake.SetSkills(projects.Repository{Owner: "acme", Repo: "org-skills", DefaultBranch: repotest.Branch, CloneURL: skills.URL()})
	root := t.TempDir()
	engine, _, err := repo.New(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	reader := files.Reader{Engine: engine, Projects: fake, Org: "default"}
	h := newMCPHarness(t, &fakeUpstream{}, withSnapshots(reader))
	return &lookupHarness{mcpHarness: h, root: root, engine: engine, origin: origin, skills: skills, projects: fake}
}

// get sends a GET on the socket and answers the status and body.
func (h *lookupHarness) get(path string) (int, string) {
	h.t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://mcp"+path, nil)
	if err != nil {
		h.t.Fatal(err)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

type lookupReply struct {
	Known      bool     `json:"known"`
	HeadSha    string   `json:"headSha"`
	SkillsSha  string   `json:"skillsSha"`
	References []string `json:"references"`
}

func (h *lookupHarness) lookup(path string) lookupReply {
	h.t.Helper()
	code, body := h.get(path)
	if code != http.StatusOK {
		h.t.Fatalf("GET %s = %d %s", path, code, body)
	}
	var r lookupReply
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		h.t.Fatalf("body: %v\n%s", err, body)
	}
	return r
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// 04 §6 / 09 §2: the lookup writes snapshots/projects/greeter/<head>/ and
// snapshots/skills/<sha>/, overlays the stored references, and answers both
// shas and the sorted reference names.
func TestLookup_WritesSnapshotsAndAnswersShas(t *testing.T) {
	h := newLookupHarness(t)
	if err := h.engine.PutReferences(context.Background(), repo.OwnerRepo{Owner: "acme", Repo: "greeter"}, []repo.ReferenceDoc{
		{Name: "z-notes.md", Content: []byte("z\n")},
		{Name: "a.pdf", Content: []byte("%PDF-1")},
	}); err != nil {
		t.Fatal(err)
	}

	got := h.lookup("/projects/greeter")
	if want := (lookupReply{Known: true, HeadSha: h.origin.HeadSHA(t), SkillsSha: h.skills.HeadSHA(t), References: []string{"a.pdf", "z-notes.md"}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("lookup = %+v, want %+v", got, want)
	}
	snap := filepath.Join(h.root, "snapshots", "projects", "greeter", got.HeadSha)
	if c := readFile(t, filepath.Join(snap, "specs", "requirements", "prd.md")); c != "req v1\n" {
		t.Fatalf("prd.md = %q", c)
	}
	if c := readFile(t, filepath.Join(snap, "specs", "requirements", "references", "a.pdf")); c != "%PDF-1" {
		t.Fatalf("overlaid a.pdf = %q", c)
	}
	if c := readFile(t, filepath.Join(snap, "specs", "requirements", "references", ".aep-references.json")); !strings.Contains(c, `"a.pdf"`) {
		t.Fatalf("manifest = %q", c)
	}
	if c := readFile(t, filepath.Join(h.root, "snapshots", "skills", got.SkillsSha, "designer", "SKILL.md")); c != "# designer\n" {
		t.Fatalf("skill = %q", c)
	}

	// No references stored: an empty list, never null.
	h.projects.Set("bare", projects.Repository{Owner: "acme", Repo: "bare", DefaultBranch: repotest.Branch, CloneURL: h.origin.URL()})
	if code, body := h.get("/projects/bare"); code != http.StatusOK || !strings.Contains(body, `"references":[]`) {
		t.Fatalf("bare = %d %s", code, body)
	}
}

// ?at=<old sha> serves that older commit (a continued conversation's fold
// base); a commit the repository does not have is 404 ref_not_found.
func TestLookup_AtServesOlderCommit(t *testing.T) {
	h := newLookupHarness(t)
	old := h.origin.HeadSHA(t)
	head := h.origin.Commit(t, map[string]string{"specs/requirements/prd.md": "req v2\n"}, "v2")

	if got := h.lookup("/projects/greeter"); got.HeadSha != head {
		t.Fatalf("head lookup = %s, want %s", got.HeadSha, head)
	}
	got := h.lookup("/projects/greeter?at=" + old)
	if got.HeadSha != old {
		t.Fatalf("at lookup = %s, want %s", got.HeadSha, old)
	}
	if c := readFile(t, filepath.Join(h.root, "snapshots", "projects", "greeter", old, "specs", "requirements", "prd.md")); c != "req v1\n" {
		t.Fatalf("old prd.md = %q", c)
	}
	code, body := h.get("/projects/greeter?at=" + strings.Repeat("ab", 20))
	if code != http.StatusNotFound || !strings.Contains(body, `"ref_not_found"`) {
		t.Fatalf("unknown at = %d %s", code, body)
	}
	if code, body := h.get("/projects/greeter?at=main"); code != http.StatusBadRequest {
		t.Fatalf("symbolic at = %d %s", code, body)
	}
}

// A project aep-api does not know for this org is 404 project_unknown, and no
// snapshot is written: the check and the snapshot are one call.
func TestLookup_UnknownProjectWritesNothing(t *testing.T) {
	h := newLookupHarness(t)
	code, body := h.get("/projects/other")
	if code != http.StatusNotFound || !strings.Contains(body, `"project_unknown"`) {
		t.Fatalf("unknown = %d %s", code, body)
	}
	for _, d := range []string{repo.ProjectSnapshotsDir(h.root), repo.SkillsSnapshotsDir(h.root), repo.ReposDir(h.root)} {
		entries, err := os.ReadDir(d)
		if err != nil || len(entries) != 0 {
			t.Fatalf("%s has %d entries (%v), want none", d, len(entries), err)
		}
	}
}

// 20 §2: at 90 % a new snapshot is refused with 503 disk_full.
func TestLookup_AdmissionAt90IsDiskFull(t *testing.T) {
	h := newLookupHarness(t)
	h.engine.SetDiskUsagePct(90)
	code, body := h.get("/projects/greeter")
	if code != http.StatusServiceUnavailable || !strings.Contains(body, `"disk_full"`) {
		t.Fatalf("at 90%% = %d %s", code, body)
	}
	entries, _ := os.ReadDir(repo.ProjectSnapshotsDir(h.root))
	if len(entries) != 0 {
		t.Fatalf("a refused lookup wrote %d project snapshot dirs", len(entries))
	}
	if code, body := h.get("/skills"); code != http.StatusServiceUnavailable || !strings.Contains(body, `"disk_full"`) {
		t.Fatalf("skills at 90%% = %d %s", code, body)
	}
}

// aep-api down (or no skills repository for the org) is 503
// aep_api_unavailable on both routes.
func TestLookup_AEPAPIUnavailable(t *testing.T) {
	h := newLookupHarness(t)
	h.projects.SetErr(projects.ErrUnavailable)
	for _, path := range []string{"/projects/greeter", "/skills"} {
		code, body := h.get(path)
		if code != http.StatusServiceUnavailable || !strings.Contains(body, `"aep_api_unavailable"`) {
			t.Fatalf("GET %s = %d %s", path, code, body)
		}
	}
}

// 07 §6: GET /skills writes the Org skills snapshot and answers its sha.
func TestSkills_WritesSnapshot(t *testing.T) {
	h := newLookupHarness(t)
	code, body := h.get("/skills")
	if code != http.StatusOK {
		t.Fatalf("GET /skills = %d %s", code, body)
	}
	var got struct {
		SkillsSha string `json:"skillsSha"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil || got.SkillsSha != h.skills.HeadSHA(t) {
		t.Fatalf("body = %s (%v), want skillsSha %s", body, err, h.skills.HeadSHA(t))
	}
	if c := readFile(t, filepath.Join(h.root, "snapshots", "skills", got.SkillsSha, "designer", "SKILL.md")); c != "# designer\n" {
		t.Fatalf("skill = %q", c)
	}
	entries, _ := os.ReadDir(repo.ProjectSnapshotsDir(h.root))
	if len(entries) != 0 {
		t.Fatalf("GET /skills wrote a project snapshot")
	}
}

// 07 §1: the lookup answers the idea captured in specs/.agentic-engineer.toml
// at the snapshotted commit (the agent cannot read the dot-led descriptor
// itself). No descriptor, or one that does not parse, answers no idea: the
// read is best-effort, as readProjectIdea was in aep-api.
func TestLookupIdea(t *testing.T) {
	h := newLookupHarness(t)
	type ideaReply struct {
		Idea *string `json:"idea"`
	}
	ideaOf := func(path string) *string {
		t.Helper()
		code, body := h.get(path)
		if code != http.StatusOK {
			t.Fatalf("GET %s = %d %s", path, code, body)
		}
		var r ideaReply
		if err := json.Unmarshal([]byte(body), &r); err != nil {
			t.Fatalf("body: %v\n%s", err, body)
		}
		return r.Idea
	}

	if idea := ideaOf("/projects/greeter"); idea != nil {
		t.Fatalf("no descriptor: idea = %q, want absent", *idea)
	}
	before := h.origin.HeadSHA(t)

	descriptor := "apiVersion = \"agentic-engineer/v1\"\nname = \"greeter\"\ncreatedAt = \"2026-01-01T00:00:00Z\"\nidea = \"A greeter that says \\\"hi\\\"\\nand waves\"\n"
	h.origin.Commit(t, map[string]string{"specs/.agentic-engineer.toml": descriptor}, "descriptor")
	if idea := ideaOf("/projects/greeter"); idea == nil || *idea != "A greeter that says \"hi\"\nand waves" {
		t.Fatalf("descriptor idea = %v", idea)
	}
	if idea := ideaOf("/projects/greeter?at=" + before); idea != nil {
		t.Fatalf("at a commit before the descriptor: idea = %q, want absent", *idea)
	}

	h.origin.Commit(t, map[string]string{"specs/.agentic-engineer.toml": "idea = \"unterminated\n"}, "corrupt")
	if idea := ideaOf("/projects/greeter"); idea != nil {
		t.Fatalf("malformed descriptor: idea = %q, want absent", *idea)
	}

	h.origin.Commit(t, map[string]string{"specs/.agentic-engineer.toml": "apiVersion = \"agentic-engineer/v1\"\nidea = \"   \"\n"}, "blank")
	if idea := ideaOf("/projects/greeter"); idea != nil {
		t.Fatalf("blank idea: idea = %q, want absent", *idea)
	}
}
