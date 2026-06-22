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

package skills

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/wso2/asdlc/asdlc-service/internal/credentials"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/gitrepo"
	"github.com/wso2/asdlc/asdlc-service/models"
)

// ---- in-memory fake git server --------------------------------------------

// memGit models a single repo as a sequence of commit snapshots so the store's
// real GetRef→GetCommit→GetTree→GetBlob reads and CreateBlob→CreateTree→
// CreateCommit→UpdateRef writes round-trip against actual data.
type memGit struct {
	gitrepo.GitHubClient // embedded: unimplemented methods would panic (untouched by these tests)

	mu          sync.Mutex
	snapshots   map[string]map[string]string // commitSHA → {path: content}
	trees       map[string]map[string]string // treeSHA → {path: content}
	blobs       map[string]string            // blobSHA → content
	head        string
	seq         int
	getRefCalls int
}

func newMemGit() *memGit {
	return &memGit{
		snapshots: map[string]map[string]string{"c0": {"README.md": "init"}},
		trees:     map[string]map[string]string{},
		blobs:     map[string]string{},
		head:      "c0",
	}
}

func cloneMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (m *memGit) GetRef(_ context.Context, _, _ string, _ credentials.Credential, _ string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getRefCalls++
	return m.head, nil
}

func (m *memGit) GetCommit(_ context.Context, _, _ string, _ credentials.Credential, sha string) (*gitrepo.CommitObject, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	treeSHA := "t-" + sha
	m.trees[treeSHA] = cloneMap(m.snapshots[sha])
	return &gitrepo.CommitObject{SHA: sha, TreeSHA: treeSHA}, nil
}

func (m *memGit) GetTree(_ context.Context, _, _ string, _ credentials.Credential, treeSHA string, _ bool) (*gitrepo.TreeObject, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	files := m.trees[treeSHA]
	entries := make([]gitrepo.TreeEntryResult, 0, len(files))
	for path, content := range files {
		bSHA := "b|" + treeSHA + "|" + path
		m.blobs[bSHA] = content
		entries = append(entries, gitrepo.TreeEntryResult{Path: path, Type: "blob", SHA: bSHA, Mode: "100644"})
	}
	return &gitrepo.TreeObject{SHA: treeSHA, Entries: entries}, nil
}

func (m *memGit) GetBlob(_ context.Context, _, _ string, _ credentials.Credential, sha string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	content, ok := m.blobs[sha]
	if !ok {
		return nil, fmt.Errorf("memGit: blob %q not found", sha)
	}
	return []byte(content), nil
}

func (m *memGit) CreateBlob(_ context.Context, _, _ string, _ credentials.Credential, content []byte) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	bSHA := fmt.Sprintf("nb%d", m.seq)
	m.seq++
	m.blobs[bSHA] = string(content)
	return bSHA, nil
}

func (m *memGit) CreateTree(_ context.Context, _, _ string, _ credentials.Credential, baseTree string, entries []gitrepo.TreeEntry) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	files := cloneMap(m.trees[baseTree])
	for _, e := range entries {
		if e.SHA == "" {
			delete(files, e.Path) // sha:null → deletion
			continue
		}
		files[e.Path] = m.blobs[e.SHA]
	}
	newTree := fmt.Sprintf("t%d", m.seq)
	m.seq++
	m.trees[newTree] = files
	return newTree, nil
}

func (m *memGit) CreateCommit(_ context.Context, _, _ string, _ credentials.Credential, req gitrepo.CreateCommitRequest) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := fmt.Sprintf("c%d", m.seq)
	m.seq++
	m.snapshots[c] = cloneMap(m.trees[req.TreeSHA])
	return c, nil
}

func (m *memGit) UpdateRef(_ context.Context, _, _ string, _ credentials.Credential, _, sha string, _ bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.head = sha
	return nil
}

// removeAtHead drops a path by recording a NEW commit (advancing HEAD) — the
// way real git surfaces a content change, so the store's HEAD-sha revalidation
// detects it.
func (m *memGit) removeAtHead(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := cloneMap(m.snapshots[m.head])
	delete(next, path)
	newHead := fmt.Sprintf("c-rm%d", m.seq)
	m.seq++
	m.snapshots[newHead] = next
	m.head = newHead
}

func (m *memGit) refCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.getRefCalls
}

// ---- fake credential + gitops + repo svc -----------------------------------

type fakeCred struct{}

func (fakeCred) Token(context.Context) (string, time.Time, error)  { return "tok", time.Time{}, nil }
func (fakeCred) Identity() credentials.Identity                    { return credentials.Identity{Name: "Bot", Email: "bot@asdlc.dev"} }
func (fakeCred) RepoOwner() string                                 { return "test-org" }
func (fakeCred) WebhookStrategy() credentials.WebhookStrategy      { return credentials.WebhookPlatform }

type fakeResolver struct{}

func (fakeResolver) Resolve(context.Context, string) (credentials.Credential, error) {
	return fakeCred{}, nil
}

type fakeGitOps struct {
	gitrepo.GitOpsService
	gh *memGit
}

func (f *fakeGitOps) GitHubClient() gitrepo.GitHubClient    { return f.gh }
func (f *fakeGitOps) Resolver() credentials.Resolver        { return fakeResolver{} }
func (f *fakeGitOps) ResolveSaveIdentities(credentials.Credential) (*gitrepo.GitIdentity, *gitrepo.GitIdentity) {
	gi := &gitrepo.GitIdentity{Name: "Bot", Email: "bot@asdlc.dev"}
	return gi, gi
}

type fakeRepoSvc struct {
	gitrepo.RepoService
	mu    sync.Mutex // models the DB's concurrency safety (the real GetRepo/EnsureBareRepo are serialized by Postgres)
	repos map[string]*models.GitRepository
}

func (f *fakeRepoSvc) GetRepo(_ context.Context, orgID, _ string) (*models.GitRepository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r, ok := f.repos[orgID]; ok {
		return r, nil
	}
	return nil, gitrepo.ErrRepoNotFound
}

func (f *fakeRepoSvc) EnsureBareRepo(_ context.Context, orgID, projectID, repoName string) (*models.GitRepository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r, ok := f.repos[orgID]; ok {
		return r, nil
	}
	r := &models.GitRepository{
		OrgID:         orgID,
		ProjectID:     projectID,
		RepoURL:       "https://github.com/test-org/" + repoName,
		DefaultBranch: "main",
		Status:        "ready",
	}
	f.repos[orgID] = r
	return r, nil
}

func newTestStore() (*SkillService, *memGit) {
	gh := newMemGit()
	gitops := &fakeGitOps{gh: gh}
	repos := &fakeRepoSvc{repos: map[string]*models.GitRepository{}}
	return NewSkillService(gitops, repos), gh
}

func nameSet(skills []Skill) map[string]Skill {
	out := map[string]Skill{}
	for _, sk := range skills {
		out[sk.Name] = sk
	}
	return out
}

// ---- tests -----------------------------------------------------------------

func TestList_SeedsBuiltinsOnFirstRead(t *testing.T) {
	svc, _ := newTestStore()
	got, err := svc.List(context.Background(), "org1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	by := nameSet(got)
	for _, want := range []string{"go", "api-management", "react-webapp", "thunder-authentication"} {
		sk, ok := by[want]
		if !ok {
			t.Fatalf("expected builtin %q to be seeded; got %v", want, keysOf(by))
		}
		if sk.Kind != "builtin" {
			t.Fatalf("skill %q: kind = %q, want builtin", want, sk.Kind)
		}
		if sk.Version < 1 {
			t.Fatalf("skill %q: version = %d, want >= 1", want, sk.Version)
		}
	}
}

func TestReconcile_RewritesMissingBuiltin(t *testing.T) {
	svc, gh := newTestStore()
	ctx := context.Background()
	if _, err := svc.List(ctx, "org1"); err != nil { // triggers seed
		t.Fatalf("seed: %v", err)
	}
	// Simulate the `go` built-in being deleted from the repo.
	gh.removeAtHead(skillRepoPath("builtin", "go"))

	n, err := svc.Reconcile(ctx, "org1")
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if n != 1 {
		t.Fatalf("Reconcile wrote %d, want 1 (only `go` was missing)", n)
	}
	got, _ := svc.List(ctx, "org1")
	if _, ok := nameSet(got)["go"]; !ok {
		t.Fatalf("`go` should be restored after reconcile")
	}
}

func TestReconcile_NoopWhenUpToDate(t *testing.T) {
	svc, _ := newTestStore()
	ctx := context.Background()
	if _, err := svc.List(ctx, "org1"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	n, err := svc.Reconcile(ctx, "org1")
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if n != 0 {
		t.Fatalf("Reconcile wrote %d, want 0 (already seeded at current versions)", n)
	}
	ups, err := svc.UpdatesAvailable(ctx, "org1")
	if err != nil {
		t.Fatalf("UpdatesAvailable: %v", err)
	}
	if len(ups) != 0 {
		t.Fatalf("UpdatesAvailable = %v, want none", ups)
	}
}

func TestCreateAndDeleteCustomSkill(t *testing.T) {
	svc, _ := newTestStore()
	mut := NewSkillMutationService(svc)
	ctx := context.Background()
	if _, err := svc.List(ctx, "org1"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	const skillMD = "---\n" +
		"name: payments-pci\n" +
		"description: PCI handling rules for payment components.\n" +
		"metadata:\n" +
		"  asdlc.version: \"1\"\n" +
		"---\n\nAlways tokenize PANs before persistence.\n"

	created, err := mut.Create(ctx, "org1", "tester", CreateSkillInput{Name: "payments-pci", SkillMD: skillMD})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created == nil || created.Kind != "custom" {
		t.Fatalf("created skill = %+v, want kind=custom", created)
	}

	resolved, err := svc.Resolve(ctx, "org1", "payments-pci")
	if err != nil || resolved == nil {
		t.Fatalf("Resolve after create: %v / %v", resolved, err)
	}
	summaries, _ := svc.ListSummaries(ctx, "org1")
	var found *SkillSummary
	for i := range summaries {
		if summaries[i].Name == "payments-pci" {
			found = &summaries[i]
		}
	}
	if found == nil || !found.Editable {
		t.Fatalf("custom skill should appear in summaries as editable; got %+v", found)
	}

	// Duplicate create → collision.
	if _, err := mut.Create(ctx, "org1", "tester", CreateSkillInput{Name: "payments-pci", SkillMD: skillMD}); err != ErrSkillNameCollision {
		t.Fatalf("duplicate create err = %v, want ErrSkillNameCollision", err)
	}

	// Delete → gone.
	if err := mut.Delete(ctx, "org1", "tester", "payments-pci"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	gone, _ := svc.Resolve(ctx, "org1", "payments-pci")
	if gone != nil {
		t.Fatalf("skill should be gone after delete, got %+v", gone)
	}
}

func TestDeleteBuiltinIsForbidden(t *testing.T) {
	svc, _ := newTestStore()
	mut := NewSkillMutationService(svc)
	ctx := context.Background()
	if _, err := svc.List(ctx, "org1"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := mut.Delete(ctx, "org1", "tester", "go"); err != ErrSkillNotEditable {
		t.Fatalf("delete builtin err = %v, want ErrSkillNotEditable", err)
	}
}

func TestCache_SecondReadServedFromCache(t *testing.T) {
	svc, gh := newTestStore()
	ctx := context.Background()
	if _, err := svc.List(ctx, "org1"); err != nil { // seeds + warms cache
		t.Fatalf("List: %v", err)
	}
	before := gh.refCalls()
	if _, err := svc.List(ctx, "org1"); err != nil { // within soft TTL → cache, no GetRef
		t.Fatalf("List 2: %v", err)
	}
	if got := gh.refCalls(); got != before {
		t.Fatalf("second List made %d extra GetRef calls, want 0 (soft-TTL cache)", got-before)
	}
}

func TestConcurrentReads_ProvisionOnceConsistently(t *testing.T) {
	svc, _ := newTestStore()
	ctx := context.Background()
	const n = 8
	var wg sync.WaitGroup
	results := make([][]Skill, n)
	errs := make([]error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = svc.List(ctx, "org1")
		}(i)
	}
	wg.Wait()
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: List error %v", i, errs[i])
		}
		if _, ok := nameSet(results[i])["go"]; !ok {
			t.Fatalf("goroutine %d: expected built-ins present, got %v", i, keysOf(nameSet(results[i])))
		}
	}
}

func keysOf(m map[string]Skill) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
